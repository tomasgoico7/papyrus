import pytest

from app.schemas.analysis import (
    AnalysisResult,
    LLMAnalysis,
    Localized,
    LocalizedList,
    Suggestion,
)
from app.services.analyzer import AnalysisError, CVAnalyzer, verdict_for


class _FakeChain:
    """Stands in for the LangChain runnable so analysis logic is testable
    without a model provider."""

    def __init__(self, result):
        self._result = result

    async def ainvoke(self, _inputs):
        if isinstance(self._result, Exception):
            raise self._result
        return self._result


@pytest.mark.parametrize(
    "score, expected",
    [
        (0, "weak"),
        (49, "weak"),
        (50, "moderate"),
        (74, "moderate"),
        (75, "strong"),
        (100, "strong"),
    ],
)
def test_verdict_for_bands(score, expected):
    assert verdict_for(score) == expected


async def test_analyze_derives_verdict_and_shapes_result():
    llm_output = LLMAnalysis(
        score=82,
        summary=Localized(
            en="Strong backend overlap; light on orchestration.",
            es="Fuerte solapamiento en backend; flojo en orquestación.",
        ),
        matched_skills=LocalizedList(en=["Go", "PostgreSQL"], es=["Go", "PostgreSQL"]),
        missing_skills=LocalizedList(en=["Kubernetes"], es=["Kubernetes"]),
        suggestions=[
            Suggestion(
                title=Localized(en="Quantify cluster scale", es="Cuantificá la escala del clúster"),
                detail=Localized(en="Add numbers.", es="Agregá números."),
                priority="high",
            ),
        ],
    )
    analyzer = CVAnalyzer(_FakeChain(llm_output))

    result = await analyzer.analyze(cv_text="cv", job_offer="offer", job_title="Backend")

    assert isinstance(result, AnalysisResult)
    assert result.verdict == "strong"
    assert result.matched_skills.en == ["Go", "PostgreSQL"]
    assert result.summary.es.startswith("Fuerte")


async def test_analyze_wraps_provider_failures():
    analyzer = CVAnalyzer(_FakeChain(RuntimeError("provider exploded")))

    with pytest.raises(AnalysisError):
        await analyzer.analyze(cv_text="cv", job_offer="offer", job_title=None)
