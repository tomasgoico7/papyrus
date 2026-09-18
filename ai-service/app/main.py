from collections.abc import AsyncIterator
from contextlib import asynccontextmanager

from fastapi import FastAPI, Response

from app.api.errors import register_exception_handlers
from app.api.middleware import METRICS_PATH, ObservabilityMiddleware
from app.api.routes import router
from app.core.config import get_settings
from app.core.logging import configure_logging
from app.core.metrics import Metrics
from app.services.analyzer import CVAnalyzer
from app.services.tailor import CVTailor

CONTENT_TYPE_METRICS = "text/plain; version=0.0.4; charset=utf-8"


@asynccontextmanager
async def lifespan(app: FastAPI) -> AsyncIterator[None]:
    settings = get_settings()
    configure_logging(settings.log_level, json_output=settings.is_production)

    # Built once here rather than on first use: a request should not pay for
    # constructing the model clients, and two concurrent first requests must
    # not race to build them twice.
    app.state.analyzer = CVAnalyzer.from_settings(settings)
    app.state.tailor = CVTailor.from_settings(settings)
    yield


def create_app() -> FastAPI:
    settings = get_settings()
    app = FastAPI(
        title="Papyrus AI Service",
        version="1.0.0",
        lifespan=lifespan,
        docs_url=None if settings.is_production else "/docs",
        redoc_url=None,
    )
    register_exception_handlers(app)
    app.include_router(router)

    metrics = Metrics()

    @app.get(METRICS_PATH, include_in_schema=False)
    async def serve_metrics() -> Response:
        return Response(content=metrics.render(), media_type=CONTENT_TYPE_METRICS)

    app.add_middleware(
        ObservabilityMiddleware,
        metrics=metrics,
        routes=_registered_paths(app),
    )
    return app


def _registered_paths(app: FastAPI) -> frozenset[str]:
    """The paths the service actually serves, used to bound metric cardinality."""
    return frozenset(
        path for route in app.routes if isinstance(path := getattr(route, "path", None), str)
    )


app = create_app()
