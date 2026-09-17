from fastapi import APIRouter, Depends, File, Form, Header, Request, UploadFile
from starlette.concurrency import run_in_threadpool

from app.api.errors import ApiError
from app.core.config import Settings, get_settings
from app.schemas.analysis import AnalysisResult
from app.schemas.tailor import GenerateRequest, Locale, QuestionsResponse, TailoredCV
from app.services.analyzer import CVAnalyzer
from app.services.pdf_extractor import extract_text
from app.services.tailor import CVTailor

router = APIRouter()

MIN_JOB_OFFER_LENGTH = 40
ACCEPTED_PDF_TYPES = frozenset({"application/pdf", "application/octet-stream"})


def get_analyzer(request: Request) -> CVAnalyzer:
    analyzer: CVAnalyzer = request.app.state.analyzer
    return analyzer


def get_tailor(request: Request) -> CVTailor:
    tailor: CVTailor = request.app.state.tailor
    return tailor


def _normalize_locale(value: str) -> Locale:
    return "es" if value == "es" else "en"


def verify_internal_token(
    x_internal_token: str | None = Header(default=None),
    settings: Settings = Depends(get_settings),
) -> None:
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


@router.post(
    "/tailor/questions",
    response_model=QuestionsResponse,
    response_model_by_alias=True,
    dependencies=[Depends(verify_internal_token)],
)
async def tailor_questions(
    cv: UploadFile = File(...),
    job_offer: str = Form(..., alias="jobOffer"),
    job_title: str | None = Form(default=None, alias="jobTitle"),
    locale: str = Form(default="en"),
    settings: Settings = Depends(get_settings),
    tailor: CVTailor = Depends(get_tailor),
) -> QuestionsResponse:
    if not _looks_like_pdf(cv):
        raise ApiError(415, "unsupported_media_type", "Only PDF files are supported.")

    payload = await cv.read()
    if not payload:
        raise ApiError(400, "cv_required", "The CV file is empty.")

    cleaned_offer = job_offer.strip()
    if len(cleaned_offer) < MIN_JOB_OFFER_LENGTH:
        raise ApiError(400, "invalid_job_offer", "The job posting is too short to analyze.")

    cv_text = await run_in_threadpool(extract_text, payload, settings.max_cv_chars)
    questions = await tailor.questions(
        cv_text=cv_text,
        job_offer=cleaned_offer,
        job_title=job_title.strip() if job_title else None,
        locale=_normalize_locale(locale),
    )
    return QuestionsResponse(questions=questions, cv_text=cv_text)


@router.post(
    "/tailor/generate",
    response_model=TailoredCV,
    response_model_by_alias=True,
    dependencies=[Depends(verify_internal_token)],
)
async def tailor_generate(
    body: GenerateRequest,
    tailor: CVTailor = Depends(get_tailor),
) -> TailoredCV:
    cv_text = body.cv_text.strip()
    if not cv_text:
        raise ApiError(400, "cv_required", "The CV text is empty.")

    cleaned_offer = body.job_offer.strip()
    if len(cleaned_offer) < MIN_JOB_OFFER_LENGTH:
        raise ApiError(400, "invalid_job_offer", "The job posting is too short to analyze.")

    return await tailor.generate(
        cv_text=cv_text,
        job_offer=cleaned_offer,
        job_title=body.job_title.strip() if body.job_title else None,
        answers=body.answers,
        extra=body.extra,
    )


def _looks_like_pdf(cv: UploadFile) -> bool:
    if cv.content_type in ACCEPTED_PDF_TYPES:
        return True
    return bool(cv.filename and cv.filename.lower().endswith(".pdf"))
