# 0005. Keep deterministic work out of the model

- **Status:** accepted
- **Date:** 2026-09-18 (recorded retroactively; decided during the initial build)

## Context

A language model will happily answer questions it should not be asked. Given a
score it will also return a verdict, and the answer will usually be right — until
the run where a score of 76 comes back as "moderate" and a score of 74 as
"strong", with nothing in the output explaining why.

Anything the model produces has to be validated, bounded and second-guessed.
Anything computed in code is simply correct, testable in isolation, and free.

## Decision

The model is asked only for judgement that requires reading: the score, the
prose, which required skills the CV evidences.

Everything derivable from that output is computed in Python. The verdict comes
from fixed score bands. The score is clamped to 0–100 rather than trusted. The
response shape is validated against a schema before anything else runs.

The same rule applies to work added later: if the answer can be computed, it is
computed.

## Consequences

The verdict bands are a three-line function with a parametrised test covering
every boundary, instead of a behaviour that has to be sampled to be believed.
Changing a threshold is a code change with a diff, not a prompt change with a
hope.

The cost is that some coupling now lives in code rather than in a prompt. Moving
the bands means editing Python, not a string.

This rule is what makes a cache key possible at all: identical inputs must give
an identical result for a cached answer to be correct.
