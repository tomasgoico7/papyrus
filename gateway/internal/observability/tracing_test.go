package observability_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/papyrus/gateway/internal/observability"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestTracingIsHarmlessWithNoBackendConfigured(t *testing.T) {
	tracing, err := observability.NewTracing(context.Background(), "", "test", 1, quietLogger())
	if err != nil {
		t.Fatalf("setting up tracing with no endpoint should not fail: %v", err)
	}
	t.Cleanup(func() { _ = tracing.Shutdown(context.Background()) })

	if tracing.Enabled() {
		t.Error("tracing reports itself enabled with nowhere to send spans")
	}

	// The instrumentation stays in the code path when tracing is off, so it has
	// to be safe to call. A nil tracer here is a panic in production the first
	// time somebody deploys without an endpoint.
	_, span := observability.Tracer().Start(context.Background(), "probe")
	observability.EndSpan(span, nil)

	if err := tracing.Shutdown(context.Background()); err != nil {
		t.Errorf("shutdown with no backend: %v", err)
	}
}

func TestTracingAlwaysInstallsThePropagators(t *testing.T) {
	// Even a process that exports nothing has to understand an inbound
	// traceparent and pass it on, or it puts a hole in somebody else's trace.
	tracing, err := observability.NewTracing(context.Background(), "", "test", 1, quietLogger())
	if err != nil {
		t.Fatalf("new tracing: %v", err)
	}
	t.Cleanup(func() { _ = tracing.Shutdown(context.Background()) })

	const parent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("traceparent", parent)

	ctx := otel.GetTextMapPropagator().Extract(
		context.Background(),
		propagation.HeaderCarrier(req.Header),
	)

	got := trace.SpanContextFromContext(ctx)
	if !got.IsValid() {
		t.Fatal("an inbound traceparent was not extracted; the trace stops here")
	}
	if want := "4bf92f3577b34da6a3ce929d0e0e4736"; got.TraceID().String() != want {
		t.Errorf("trace id = %s, want %s", got.TraceID(), want)
	}

	// And back out again, or the next service starts a trace of its own.
	carrier := propagation.HeaderCarrier{}
	otel.GetTextMapPropagator().Inject(
		trace.ContextWithSpanContext(context.Background(), got),
		carrier,
	)
	if carrier.Get("traceparent") == "" {
		t.Error("nothing was injected; a downstream call would look unrelated")
	}
}

func TestShutdownOnAZeroTracingDoesNothing(t *testing.T) {
	// The binaries defer this before they know whether setup succeeded.
	var tracing *observability.Tracing
	if err := tracing.Shutdown(context.Background()); err != nil {
		t.Errorf("shutdown on a nil tracing: %v", err)
	}
	if tracing.Enabled() {
		t.Error("a nil tracing reports itself enabled")
	}
}
