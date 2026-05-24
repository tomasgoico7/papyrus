from typing import Literal

from pydantic import BaseModel, ConfigDict, Field
from pydantic.alias_generators import to_camel

Priority = Literal["high", "medium", "low"]
Verdict = Literal["strong", "moderate", "weak"]


class CamelModel(BaseModel):
    """Serializes to camelCase so the JSON contract matches the gateway and
    frontend, while keeping idiomatic snake_case field names in Python."""

    model_config = ConfigDict(alias_generator=to_camel, populate_by_name=True)


class Localized(BaseModel):
    """A single string in both supported languages."""

    en: str
    es: str


class LocalizedList(BaseModel):
    """A list of strings in both supported languages, kept index-aligned."""

    en: list[str]
    es: list[str]


class Suggestion(CamelModel):
    title: Localized
    detail: Localized
    priority: Priority


class LLMAnalysis(BaseModel):
    """The structured, bilingual output requested from the model. The verdict is
    derived deterministically from the score, so it is intentionally absent."""

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
