package services_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/papyrus/gateway/internal/cache"
	"github.com/papyrus/gateway/internal/observability"
	"github.com/papyrus/gateway/internal/services"
	"github.com/papyrus/gateway/internal/transport"
)

// countingAnalyzer stands in for the AI service and records what reached it.
type countingAnalyzer struct {
	calls atomic.Int64
	delay time.Duration
	err   error

	mu     sync.Mutex
	lastCV string
}

func (a *countingAnalyzer) Analyze(_ context.Context, req services.AnalyzeRequest) (*transport.Analysis, error) {
	a.calls.Add(1)
	if a.delay > 0 {
		time.Sleep(a.delay)
	}

	cv, _ := io.ReadAll(req.CV)
	a.mu.Lock()
	a.lastCV = string(cv)
	a.mu.Unlock()

	if a.err != nil {
		return nil, a.err
	}
	analysis := sampleAnalysis()
	return &analysis, nil
}

func (a *countingAnalyzer) cv() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastCV
}

// versionServer serves the AI service's /version, with a fingerprint the test
// can change to simulate a redeploy.
func versionServer(t *testing.T, promptVersion *string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"promptVersion":"` + *promptVersion + `","model":"test-model"}`))
	}))
	t.Cleanup(server.Close)
	return server
}

// brokenStore is a tier that is down: every operation fails.
type brokenStore struct{}

func (brokenStore) Get(context.Context, string) ([]byte, error) {
	return nil, errors.New("cache unavailable")
}

func (brokenStore) Set(context.Context, string, []byte, time.Duration) error {
	return errors.New("cache unavailable")
}

// corruptibleStore hands back a value an older build might have written.
type corruptibleStore struct {
	inner   cache.Store
	corrupt atomic.Bool
}

func (s *corruptibleStore) Get(ctx context.Context, key string) ([]byte, error) {
	value, err := s.inner.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	if s.corrupt.Load() {
		return []byte("{not json"), nil
	}
	return value, nil
}

func (s *corruptibleStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return s.inner.Set(ctx, key, value, ttl)
}

type cachedFixture struct {
	analyzer *countingAnalyzer
	cached   *services.CachedAnalyzer
	store    cache.Store
	version  *string
}

func newCachedFixture(t *testing.T, store cache.Store) cachedFixture {
	t.Helper()

	version := "v1"
	server := versionServer(t, &version)
	next := &countingAnalyzer{}

	return cachedFixture{
		analyzer: next,
		store:    store,
		version:  &version,
		cached: services.NewCachedAnalyzer(
			next,
			store,
			services.NewVersionClient(server.URL, "", server.Client(), time.Millisecond),
			observability.NewMetrics(),
			time.Hour,
			5*time.Second,
		),
	}
}

func request(cv, offer, title string) services.AnalyzeRequest {
	return services.AnalyzeRequest{
		CV:       strings.NewReader(cv),
		Filename: "cv.pdf",
		JobOffer: offer,
		JobTitle: title,
	}
}

func TestCachedAnalyzerServesARepeatFromCache(t *testing.T) {
	f := newCachedFixture(t, cache.NewLRU(8))
	ctx := context.Background()

	first, err := f.cached.Analyze(ctx, request("%PDF cv", "A backend role.", "Backend"))
	if err != nil {
		t.Fatalf("first analyze: %v", err)
	}
	second, err := f.cached.Analyze(ctx, request("%PDF cv", "A backend role.", "Backend"))
	if err != nil {
		t.Fatalf("second analyze: %v", err)
	}

	if got := f.analyzer.calls.Load(); got != 1 {
		t.Errorf("upstream calls = %d, want the repeat served from cache", got)
	}
	if first.Score != second.Score || first.Summary.En != second.Summary.En {
		t.Error("the cached analysis differs from the one that was stored")
	}
}

func TestCachedAnalyzerKeysOnEveryInputThatChangesTheAnswer(t *testing.T) {
	base := request("%PDF cv", "A backend role.", "Backend")

	tests := []struct {
		name  string
		build func() services.AnalyzeRequest
	}{
		{"a different cv", func() services.AnalyzeRequest { return request("%PDF other", "A backend role.", "Backend") }},
		{"a different posting", func() services.AnalyzeRequest { return request("%PDF cv", "A frontend role.", "Backend") }},
		{"a different role title", func() services.AnalyzeRequest { return request("%PDF cv", "A backend role.", "Staff") }},
		// The prompt puts the title in front of the model, so it changes the
		// answer even though it looks like metadata.
		{"no role title at all", func() services.AnalyzeRequest { return request("%PDF cv", "A backend role.", "") }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newCachedFixture(t, cache.NewLRU(8))
			ctx := context.Background()

			if _, err := f.cached.Analyze(ctx, base); err != nil {
				t.Fatalf("base analyze: %v", err)
			}
			base.CV = strings.NewReader("%PDF cv") // the reader was consumed

			if _, err := f.cached.Analyze(ctx, tc.build()); err != nil {
				t.Fatalf("variant analyze: %v", err)
			}

			if got := f.analyzer.calls.Load(); got != 2 {
				t.Errorf("upstream calls = %d, want the variant to miss the cache", got)
			}
		})
	}
}

func TestCachedAnalyzerIgnoresWhitespaceButNotCase(t *testing.T) {
	t.Run("whitespace is normalised", func(t *testing.T) {
		f := newCachedFixture(t, cache.NewLRU(8))
		ctx := context.Background()

		_, _ = f.cached.Analyze(ctx, request("%PDF cv", "A backend role.", "Backend"))
		_, _ = f.cached.Analyze(ctx, request("%PDF cv", "  A   backend\n role.  ", "Backend"))

		if got := f.analyzer.calls.Load(); got != 1 {
			t.Errorf("upstream calls = %d; a re-paste with different spacing should hit", got)
		}
	})

	t.Run("case is not folded", func(t *testing.T) {
		f := newCachedFixture(t, cache.NewLRU(8))
		ctx := context.Background()

		_, _ = f.cached.Analyze(ctx, request("%PDF cv", "A backend role.", "Backend"))
		_, _ = f.cached.Analyze(ctx, request("%PDF cv", "a BACKEND role.", "Backend"))

		// The stored answer quotes the posting it was produced from, so two
		// postings that read differently must not share it.
		if got := f.analyzer.calls.Load(); got != 2 {
			t.Errorf("upstream calls = %d; a differently-cased posting must miss", got)
		}
	})
}

func TestCachedAnalyzerInvalidatesWhenThePromptChanges(t *testing.T) {
	f := newCachedFixture(t, cache.NewLRU(8))
	ctx := context.Background()

	_, _ = f.cached.Analyze(ctx, request("%PDF cv", "A backend role.", "Backend"))

	// A redeploy with an edited prompt.
	*f.version = "v2"
	time.Sleep(2 * time.Millisecond) // let the version ttl lapse

	_, _ = f.cached.Analyze(ctx, request("%PDF cv", "A backend role.", "Backend"))

	if got := f.analyzer.calls.Load(); got != 2 {
		t.Errorf("upstream calls = %d, want the old entry abandoned after a prompt change", got)
	}
}

func TestCachedAnalyzerStillWorksWhenTheCacheIsDown(t *testing.T) {
	f := newCachedFixture(t, brokenStore{})
	ctx := context.Background()

	analysis, err := f.cached.Analyze(ctx, request("%PDF cv", "A backend role.", "Backend"))
	if err != nil {
		t.Fatalf("a broken cache must not fail the analysis: %v", err)
	}
	if analysis == nil || analysis.Score == 0 {
		t.Error("expected the upstream answer to be returned")
	}
	if got := f.analyzer.calls.Load(); got != 1 {
		t.Errorf("upstream calls = %d, want 1", got)
	}
}

func TestCachedAnalyzerBypassesWhenTheVersionIsUnavailable(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	client := down.Client()
	down.Close()

	next := &countingAnalyzer{}
	cached := services.NewCachedAnalyzer(
		next,
		cache.NewLRU(8),
		services.NewVersionClient(down.URL, "", client, time.Minute),
		observability.NewMetrics(),
		time.Hour,
		5*time.Second,
	)
	ctx := context.Background()

	// Twice: without a fingerprint no key can be trusted, so nothing is cached
	// and both calls go upstream rather than risking a wrong hit.
	if _, err := cached.Analyze(ctx, request("%PDF cv", "A backend role.", "Backend")); err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if _, err := cached.Analyze(ctx, request("%PDF cv", "A backend role.", "Backend")); err != nil {
		t.Fatalf("analyze: %v", err)
	}

	if got := next.calls.Load(); got != 2 {
		t.Errorf("upstream calls = %d, want the cache skipped entirely", got)
	}
}

func TestCachedAnalyzerCollapsesConcurrentIdenticalRequests(t *testing.T) {
	f := newCachedFixture(t, cache.NewLRU(8))
	f.analyzer.delay = 80 * time.Millisecond
	ctx := context.Background()

	const callers = 25
	var wg sync.WaitGroup
	errs := make(chan error, callers)

	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := f.cached.Analyze(ctx, request("%PDF cv", "A backend role.", "Backend")); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("analyze: %v", err)
	}

	// This is the whole point of singleflight: a cold cache under a burst must
	// produce one model call, not one per caller.
	if got := f.analyzer.calls.Load(); got != 1 {
		t.Errorf("upstream calls = %d, want the burst collapsed into 1", got)
	}
}

func TestCachedAnalyzerFinishesSharedWorkAfterTheFirstCallerLeaves(t *testing.T) {
	f := newCachedFixture(t, cache.NewLRU(8))
	f.analyzer.delay = 60 * time.Millisecond

	leaving, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = f.cached.Analyze(leaving, request("%PDF cv", "A backend role.", "Backend"))
	}()

	time.Sleep(10 * time.Millisecond)
	cancel() // the caller that started the shared call walks away
	wg.Wait()

	// The answer still lands in the cache, so the next caller pays nothing.
	time.Sleep(120 * time.Millisecond)
	if _, err := f.cached.Analyze(context.Background(), request("%PDF cv", "A backend role.", "Backend")); err != nil {
		t.Fatalf("analyze: %v", err)
	}

	if got := f.analyzer.calls.Load(); got != 1 {
		t.Errorf("upstream calls = %d; the abandoned call should still have completed and been stored", got)
	}
}

func TestCachedAnalyzerTreatsAnUndecodableEntryAsAMiss(t *testing.T) {
	store := &corruptibleStore{inner: cache.NewLRU(8)}
	f := newCachedFixture(t, store)
	ctx := context.Background()

	if _, err := f.cached.Analyze(ctx, request("%PDF cv", "A backend role.", "Backend")); err != nil {
		t.Fatalf("analyze: %v", err)
	}

	// Now the cache returns something an older build might have left behind.
	store.corrupt.Store(true)

	if _, err := f.cached.Analyze(ctx, request("%PDF cv", "A backend role.", "Backend")); err != nil {
		t.Fatalf("a corrupt entry must not fail the analysis: %v", err)
	}
	if got := f.analyzer.calls.Load(); got != 2 {
		t.Errorf("upstream calls = %d, want the corrupt entry ignored", got)
	}
}

func TestCachedAnalyzerReplaysTheExactUploadUpstream(t *testing.T) {
	f := newCachedFixture(t, cache.NewLRU(8))

	if _, err := f.cached.Analyze(context.Background(), request("%PDF-1.4 the real bytes", "A backend role.", "Backend")); err != nil {
		t.Fatalf("analyze: %v", err)
	}

	// Reading the upload to hash it must not cost the upstream its copy.
	if got := f.analyzer.cv(); got != "%PDF-1.4 the real bytes" {
		t.Errorf("upstream saw %q, want the original bytes", got)
	}
}

func TestCachedAnalyzerPropagatesAnUpstreamFailure(t *testing.T) {
	f := newCachedFixture(t, cache.NewLRU(8))
	f.analyzer.err = errors.New("upstream exploded")

	if _, err := f.cached.Analyze(context.Background(), request("%PDF cv", "A backend role.", "Backend")); err == nil {
		t.Fatal("expected the upstream failure to reach the caller")
	}

	// And a failure is never stored: the next attempt has to try again.
	f.analyzer.err = nil
	if _, err := f.cached.Analyze(context.Background(), request("%PDF cv", "A backend role.", "Backend")); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if got := f.analyzer.calls.Load(); got != 2 {
		t.Errorf("upstream calls = %d, want the failure not to have been cached", got)
	}
}

func TestLookupReportsAKnownResultWithoutCallingUpstream(t *testing.T) {
	f := newCachedFixture(t, cache.NewLRU(8))
	ctx := context.Background()

	if _, err := f.cached.Analyze(ctx, request("%PDF cv", "A backend role.", "Backend")); err != nil {
		t.Fatalf("seeding the cache: %v", err)
	}
	before := f.analyzer.calls.Load()

	found, err := f.cached.Lookup(ctx, request("%PDF cv", "A backend role.", "Backend"))
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}

	if found.Analysis == nil {
		t.Fatal("expected the cached analysis")
	}
	if found.Key == "" {
		t.Error("a hit should still report the key the job would use")
	}
	if got := f.analyzer.calls.Load(); got != before {
		t.Errorf("upstream calls went from %d to %d; a lookup must never do the work", before, got)
	}
}

func TestLookupReportsAMissWithTheKeyAndTheUpload(t *testing.T) {
	f := newCachedFixture(t, cache.NewLRU(8))

	found, err := f.cached.Lookup(context.Background(), request("%PDF-1.4 real bytes", "A backend role.", "Backend"))
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}

	if found.Analysis != nil {
		t.Error("nothing was cached, so nothing should come back")
	}
	if found.Key == "" {
		t.Error("a miss still needs a key, so the job can be deduplicated by it")
	}
	// The caller enqueues these bytes; reading them here must not consume them.
	if string(found.CV) != "%PDF-1.4 real bytes" {
		t.Errorf("cv = %q, want the upload handed back", found.CV)
	}
	if f.analyzer.calls.Load() != 0 {
		t.Error("a lookup must never call the upstream")
	}
}

func TestLookupWithholdsTheKeyWhenTheVersionIsUnavailable(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	client := down.Client()
	down.Close()

	cached := services.NewCachedAnalyzer(
		&countingAnalyzer{},
		cache.NewLRU(8),
		services.NewVersionClient(down.URL, "", client, time.Minute),
		observability.NewMetrics(),
		time.Hour,
		5*time.Second,
	)

	found, err := cached.Lookup(context.Background(), request("%PDF cv", "A backend role.", "Backend"))
	if err != nil {
		t.Fatalf("an unreachable version endpoint must not fail the lookup: %v", err)
	}

	// No fingerprint means no key worth trusting; the caller gives the job one
	// that cannot collide with anything.
	if found.Key != "" {
		t.Errorf("key = %q, want none without a prompt fingerprint", found.Key)
	}
	if len(found.CV) == 0 {
		t.Error("the upload should still come back, so the job can run uncached")
	}
}

// TestLookupDoesNotWaitOnAColdUpstream is the trace that found this: POST
// /analyses took twenty four seconds, all of it inside a version fetch to an AI
// service that was asleep. Enqueueing a row must not depend on that service
// being awake — that dependency is the one the queue exists to remove.
func TestLookupDoesNotWaitOnAColdUpstream(t *testing.T) {
	cold := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(10 * time.Second):
		case <-r.Context().Done():
			return
		}
		_, _ = w.Write([]byte(`{"promptVersion":"v1","model":"gemini-test"}`))
	}))
	defer func() {
		// The refresh this starts outlives the lookup on purpose. Cut it off
		// rather than wait out the whole cold start for the server to close.
		cold.CloseClientConnections()
		cold.Close()
	}()

	cached := services.NewCachedAnalyzer(
		&countingAnalyzer{},
		cache.NewLRU(8),
		services.NewVersionClient(cold.URL, "", cold.Client(), time.Minute),
		observability.NewMetrics(),
		time.Hour,
		5*time.Second,
	)

	started := time.Now()
	found, err := cached.Lookup(context.Background(), request("%PDF cv", "A backend role.", "Backend"))
	waited := time.Since(started)

	if err != nil {
		t.Fatalf("a cold upstream must not fail the lookup: %v", err)
	}
	// The budget is two seconds; the upstream would take ten. Anything near ten
	// means the request path is waiting out the cold start again.
	if waited > 4*time.Second {
		t.Errorf("lookup waited %v on a cold upstream; the request path should give up after its budget", waited)
	}
	if found.Key != "" {
		t.Error("no version arrived in time, so there should be no cache key")
	}
	if len(found.CV) == 0 {
		t.Error("the upload should still come back so the job can be queued")
	}
}
