package observability_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/papyrus/gateway/internal/observability"
)

func metricsEngine() (*gin.Engine, *observability.Metrics) {
	gin.SetMode(gin.TestMode)
	metrics := observability.NewMetrics()

	engine := gin.New()
	engine.Use(metrics.Middleware())
	engine.GET(metrics.Path(), gin.WrapH(metrics.Handler()))
	engine.GET("/items/:id", func(c *gin.Context) { c.Status(http.StatusOK) })
	engine.POST("/analyze", func(c *gin.Context) { c.Status(http.StatusBadGateway) })
	return engine, metrics
}

func call(engine *gin.Engine, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func scrape(t *testing.T, engine *gin.Engine, metrics *observability.Metrics) string {
	t.Helper()
	rec := call(engine, http.MethodGet, metrics.Path())
	if rec.Code != http.StatusOK {
		t.Fatalf("scrape returned %d", rec.Code)
	}
	return rec.Body.String()
}

func TestMetricsLabelsByRouteTemplateNotByPath(t *testing.T) {
	engine, metrics := metricsEngine()

	call(engine, http.MethodGet, "/items/123")
	call(engine, http.MethodGet, "/items/456")

	body := scrape(t, engine, metrics)

	// One series for the template, whatever the id — this is what keeps the
	// cardinality bounded.
	if !strings.Contains(body, `http_requests_total{method="GET",route="/items/:id",status="200"} 2`) {
		t.Errorf("expected two hits on the route template; got:\n%s", body)
	}
	for _, path := range []string{`route="/items/123"`, `route="/items/456"`} {
		if strings.Contains(body, path) {
			t.Errorf("the raw path leaked into a label: %s", path)
		}
	}
}

func TestMetricsRecordStatusAndDuration(t *testing.T) {
	engine, metrics := metricsEngine()

	call(engine, http.MethodPost, "/analyze")
	body := scrape(t, engine, metrics)

	if !strings.Contains(body, `http_requests_total{method="POST",route="/analyze",status="502"} 1`) {
		t.Errorf("the upstream failure status was not recorded; got:\n%s", body)
	}
	if !strings.Contains(body, `http_request_duration_seconds_count{method="POST",route="/analyze"} 1`) {
		t.Errorf("no latency observation was recorded; got:\n%s", body)
	}
}

func TestMetricsLabelUnmatchedRoutesTogether(t *testing.T) {
	engine, metrics := metricsEngine()

	call(engine, http.MethodGet, "/nope/one")
	call(engine, http.MethodGet, "/nope/two")

	body := scrape(t, engine, metrics)
	if !strings.Contains(body, `route="unmatched"`) {
		t.Errorf("unmatched routes should share one series; got:\n%s", body)
	}
}

func TestMetricsExcludeTheScrapeEndpointItself(t *testing.T) {
	engine, metrics := metricsEngine()

	scrape(t, engine, metrics)
	body := scrape(t, engine, metrics)

	if strings.Contains(body, `route="/metrics"`) {
		t.Error("scrapes must not inflate the service's own request metrics")
	}
}

func TestMetricsCoverTheLatencyOfASlowAnalysis(t *testing.T) {
	// The default client buckets stop at 10s, which would make every real
	// analysis indistinguishable. Assert the histogram reaches past that.
	engine, metrics := metricsEngine()
	call(engine, http.MethodPost, "/analyze")

	body := scrape(t, engine, metrics)
	for _, bucket := range []string{`le="30"`, `le="45"`, `le="60"`} {
		if !strings.Contains(body, bucket) {
			t.Errorf("missing bucket %s; a 30s analysis would not be measurable", bucket)
		}
	}
}
