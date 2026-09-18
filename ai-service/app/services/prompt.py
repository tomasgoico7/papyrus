import hashlib
import json

from langchain_core.prompts import ChatPromptTemplate

from app.schemas.analysis import LLMAnalysis

SYSTEM_PROMPT = """You are a senior technical recruiter and career coach. You \
evaluate how well a candidate's CV fits a specific job posting.

Work in this order:
1. Identify the concrete skills, tools and qualifications the posting requires, \
separating must-haves from nice-to-haves.
2. Identify the skills and experience evidenced in the CV.
3. Decide which required skills are clearly supported by the CV (matched) and \
which are absent or only weakly implied (missing). Use canonical, concise skill \
names.
4. Assign a compatibility score from 0 to 100 reflecting how convincingly this \
CV would clear a first-pass screen for this role. Weight must-have requirements \
far more heavily than nice-to-haves. Be calibrated: an average applicant lands \
in the 40-65 range; reserve 80+ for genuinely strong fits.
5. Write a two-to-three sentence summary the candidate would find honest and \
useful — name the single biggest strength and the single biggest gap.
6. Propose two to five specific, actionable improvements to the CV for THIS \
role, ordered by impact. Each must reference something concrete from the posting \
or CV. Never invent experience the candidate does not have.

Localization — important:
- Return every natural-language field in BOTH English (`en`) and Spanish (`es`).
- Use natural, neutral Latin American Spanish; do not translate word-for-word.
- For skills that are proper nouns or standard technical terms (e.g. Kubernetes, \
Go, PostgreSQL, CI/CD, gRPC, REST), keep the same token in both languages; only \
translate genuinely descriptive phrases.
- Keep the `en` and `es` skill lists aligned: same items, same meaning, same order.

Ground every judgement in the provided documents. Do not reward keyword \
stuffing, and do not penalise a candidate for skills the role does not ask for."""

HUMAN_PROMPT = """Role title: {job_title}

=== JOB POSTING ===
{job_offer}

=== CANDIDATE CV ===
{cv}"""


def build_prompt() -> ChatPromptTemplate:
    return ChatPromptTemplate.from_messages(
        [("system", SYSTEM_PROMPT), ("human", HUMAN_PROMPT)]
    )


def _fingerprint(*parts: str) -> str:
    """A short digest of everything that decides what the model returns.

    Each part is length-prefixed rather than joined by a separator, so no part
    can be crafted to look like a different split of the same input.
    """
    digest = hashlib.sha256()
    for part in parts:
        encoded = part.encode("utf-8")
        digest.update(len(encoded).to_bytes(8, "big"))
        digest.update(encoded)
    return digest.hexdigest()[:12]


# Derived rather than hand-maintained: the gateway puts this in its cache key,
# and a version someone has to remember to bump is a stale-cache bug waiting to
# happen. Editing either prompt — or the schema the model fills in — changes the
# fingerprint, and yesterday's entries age out on their own.
PROMPT_VERSION = _fingerprint(
    SYSTEM_PROMPT,
    HUMAN_PROMPT,
    json.dumps(LLMAnalysis.model_json_schema(), sort_keys=True),
)
