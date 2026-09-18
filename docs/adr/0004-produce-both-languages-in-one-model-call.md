# 0004. Produce both languages in one model call

- **Status:** accepted
- **Date:** 2026-09-18 (recorded retroactively; decided during the initial build)

## Context

The interface is bilingual. An analysis is mostly prose — a summary, a title and
a detail per suggestion, skill lists — so switching the interface language has to
change the result too, not just the surrounding chrome.

Translating on demand means a second model call, seconds of waiting, and a result
that can disagree with the one already on screen. Storing only the language the
user happened to be using at the time means re-running the analysis to see the
other one.

## Decision

The model returns every natural-language field as `{ "en": …, "es": … }` in a
single structured call. Both versions are stored. The interface reads whichever
half matches the current language.

The prompt asks for neutral Latin American Spanish and requires that proper nouns
and standard technical terms stay identical across both lists, so the skill sets
line up item for item.

## Consequences

Switching language re-renders instantly, with no request and no re-analysis, and
the two versions can never drift apart because they were produced together.

The cost is roughly double the output tokens per analysis. On the free tier this
is not a constraint; on a metered plan it would be the first thing to measure.

Score and verdict are language-neutral and stay outside this entirely — see
[0005](0005-keep-deterministic-work-out-of-the-model.md).

Revisit if a third language is added: at that point the per-call cost grows
linearly and on-demand translation with a cache becomes the better shape.
