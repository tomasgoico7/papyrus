import logging

from langchain_core.runnables import Runnable
from starlette.concurrency import run_in_threadpool

from app.core.config import Settings
from app.schemas.tailor import (
    EducationItem,
    ExperienceItem,
    LLMQuestions,
    LLMTailoredCV,
    Locale,
    Section,
    TailoredCV,
    TailoringAnswer,
    TailoringQuestion,
)
from app.services.tailor_prompt import build_questions_prompt, build_tailor_prompt

logger = logging.getLogger(__name__)

LANGUAGE_NAMES: dict[Locale, str] = {"en": "English", "es": "Spanish"}


class TailorError(Exception):
    pass


def _format_answers(answers: list[TailoringAnswer], extra: str | None) -> str:
    lines = [f"- {a.topic}: {a.answer.strip()}" for a in answers if a.answer.strip()]
    if extra and extra.strip():
        lines.append(f"- Additional notes from the candidate: {extra.strip()}")
    return "\n".join(lines) if lines else "(The candidate added no new information.)"


class CVTailor:
    def __init__(self, questions_chain: Runnable, cv_chain: Runnable) -> None:
        self._questions_chain = questions_chain
        self._cv_chain = cv_chain

    @classmethod
    def from_settings(cls, settings: Settings) -> "CVTailor":
        from langchain_google_genai import ChatGoogleGenerativeAI

        model = ChatGoogleGenerativeAI(
            model=settings.gemini_model,
            google_api_key=settings.gemini_api_key,
            temperature=settings.gemini_temperature,
            transport="rest",
            timeout=settings.gemini_timeout,
        )
        questions_chain = build_questions_prompt() | model.with_structured_output(LLMQuestions)
        cv_chain = build_tailor_prompt() | model.with_structured_output(LLMTailoredCV)
        return cls(questions_chain, cv_chain)

    async def questions(
        self,
        *,
        cv_text: str,
        job_offer: str,
        job_title: str | None,
        locale: Locale,
    ) -> list[TailoringQuestion]:
        try:
            raw = await run_in_threadpool(
                self._questions_chain.invoke,
                {
                    "cv": cv_text,
                    "job_offer": job_offer,
                    "job_title": job_title or "Not specified",
                    "language": LANGUAGE_NAMES[locale],
                },
            )
        except Exception as exc:  # noqa: BLE001 — provider/parse failures collapse here
            logger.exception("Tailoring questions failed")
            raise TailorError("The tailoring questions could not be prepared.") from exc

        if not isinstance(raw, LLMQuestions):
            raise TailorError("The model returned an unexpected response shape.")

        return [
            TailoringQuestion(topic=q.topic, question=q.question) for q in raw.questions
        ]

    async def generate(
        self,
        *,
        cv_text: str,
        job_offer: str,
        job_title: str | None,
        answers: list[TailoringAnswer],
        extra: str | None,
    ) -> TailoredCV:
        try:
            raw = await run_in_threadpool(
                self._cv_chain.invoke,
                {
                    "cv": cv_text,
                    "job_offer": job_offer,
                    "job_title": job_title or "Not specified",
                    "answers": _format_answers(answers, extra),
                },
            )
        except Exception as exc:  # noqa: BLE001 — provider/parse failures collapse here
            logger.exception("CV tailoring failed")
            raise TailorError("The tailored CV could not be generated.") from exc

        if not isinstance(raw, LLMTailoredCV):
            raise TailorError("The model returned an unexpected response shape.")

        return TailoredCV(
            full_name=raw.full_name,
            contact=raw.contact,
            headline=raw.headline,
            summary=raw.summary,
            experience=[
                ExperienceItem(
                    role=item.role,
                    company=item.company,
                    period=item.period,
                    highlights=item.highlights,
                )
                for item in raw.experience
            ],
            skills=raw.skills,
            education=[
                EducationItem(
                    degree=item.degree,
                    institution=item.institution,
                    period=item.period,
                )
                for item in raw.education
            ],
            additional=[
                Section(title=item.title, items=item.items)
                for item in raw.additional
            ],
        )
