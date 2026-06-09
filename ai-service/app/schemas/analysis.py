from typing import Literal

from pydantic import BaseModel, ConfigDict, Field
from pydantic.alias_generators import to_camel

Priority = Literal["high", "medium", "low"]
Verdict = Literal["strong", "moderate", "weak"]


class CamelModel(BaseModel):
    model_config = ConfigDict(alias_generator=to_camel, populate_by_name=True)


class Localized(BaseModel):
    en: str
    es: str


class LocalizedList(BaseModel):
    en: list[str]
    es: list[str]


class Suggestion(CamelModel):
    title: Localized
    detail: Localized
    priority: Priority


class LLMAnalysis(BaseModel):
    score: int = Field(ge=0, le=100, description="Compatibility score from 0 to 100.")
    summary: Localized = Field(
        description="Two to three honest, candidate-facing sentences, in both languages.",
    )
    matched_skills: LocalizedList = Field(
        description="Required skills clearly evidenced in the CV, in both languages.",
    )
    missing_skills: LocalizedList = Field(
        description="Required skills absent from the CV, in both languages.",
    )
    suggestions: list[Suggestion] = Field(
        description="Two to five specific, impact-ordered improvements, in both languages.",
    )


class AnalysisResult(CamelModel):
    score: int = Field(ge=0, le=100)
    verdict: Verdict
    summary: Localized
    matched_skills: LocalizedList
    missing_skills: LocalizedList
    suggestions: list[Suggestion]
