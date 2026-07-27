package api

import (
	"fmt"
	"runtime"
	"sync/atomic"
	"time"
	"github.com/gin-gonic/gin"
)

type Metrics struct {
	RequestsTotal, RequestsError, OrdersPlaced, OrdersFilled, OrdersCancelled, TradesExecuted atomic.Uint64
	RequestsActive, WSConnections, EngineQueueDepth atomic.Int64
	RequestsLatencyMs atomic.Int64
	startTime time.Time
}

func NewMetrics() *Metrics { return &Metrics{startTime: time.Now()} }

func (m *Metrics) Snapshot() gin.H {
	uptime := time.Since(m.startTime).Seconds()
	total := m.RequestsTotal.Load()
	var avgLat float64
	if total > 0 { avgLat = float64(m.RequestsLatencyMs.Load()) / float64(total) }
	var mem runtime.MemStats; runtime.ReadMemStats(&mem)
	return gin.H{
		"uptime_s": uptime, "requests": total, "active": m.RequestsActive.Load(),
		"errors": m.RequestsError.Load(), "avg_latency_ms": avgLat,
		"orders": m.OrdersPlaced.Load(), "trades": m.TradesExecuted.Load(),
		"ws_connections": m.WSConnections.Load(), "goroutines": runtime.NumGoroutine(),
		"memory_mb": mem.Alloc / 1024 / 1024,
	}
}

var AppMetrics = NewMetrics()

func MetricsHandler() gin.HandlerFunc {
	return func(c *gin.Context) { c.JSON(200, AppMetrics.Snapshot()) }
}

func PrometheusHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		snap := AppMetrics.Snapshot()
		out := ""
		for k, v := range snap {
			switch val := v.(type) {
			case float64: out += fmt.Sprintf("nexa_%s %f\n", k, val)
			case int64: out += fmt.Sprintf("nexa_%s %d\n", k, val)
			case uint64: out += fmt.Sprintf("nexa_%s %d\n", k, val)
			}
		}
		var m runtime.MemStats; runtime.ReadMemStats(&m)
		out += fmt.Sprintf("go_memstats_alloc_bytes %d\ngo_goroutines %d\n", m.Alloc, runtime.NumGoroutine())
		c.Data(200, "text/plain", []byte(out))
	}
}
