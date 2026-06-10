import logging

from langchain_core.runnables import Runnable

from app.core.config import Settings
from app.schemas.analysis import AnalysisResult, LLMAnalysis, Verdict
from app.services.prompt import build_prompt

logger = logging.getLogger(__name__)


class AnalysisError(Exception):
    pass


def verdict_for(score: int) -> Verdict:
    if score >= 75:
        return "strong"
    if score >= 50:
        return "moderate"
    return "weak"


class CVAnalyzer:
    def __init__(self, chain: Runnable) -> None:
        self._chain = chain

    @classmethod
    def from_settings(cls, settings: Settings) -> "CVAnalyzer":
        # Imported lazily so tests that inject a fake chain never load the SDK.
        from langchain_google_genai import ChatGoogleGenerativeAI

        model = ChatGoogleGenerativeAI(
            model=settings.gemini_model,
            google_api_key=settings.gemini_api_key,
            temperature=settings.gemini_temperature,
            timeout=settings.gemini_timeout,
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
