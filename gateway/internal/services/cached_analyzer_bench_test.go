package services_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/papyrus/gateway/internal/cache"
	"github.com/papyrus/gateway/internal/observability"
	"github.com/papyrus/gateway/internal/services"
)

// A CV of a realistic size: the whole file is hashed on every lookup, so its
// size is part of what a hit costs.
var benchCV = bytes.Repeat([]byte("%PDF-1.4 sample bytes "), 4096)

var benchOffer = strings.Repeat("Backend engineer. Go, PostgreSQL, Docker. ", 20)

func benchmarkAnalyzer(b *testing.B, store cache.Store) *services.CachedAnalyzer {
	b.Helper()

	version := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"promptVersion":"bench","model":"test-model"}`))
	}))
	b.Cleanup(version.Close)

	return services.NewCachedAnalyzer(
		&countingAnalyzer{},
		store,
		services.NewVersionClient(version.URL, "", version.Client(), time.Hour),
		observability.NewMetrics(),
		time.Hour,
		5*time.Second,
	)
}

func benchRequest() services.AnalyzeRequest {
	return services.AnalyzeRequest{
		CV:       bytes.NewReader(benchCV),
		Filename: "cv.pdf",
		JobOffer: benchOffer,
		JobTitle: "Backend Engineer",
	}
}

// BenchmarkAnalysisCacheHit measures what a repeated analysis costs once the
// answer is already known: reading the upload, hashing it into a key, and
// decoding the stored result. In production this replaces a call to the model,
// which takes tens of seconds.
func BenchmarkAnalysisCacheHit(b *testing.B) {
	analyzer := benchmarkAnalyzer(b, cache.NewLRU(8))
	ctx := context.Background()

	// Warm it, so the loop only measures hits.
	if _, err := analyzer.Analyze(ctx, benchRequest()); err != nil {
		b.Fatalf("warm-up: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if _, err := analyzer.Analyze(ctx, benchRequest()); err != nil {
			b.Fatalf("analyze: %v", err)
		}
	}
}

// BenchmarkAnalysisCacheMiss is the same path with nothing stored. It still pays
// for the upstream call — a local stub here, the model in production — so the
// gap between the two benchmarks is not the cache's overhead; it is the stub's
// work plus one store write.
func BenchmarkAnalysisCacheMiss(b *testing.B) {
	analyzer := benchmarkAnalyzer(b, alwaysMissingStore{})
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if _, err := analyzer.Analyze(ctx, benchRequest()); err != nil {
			b.Fatalf("analyze: %v", err)
		}
	}
}

// alwaysMissingStore never holds anything, so every lookup is a miss and every
// write is discarded.
type alwaysMissingStore struct{}

func (alwaysMissingStore) Get(context.Context, string) ([]byte, error) {
	return nil, cache.ErrMiss
}

func (alwaysMissingStore) Set(context.Context, string, []byte, time.Duration) error {
	return nil
}
