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
	}

	m.registry.MustRegister(
		m.requests,
		m.duration,
		m.inFlight,
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
