package observability

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/papyrus/gateway/internal/buildinfo"
)

// ServiceName is how this process identifies itself in a trace.
const ServiceName = "papyrus-gateway"

// shutdownTimeout bounds the final flush. Spans held in a buffer when the
// process exits are spans nobody will ever see, and a deploy is exactly when
// the interesting ones are produced — but a service that will not exit is worse
// than a trace that goes missing.
const shutdownTimeout = 5 * time.Second

// Tracing is the tracer provider's lifetime, handed back so the caller can
// flush it on the way out.
type Tracing struct {
	shutdown func(context.Context) error
	enabled  bool
}

// Enabled reports whether spans are being exported anywhere.
func (t *Tracing) Enabled() bool { return t != nil && t.enabled }

// Shutdown flushes whatever is still buffered.
func (t *Tracing) Shutdown(ctx context.Context) error {
	if t == nil || t.shutdown == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, shutdownTimeout)
	defer cancel()
	return t.shutdown(ctx)
}

// NewTracing installs a tracer provider and the W3C propagators.
//
// Tracing is off unless an OTLP endpoint is configured, and off means a no-op
// provider rather than a disabled code path: the instrumentation stays in place
// and costs an interface call, so there is no second, untested configuration in
// which the service runs without it. Everything else the exporter needs —
// endpoint, headers, protocol — comes from the standard OTEL_* variables, which
// are what every backend's setup instructions already tell you to set.
//
// A failure to reach the collector is not a failure to start. Traces are for
// understanding the service, and a service that refuses to run because nobody
// is listening to its traces has the priority backwards.
func NewTracing(ctx context.Context, endpoint, environment string, sampleRatio float64, logger *slog.Logger) (*Tracing, error) {
	// Set regardless: a process that exports nothing still has to understand an
	// incoming traceparent and pass it on, or it breaks the trace for everyone
	// downstream of it.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	if endpoint == "" {
		otel.SetTracerProvider(noop.NewTracerProvider())
		logger.Info("tracing is off: no otlp endpoint configured")
		return &Tracing{}, nil
	}

	exporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("observability: otlp exporter: %w", err)
	}

	attributes := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(ServiceName),
		// The same short commit the health check reports, so a trace can be tied
		// to the build that produced it without asking anybody.
		semconv.ServiceVersion(buildinfo.Revision()),
		attribute.String("deployment.environment.name", environment),
	)

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(attributes),
		// Parent-based: once a trace is being recorded every service on it
		// records, and once it is not, none do. Sampling each service
		// independently produces traces with holes in them, which are worse than
		// no trace at all because they look complete.
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(sampleRatio))),
	)
	otel.SetTracerProvider(provider)

	logger.Info("tracing is on",
		slog.String("endpoint", endpoint),
		slog.Float64("sample_ratio", sampleRatio),
	)

	return &Tracing{shutdown: provider.Shutdown, enabled: true}, nil
}

// Tracer returns the named tracer for this service.
func Tracer() trace.Tracer {
	return otel.Tracer(ServiceName)
}

// EndSpan closes span, recording err when there is one.
//
// It exists so that the three lines every instrumented operation ends with are
// written once. Forgetting the status is the common mistake, and a span that
// failed but is not marked failed is invisible in every "show me the errors"
// query there is.
func EndSpan(span trace.Span, err error) {
	if err != nil && !errors.Is(err, context.Canceled) {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}
