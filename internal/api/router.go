package api

import (
	"net/http"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"
)

type Router struct {
	engine         *gin.Engine
	orderHandler   *OrderHandler
	authHandler    *AuthHandler
	wsHandler      *WSHandler
	authMiddleware gin.HandlerFunc
	startedAt      time.Time
}

func NewRouter(orderHandler *OrderHandler, authHandler *AuthHandler, wsHandler *WSHandler, authMiddleware gin.HandlerFunc) *Router {
	return &Router{
		engine:         gin.New(),
		orderHandler:   orderHandler,
		authHandler:    authHandler,
		wsHandler:      wsHandler,
		authMiddleware: authMiddleware,
		startedAt:      time.Now(),
	}
}

func (r *Router) Setup() *gin.Engine {
	r.engine.Use(gin.Recovery())

	// Health / Readiness / Metrics (no auth, for k8s probes)
	r.engine.GET("/health", r.healthHandler)
	r.engine.GET("/ready", r.readyHandler)
	r.engine.GET("/metrics", r.metricsHandler)

	api := r.engine.Group("/api/v2")
	api.GET("/ping", r.healthHandler)

	auth := api.Group("/auth")
	auth.POST("/login", r.authHandler.Login)
	auth.POST("/register", r.authHandler.Register)

	protected := api.Group("")
	protected.Use(r.authMiddleware)
	protected.POST("/order", r.orderHandler.PlaceOrder)
	protected.DELETE("/order/:id", r.orderHandler.CancelOrder)
	protected.GET("/order/:id", r.orderHandler.GetOrder)
	protected.GET("/orders", r.orderHandler.ListOrders)

	api.GET("/orderbook/:pair", r.orderHandler.GetOrderbook)
	api.GET("/trades/:pair", r.orderHandler.GetTrades)
	api.GET("/ticker/:pair", r.orderHandler.GetTicker)
	api.GET("/candles/:pair", r.orderHandler.GetCandles)
	r.engine.GET("/ws", r.wsHandler.HandleWebSocket)

	return r.engine
}

func (r *Router) healthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "ok", "service": "nexa-api-gateway",
		"version": "1.0.0", "timestamp": time.Now().Unix(),
	})
}

func (r *Router) readyHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "ready", "uptime": time.Since(r.startedAt).Seconds(),
		"goroutines": runtime.NumGoroutine(),
	})
}

func (r *Router) metricsHandler(c *gin.Context) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	c.JSON(http.StatusOK, gin.H{
		"go_version": runtime.Version(),
		"goroutines": runtime.NumGoroutine(),
		"cpu_cores":  runtime.NumCPU(),
		"memory_alloc_mb": m.Alloc / 1024 / 1024,
		"memory_sys_mb":   m.Sys / 1024 / 1024,
		"gc_total":   m.NumGC,
		"uptime_seconds": time.Since(r.startedAt).Seconds(),
	})
}
