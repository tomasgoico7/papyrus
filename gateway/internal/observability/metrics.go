package observability

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// latencyBuckets span from a cache-fast reply to an analysis that nearly times
// out. The client default tops out at ten seconds, which would drop every real
// analysis into the overflow bucket and leave the p95 unusable.
var latencyBuckets = []float64{
	0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 20, 30, 45, 60,
}

// Metrics owns its own registry rather than the package-global default one, so
// a test can build an isolated instance and assert on it.
type Metrics struct {
	registry *prometheus.Registry
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	inFlight prometheus.Gauge

	cacheLookups *prometheus.CounterVec
	cacheShared  prometheus.Counter
}

func NewMetrics() *Metrics {
	m := &Metrics{
		registry: prometheus.NewRegistry(),
		requests: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "http_requests_total",
				Help: "Requests handled, by method, matched route and status code.",
			},
			[]string{"method", "route", "status"},
		),
		duration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "http_request_duration_seconds",
				Help:    "Request latency, by method and matched route.",
				Buckets: latencyBuckets,
			},
			[]string{"method", "route"},
		),
		inFlight: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "http_requests_in_flight",
				Help: "Requests currently being served.",
			},
		),
		cacheLookups: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "analysis_cache_lookups_total",
				Help: "Analysis cache lookups, by outcome: hit, miss, error or bypass.",
			},
			[]string{"result"},
		),
		cacheShared: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "analysis_cache_shared_total",
				Help: "Requests answered by joining an identical analysis already in flight.",
			},
		),
	}

	m.registry.MustRegister(
		m.requests,
		m.duration,
		m.inFlight,
		m.cacheLookups,
		m.cacheShared,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	return m
}

// Middleware records the rate, errors and duration of every handled request.
//
// The route label is the matched template, never the raw path: labelling by
// path — or by anything carrying a user id — would grow a new time series per
// distinct value and eventually take the scrape down with it.
func (m *Metrics) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		if route == metricsPath {
			c.Next()
			return
		}

		start := time.Now()
		m.inFlight.Inc()

		c.Next()

		m.inFlight.Dec()
		elapsed := time.Since(start).Seconds()

		m.duration.WithLabelValues(c.Request.Method, route).Observe(elapsed)
		m.requests.WithLabelValues(
			c.Request.Method,
			route,
			strconv.Itoa(c.Writer.Status()),
		).Inc()
	}
}

const metricsPath = "/metrics"

// Handler serves the registry in the Prometheus text format.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// Path is where the handler is expected to be mounted; the middleware uses it
// to keep scrapes out of the service's own request metrics.
func (m *Metrics) Path() string {
	return metricsPath
}

// Cache outcomes. Bypass is not a miss: it means no lookup was attempted,
// because the key could not be built.
const (
	CacheHit    = "hit"
	CacheMiss   = "miss"
	CacheError  = "error"
	CacheBypass = "bypass"
)

// RecordCacheLookup counts one analysis cache lookup. The hit rate is
// hit / (hit + miss), which is why error and bypass are kept apart from both:
// folding them into misses would make a broken cache look merely cold.
func (m *Metrics) RecordCacheLookup(result string) {
	m.cacheLookups.WithLabelValues(result).Inc()
}

// RecordCacheShared counts a request that waited on an identical analysis
// already running instead of starting its own.
func (m *Metrics) RecordCacheShared() {
	m.cacheShared.Inc()
}
