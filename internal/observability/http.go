package observability

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

var (
	httpRequestsTotal    = MustCounter("http_requests_total", "Total HTTP requests received.")
	httpRequestsInFlight = MustGauge("http_requests_in_flight", "Current in-flight HTTP requests.")
	httpRequestDuration  = MustHistogram("http_request_duration_seconds", "HTTP request latency distribution.", nil)
	httpResponseSize     = MustHistogram("http_response_size_bytes", "HTTP response size distribution.", []float64{100, 1000, 10000, 100000, 1000000})
)

// PrometheusMiddleware instruments the Gin engine with default HTTP metrics.
func PrometheusMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.URL.Path == "/metrics" {
			c.Next()
			return
		}
		start := time.Now()
		httpRequestsInFlight.Add(1)

		defer func() {
			httpRequestsInFlight.Sub(1)
			httpRequestDuration.Observe(time.Since(start).Seconds())
			httpRequestsTotal.Inc()
			httpResponseSize.Observe(float64(c.Writer.Size()))
		}()

		c.Next()
	}
}

// MetricsHandler exposes Prometheus text-format metrics.
func MetricsHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		CollectGoRuntime()
		c.Header("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		Default.WritePrometheus(c.Writer)
	}
}

// MetricsHTTPHandler is the net/http compatible handler.
func MetricsHTTPHandler(w http.ResponseWriter, r *http.Request) {
	CollectGoRuntime()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	Default.WritePrometheus(w)
}
