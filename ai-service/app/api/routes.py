import logging

from fastapi import APIRouter, Depends, File, Form, Header, UploadFile
from starlette.concurrency import run_in_threadpool

from app.api.errors import ApiError
from app.core.config import Settings, get_settings
from app.schemas.analysis import AnalysisResult
from app.services.analyzer import CVAnalyzer
from app.services.pdf_extractor import extract_text

logger = logging.getLogger(__name__)
router = APIRouter()

MIN_JOB_OFFER_LENGTH = 40
ACCEPTED_PDF_TYPES = frozenset({"application/pdf", "application/octet-stream"})

# The analyzer holds the model client, so it is built once and reused.
_analyzer: CVAnalyzer | None = None


def get_analyzer(settings: Settings = Depends(get_settings)) -> CVAnalyzer:
    global _analyzer
    if _analyzer is None:
        _analyzer = CVAnalyzer.from_settings(settings)
    return _analyzer


def verify_internal_token(
    x_internal_token: str | None = Header(default=None),
    settings: Settings = Depends(get_settings),
) -> None:
    """Reject calls that bypass the gateway. Skipped when no secret is set so
    local development (and the gateway without a token) keeps working."""
    if settings.internal_api_key and x_internal_token != settings.internal_api_key:
        raise ApiError(401, "unauthorized", "A valid internal token is required.")


@router.get("/health")
async def health() -> dict[str, str]:
    return {"status": "ok"}


@router.post(
    "/analyze",
    response_model=AnalysisResult,
    response_model_by_alias=True,
    dependencies=[Depends(verify_internal_token)],
)
async def analyze(
    cv: UploadFile = File(...),
    job_offer: str = Form(..., alias="jobOffer"),
    job_title: str | None = Form(default=None, alias="jobTitle"),
    settings: Settings = Depends(get_settings),
    analyzer: CVAnalyzer = Depends(get_analyzer),
) -> AnalysisResult:
    if not _looks_like_pdf(cv):
        raise ApiError(415, "unsupported_media_type", "Only PDF files are supported.")

    payload = await cv.read()
    if not payload:
        raise ApiError(400, "cv_required", "The CV file is empty.")

    cleaned_offer = job_offer.strip()
    if len(cleaned_offer) < MIN_JOB_OFFER_LENGTH:
        raise ApiError(400, "invalid_job_offer", "The job posting is too short to analyze.")

    cv_text = await run_in_threadpool(extract_text, payload, settings.max_cv_chars)

    return await analyzer.analyze(
        cv_text=cv_text,
        job_offer=cleaned_offer,
        job_title=job_title.strip() if job_title else None,
    )


def _looks_like_pdf(cv: UploadFile) -> bool:
    if cv.content_type in ACCEPTED_PDF_TYPES:
        return True
    return bool(cv.filename and cv.filename.lower().endswith(".pdf"))
