package middleware_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/papyrus/gateway/internal/middleware"
	"github.com/papyrus/gateway/internal/observability"
	"github.com/papyrus/gateway/internal/requestid"
)

func requestIDEngine(t *testing.T, seen *string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(middleware.RequestID(slog.New(slog.NewTextHandler(io.Discard, nil))))
	engine.GET("/probe", func(c *gin.Context) {
		*seen = requestid.FromContext(c.Request.Context())
		c.Status(http.StatusOK)
	})
	return engine
}

func TestRequestIDGeneratesAnIdentifierWhenNoneIsSupplied(t *testing.T) {
	var onContext string
	rec := httptest.NewRecorder()
	requestIDEngine(t, &onContext).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/probe", nil))

	echoed := rec.Header().Get(requestid.Header)
	if len(echoed) != 32 {
		t.Fatalf("echoed id = %q, want a generated 32-character id", echoed)
	}
	if onContext != echoed {
		t.Errorf("context id = %q, echoed id = %q; they must match", onContext, echoed)
	}
}

func TestRequestIDReusesASafeInboundIdentifier(t *testing.T) {
	var onContext string
	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req.Header.Set(requestid.Header, "caller-supplied-42")

	rec := httptest.NewRecorder()
	requestIDEngine(t, &onContext).ServeHTTP(rec, req)

	if onContext != "caller-supplied-42" {
		t.Errorf("context id = %q, want the inbound id to be reused", onContext)
	}
	if echoed := rec.Header().Get(requestid.Header); echoed != "caller-supplied-42" {
		t.Errorf("echoed id = %q, want it echoed back unchanged", echoed)
	}
}

func TestRequestIDReplacesAnUnsafeInboundIdentifier(t *testing.T) {
	var onContext string
	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	// A header carrying a newline would let a caller forge extra log lines.
	req.Header.Set(requestid.Header, "abc\r\nlevel=error msg=forged")

	rec := httptest.NewRecorder()
	requestIDEngine(t, &onContext).ServeHTTP(rec, req)

	if len(onContext) != 32 {
		t.Errorf("context id = %q, want the unsafe value discarded for a fresh one", onContext)
	}
}

func TestRequestIDPutsALoggerOnTheContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	base := slog.New(slog.NewTextHandler(io.Discard, nil))

	var scoped *slog.Logger
	engine := gin.New()
	engine.Use(middleware.RequestID(base))
	engine.GET("/probe", func(c *gin.Context) {
		scoped = observability.LoggerFrom(c.Request.Context())
		c.Status(http.StatusOK)
	})

	engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/probe", nil))

	if scoped == nil {
		t.Fatal("no logger was placed on the request context")
	}
	if scoped == base {
		t.Error("the context logger should be derived from the base one, carrying the request id")
	}
}

// tracedEngine puts a real recording span in front of the middleware, which is
// the arrangement in production: the tracing middleware runs first so the
// correlation id has a trace to adopt.
func tracedEngine(t *testing.T, seen *string) (*gin.Engine, trace.TraceID) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	provider := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	var traceID trace.TraceID
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		ctx, span := provider.Tracer("test").Start(c.Request.Context(), "server")
		defer span.End()
		traceID = span.SpanContext().TraceID()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	engine.Use(middleware.RequestID(slog.New(slog.NewTextHandler(io.Discard, nil))))
	engine.GET("/probe", func(c *gin.Context) {
		*seen = requestid.FromContext(c.Request.Context())
		c.Status(http.StatusOK)
	})
	return engine, traceID
}

func TestRequestIDAdoptsTheTraceAsTheCorrelationID(t *testing.T) {
	var onContext string
	engine, _ := tracedEngine(t, &onContext)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/probe", nil))

	// One identifier that finds the logs and the trace beats two that each find
	// half of the story. The two are the same width for this reason.
	if onContext == "" {
		t.Fatal("no correlation id on the context")
	}
	if _, err := trace.TraceIDFromHex(onContext); err != nil {
		t.Errorf("correlation id = %q, want it to be the trace id: %v", onContext, err)
	}
	if echoed := rec.Header().Get(requestid.Header); echoed != onContext {
		t.Errorf("echoed %q but carried %q", echoed, onContext)
	}
}

func TestRequestIDStillPrefersAnInboundIdentifierOverTheTrace(t *testing.T) {
	var onContext string
	engine, traceID := tracedEngine(t, &onContext)

	const inbound = "callerssuppliedid"
	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req.Header.Set(requestid.Header, inbound)

	engine.ServeHTTP(httptest.NewRecorder(), req)

	// The caller is correlating from their side and has already written this id
	// down somewhere. Replacing it with ours would break their half.
	if onContext != inbound {
		t.Errorf("correlation id = %q, want the inbound %q", onContext, inbound)
	}
	if onContext == traceID.String() {
		t.Error("the trace id overwrote an identifier the caller supplied")
	}
}

func TestRequestIDAdoptsTheTraceStartedByTheRealInstrumentation(t *testing.T) {
	// The test above stands in for the tracing middleware with a hand-rolled
	// one, which proves the middleware reads a span but not that it will find
	// the one otelgin puts there. Whether the span lands on the request context
	// or only on the gin context is precisely the kind of difference that makes
	// this work in a test and do nothing in production.
	gin.SetMode(gin.TestMode)
	provider := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	var onContext string
	engine := gin.New()
	engine.Use(
		otelgin.Middleware("test", otelgin.WithTracerProvider(provider)),
		middleware.RequestID(slog.New(slog.NewTextHandler(io.Discard, nil))),
	)
	engine.GET("/probe", func(c *gin.Context) {
		onContext = requestid.FromContext(c.Request.Context())
		c.Status(http.StatusOK)
	})

	engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/probe", nil))

	if _, err := trace.TraceIDFromHex(onContext); err != nil {
		t.Errorf("correlation id = %q, want the trace id otelgin started: %v", onContext, err)
	}
}
