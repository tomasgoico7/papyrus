from collections.abc import AsyncIterator
from contextlib import asynccontextmanager

from fastapi import FastAPI

from app.api.errors import register_exception_handlers
from app.api.routes import router
from app.core.config import get_settings
from app.core.logging import configure_logging
from app.services.analyzer import CVAnalyzer
from app.services.tailor import CVTailor


@asynccontextmanager
async def lifespan(app: FastAPI) -> AsyncIterator[None]:
    settings = get_settings()
    configure_logging(settings.log_level)

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
    return app


app = create_app()
