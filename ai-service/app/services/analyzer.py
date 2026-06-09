import logging

from langchain_core.runnables import Runnable

from app.core.config import Settings
from app.schemas.analysis import AnalysisResult, LLMAnalysis, Verdict
from app.services.prompt import build_prompt

logger = logging.getLogger(__name__)


class AnalysisError(Exception):
    """Raised when the model call fails or returns something unusable."""


def verdict_for(score: int) -> Verdict:
    """Map a 0-100 score onto a coarse verdict. Kept deterministic and separate
    from the model so the band thresholds live in one auditable place."""
    if score >= 75:
        return "strong"
    if score >= 50:
        return "moderate"
    return "weak"


class CVAnalyzer:
    """Runs a CV/job-posting pair through the LLM and shapes the result.

    The model chain is injected, which keeps the LLM provider out of the unit
    tests and makes the analysis logic verifiable in isolation.
    """

    def __init__(self, chain: Runnable) -> None:
        self._chain = chain

    @classmethod
    def from_settings(cls, settings: Settings) -> "CVAnalyzer":
        # Imported lazily so tests that inject a fake chain never load the SDK.
        from langchain_google_genai import ChatGoogleGenerativeAI

        model = ChatGoogleGenerativeAI(
            model=settings.gemini_model,
            google_api_key=settings.gemini_api_key,
            temperature=0.2,
            # REST avoids gRPC egress that hangs on some hosts (e.g. Render); the
            # timeout makes a stuck call fail fast instead of holding the worker.
            transport="rest",
            timeout=45,
        )
        chain = build_prompt() | model.with_structured_output(LLMAnalysis)
        return cls(chain)

    async def analyze(
        self,
        *,
        cv_text: str,
        job_offer: str,
        job_title: str | None,
    ) -> AnalysisResult:
        try:
            raw = await self._chain.ainvoke(
                {
                    "cv": cv_text,
                    "job_offer": job_offer,
                    "job_title": job_title or "Not specified",
                }
            )
        except Exception as exc:  # noqa: BLE001 — provider/parse failures collapse here
            logger.exception("LLM analysis failed")
            raise AnalysisError("The analysis could not be completed.") from exc

        if not isinstance(raw, LLMAnalysis):
            raise AnalysisError("The model returned an unexpected response shape.")

        score = max(0, min(100, raw.score))
        return AnalysisResult(
            score=score,
            verdict=verdict_for(score),
            summary=raw.summary,
            matched_skills=raw.matched_skills,
            missing_skills=raw.missing_skills,
            suggestions=raw.suggestions,
        )
