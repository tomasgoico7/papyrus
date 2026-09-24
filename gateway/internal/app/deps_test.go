package app_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/papyrus/gateway/internal/app"
	"github.com/papyrus/gateway/internal/config"
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
