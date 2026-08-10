package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/WkT010/nexa-exchange/internal/cache"
	"github.com/WkT010/nexa-exchange/internal/config"
	"github.com/WkT010/nexa-exchange/internal/observability"
	"github.com/gin-gonic/gin"
)

type Router struct {
	engine    *gin.Engine
	config    *config.Config
	oh        *OrderHandler
	authH     *AuthHandler
	wh        *WSHandler
	ph        *PriceHandler
	walletH   *WalletHandler
	accountH  *AccountHandler
	adminH    *AdminHandler
	futuresH  *FuturesHandler
	ammH      *AmmHandler
	authMW    gin.HandlerFunc
	apiKeyMW  gin.HandlerFunc
	rateMW    gin.HandlerFunc
	health    *observability.HealthCollector
	startedAt time.Time
	staticDir string
	cache     cache.Cache
}

func NewRouter(
	oh *OrderHandler,
	authH *AuthHandler,
	wh *WSHandler,
	ph *PriceHandler,
	walletH *WalletHandler,
	accountH *AccountHandler,
	adminH *AdminHandler,
	futuresH *FuturesHandler,
	ammH *AmmHandler,
	authMW gin.HandlerFunc,
	apiKeyMW gin.HandlerFunc,
	cfg *config.Config,
	health *observability.HealthCollector,
) *Router {
	r := &Router{
		engine:    gin.New(),
		config:    cfg,
		oh:        oh,
		authH:     authH,
		wh:        wh,
		ph:        ph,
		walletH:   walletH,
		accountH:  accountH,
		adminH:    adminH,
		futuresH:  futuresH,
		ammH:      ammH,
		authMW:    authMW,
		apiKeyMW:  apiKeyMW,
		health:    health,
		startedAt: time.Now(),
	}
	return r
}

func (r *Router) SetStaticDir(dir string) { r.staticDir = dir }

// SetRateLimiter wires the global per-IP HTTP rate limiter. Callers choose
// between the in-memory RateLimiter and the distributed RedisRateLimiter
// based on cfg.EnableRedisRateLimit; without it the gateway runs unlimited
// apart from the WS-specific limiter.
func (r *Router) SetRateLimiter(mw gin.HandlerFunc) { r.rateMW = mw }

// SetCache wires the shared cache abstraction used by the WebSocket
// connection rate limiter. Optional: without it the limiter degrades to
// in-memory per-instance counting.
func (r *Router) SetCache(cc cache.Cache) { r.cache = cc }

func (r *Router) Setup() *gin.Engine {
	// Panic recovery first so a bug in any handler/middleware turns into a
	// 500 instead of killing the whole gateway process.
	r.engine.Use(gin.Recovery())

	// Global per-IP request rate limit (wired by main; Redis-backed or
	// in-memory depending on cfg.EnableRedisRateLimit).
	if r.rateMW != nil {
		r.engine.Use(r.rateMW)
	}

	// CORS: strict allow-list matching. Outside development the wildcard is
	// rejected at config load time; the same OriginChecker is shared with the
	// WebSocket upgrader so HTTP and WS enforce one identical policy.
	originChecker := NewOriginChecker(r.config.CORSAllowOrigins)
	r.engine.Use(CORSMiddlewareConfig(r.config.CORSAllowOrigins, r.config.CORSAllowCreds))

	// Global request-body cap (default 1 MB); WebSocket upgrades exempt.
	r.engine.Use(RequestBodyLimit(int64(r.config.MaxRequestBodyBytes)))

	// Wire the WS hardening knobs that live outside the constructors.
	r.wh.SetOriginChecker(originChecker)
	r.wh.SetTokenBlacklist(r.authH.TokenBlacklist())
	r.wh.SetMaxConnections(r.config.WSMaxConnections)

	r.engine.GET("/health", r.health.Handler())
	r.engine.GET("/ready", r.health.ReadyHandler())
	r.engine.GET("/metrics", observability.MetricsHandler())

	api := r.engine.Group("/api/v2")
	api.GET("/ping", r.health.Handler())
	api.GET("/time", func(c *gin.Context) {
		c.JSON(200, gin.H{"time": time.Now().UnixNano()})
	})

	auth := api.Group("/auth")
	auth.POST("/login", r.authH.Login)
	auth.POST("/register", r.authH.Register)
	auth.POST("/refresh", r.authH.RefreshToken)
	auth.POST("/logout", r.authMW, r.authH.Logout)
	auth.POST("/change-password", r.authMW, r.authH.ChangePassword)

	prot := api.Group("", r.authMW)
	prot.POST("/order", r.oh.PlaceOrder)
	prot.DELETE("/order/:id", r.oh.CancelOrder)
	prot.DELETE("/orders", r.oh.CancelAllOrders)
	prot.GET("/order/:id", r.oh.GetOrder)
	prot.GET("/orders", r.oh.ListOrders)
	prot.GET("/wallet/balances", r.walletH.GetBalances)
	prot.GET("/wallet/balances/:asset", r.walletH.GetBalance)
	prot.POST("/wallet/deposit/address", r.walletH.GetDepositAddress)
	// NOTE: POST /wallet/deposit credits balances and is admin-only — it was
	// moved out of the authenticated user group (privilege escalation). The
	// legacy path stays registered with 410 Gone + a migration note so old
	// clients get an actionable response instead of a 404.
	api.POST("/wallet/deposit", r.walletH.DepositGone)
	prot.POST("/wallet/withdraw", r.walletH.Withdraw)
	prot.GET("/wallet/transactions", r.walletH.ListTransactions)
	prot.GET("/wallet/assets", r.walletH.ListSupportedAssets)

	prot.GET("/account", r.accountH.GetAccount)
	prot.GET("/account/profile", r.accountH.GetProfile)
	prot.GET("/account/pnl", r.accountH.GetPnL)
	prot.GET("/account/pnl/history", r.accountH.GetPnLHistory)
	prot.POST("/account/api-keys", r.accountH.CreateAPIKey)
	prot.GET("/account/api-keys", r.accountH.ListAPIKeys)
	prot.DELETE("/account/api-keys/:id", r.accountH.RevokeAPIKey)

	// Futures trading (real prices, real wallet collateral)
	api.GET("/futures/mark-price/*pair", r.futuresH.GetMarkPrice)
	prot.GET("/futures/funding-history/*pair", r.futuresH.GetFundingHistory)
	prot.GET("/futures/account/summary", r.futuresH.AccountSummary)
	prot.GET("/futures/positions", r.futuresH.GetPositions)
	prot.POST("/futures/positions", r.futuresH.OpenPosition)
	prot.POST("/futures/positions/:id/close", r.futuresH.ClosePosition)
	prot.POST("/futures/positions/:id/close-partial", r.futuresH.ClosePositionPartial)
	prot.POST("/futures/positions/:id/add-margin", r.futuresH.AddMargin)
	prot.POST("/futures/positions/:id/reduce-margin", r.futuresH.ReduceMargin)
	prot.POST("/futures/positions/:id/liquidate", r.futuresH.LiquidatePosition)
	prot.GET("/futures/orders", r.futuresH.ListOrders)
	prot.POST("/futures/orders", r.futuresH.CreateOrder)
	prot.DELETE("/futures/orders/:id", r.futuresH.CancelOrder)

	// Market data — all use *pair catch-all to support BTC/USDT format
	api.GET("/tickers/24h", r.ph.GetAllTickers)
	api.GET("/tickers", r.ph.GetAllTickers)
	api.GET("/ticker/*pair", r.ph.GetTicker)
	api.GET("/orderbook/*pair", r.oh.GetOrderbook)
	api.GET("/trades/*pair", r.oh.GetTrades)
	api.GET("/klines/*pair", r.oh.GetCandles)
	api.GET("/price/uniswap/*pair", r.ph.GetUniswapTicker)
	api.GET("/price/compare/*pair", r.ph.GetPriceComparison)

	// AMM swap (public quote + unsigned tx builder).
	api.POST("/swap/quote", r.ph.QuoteSwap)
	api.POST("/swap/build", r.ph.BuildSwap)

	// Internal AMM pools and swaps.
	// ListPools/GetPool/QuoteSwap are public (read-only); CreatePool is
	// admin-only (see admin group below) so ordinary users cannot mint markets.
	api.GET("/amm/pools", r.ammH.ListPools)
	api.GET("/amm/pools/:id", r.ammH.GetPool)
	prot.GET("/amm/pools/:id/position", r.ammH.GetPosition)
	prot.POST("/amm/pools/:id/add-liquidity", r.ammH.AddLiquidity)
	prot.POST("/amm/pools/:id/remove-liquidity", r.ammH.RemoveLiquidity)
	prot.GET("/amm/pools/:id/swaps", r.ammH.ListSwaps)
	prot.GET("/amm/positions", r.ammH.ListPositions)
	api.POST("/amm/swap/quote", r.ammH.QuoteSwap)
	prot.POST("/amm/swap", r.ammH.ExecuteSwap)

	// Admin
	admin := api.Group("/admin")
	admin.Use(r.authMW, AdminOnly())
	admin.GET("/users", r.accountH.AdminListUsers)
	// Manual balance credits are restricted to admins (see note above).
	admin.POST("/wallet/deposit", r.walletH.Deposit)
	// AMM admin: create pools, re-seed, control the market simulator.
	admin.POST("/amm/pools", r.ammH.CreatePool)
	admin.POST("/amm/seed", r.adminH.SeedAMM)
	admin.POST("/amm/simulator/start", r.adminH.StartSimulator)
	admin.POST("/amm/simulator/stop", r.adminH.StopSimulator)
	admin.GET("/amm/simulator", r.adminH.SimulatorStatus)
	admin.GET("/withdrawals", r.adminH.ListWithdrawals)
	admin.POST("/withdrawals/:id/approve", r.adminH.ApproveWithdrawal)
	admin.POST("/withdrawals/:id/reject", r.adminH.RejectWithdrawal)
	admin.GET("/users/:id/withdrawals", r.adminH.ListUserWithdrawals)
	admin.GET("/users/:id/addresses", r.adminH.ListAddresses)
	admin.POST("/users/:id/addresses", r.adminH.AddAddress)
	admin.POST("/users/:id/limits", r.adminH.SetDailyLimit)
	admin.GET("/risk/pairs", r.adminH.ListPairRisk)
	admin.GET("/risk/pairs/:pair", r.adminH.GetPairRisk)
	admin.PUT("/risk/pairs/:pair", r.adminH.UpdatePairRisk)
	admin.GET("/risk/users/:id", r.adminH.GetUserRisk)
	admin.PUT("/risk/users/:id", r.adminH.UpdateUserRisk)
	admin.POST("/pairs/:pair/pause", r.adminH.PausePair)
	admin.POST("/pairs/:pair/resume", r.adminH.ResumePair)
	admin.POST("/snapshots", r.adminH.TriggerSnapshot)

	// WebSocket endpoint: per-IP connection-attempt rate limit (cache-backed,
	// in-memory fallback) plus the hub's global connection cap.
	r.engine.GET("/ws", WSConnectLimiter(r.cache, r.config.WSConnRatePerMin, time.Minute), r.wh.HandleWebSocket)

	// Static frontend SPA
	if r.staticDir != "" {
		staticRoot, err := filepath.Abs(r.staticDir)
		if err != nil {
			staticRoot = r.staticDir
		}
		r.engine.NoRoute(func(c *gin.Context) {
			path := c.Request.URL.Path
			if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/ws") ||
				path == "/health" || path == "/ready" || path == "/metrics" {
				c.AbortWithStatus(http.StatusNotFound)
				return
			}
			// Path-traversal guard: normalize the request path relative to the
			// site root and verify the result stays inside staticRoot.
			clean := filepath.Clean("/" + path)
			fullPath := filepath.Join(staticRoot, clean)
			if fullPath != staticRoot && !strings.HasPrefix(fullPath, staticRoot+string(filepath.Separator)) {
				c.AbortWithStatus(http.StatusNotFound)
				return
			}
			if fi, err := os.Stat(fullPath); err == nil && !fi.IsDir() {
				c.File(fullPath)
				return
			}
			c.File(filepath.Join(staticRoot, "index.html"))
		})
	}

	return r.engine
}

func (r *Router) healthHandler(c *gin.Context) {
	c.JSON(200, gin.H{"status": "ok", "service": "fliance"})
}
