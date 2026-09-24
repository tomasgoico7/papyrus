package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/papyrus/gateway/internal/app"
	"github.com/papyrus/gateway/internal/config"
	"github.com/papyrus/gateway/internal/observability"
	"github.com/papyrus/gateway/internal/services"
	"github.com/papyrus/gateway/internal/transport"
)

// TestUpstreamBudgetClearsEveryCallersDeadline is the guard on a coupling that
// has already been got wrong once. One HTTP client serves the request path, the
// worker and the readiness probe; its timeout applies to all three, so raising
// any one of their deadlines without raising the client's makes that deadline
// do nothing. The job timeout was two minutes and the probe ninety seconds
// while the client cut everything at sixty-five.
func TestUpstreamBudgetClearsEveryCallersDeadline(t *testing.T) {
	cases := []struct {
		name string
		cfg  config.Config
	}{
		{"defaults", config.Config{RequestTimeout: 60 * time.Second, JobTimeout: 120 * time.Second}},
		{"long request path", config.Config{RequestTimeout: 10 * time.Minute, JobTimeout: 30 * time.Second}},
		{"long job", config.Config{RequestTimeout: 5 * time.Second, JobTimeout: 10 * time.Minute}},
		{"nothing configured", config.Config{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			budget := app.UpstreamBudget(&tc.cfg)

			if budget < tc.cfg.RequestTimeout {
				t.Errorf("budget %v is under the request timeout %v", budget, tc.cfg.RequestTimeout)
			}
			if budget < tc.cfg.JobTimeout {
				t.Errorf("budget %v is under the job timeout %v", budget, tc.cfg.JobTimeout)
			}
			// The probe waits out a cold start, measured at 21 to 31 seconds, and
			// its own timeout is the one that has to be able to cover that.
			if budget < app.ReadinessTimeout {
				t.Errorf("budget %v is under the readiness timeout %v", budget, app.ReadinessTimeout)
			}

			// The client adds its own margin on top, so the deadline the caller
			// sets is the one that fires and the caller gets its own error rather
			// than a bare transport failure.
			if client := app.UpstreamClient(budget); client.Timeout <= budget {
				t.Errorf("client timeout %v does not clear the budget %v", client.Timeout, budget)
			}
		})
	}
}

func TestOutboundSpansAreNamedByTheEndpointTheyCall(t *testing.T) {
	// A trace that says "HTTP GET took twenty three seconds" makes the reader
	// guess which endpoint it was. The name should answer that on its own.
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous) })

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	client := app.UpstreamClient(5 * time.Second)
	resp, err := client.Get(upstream.URL + "/version")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	resp.Body.Close()

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("recorded %d spans, want 1", len(spans))
	}
	if got := spans[0].Name(); got != "GET /version" {
		t.Errorf("span name = %q, want the method and the path", got)
	}
}

// TestAWorkerCanWaitLongerThanTheRequestPath checks the wiring rather than any
// one component, because that is where this broke. Every piece was right on its
// own: the worker set a four minute deadline, the HTTP client allowed it, and
// the cache honoured whatever timeout it was handed. It was handed the request
// timeout, so every worker attempt died at two minutes regardless.
func TestAWorkerCanWaitLongerThanTheRequestPath(t *testing.T) {
	aiService := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/version":
			_, _ = w.Write([]byte(`{"promptVersion":"v1","model":"gemini-test"}`))
		case "/analyze":
			// Longer than the request path may wait, well inside the job's budget.
			time.Sleep(300 * time.Millisecond)
			_ = json.NewEncoder(w).Encode(transport.Analysis{
				Score:         70,
				Verdict:       "moderate",
				Summary:       transport.Localized{En: "ok", Es: "ok"},
				MatchedSkills: transport.LocalizedList{En: []string{}, Es: []string{}},
				MissingSkills: transport.LocalizedList{En: []string{}, Es: []string{}},
				Suggestions:   []transport.Suggestion{},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer aiService.Close()

	cfg := &config.Config{
		AIServiceURL:      aiService.URL,
		RequestTimeout:    100 * time.Millisecond,
		JobTimeout:        2 * time.Second,
		CacheTTL:          time.Hour,
		CacheLocalEntries: 8,
	}
	analyzer := app.Analyzer(cfg, app.UpstreamClient(app.UpstreamBudget(cfg)),
		observability.NewMetrics(), slog.New(slog.NewTextHandler(io.Discard, nil)))

	// The worker's deadline, not the request path's.
	ctx, cancel := context.WithTimeout(context.Background(), cfg.JobTimeout)
	defer cancel()

	analysis, err := analyzer.Analyze(ctx, services.AnalyzeRequest{
		CV:       bytes.NewReader([]byte("%PDF-1.4 cv")),
		Filename: "cv.pdf",
		JobOffer: "A backend role.",
	})
	if err != nil {
		t.Fatalf("a call inside the job's budget failed: %v — something is still sized for the request path", err)
	}
	if analysis.Score != 70 {
		t.Errorf("score = %d, want the upstream's answer", analysis.Score)
	}
}
