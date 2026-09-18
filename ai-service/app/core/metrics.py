from prometheus_client import (
    CollectorRegistry,
    Counter,
    Gauge,
    Histogram,
    generate_latest,
)
from prometheus_client.gc_collector import GCCollector
from prometheus_client.platform_collector import PlatformCollector
from prometheus_client.process_collector import ProcessCollector

# Spanning from a validation rejection to an analysis that nearly times out. The
# client default stops at ten seconds, which would drop every real analysis into
# the overflow bucket and leave the p95 unusable.
LATENCY_BUCKETS = (0.01, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0, 20.0, 30.0, 45.0, 60.0)


class Metrics:
    """The service's own registry, rather than the process-global default one,
    so a test can build an isolated instance and assert on it."""

    def __init__(self) -> None:
        self.registry = CollectorRegistry()
        self.requests = Counter(
            "http_requests_total",
            "Requests handled, by method, matched route and status code.",
            ["method", "route", "status"],
            registry=self.registry,
        )
        self.duration = Histogram(
            "http_request_duration_seconds",
            "Request latency, by method and matched route.",
            ["method", "route"],
            buckets=LATENCY_BUCKETS,
            registry=self.registry,
        )
        self.in_flight = Gauge(
            "http_requests_in_flight",
            "Requests currently being served.",
            registry=self.registry,
        )

        ProcessCollector(registry=self.registry)
        PlatformCollector(registry=self.registry)
        GCCollector(registry=self.registry)

    def render(self) -> bytes:
        return generate_latest(self.registry)
