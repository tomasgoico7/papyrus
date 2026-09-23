"""Trace export for the AI service.

The gateway sends a `traceparent` with every call, so this service's spans join
a trace that started at the browser rather than beginning one of their own. That
is the whole point: the model call is where most of the time goes, and it is
invisible from the gateway's side.
"""

import structlog
from fastapi import FastAPI
from opentelemetry import trace
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
from opentelemetry.instrumentation.fastapi import FastAPIInstrumentor
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.sdk.trace.sampling import ALWAYS_OFF, ParentBased, TraceIdRatioBased

SERVICE_NAME = "papyrus-ai-service"

_log = structlog.get_logger(__name__)


def configure_tracing(
    app: FastAPI,
    *,
    endpoint: str,
    environment: str,
    version: str,
    sample_ratio: float,
) -> None:
    """Install a tracer provider and instrument the application.

    With no endpoint configured the instrumentation is still installed and the
    sampler simply records nothing. Leaving it in place matters: an inbound
    `traceparent` is still read and passed on, so a service that exports nothing
    does not put a hole in somebody else's trace — and there is no second,
    untested configuration in which the app runs without instrumentation.
    """
    if endpoint:
        provider = TracerProvider(
            resource=Resource.create(
                {
                    "service.name": SERVICE_NAME,
                    "service.version": version,
                    "deployment.environment.name": environment,
                }
            ),
            # Parent-based: the gateway has already decided whether this trace is
            # being recorded. Deciding again here produces traces missing their
            # middle, which are worse than no traces because they look complete.
            sampler=ParentBased(TraceIdRatioBased(sample_ratio)),
        )
        provider.add_span_processor(BatchSpanProcessor(OTLPSpanExporter()))
        _log.info("tracing is on", endpoint=endpoint, sample_ratio=sample_ratio)
    else:
        provider = TracerProvider(sampler=ParentBased(ALWAYS_OFF))
        _log.info("tracing is off: no otlp endpoint configured")

    trace.set_tracer_provider(provider)

    # Added after the observability middleware so it ends up outside it, which
    # is what lets that middleware read the trace id and put it on every log
    # line. Starlette makes the last middleware added the outermost one.
    FastAPIInstrumentor.instrument_app(
        app,
        tracer_provider=provider,
        # Scraping is not a request anybody wants a trace of, and at one scrape
        # every fifteen seconds it would drown everything that matters.
        excluded_urls="/metrics,/health",
        # The ASGI instrumentation otherwise emits a child span per send and
        # receive event — three of them for a single response. They describe the
        # protocol rather than the work, and on a trace whose point is showing
        # where fifty-five seconds went they are noise between the reader and
        # the answer.
        exclude_spans=["send", "receive"],
    )
