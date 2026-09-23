"""The AI service's half of a distributed trace.

Most of the time an analysis takes is spent here, so a trace that stops at the
gateway shows a long gap and explains nothing. These check the two things that
have to hold for it not to: the inbound trace is continued rather than replaced,
and the trace id reaches the log lines.
"""

import json
import logging

import pytest
from fastapi import FastAPI
from fastapi.testclient import TestClient
from app.api.middleware import ObservabilityMiddleware
from app.core import request_id
from app.core.logging import configure_logging
from app.core.metrics import Metrics
from app.core.tracing import configure_tracing

# A fixed, valid W3C header, standing in for the gateway. The trailing 01 is the
# sampled flag: without it the span is not recorded and nothing here would run.
GATEWAY_TRACEPARENT = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
GATEWAY_TRACE_ID = "4bf92f3577b34da6a3ce929d0e0e4736"


@pytest.fixture
def traced_app() -> TestClient:
    app = FastAPI(version="test")

    @app.get("/probe")
    async def probe() -> dict[str, str]:
        logging.getLogger("app.test").info("probe handled")
        return {"ok": "yes"}

    app.add_middleware(
        ObservabilityMiddleware, metrics=Metrics(), routes=frozenset({"/probe"})
    )
    # No endpoint, so nothing is exported anywhere. The sampler is still
    # parent-based, and the gateway's header below says the trace is sampled, so
    # the span is recorded and current — which is all these need. Exporting is
    # the backend's problem, not this service's.
    configure_tracing(
        app, endpoint="", environment="test", version="test", sample_ratio=1.0
    )

    return TestClient(app)


def test_the_inbound_trace_is_continued(traced_app: TestClient) -> None:
    client = traced_app

    response = client.get("/probe", headers={"traceparent": GATEWAY_TRACEPARENT})

    assert response.status_code == 200
    # The correlation id echoed back is the gateway's trace, so one identifier
    # finds the logs on both sides and the trace itself.
    assert response.headers[request_id.HEADER] == GATEWAY_TRACE_ID


def test_an_inbound_request_id_still_wins(traced_app: TestClient) -> None:
    client = traced_app

    response = client.get(
        "/probe",
        headers={"traceparent": GATEWAY_TRACEPARENT, request_id.HEADER: "callersownid"},
    )

    # The caller has already written this one down somewhere.
    assert response.headers[request_id.HEADER] == "callersownid"


def test_log_lines_carry_the_trace(
    traced_app: TestClient, capsys: pytest.CaptureFixture[str]
) -> None:
    client = traced_app
    configure_logging("INFO", json_output=True)

    client.get("/probe", headers={"traceparent": GATEWAY_TRACEPARENT})

    lines = [
        json.loads(line)
        for line in capsys.readouterr().out.splitlines()
        if line.startswith("{")
    ]
    handled = [line for line in lines if line.get("event") == "probe handled"]
    assert handled, "the handler's log line was not captured"

    # Without this the trace shows where the time went and the logs say what the
    # code was thinking, and there is no way to put the two side by side.
    assert handled[0]["trace_id"] == GATEWAY_TRACE_ID
    assert handled[0]["request_id"] == GATEWAY_TRACE_ID


def test_an_untraced_request_still_gets_an_identifier(traced_app: TestClient) -> None:
    client = traced_app

    response = client.get("/probe")

    # No caller header and, depending on sampling, no recorded span. The service
    # still has to be able to correlate its own logs.
    assert len(response.headers[request_id.HEADER]) == 32
