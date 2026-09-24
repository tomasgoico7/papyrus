package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/papyrus/gateway/internal/cache"
	"github.com/papyrus/gateway/internal/observability"
	"github.com/papyrus/gateway/internal/transport"
)

// Analyzer is the behaviour the handler needs, satisfied both by the client that
// calls the AI service and by the cache that fronts it.
type Analyzer interface {
	Analyze(ctx context.Context, req AnalyzeRequest) (*transport.Analysis, error)
}

// CachedAnalyzer answers a repeated analysis from cache instead of paying for it
// again. The same CV against the same posting is a deterministic question — the
// verdict is derived in code and the model runs at a low temperature — so the
// answer keeps.
//
// Everything here degrades to "just call the upstream". A cache that is down, a
// version that cannot be read, a value that will not decode: all of them mean
// the request proceeds normally, a little slower.
type CachedAnalyzer struct {
	next     Analyzer
	store    cache.Store
	versions *VersionClient
	metrics  *observability.Metrics
	ttl      time.Duration
	// upstreamTimeout bounds the shared call, which deliberately outlives the
	// request that started it.
	upstreamTimeout time.Duration
	group           singleflight.Group
}

func NewCachedAnalyzer(
	next Analyzer,
	store cache.Store,
	versions *VersionClient,
	metrics *observability.Metrics,
	ttl time.Duration,
	upstreamTimeout time.Duration,
) *CachedAnalyzer {
	return &CachedAnalyzer{
		next:            next,
		store:           store,
		versions:        versions,
		metrics:         metrics,
		ttl:             ttl,
		upstreamTimeout: upstreamTimeout,
	}
}

func (a *CachedAnalyzer) Analyze(ctx context.Context, req AnalyzeRequest) (*transport.Analysis, error) {
	logger := observability.LoggerFrom(ctx)

	// The bytes are needed up front: a cache lookup cannot start until the input
	// has been read in full, so streaming the upload straight through is not an
	// option once caching is in play.
	cv, err := io.ReadAll(req.CV)
	if err != nil {
		return nil, fmt.Errorf("reading cv: %w", err)
	}

	version, err := a.versions.Current(ctx)
	if err != nil {
		// Without the prompt fingerprint a key would be a guess, and a wrong key
		// serves someone else's analysis. Skipping the cache is the safe answer.
		a.metrics.RecordCacheLookup(observability.CacheBypass)
		logger.Warn("analysis cache bypassed: version unavailable", slog.Any("error", err))
		return a.callUpstream(ctx, req, cv)
	}

	key := analysisKey(cv, req.JobOffer, req.JobTitle, version)

	if analysis, ok := a.load(ctx, key, logger); ok {
		a.metrics.RecordCacheLookup(observability.CacheHit)
		return analysis, nil
	}
	a.metrics.RecordCacheLookup(observability.CacheMiss)

	return a.fetchOnce(ctx, key, req, cv, logger)
}

func (a *CachedAnalyzer) load(ctx context.Context, key string, logger *slog.Logger) (*transport.Analysis, bool) {
	raw, err := a.store.Get(ctx, key)
	if err != nil {
		if !isMiss(err) {
			a.metrics.RecordCacheLookup(observability.CacheError)
			logger.Warn("analysis cache read failed", slog.Any("error", err))
		}
		return nil, false
	}

	var analysis transport.Analysis
	if err := json.Unmarshal(raw, &analysis); err != nil {
		// A value written by an older build, most likely. Treat it as absent.
		logger.Warn("analysis cache holds an undecodable entry", slog.Any("error", err))
		return nil, false
	}
	return &analysis, true
}

// fetchOnce collapses concurrent requests for the same key into a single
// upstream call. Without it, a hundred identical requests arriving on a cold
// cache become a hundred model calls — the cache would be doing its worst work
// at exactly the moment it matters most.
func (a *CachedAnalyzer) fetchOnce(
	ctx context.Context,
	key string,
	req AnalyzeRequest,
	cv []byte,
	logger *slog.Logger,
) (*transport.Analysis, error) {
	results := a.group.DoChan(key, func() (any, error) {
		// Detached from the caller on purpose: whoever started the call may walk
		// away, and the others waiting on it should not lose their answer too.
		// The timeout replaces the deadline that cancellation would have carried.
		shared, cancel := context.WithTimeout(context.WithoutCancel(ctx), a.upstreamTimeout)
		defer cancel()

		analysis, err := a.callUpstream(shared, req, cv)
		if err != nil {
			return nil, err
		}

		if encoded, err := json.Marshal(analysis); err != nil {
			logger.Warn("analysis could not be encoded for the cache", slog.Any("error", err))
		} else if err := a.store.Set(shared, key, encoded, a.ttl); err != nil {
			a.metrics.RecordCacheLookup(observability.CacheError)
			logger.Warn("analysis cache write failed", slog.Any("error", err))
		}

		return analysis, nil
	})

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-results:
		if result.Shared {
			a.metrics.RecordCacheShared()
		}
		if result.Err != nil {
			return nil, result.Err
		}
		return result.Val.(*transport.Analysis), nil
	}
}

func (a *CachedAnalyzer) callUpstream(ctx context.Context, req AnalyzeRequest, cv []byte) (*transport.Analysis, error) {
	req.CV = bytes.NewReader(cv)
	return a.next.Analyze(ctx, req)
}

func isMiss(err error) bool {
	return errors.Is(err, cache.ErrMiss)
}

// analysisKey covers every input that can change the answer: the file itself,
// the posting, the role title the prompt is given, the model, and the prompt
// fingerprint. Leaving any of them out would serve one request's analysis to a
// different question.
//
// Only whitespace is normalised in the text. Folding case would let two postings
// that read differently share a key, and the stored answer quotes the posting it
// was actually produced from.
func analysisKey(cv []byte, jobOffer, jobTitle string, version Version) string {
	cvSum := sha256.Sum256(cv)
	offerSum := sha256.Sum256([]byte(normalizeText(jobOffer)))
	titleSum := sha256.Sum256([]byte(normalizeText(jobTitle)))

	return fmt.Sprintf(
		"analysis:1:%s:%s:%x:%x:%x",
		version.PromptVersion,
		version.Model,
		cvSum,
		offerSum,
		titleSum,
	)
}

func normalizeText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

// Lookup is the read half of Analyze, for the asynchronous path. The handler
// needs two things before it can decide what to answer: whether the result is
// already known, and the key a job for it should be deduplicated by.
type Lookup struct {
	// Key is empty when no trustworthy key could be built, which means the
	// request must not be deduplicated against anything.
	Key string
	// CV is the upload, read once here so the caller does not have to read it
	// again to enqueue it.
	CV []byte
	// Analysis is nil unless the answer was already cached.
	Analysis *transport.Analysis
}

// lookupVersionWait is how long enqueueing will wait for the AI service's
// version. Long enough to cover a warm fetch several times over, short enough
// that a cold one never reaches the person waiting on the response.
const lookupVersionWait = 2 * time.Second

// Lookup reads the upload and reports what the cache already knows about it. It
// never calls the upstream: deciding to do the work is the caller's business.
func (a *CachedAnalyzer) Lookup(ctx context.Context, req AnalyzeRequest) (Lookup, error) {
	logger := observability.LoggerFrom(ctx)

	cv, err := io.ReadAll(req.CV)
	if err != nil {
		return Lookup{}, fmt.Errorf("reading cv: %w", err)
	}

	// This runs on the request path, ahead of enqueueing, so it gets a short
	// leash. Waiting for the version here made the asynchronous endpoint wait on
	// the AI service after all: with the service asleep, the request that only
	// had to write a row took twenty four seconds, which is the whole cold start
	// moved from the worker to the person clicking. Past the budget the job is
	// queued without a cache key, and the refresh this started keeps running
	// and wakes the service for the worker that picks the job up.
	versionCtx, cancel := context.WithTimeout(ctx, lookupVersionWait)
	version, err := a.versions.Recent(versionCtx)
	cancel()
	if err != nil {
		// No fingerprint means no key worth trusting. The work still has to
		// happen; it just cannot be shared with anything else.
		a.metrics.RecordCacheLookup(observability.CacheBypass)
		logger.Warn("analysis cache bypassed: version unavailable", slog.Any("error", err))
		return Lookup{CV: cv}, nil
	}

	key := analysisKey(cv, req.JobOffer, req.JobTitle, version)

	if analysis, ok := a.load(ctx, key, logger); ok {
		a.metrics.RecordCacheLookup(observability.CacheHit)
		return Lookup{Key: key, CV: cv, Analysis: analysis}, nil
	}

	a.metrics.RecordCacheLookup(observability.CacheMiss)
	return Lookup{Key: key, CV: cv}, nil
}
