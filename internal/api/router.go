package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/WkT010/nexa-exchange/internal/config"
	"github.com/WkT010/nexa-exchange/internal/observability"
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
	health    *observability.HealthCollector
	startedAt time.Time
	staticDir string
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

func (r *Router) Setup() *gin.Engine {
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
	prot.POST("/wallet/deposit", r.walletH.Deposit)
	prot.POST("/wallet/withdraw", r.walletH.Withdraw)
	prot.GET("/wallet/transactions", r.walletH.ListTransactions)
	prot.GET("/wallet/assets", r.walletH.ListSupportedAssets)

	prot.GET("/account", r.accountH.GetAccount)
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
	api.GET("/amm/pools", r.ammH.ListPools)
	api.GET("/amm/pools/:id", r.ammH.GetPool)
	api.POST("/amm/pools", r.ammH.CreatePool)
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

	r.engine.GET("/ws", r.wh.HandleWebSocket)

	// Static frontend SPA
	if r.staticDir != "" {
		r.engine.NoRoute(func(c *gin.Context) {
			path := c.Request.URL.Path
			if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/ws") ||
				path == "/health" || path == "/ready" || path == "/metrics" {
				c.AbortWithStatus(http.StatusNotFound)
				return
			}
			fullPath := filepath.Join(r.staticDir, filepath.Clean(path))
			if fi, err := os.Stat(fullPath); err == nil && !fi.IsDir() {
				c.File(fullPath)
				return
			}
			c.File(filepath.Join(r.staticDir, "index.html"))
		})
	}

	return r.engine
}

func (r *Router) healthHandler(c *gin.Context) {
	c.JSON(200, gin.H{"status": "ok", "service": "nexa"})
}