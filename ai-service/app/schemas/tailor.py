from typing import Literal

from pydantic import BaseModel, ConfigDict, Field
from pydantic.alias_generators import to_camel

Locale = Literal["en", "es"]


class CamelModel(BaseModel):
    model_config = ConfigDict(alias_generator=to_camel, populate_by_name=True)


# ─── Step 1: questions the model asks before writing anything ──────────────────

class _LLMQuestion(BaseModel):
    topic: str = Field(
        description="The skill or area the posting wants, as a short proper noun.",
    )
    question: str = Field(
        description="A direct question asking the candidate whether they have real "
        "experience with that topic and to describe it.",
    )


class LLMQuestions(BaseModel):
    questions: list[_LLMQuestion] = Field(
        description="Four to seven honest questions about gaps between CV and posting.",
    )


class TailoringQuestion(CamelModel):
    topic: str
    question: str


class QuestionsResponse(CamelModel):
    questions: list[TailoringQuestion]
    # Echoed so the generate step reuses it instead of re-parsing the PDF.
    cv_text: str


# ─── Step 2 → 3: the candidate's answers and the tailored CV ───────────────────

class TailoringAnswer(CamelModel):
    topic: str
    answer: str


class GenerateRequest(CamelModel):
    cv_text: str
    job_offer: str
    job_title: str | None = None
    answers: list[TailoringAnswer] = []
    extra: str | None = None


class _LLMExperience(BaseModel):
    role: str
    company: str
    period: str
    highlights: list[str] = Field(
        description="Concise, achievement-focused bullet points.",
    )


class _LLMEducation(BaseModel):
    degree: str
    institution: str
    period: str


class LLMTailoredCV(BaseModel):
    full_name: str
    contact: str = Field(
        description="A single line carried over from the CV: email, phone, location, "
        "and links, separated by middots. Empty if the CV has none.",
    )
    headline: str = Field(description="A short professional title aligned to the role.")
    summary: str = Field(description="A tailored professional summary, 2-4 sentences.")
    experience: list[_LLMExperience]
    skills: list[str]
    education: list[_LLMEducation]


class ExperienceItem(CamelModel):
    role: str
    company: str
    period: str
    highlights: list[str]


class EducationItem(CamelModel):
    degree: str
    institution: str
    period: str


class TailoredCV(CamelModel):
    full_name: str
    contact: str
    headline: str
    summary: str
    experience: list[ExperienceItem]
    skills: list[str]
    education: list[EducationItem]
