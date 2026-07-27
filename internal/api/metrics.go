package api

import (
	"fmt"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// MetricsCollector provides lightweight Prometheus-compatible metrics without
// external dependencies. Metrics are served at GET /metrics in Prometheus text
// format so they can be scraped by any standard Prometheus server.
type MetricsCollector struct {
	mu          sync.RWMutex
	startedAt   time.Time

	// Counters
	ordersPlaced     int64
	ordersFilled     int64
	ordersCancelled  int64
	httpRequests     int64
	httpErrors       int64
	wsConnections    int64

	// Histogram-style buckets (latency in ms)
	latencyBuckets   map[string]*latencyHist
}

type latencyHist struct {
	mu       sync.Mutex
	buckets  map[float64]int64
	total    float64
	count    int64
}

func newLatencyHist() *latencyHist {
	return &latencyHist{
		buckets: map[float64]int64{5: 0, 10: 0, 25: 0, 50: 0, 100: 0, 250: 0, 500: 0, 1000: 0, 5000: 0},
	}
}

func (h *latencyHist) observe(ms float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.total += ms
	h.count++
	for b := range h.buckets {
		if ms <= b {
			h.buckets[b]++
		}
	}
}

var globalMetrics = NewMetricsCollector()

func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		startedAt:      time.Now(),
		latencyBuckets: make(map[string]*latencyHist),
	}
}

// RecordOrder increments order counter.
func (m *MetricsCollector) RecordOrder() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ordersPlaced++
}

// RecordFill increments filled order counter.
func (m *MetricsCollector) RecordFill() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ordersFilled++
}

// RecordCancel increments cancelled order counter.
func (m *MetricsCollector) RecordCancel() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ordersCancelled++
}

// RecordRequest increments the HTTP request counter.
func (m *MetricsCollector) RecordRequest() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.httpRequests++
}

// RecordError increments the HTTP error counter.
func (m *MetricsCollector) RecordError() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.httpErrors++
}

// RecordLatency records request latency for a given path.
func (m *MetricsCollector) RecordLatency(path string, ms float64) {
	m.mu.Lock()
	h, ok := m.latencyBuckets[path]
	if !ok {
		h = newLatencyHist()
		m.latencyBuckets[path] = h
	}
	m.mu.Unlock()
	h.observe(ms)
}

// RecordWSConnection records WebSocket connection count.
func (m *MetricsCollector) RecordWSConnection(delta int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.wsConnections += int64(delta)
}

// PrometheusHandler returns a gin handler that outputs metrics in Prometheus
// text exposition format (https://prometheus.io/docs/instrumenting/exposition_formats/).
func (m *MetricsCollector) PrometheusHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		m.mu.RLock()
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)

		uptime := time.Since(m.startedAt).Seconds()

		out := ""
		out += "# HELP nexa_uptime_seconds Service uptime\n"
		out += "# TYPE nexa_uptime_seconds gauge\n"
		out += fmt.Sprintf("nexa_uptime_seconds %.0f\n", uptime)

		out += "# HELP nexa_goroutines Current number of goroutines\n"
		out += "# TYPE nexa_goroutines gauge\n"
		out += fmt.Sprintf("nexa_goroutines %d\n", runtime.NumGoroutine())

		out += "# HELP nexa_memory_alloc_bytes Current memory allocation\n"
		out += "# TYPE nexa_memory_alloc_bytes gauge\n"
		out += fmt.Sprintf("nexa_memory_alloc_bytes %d\n", mem.Alloc)

		out += "# HELP nexa_orders_placed_total Total orders placed\n"
		out += "# TYPE nexa_orders_placed_total counter\n"
		out += fmt.Sprintf("nexa_orders_placed_total %d\n", m.ordersPlaced)

		out += "# HELP nexa_orders_filled_total Total orders filled\n"
		out += "# TYPE nexa_orders_filled_total counter\n"
		out += fmt.Sprintf("nexa_orders_filled_total %d\n", m.ordersFilled)

		out += "# HELP nexa_orders_cancelled_total Total orders cancelled\n"
		out += "# TYPE nexa_orders_cancelled_total counter\n"
		out += fmt.Sprintf("nexa_orders_cancelled_total %d\n", m.ordersCancelled)

		out += "# HELP nexa_http_requests_total Total HTTP requests\n"
		out += "# TYPE nexa_http_requests_total counter\n"
		out += fmt.Sprintf("nexa_http_requests_total %d\n", m.httpRequests)

		out += "# HELP nexa_http_errors_total Total HTTP errors\n"
		out += "# TYPE nexa_http_errors_total counter\n"
		out += fmt.Sprintf("nexa_http_errors_total %d\n", m.httpErrors)

		out += "# HELP nexa_ws_connections_current Current WebSocket connections\n"
		out += "# TYPE nexa_ws_connections_current gauge\n"
		out += fmt.Sprintf("nexa_ws_connections_current %d\n", m.wsConnections)
		m.mu.RUnlock()

		// Per-path latency histogram
		m.mu.RLock()
		for path, h := range m.latencyBuckets {
			h.mu.Lock()
			avg := float64(0)
			if h.count > 0 {
				avg = h.total / float64(h.count)
			}
			// le buckets
			for b, count := range h.buckets {
				out += fmt.Sprintf("nexa_http_request_duration_ms_bucket{path=\"%s\",le=\"%.0f\"} %d\n", path, b, count)
			}
			out += fmt.Sprintf("nexa_http_request_duration_ms_count{path=\"%s\"} %d\n", path, h.count)
			out += fmt.Sprintf("nexa_http_request_duration_ms_sum{path=\"%s\"} %.2f\n", path, h.total)
			out += fmt.Sprintf("nexa_http_request_duration_ms_avg{path=\"%s\"} %.2f\n", path, avg)
			h.mu.Unlock()
		}
		m.mu.RUnlock()

		out += "# HELP nexa_build_info Build information\n"
		out += "# TYPE nexa_build_info gauge\n"
		out += fmt.Sprintf("nexa_build_info{version=\"3.0.0\",go=\"%s\"} 1\n", runtime.Version())

		c.String(http.StatusOK, out)
	}
}