package api

import (
	"net/http"
	"runtime"
	"time"
	"github.com/gin-gonic/gin"
)

type Router struct {
	engine    *gin.Engine
	oh        *OrderHandler
	ah        *AuthHandler
	wh        *WSHandler
	authMW    gin.HandlerFunc
	startedAt time.Time
}

func NewRouter(oh *OrderHandler, ah *AuthHandler, wh *WSHandler, auth gin.HandlerFunc) *Router {
	r := &Router{engine: gin.New(), oh: oh, ah: ah, wh: wh, authMW: auth, startedAt: time.Now()}
	r.engine.Use(gin.Recovery(), LoggerMiddleware(), CORSMiddleware(), RateLimiter(100, time.Second))
	return r
}

func (r *Router) Setup() *gin.Engine {
	r.engine.GET("/health", r.health)
	r.engine.GET("/ready", r.ready)
	r.engine.GET("/metrics", r.metrics)
	api := r.engine.Group("/api/v2")
	api.GET("/ping", r.health)
	auth := api.Group("/auth")
	auth.POST("/login", r.ah.Login)
	auth.POST("/register", r.ah.Register)
	protected := api.Group("")
	protected.Use(r.authMW)
	protected.POST("/order", r.oh.PlaceOrder)
	protected.DELETE("/order/:id", r.oh.CancelOrder)
	protected.GET("/order/:id", r.oh.GetOrder)
	protected.GET("/orders", r.oh.ListOrders)
	api.GET("/orderbook/:pair", r.oh.GetOrderbook)
	api.GET("/trades/:pair", r.oh.GetTrades)
	api.GET("/ticker/:pair", r.oh.GetTicker)
	api.GET("/candles/:pair", r.oh.GetCandles)
	r.engine.GET("/ws", r.wh.HandleWebSocket)
	return r.engine
}

func (r *Router) health(c *gin.Context) { c.JSON(200, gin.H{"status":"ok","service":"nexa-api","version":"1.0.0"}) }
func (r *Router) ready(c *gin.Context) { c.JSON(200, gin.H{"status":"ready","uptime":time.Since(r.startedAt).Seconds(),"goroutines":runtime.NumGoroutine()}) }
func (r *Router) metrics(c *gin.Context) {
	var m runtime.MemStats; runtime.ReadMemStats(&m)
	c.JSON(200, gin.H{"go_version":runtime.Version(),"goroutines":runtime.NumGoroutine(),"cpu_cores":runtime.NumCPU(),"memory_alloc_mb":m.Alloc/1024/1024,"memory_sys_mb":m.Sys/1024/1024,"gc_total":m.NumGC,"uptime":time.Since(r.startedAt).Seconds()})
}
