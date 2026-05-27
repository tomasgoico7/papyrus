import logging

from fastapi import FastAPI, Request
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse

from app.services.analyzer import AnalysisError
from app.services.pdf_extractor import UnreadableCVError

logger = logging.getLogger(__name__)


class ApiError(Exception):
    """A handled, client-facing error with an explicit status and code."""

    def __init__(self, status_code: int, code: str, message: str) -> None:
        super().__init__(message)
        self.status_code = status_code
        self.code = code
        self.message = message


def _envelope(status_code: int, code: str, message: str) -> JSONResponse:
    return JSONResponse(
        status_code=status_code,
        content={"error": {"code": code, "message": message}},
    )


def register_exception_handlers(app: FastAPI) -> None:
    """Register handlers so every error shares the `{ "error": { code, message } }`
    envelope the gateway expects."""

    @app.exception_handler(ApiError)
    async def _handle_api_error(_: Request, exc: ApiError) -> JSONResponse:
        return _envelope(exc.status_code, exc.code, exc.message)

    @app.exception_handler(UnreadableCVError)
    async def _handle_unreadable_cv(_: Request, exc: UnreadableCVError) -> JSONResponse:
        return _envelope(422, "unreadable_cv", str(exc))

    @app.exception_handler(AnalysisError)
    async def _handle_analysis_error(_: Request, exc: AnalysisError) -> JSONResponse:
        return _envelope(502, "analysis_failed", str(exc))

    @app.exception_handler(RequestValidationError)
    async def _handle_validation_error(_: Request, __: RequestValidationError) -> JSONResponse:
        return _envelope(
            400, "invalid_request", "The request was malformed or missing required fields."
        )
