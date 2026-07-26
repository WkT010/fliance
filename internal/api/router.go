package api

import (
	"runtime"
	"strings"
	"time"

	"github.com/WkT010/nexa-exchange/internal/auth"
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
	authMW     gin.HandlerFunc
	apiKeyMW   gin.HandlerFunc
	startedAt  time.Time
}

// NewRouter constructs the HTTP router. apiKeyStore may be nil to disable
// API-key authentication. cfg drives CORS and rate-limit configuration.
func NewRouter(
	oh *OrderHandler,
	ah *AuthHandler,
	wh *WSHandler,
	walletH *WalletHandler,
	accountH *AccountHandler,
	authMW gin.HandlerFunc,
	apiKeyStore auth.APIKeyStore,
	cfg *config.Config,
) *Router {
	r := &Router{
		engine:    gin.New(),
		oh:        oh,
		ah:        ah,
		wh:        wh,
		walletH:   walletH,
		accountH:  accountH,
		authMW:    authMW,
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
		ErrorHandler(),
	)
	return r
}

func (r *Router) Setup() *gin.Engine {
	r.engine.GET("/health", r.health)
	r.engine.GET("/ready", r.ready)
	r.engine.GET("/metrics", r.metrics)
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
			market.GET("/depth/:pair", r.oh.GetOrderbook) // alias
		}

		// Auth (no auth required to login/register).
		authGrp := api.Group("/auth")
		{
			authGrp.POST("/login", r.ah.Login)
			authGrp.POST("/register", r.ah.Register)
			authGrp.POST("/refresh", r.ah.RefreshToken)
			authGrp.POST("/logout", r.ah.Logout)
		}

		// Protected endpoints: JWT or API key.
		// The API-key middleware runs first; if it authenticates the request
		// (sets user_id), the JWT middleware honours it and skips. Otherwise
		// the JWT middleware validates the Bearer token.
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

			// Admin endpoints (admin role enforced inside handler).
			adminGrp := protected.Group("/admin")
			{
				adminGrp.GET("/users", r.accountH.AdminListUsers)
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
	c.JSON(200, gin.H{"status": "ready", "uptime": time.Since(r.startedAt).Seconds(), "goroutines": runtime.NumGoroutine()})
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
func (r *Router) metrics(c *gin.Context) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	c.JSON(200, gin.H{
		"go_version": runtime.Version(), "goroutines": runtime.NumGoroutine(),
		"cpu_cores":     runtime.NumCPU(),
		"memory_alloc_mb": m.Alloc / 1024 / 1024, "memory_sys_mb": m.Sys / 1024 / 1024,
		"gc_total": m.NumGC, "uptime": time.Since(r.startedAt).Seconds(),
	})
}

// RequestIDMiddleware injects an X-Request-Id into the context (generating one
// if the client did not supply one) and echoes it on the response. This makes
// tracing a request across logs, downstream services and the client trivial.
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		rid := c.GetHeader("X-Request-Id")
		if rid == "" {
			rid = randomID(16)
		}
		c.Set("request_id", rid)
		c.Header("X-Request-Id", rid)
		c.Next()
	}
}

// APIKeyMiddleware authenticates requests via an X-API-Key header. If the
// header is absent the request falls through to the next middleware (which is
// typically JWT auth). If present, the key is looked up, validated against the
// supplied secret (X-API-Secret header) and the user_id/permissions are set
// on the context. This is the programmatic-trading entrypoint.
func APIKeyMiddleware(store auth.APIKeyStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		keyID := c.GetHeader("X-API-Key")
		if keyID == "" {
			c.Next()
			return
		}
		secret := c.GetHeader("X-API-Secret")
		k, err := store.Get(keyID)
		if err != nil || k == nil || !k.Active {
			c.AbortWithStatusJSON(401, gin.H{"error": "invalid api key"})
			return
		}
		if !k.ExpiresAt.IsZero() && time.Now().After(k.ExpiresAt) {
			c.AbortWithStatusJSON(401, gin.H{"error": "api key expired"})
			return
		}
		if !k.Validate(secret) {
			c.AbortWithStatusJSON(401, gin.H{"error": "invalid api key secret"})
			return
		}
		c.Set("user_id", k.UserID)
		c.Set("api_key_id", k.KeyID)
		c.Set("permissions", k.Permissions)
		// API keys have user role by default; admin keys would carry an
		// "admin" permission.
		role := "user"
		for _, p := range k.Permissions {
			if p == "admin" {
				role = "admin"
				break
			}
		}
		c.Set("role", role)
		c.Next()
	}
}

// CORSMiddlewareConfig returns a CORS middleware driven by configuration. When
// origins contains "*" credentials are forced off (browsers reject credentialed
// wildcard CORS). Otherwise the request Origin is matched against the allowlist
// and echoed back.
func CORSMiddlewareConfig(origins []string, allowCreds bool) gin.HandlerFunc {
	allowAll := false
	allowSet := make(map[string]bool, len(origins))
	for _, o := range origins {
		o = strings.TrimSpace(o)
		if o == "*" {
			allowAll = true
		}
		allowSet[o] = true
	}
	if allowAll {
		allowCreds = false // browsers reject credentialed wildcard CORS
	}
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		switch {
		case allowAll:
			c.Header("Access-Control-Allow-Origin", "*")
		case origin != "" && allowSet[origin]:
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
		}
		if allowCreds {
			c.Header("Access-Control-Allow-Credentials", "true")
		}
		c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin,Content-Type,Accept,Authorization,X-API-Key,X-API-Secret,X-Request-Id")
		c.Header("Access-Control-Expose-Headers", "X-Request-Id")
		c.Header("Access-Control-Max-Age", "86400")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	}
}
