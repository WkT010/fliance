package api

import (
	"strings"
	"time"

	"github.com/WkT010/nexa-exchange/internal/auth"
	"github.com/WkT010/nexa-exchange/internal/cache"
	"github.com/WkT010/nexa-exchange/internal/config"
	"github.com/gin-gonic/gin"
)

type Router struct {
	engine     *gin.Engine
	oh         *OrderHandler
	ah         *AuthHandler
	wh         *WSHandler
	walletH    *WalletHandler
	accountH   *AccountHandler
	legalH     *LegalHandler
	feeH       *FeeHandler
	authMW     gin.HandlerFunc
	apiKeyMW   gin.HandlerFunc
	startedAt  time.Time
}

// NewRouter constructs the HTTP router.
func NewRouter(
	oh *OrderHandler,
	ah *AuthHandler,
	wh *WSHandler,
	walletH *WalletHandler,
	accountH *AccountHandler,
	legalH *LegalHandler,
	feeH *FeeHandler,
	authMW gin.HandlerFunc,
	apiKeyStore auth.APIKeyStore,
	redisCache *cache.RedisCache,
	cfg *config.Config,
) *Router {
	r := &Router{
		engine:    gin.New(),
		oh:        oh,
		ah:        ah,
		wh:        wh,
		walletH:   walletH,
		accountH:  accountH,
		legalH:    legalH,
		feeH:      feeH,
		authMW:    authMW,
		startedAt: time.Now(),
	}
	if apiKeyStore != nil && cfg.EnableAPIKeyAuth {
		r.apiKeyMW = APIKeyMiddleware(apiKeyStore)
	}

	// Choose rate limiter: Redis-backed for multi-instance, in-memory for single.
	var rateLimit gin.HandlerFunc
	if redisCache != nil && cfg.EnableRedisRateLimit {
		rateLimit = RedisRateLimiter(redisCache, cfg.RateLimitPerSec, time.Second)
	} else {
		rateLimit = RateLimiter(cfg.RateLimitPerSec, time.Second)
	}

	cors := CORSMiddlewareConfig(cfg.CORSAllowOrigins, cfg.CORSAllowCreds)
	r.engine.Use(
		gin.Recovery(),
		RequestIDMiddleware(),
		LoggerMiddleware(),
		MetricsMiddleware(),
		cors,
		rateLimit,
		ErrorHandler(),
	)
	return r
}

func (r *Router) Setup() *gin.Engine {
	r.engine.GET("/health", r.health)
	r.engine.GET("/ready", r.ready)

	// Prometheus-compatible metrics (replaces basic JSON metrics).
	r.engine.GET("/metrics", globalMetrics.PrometheusHandler())

	// Legal pages (public, no auth).
	legal := r.engine.Group("/legal")
	{
		legal.GET("/terms", r.legalH.Terms)
		legal.GET("/privacy", r.legalH.Privacy)
		legal.GET("/risks", r.legalH.Risks)
		legal.GET("/aml", r.legalH.AML)
		legal.GET("/cookies", r.legalH.Cookies)
	}

	api := r.engine.Group("/api/v2")
	{
		api.GET("/ping", r.ping)

		// Public market data (no auth).
		market := api.Group("/market")
		{
			market.GET("/pairs", r.listPairs)
			market.GET("/tickers", r.oh.ListTickers)
			market.GET("/ticker/:pair", r.oh.GetTicker)
			market.GET("/ticker/24h/:pair", r.oh.GetTicker24h)
			market.GET("/orderbook/:pair", r.oh.GetOrderbook)
			market.GET("/trades/:pair", r.oh.GetTrades)
			market.GET("/candles/:pair", r.oh.GetCandles)
			market.GET("/candle_intervals", r.oh.ListCandleIntervals)
			market.GET("/depth/:pair", r.oh.GetOrderbook)
		}

		// Public fee info (no auth).
		fees := api.Group("/fees")
		{
			fees.GET("", r.feeH.GetSchedule)
			fees.GET("/:pair", r.feeH.GetPairFee)
			fees.GET("/calculate", r.feeH.CalculateFee)
		}

		// Auth (no auth required).
		authGrp := api.Group("/auth")
		{
			authGrp.POST("/login", r.ah.Login)
			authGrp.POST("/register", r.ah.Register)
			authGrp.POST("/refresh", r.ah.RefreshToken)
			authGrp.POST("/logout", r.ah.Logout)
		}

		// Protected endpoints: JWT or API key.
		protected := api.Group("")
		if r.apiKeyMW != nil {
			protected.Use(r.apiKeyMW)
		}
		protected.Use(r.authMW)
		{
			protected.POST("/order", r.oh.PlaceOrder)
			protected.DELETE("/order/:id", r.oh.CancelOrder)
			protected.DELETE("/orders", r.oh.CancelAllOrders)
			protected.GET("/order/:id", r.oh.GetOrder)
			protected.GET("/orders", r.oh.ListOrders)

			// Wallet endpoints.
			walletGrp := protected.Group("/wallet")
			{
				walletGrp.GET("/balances", r.walletH.GetBalances)
				walletGrp.GET("/balances/:asset", r.walletH.GetBalance)
				walletGrp.POST("/deposit/address", r.walletH.GetDepositAddress)
				walletGrp.POST("/deposit", r.walletH.Deposit)
				walletGrp.POST("/withdraw", r.walletH.Withdraw)
				walletGrp.GET("/transactions", r.walletH.ListTransactions)
				walletGrp.GET("/assets", r.walletH.ListSupportedAssets)
			}

			// Account endpoints.
			acctGrp := protected.Group("/account")
			{
				acctGrp.GET("", r.accountH.GetAccount)
				acctGrp.GET("/profile", r.accountH.GetProfile)
				acctGrp.POST("/api-keys", r.accountH.CreateAPIKey)
				acctGrp.GET("/api-keys", r.accountH.ListAPIKeys)
				acctGrp.DELETE("/api-keys/:id", r.accountH.RevokeAPIKey)
			}

			// Admin endpoints.
			adminGrp := protected.Group("/admin")
			{
				adminGrp.GET("/users", r.accountH.AdminListUsers)
				adminGrp.PUT("/fees", r.feeH.UpdateFee)
			}
		}
	}
	r.engine.GET("/ws", r.wh.HandleWebSocket)
	return r.engine
}

func (r *Router) health(c *gin.Context) {
	c.JSON(200, gin.H{"status": "ok", "service": "nexa-api", "version": "3.0.0"})
}
func (r *Router) ready(c *gin.Context) {
	c.JSON(200, gin.H{"status": "ready", "uptime": time.Since(r.startedAt).Seconds()})
}
func (r *Router) ping(c *gin.Context) {
	c.JSON(200, gin.H{"message": "pong", "timestamp": time.Now().UnixNano()})
}
func (r *Router) listPairs(c *gin.Context) {
	pairs := make([]string, 0, len(r.oh.engines))
	for p := range r.oh.engines {
		pairs = append(pairs, p)
	}
	c.JSON(200, gin.H{"pairs": pairs})
}

// RequestIDMiddleware injects an X-Request-Id into the context.
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		rid := c.GetHeader("X-Request-Id")
		if rid == "" { rid = randomID(16) }
		c.Set("request_id", rid)
		c.Header("X-Request-Id", rid)
		c.Next()
	}
}

// APIKeyMiddleware authenticates via X-API-Key header.
func APIKeyMiddleware(store auth.APIKeyStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		keyID := c.GetHeader("X-API-Key")
		if keyID == "" { c.Next(); return }
		secret := c.GetHeader("X-API-Secret")
		k, err := store.Get(keyID)
		if err != nil || k == nil || !k.Active {
			c.AbortWithStatusJSON(401, gin.H{"error": "invalid api key"}); return
		}
		if !k.ExpiresAt.IsZero() && time.Now().After(k.ExpiresAt) {
			c.AbortWithStatusJSON(401, gin.H{"error": "api key expired"}); return
		}
		if !k.Validate(secret) {
			c.AbortWithStatusJSON(401, gin.H{"error": "invalid api key secret"}); return
		}
		c.Set("user_id", k.UserID)
		c.Set("api_key_id", k.KeyID)
		c.Set("permissions", k.Permissions)
		role := "user"
		for _, p := range k.Permissions { if p == "admin" { role = "admin"; break } }
		c.Set("role", role)
		c.Next()
	}
}

// CORSMiddlewareConfig returns a CORS middleware driven by configuration.
func CORSMiddlewareConfig(origins []string, allowCreds bool) gin.HandlerFunc {
	allowAll := false
	allowSet := make(map[string]bool, len(origins))
	for _, o := range origins {
		o = strings.TrimSpace(o)
		if o == "*" { allowAll = true }
		allowSet[o] = true
	}
	if allowAll { allowCreds = false }
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		switch {
		case allowAll:
			c.Header("Access-Control-Allow-Origin", "*")
		case origin != "" && allowSet[origin]:
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
		}
		if allowCreds { c.Header("Access-Control-Allow-Credentials", "true") }
		c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin,Content-Type,Accept,Authorization,X-API-Key,X-API-Secret,X-Request-Id")
		c.Header("Access-Control-Expose-Headers", "X-Request-Id")
		c.Header("Access-Control-Max-Age", "86400")
		if c.Request.Method == "OPTIONS" { c.AbortWithStatus(204); return }
		c.Next()
	}
}