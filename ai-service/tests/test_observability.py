import json
import logging

import pytest
from fastapi import FastAPI, Response
from fastapi.testclient import TestClient

from app.api.middleware import ObservabilityMiddleware
from app.core import request_id
from app.core.logging import configure_logging
from app.core.metrics import Metrics


def build_app() -> FastAPI:
    app = FastAPI()

    @app.get("/probe")
    async def probe() -> dict[str, str]:
        logging.getLogger("app.test").info("probe handled")
        return {"ok": "yes"}

    @app.get("/boom")
    async def boom() -> dict[str, str]:
        raise RuntimeError("unhandled")

    metrics = Metrics()

    @app.get("/metrics", include_in_schema=False)
    async def serve_metrics() -> Response:
        return Response(content=metrics.render(), media_type="text/plain")

    app.add_middleware(
        ObservabilityMiddleware,
        metrics=metrics,
        routes=frozenset({"/probe", "/boom", "/metrics"}),
    )
    return app


@pytest.mark.parametrize(
    "raw, expect_echoed",
    [
        ("caller-supplied-42", True),
        ("abc\r\nlevel=error", False),
        ("with space", False),
        ("a" * 65, False),
    ],
)
def test_request_id_is_reused_only_when_safe(raw: str, expect_echoed: bool) -> None:
    client = TestClient(build_app())
    response = client.get("/probe", headers={request_id.HEADER: raw})

    echoed = response.headers[request_id.HEADER]
    if expect_echoed:
        assert echoed == raw
    else:
        assert echoed != raw
        assert len(echoed) == 32


def test_request_id_is_generated_when_absent() -> None:
    client = TestClient(build_app())
    response = client.get("/probe")

    assert len(response.headers[request_id.HEADER]) == 32


def test_metrics_label_by_registered_route_only() -> None:
    client = TestClient(build_app())
    client.get("/probe")
    client.get("/does-not-exist")

    body = client.get("/metrics").text

    assert 'http_requests_total{method="GET",route="/probe",status="200"} 1.0' in body
    assert 'route="unmatched"' in body
    assert 'route="/does-not-exist"' not in body


def test_metrics_exclude_the_scrape_endpoint_itself() -> None:
    client = TestClient(build_app())
    client.get("/metrics")

    assert 'route="/metrics"' not in client.get("/metrics").text


def test_metrics_count_an_unhandled_error_as_a_500() -> None:
    client = TestClient(build_app(), raise_server_exceptions=False)
    client.get("/boom")

    body = client.get("/metrics").text
    assert 'http_requests_total{method="GET",route="/boom",status="500"} 1.0' in body


def test_the_request_id_reaches_a_standard_library_log_line(
    capsys: pytest.CaptureFixture[str],
) -> None:
    configure_logging("INFO", json_output=True)
    client = TestClient(build_app())
    client.get("/probe", headers={request_id.HEADER: "corr-123"})

    records = [
        json.loads(line)
        for line in capsys.readouterr().out.splitlines()
        if line.startswith("{")
    ]
    handled = [r for r in records if r.get("event") == "probe handled"]

    assert handled, "the handler log line was not rendered as JSON"
    assert handled[0]["request_id"] == "corr-123"
