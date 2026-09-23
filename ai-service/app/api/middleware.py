import time

import structlog
from opentelemetry import trace
from starlette.middleware.base import BaseHTTPMiddleware, RequestResponseEndpoint
from starlette.requests import Request
from starlette.responses import Response
from starlette.types import ASGIApp

from app.core import request_id
from app.core.metrics import Metrics

METRICS_PATH = "/metrics"


class ObservabilityMiddleware(BaseHTTPMiddleware):
    """Bind a correlation id to every log line of a request, and record its rate,
    errors and duration.

    The route label comes from an allowlist of registered paths. Labelling by the
    raw path would let anything probing the service — a scanner walking URLs —
    mint a new time series per request and eventually take the scrape down.
    """

    def __init__(self, app: ASGIApp, metrics: Metrics, routes: frozenset[str]) -> None:
        super().__init__(app)
        self._metrics = metrics
        self._routes = routes

    async def dispatch(
        self, request: Request, call_next: RequestResponseEndpoint
    ) -> Response:
        span = trace.get_current_span().get_span_context()
        traced = span.is_valid

        # The gateway's rules, mirrored: an inbound id wins because the caller is
        # already correlating on it, and otherwise the trace is the id. The two
        # are the same width for exactly this reason, and one number that finds
        # both the logs and the trace beats two that each find half.
        correlation_id = request_id.sanitize(request.headers.get(request_id.HEADER))
        if not correlation_id:
            correlation_id = format(span.trace_id, "032x") if traced else request_id.new()

        structlog.contextvars.clear_contextvars()
        structlog.contextvars.bind_contextvars(request_id=correlation_id)
        if traced:
            # Bound even when it equals the request id: a log aggregator links to
            # the tracing backend by field name, not by what the value looks like.
            structlog.contextvars.bind_contextvars(
                trace_id=format(span.trace_id, "032x"),
                span_id=format(span.span_id, "016x"),
            )

        path = request.url.path
        if path == METRICS_PATH:
            return await call_next(request)
        route = path if path in self._routes else "unmatched"

        started = time.perf_counter()
        self._metrics.in_flight.inc()
        # Assumed until proven otherwise, so an exception escaping the router is
        # still counted rather than leaving the metrics silent.
        status = 500
        try:
            response = await call_next(request)
            status = response.status_code
            response.headers[request_id.HEADER] = correlation_id
            return response
        finally:
            self._metrics.in_flight.dec()
            self._metrics.duration.labels(request.method, route).observe(
                time.perf_counter() - started
            )
            self._metrics.requests.labels(request.method, route, str(status)).inc()
