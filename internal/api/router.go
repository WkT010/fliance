package api

import (
	"net/http"
	"runtime"
	"time"
	"github.com/gin-gonic/gin"
)

type Router struct {
	engine, oh, ah, wh, ph, authMW interface{}
	startedAt time.Time
}

func NewRouter(oh *OrderHandler, ah *AuthHandler, wh *WSHandler, ph *PriceHandler, auth gin.HandlerFunc) *Router {
	r := &Router{engine: gin.New(), startedAt: time.Now()}
	r.engine.Use(ErrorHandler(), LoggerMiddleware(), CORSMiddleware(), RateLimiter(100, time.Second))
	return r
}

func (r *Router) Setup() *gin.Engine {
	r.engine.GET("/health", r.health)
	r.engine.GET("/ready", r.ready)
	r.engine.GET("/metrics", r.metrics)

	g := r.engine.Group("/api/v2")
	g.GET("/ping", r.health)

	a := g.Group("/auth")
	a.POST("/login", r.ah.Login)
	a.POST("/register", r.ah.Register)

	p := g.Group("")
	p.Use(r.authMW)
	p.POST("/order", r.oh.PlaceOrder)
	p.DELETE("/order/:id", r.oh.CancelOrder)
	p.GET("/order/:id", r.oh.GetOrder)
	p.GET("/orders", r.oh.ListOrders)

	// Real market data from Binance
	g.GET("/ticker/:pair", r.ph.GetTicker)
	g.GET("/tickers", r.ph.GetAllTickers)
	g.GET("/orderbook/:pair", r.oh.GetOrderbook)
	g.GET("/trades/:pair", r.oh.GetTrades)

	r.engine.GET("/ws", r.wh.HandleWebSocket)
	return r.engine
}

func (r *Router) health(c *gin.Context) { c.JSON(200, gin.H{"status":"ok","service":"nexa"}) }
func (r *Router) ready(c *gin.Context) { c.JSON(200, gin.H{"status":"ready","uptime":time.Since(r.startedAt).Seconds(),"goroutines":runtime.NumGoroutine()}) }
func (r *Router) metrics(c *gin.Context) {
	var m runtime.MemStats; runtime.ReadMemStats(&m)
	c.JSON(200, gin.H{"go_version":runtime.Version(),"goroutines":runtime.NumGoroutine(),"memory_mb":m.Alloc/1024/1024,"uptime_s":time.Since(r.startedAt).Seconds()})
}
