package observability_test

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/papyrus/gateway/internal/observability"
)

func TestTraceSurvivesARoundTripThroughStorage(t *testing.T) {
	provider := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	ctx, span := provider.Tracer("test").Start(context.Background(), "enqueue")
	defer span.End()

	stored := observability.TraceParentFrom(ctx)
	if stored == "" {
		t.Fatal("nothing to store: the trace would not cross the queue")
	}

	// A different process, minutes later, with only the column to go on.
	resumed := observability.ContextWithTraceParent(context.Background(), stored)
	got := trace.SpanContextFromContext(resumed)

	if !got.IsValid() {
		t.Fatal("the stored value did not yield a usable trace")
	}
	if got.TraceID() != span.SpanContext().TraceID() {
		t.Errorf("trace id = %s, want %s", got.TraceID(), span.SpanContext().TraceID())
	}
	if got.SpanID() != span.SpanContext().SpanID() {
		t.Errorf("parent span = %s, want %s", got.SpanID(), span.SpanContext().SpanID())
	}
	if !got.IsRemote() {
		t.Error("the resumed parent should be marked remote")
	}
	// The sampling decision travels with it, or the worker's half of a sampled
	// trace is dropped and the trace has a hole exactly where the work was.
	if got.IsSampled() != span.SpanContext().IsSampled() {
		t.Error("the sampling decision did not survive the queue")
	}
}

func TestTraceParentIsEmptyWithoutATrace(t *testing.T) {
	if got := observability.TraceParentFrom(context.Background()); got != "" {
		t.Errorf("traceparent = %q with no trace in flight, want empty", got)
	}
}

func TestAJobWithNoStoredTraceIsLeftAlone(t *testing.T) {
	for _, stored := range []string{"", "not-a-traceparent", "00-tooshort-0-01"} {
		ctx := observability.ContextWithTraceParent(context.Background(), stored)
		if trace.SpanContextFromContext(ctx).IsValid() {
			t.Errorf("stored %q produced a span context; garbage should be ignored", stored)
		}
	}
}
