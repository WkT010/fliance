package api

import (
	"runtime"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/WkT010/nexa-exchange/internal/auth"
	"github.com/WkT010/nexa-exchange/internal/config"
	"github.com/WkT010/nexa-exchange/internal/observability"
)

// Router wires all HTTP handlers and middleware for the API gateway.
type Router struct {
	engine    *gin.Engine
	oh        *OrderHandler
	ah        *AuthHandler
	wh        *WSHandler
	ph        *PriceHandler
	walletH   *WalletHandler
	accountH  *AccountHandler
	adminH    *AdminHandler
	authMW    gin.HandlerFunc
	apiKeyMW  gin.HandlerFunc
	health    *observability.HealthCollector
	startedAt time.Time
}

// NewRouter constructs a router. apiKeyStore may be nil to disable API-key auth.
func NewRouter(
	oh *OrderHandler,
	ah *AuthHandler,
	wh *WSHandler,
	ph *PriceHandler,
	walletH *WalletHandler,
	accountH *AccountHandler,
	adminH *AdminHandler,
	authMW gin.HandlerFunc,
	apiKeyStore auth.APIKeyStore,
	cfg *config.Config,
	health *observability.HealthCollector,
) *Router {
	r := &Router{
		engine:    gin.New(),
		oh:        oh,
		ah:        ah,
		wh:        wh,
		ph:        ph,
		walletH:   walletH,
		accountH:  accountH,
		adminH:    adminH,
		authMW:    authMW,
		health:    health,
		startedAt: time.Now(),
	}
	if apiKeyStore != nil && cfg.EnableAPIKeyAuth {
		r.apiKeyMW = APIKeyMiddleware(apiKeyStore)
	}

	cors := CORSMiddlewareConfig(cfg.CORSAllowOrigins, cfg.CORSAllowCreds)
	rateLimit := RateLimiter(cfg.RateLimitPerSec, time.Second)
	r.engine.Use(
		gin.Recovery(),
		RequestIDMiddleware(),
		LoggerMiddleware(),
		cors,
		rateLimit,
		observability.PrometheusMiddleware(),
		ErrorHandler(),
	)
	return r
}

// Setup registers all routes and returns the configured Gin engine.
func (r *Router) Setup() *gin.Engine {
	r.engine.GET("/health", r.health.Handler())
	r.engine.GET("/ready", r.health.ReadyHandler())
	r.engine.GET("/metrics", observability.MetricsHandler())

	api := r.engine.Group("/api/v2")
	api.GET("/ping", r.health.Handler())
	api.GET("/time", func(c *gin.Context) {
		c.JSON(200, gin.H{"server_time": time.Now().UTC().UnixNano() / 1e6})
	})

	// Public auth endpoints.
	auth := api.Group("/auth")
	auth.POST("/login", r.ah.Login)
	auth.POST("/register", r.ah.Register)
	auth.POST("/refresh", r.ah.RefreshToken)
	auth.POST("/logout", r.authMW, r.ah.Logout)

	// Authenticated routes.
	p := api.Group("")
	p.Use(r.authMW)
	if r.apiKeyMW != nil {
		p.Use(r.apiKeyMW)
	}

	// Trading.
	p.POST("/order", r.oh.PlaceOrder)
	p.DELETE("/order/:id", r.oh.CancelOrder)
	p.DELETE("/orders", r.oh.CancelAllOrders)
	p.GET("/order/:id", r.oh.GetOrder)
	p.GET("/orders", r.oh.ListOrders)

	// Wallet.
	p.GET("/wallet/balances", r.walletH.GetBalances)
	p.GET("/wallet/balances/:asset", r.walletH.GetBalance)
	p.POST("/wallet/deposit/address", r.walletH.GetDepositAddress)
	p.POST("/wallet/deposit", r.walletH.Deposit)
	p.POST("/wallet/withdraw", r.walletH.Withdraw)
	p.GET("/wallet/transactions", r.walletH.ListTransactions)
	p.GET("/wallet/assets", r.walletH.ListSupportedAssets)

	// Account.
	p.GET("/account", r.accountH.GetAccount)
	p.GET("/account/profile", r.accountH.GetProfile)
	p.GET("/account/pnl", r.accountH.GetPnL)
	p.POST("/account/api-keys", r.accountH.CreateAPIKey)
	p.GET("/account/api-keys", r.accountH.ListAPIKeys)
	p.DELETE("/account/api-keys/:id", r.accountH.RevokeAPIKey)

	// Market data (public).
	api.GET("/ticker/:pair", r.ph.GetTicker)
	api.GET("/tickers", r.ph.GetAllTickers)
	api.GET("/ticker/24h/:pair", r.oh.GetTicker24h)
	api.GET("/tickers/24h", r.oh.ListTickers)
	api.GET("/orderbook/:pair", r.oh.GetOrderbook)
	api.GET("/trades/:pair", r.oh.GetTrades)
	api.GET("/klines/:pair", r.oh.GetCandles)
	api.GET("/klines/intervals", r.oh.ListCandleIntervals)
	api.GET("/price/uniswap/:pair", r.ph.GetUniswapTicker)
	api.GET("/price/compare/:pair", r.ph.GetPriceComparison)

	// Admin routes.
	admin := api.Group("/admin")
	admin.Use(r.authMW, AdminOnly())
	admin.GET("/users", r.accountH.AdminListUsers)

	// Withdrawal operations.
	admin.GET("/withdrawals", r.adminH.ListWithdrawals)
	admin.POST("/withdrawals/:id/approve", r.adminH.ApproveWithdrawal)
	admin.POST("/withdrawals/:id/reject", r.adminH.RejectWithdrawal)

	// User withdrawal controls.
	admin.GET("/users/:id/withdrawals", r.adminH.ListUserWithdrawals)
	admin.GET("/users/:id/addresses", r.adminH.ListAddresses)
	admin.POST("/users/:id/addresses", r.adminH.AddAddress)
	admin.POST("/users/:id/limits", r.adminH.SetDailyLimit)

	// Risk management.
	admin.GET("/risk/pairs", r.adminH.ListPairRisk)
	admin.GET("/risk/pairs/:pair", r.adminH.GetPairRisk)
	admin.PUT("/risk/pairs/:pair", r.adminH.UpdatePairRisk)
	admin.GET("/risk/users/:id", r.adminH.GetUserRisk)
	admin.PUT("/risk/users/:id", r.adminH.UpdateUserRisk)

	// Trading controls.
	admin.POST("/pairs/:pair/pause", r.adminH.PausePair)
	admin.POST("/pairs/:pair/resume", r.adminH.ResumePair)

	// Operational.
	admin.POST("/snapshots", r.adminH.TriggerSnapshot)

	// WebSocket.
	r.engine.GET("/ws", r.wh.HandleWebSocket)

	return r.engine
}

func (r *Router) healthHandler(c *gin.Context) {
	c.JSON(200, gin.H{"status": "ok", "service": "nexa"})
}

func (r *Router) readyHandler(c *gin.Context) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	c.JSON(200, gin.H{
		"status":     "ready",
		"uptime":     time.Since(r.startedAt).Seconds(),
		"goroutines": runtime.NumGoroutine(),
		"memory_mb":  m.Alloc / 1024 / 1024,
	})
}
