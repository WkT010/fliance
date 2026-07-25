package api

import (
	"net/http"
	"github.com/gin-gonic/gin"
)

type Router struct {
	engine *gin.Engine
	oh *OrderHandler
	ah *AuthHandler
	wh *WSHandler
	auth gin.HandlerFunc
}

func NewRouter(oh *OrderHandler, ah *AuthHandler, wh *WSHandler, auth gin.HandlerFunc) *Router {
	return &Router{engine: gin.Default(), oh: oh, ah: ah, wh: wh, auth: auth}
}

func (r *Router) Setup() *gin.Engine {
	api := r.engine.Group("/api/v2")
	api.GET("/ping", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	a := api.Group("/auth"); a.POST("/login", r.ah.Login); a.POST("/register", r.ah.Register)
	p := api.Group(""); p.Use(r.auth)
	p.POST("/order", r.oh.PlaceOrder)
	p.DELETE("/order/:id", r.oh.CancelOrder)
	p.GET("/order/:id", r.oh.GetOrder)
	p.GET("/orders", r.oh.ListOrders)
	api.GET("/orderbook/:pair", r.oh.GetOrderbook)
	api.GET("/trades/:pair", r.oh.GetTrades)
	api.GET("/ticker/:pair", r.oh.GetTicker)
	api.GET("/candles/:pair", r.oh.GetCandles)
	r.engine.GET("/ws", r.wh.HandleWebSocket)
	return r.engine
}