package worker_test

import (
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

// swapTracerProvider installs provider globally for one test and hands back the
// undo. The worker reaches for the global provider the way production code
// does, so a test that wants to see its spans has to replace that rather than
// inject a different one.
func swapTracerProvider(t *testing.T, provider trace.TracerProvider) func() {
	t.Helper()
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	return func() { otel.SetTracerProvider(previous) }
}
