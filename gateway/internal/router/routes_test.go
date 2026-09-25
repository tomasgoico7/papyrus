package router_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/papyrus/gateway/internal/app"
	"github.com/papyrus/gateway/internal/config"
	"github.com/papyrus/gateway/internal/jobs"
	"github.com/papyrus/gateway/internal/observability"
	"github.com/papyrus/gateway/internal/router"
)

// noQueue satisfies the handler's queue without a database; these tests only
// look at which routes exist, and never send a request through them.
type noQueue struct{}

func (noQueue) Enqueue(context.Context, jobs.NewJob) (*jobs.Job, bool, error) {
	return nil, false, nil
}
func (noQueue) Get(context.Context, string, string) (*jobs.Job, error) { return nil, nil }

// TestTheQueuedRoutesAreWhereTheClientLooksForThem guards a failure that would
// make no noise at all. The queued routes sit in a group of their own; had the
// grouping produced a doubled slash, POST /analyses would answer 404 for good,
// the client would take that as "no queue" and fall back to the synchronous
// endpoint every time, and the queue would be switched off in production with
// not one error logged anywhere.
func TestTheQueuedRoutesAreWhereTheClientLooksForThem(t *testing.T) {
	cfg := &config.Config{
		AIServiceURL:   "http://ai.invalid",
		JWKSURL:        "http://auth.invalid/jwks",
		AllowedOrigins: []string{"http://localhost:3000"},
		RateLimitRPM:   20,
		MaxUploadBytes: 1 << 20,
		RequestTimeout: time.Second,
		JobTimeout:     time.Second,
		CacheTTL:       time.Hour,
	}
	metrics := observability.NewMetrics()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	analyzer := app.Analyzer(cfg, app.UpstreamClient(app.UpstreamBudget(cfg)), metrics, logger)

	engine := router.New(cfg, logger, metrics, analyzer, noQueue{}, func() bool { return true })

	registered := map[string]bool{}
	for _, route := range engine.Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	for _, want := range []string{"POST /analyses", "GET /analyses/:id", "POST /analyze"} {
		if !registered[want] {
			t.Errorf("%s is not registered; routes are %v", want, keys(registered))
		}
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
