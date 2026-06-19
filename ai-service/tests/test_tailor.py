import pytest

from app.schemas.tailor import (
    LLMQuestions,
    LLMTailoredCV,
    TailoredCV,
    TailoringAnswer,
)
from app.services.tailor import CVTailor, TailorError, _format_answers


class _FakeChain:
    """Stands in for the LangChain runnable so the tailoring logic is testable
    without a model provider."""

    def __init__(self, result):
        self._result = result

    def invoke(self, _inputs):
        if isinstance(self._result, Exception):
            raise self._result
        return self._result


def test_format_answers_keeps_real_and_skips_empty():
    answers = [
        TailoringAnswer(topic="Kubernetes", answer="Two years in production"),
        TailoringAnswer(topic="Scala", answer="   "),
    ]

    formatted = _format_answers(answers, extra="Led a data migration")

    assert "Kubernetes: Two years in production" in formatted
    assert "Scala" not in formatted
    assert "Led a data migration" in formatted


def test_format_answers_falls_back_when_empty():
    assert "no new information" in _format_answers([], extra=None).lower()


async def test_questions_maps_into_response():
    chain = _FakeChain(
        LLMQuestions(
            questions=[{"topic": "Kubernetes", "question": "Do you have experience?"}]
        )
    )
    tailor = CVTailor(chain, _FakeChain(None))

    questions = await tailor.questions(
        cv_text="cv", job_offer="offer", job_title="Backend", locale="en"
    )

    assert len(questions) == 1
    assert questions[0].topic == "Kubernetes"


async def test_generate_shapes_the_tailored_cv():
    llm_cv = LLMTailoredCV(
        full_name="Tomas Goicoechea",
        contact="email · phone",
        headline="Backend Engineer",
        summary="A tailored summary.",
        experience=[
            {
                "role": "Developer",
                "company": "Koovra",
                "period": "2025 – present",
                "highlights": ["Built services", "Tuned queries"],
            }
        ],
        skills=["Backend: Go, Java"],
        education=[
            {"degree": "BSc", "institution": "UNLaM", "period": "2018 – 2025"}
        ],
        additional=[{"title": "Languages", "items": ["English (B2)"]}],
    )
    tailor = CVTailor(_FakeChain(None), _FakeChain(llm_cv))

    cv = await tailor.generate(
        cv_text="cv", job_offer="offer", job_title=None, answers=[], extra=None
    )

    assert isinstance(cv, TailoredCV)
    assert cv.full_name == "Tomas Goicoechea"
    assert cv.experience[0].highlights == ["Built services", "Tuned queries"]
    assert cv.skills == ["Backend: Go, Java"]
    assert cv.additional[0].title == "Languages"


async def test_generate_wraps_provider_failures():
    tailor = CVTailor(_FakeChain(None), _FakeChain(RuntimeError("provider exploded")))

    with pytest.raises(TailorError):
        await tailor.generate(
            cv_text="cv", job_offer="offer", job_title=None, answers=[], extra=None
        )
