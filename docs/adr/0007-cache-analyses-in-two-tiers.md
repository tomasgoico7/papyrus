# 0007. Cache analyses in two tiers

- **Status:** accepted
- **Date:** 2026-09-18

## Context

An analysis costs tens of seconds of model time and a slice of a free-tier quota,
and nothing was kept. Re-opening a result, retrying after a timeout, or measuring
one CV against a posting a second time all paid full price for an answer the
system had already produced.

The answer is reproducible in a way most model output is not:
[0005](0005-keep-deterministic-work-out-of-the-model.md) keeps everything
derivable out of the model, the temperature is low, and the response is validated
against a fixed schema. Identical inputs are meant to give the same result, which
is the precondition for caching anything at all.

Two constraints shaped the design. A managed Redis on a free tier sits tens of
milliseconds away, which is most of the latency caching was supposed to remove.
And a cache that can fail is a new way for analyses to fail, which would trade a
slow feature for an unreliable one.

## Decision

**The key covers everything that can change the answer**: a digest of the CV
bytes, the posting, the role title, the model name, and a fingerprint of the
prompt. Leaving any of them out serves one question's answer to a different
question.

**The prompt fingerprint is derived, not maintained.** The AI service hashes its
prompts together with the JSON schema the model fills in, and serves the digest
on `/version`. Editing a prompt changes the fingerprint, so yesterday's entries
stop being addressed and age out; a version number someone has to remember to
bump is a stale-cache bug waiting to happen.

**Two tiers.** An in-process LRU absorbs the repeats; Redis carries what a single
process cannot know — entries written by another replica, and entries that
survive a restart. A hit in the shared tier is promoted to the local one. The
local copy gets a shorter lifetime than the shared entry, so replicas converge.

**One upstream call per key, not per caller.** `singleflight` collapses concurrent
requests for the same key, so a burst on a cold cache produces one model call
instead of one per caller. The shared call is deliberately detached from the
request that started it: whoever arrived first may walk away, and the callers
waiting behind them should not lose the answer.

**Every failure degrades to calling the upstream.** A cache that is down, a
version that cannot be read, a stored value that will not decode — all of them
mean the request proceeds normally, a little slower. Redis is optional: without
it the gateway still caches in process.

**Only whitespace is normalised.** Re-pasting a posting with different spacing
hits; a posting that differs in case does not. The stored answer quotes the text
it was produced from, so two postings that read differently must not share it.

## Consequences

A repeated analysis costs **~0.12 ms** on the gateway instead of a call to the
model, measured with `BenchmarkAnalysisCacheHit` against a warm local tier. What
it replaces is tens of seconds of provider time and a slice of the daily quota.

The upload can no longer be streamed. A lookup cannot begin until the input has
been read in full, so the gateway now holds the whole CV in memory to hash it —
making explicit a cost that was already being paid by the multipart encoder.

Redis is a new dependency, and a new thing to run, misconfigure and watch. It is
optional by design, and its absence is a warning in the log rather than a failed
startup.

When `/version` is briefly unreachable the last known fingerprint is reused
rather than disabling the cache. A genuinely outdated fingerprint can therefore
stay in use for up to the version ttl after a deploy — bounded, and cheaper than
losing caching over a blip.

The hit rate is exposed as `analysis_cache_lookups_total`, with errors and
bypasses counted apart from misses: folding them together would make a broken
cache look merely cold.

Revisit if analyses stop being reproducible — a higher temperature, or any
per-request input that does not reach the key would both break the assumption
this rests on.
