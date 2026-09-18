import time

import structlog
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
        correlation_id = (
            request_id.sanitize(request.headers.get(request_id.HEADER)) or request_id.new()
        )
        structlog.contextvars.clear_contextvars()
        structlog.contextvars.bind_contextvars(request_id=correlation_id)

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
