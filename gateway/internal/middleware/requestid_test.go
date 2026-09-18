package middleware_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

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
