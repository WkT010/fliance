package main

import (
	"context"
	"database/sql"
	"log"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/WkT010/nexa-exchange/internal/api"
	"github.com/WkT010/nexa-exchange/internal/auth"
	"github.com/WkT010/nexa-exchange/internal/config"
	"github.com/WkT010/nexa-exchange/internal/market"
	"github.com/WkT010/nexa-exchange/internal/matching"
	"github.com/WkT010/nexa-exchange/internal/observability"
	"github.com/WkT010/nexa-exchange/internal/pnl"
	"github.com/WkT010/nexa-exchange/internal/risk"
	"github.com/WkT010/nexa-exchange/internal/store"
	"github.com/WkT010/nexa-exchange/internal/wallet"
	"github.com/WkT010/nexa-exchange/internal/wsbridge"
	"github.com/WkT010/nexa-exchange/pkg/websocket"
)

const version = "4.0.402"

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	cfg := config.Load()
	log.Printf("[NEXA] api-gateway starting (env=%s, version=%s)", cfg.Environment, version)

	// ── Persistence ──
	var db *sql.DB
	var orderStore api.OrderStore
	var userStore api.UserStore
	var apiKeyStore auth.APIKeyStore
	var walletStore wallet.WalletStore
	if cfg.PostgresDSN != "" {
		var err error
		db, err = store.NewPG(cfg.PostgresDSN)
		if err != nil {
			log.Printf("[NEXA] postgres connection failed: %v; running in best-effort mode", err)
		} else {
			defer db.Close()
			pgOrder := store.NewPGOrderStore(db)
			pgUser := store.NewPGUserStore(db)
			pgWallet := store.NewPGWalletStore(db)
			pgAPIKey := store.NewPGAPIKeyStore(db)
			orderStore = pgOrder
			userStore = pgUser
			walletStore = pgWallet
			apiKeyStore = pgAPIKey
			log.Println("[NEXA] postgres connected")
		}
	}

	// ── Blockchain clients (mock for BTC; Alchemy for EVM chains) ──
	clients := map[string]wallet.BlockchainClient{
		"BTC": wallet.NewMockBlockchainClient("BTC"),
	}
	if cfg.AlchemyAPIKey != "" {
		clients["ETH"] = wallet.NewAlchemyClient("ETH", cfg.AlchemyEthURL)
		clients["POLYGON"] = wallet.NewAlchemyClient("POLYGON", cfg.AlchemyPolygonURL)
	}

	// ── Wallet service (trade settlement + withdrawal controls) ──
	walletSvc := wallet.NewService(walletStore, clients, defaultFeeSchedule())
	withdrawalSvc := wallet.NewWithdrawalService(walletSvc)
	// Production defaults: require address whitelisting and daily limits.
	withdrawalSvc.SetReviewThreshold(big.NewFloat(10000))

	// ── Risk engine ──
	riskEng := risk.NewEngine()
	for _, pair := range cfg.TradingPairs {
		riskEng.SetPairConfig(defaultPairRisk(pair))
	}

	// ── Exchange facade (multi-pair matching + WAL + snapshots) ──
	walDir := os.Getenv("WAL_DIR")
	if walDir == "" {
		walDir = "./data/wal"
	}
	snapshotDir := os.Getenv("SNAPSHOT_DIR")
	if snapshotDir == "" {
		snapshotDir = "./data/snapshots"
	}
	_ = os.MkdirAll(walDir, 0750)
	_ = os.MkdirAll(snapshotDir, 0750)
	exchange := matching.NewExchangeEngine(riskEng, walDir, snapshotDir)
	for _, pair := range cfg.TradingPairs {
		if _, err := exchange.RegisterPair(pair, 1<<20); err != nil {
			log.Fatalf("[NEXA] failed to register %s: %v", pair, err)
		}
		log.Printf("[NEXA] registered pair: %s", pair)
	}

	// Periodic snapshots keep recovery time bounded as WAL grows.
	snapshotInterval := 60 * time.Second
	if v := os.Getenv("SNAPSHOT_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			snapshotInterval = d
		}
	}
	snapCtx, snapCancel := context.WithCancel(context.Background())
	defer snapCancel()
	go func() {
		t := time.NewTicker(snapshotInterval)
		defer t.Stop()
		for {
			select {
			case <-snapCtx.Done():
				return
			case <-t.C:
				if err := exchange.SnapshotAll(); err != nil {
					log.Printf("[NEXA] snapshot failed: %v", err)
				}
			}
		}
	}()

	// ── Candle service (OHLCV persistence) ──
	var candleSvc *market.CandleService
	if db != nil {
		candleSvc = market.NewCandleService(market.NewPGCandleStore(db))
	} else {
		candleSvc = market.NewCandleService(market.NewMemoryCandleStore())
	}

	// ── Realized PnL tracker ──
	pnlSvc := pnl.NewService()

	// ── Handlers ──
	orderH := api.NewOrderHandlerWithExchange(exchange, orderStore, riskEng)
	orderH.SetWallet(withdrawalSvc, withdrawalSvc)
	orderH.SetCandleStore(candleSvc)

	walletH := api.NewWalletHandler(withdrawalSvc, clients)

	priceH := api.NewPriceHandler(cfg.AlchemyAPIKey)
	futuresH := api.NewFuturesHandler(priceH, walletSvc, nil)

	// ── Futures liquidation monitor ──
	futuresLiquidationCtx, futuresLiquidationCancel := context.WithCancel(context.Background())
	defer futuresLiquidationCancel()
	go func() {
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-futuresLiquidationCtx.Done():
				return
			case <-t.C:
				futuresH.CheckLiquidations()
			}
		}
	}()

	if cfg.AlchemyAPIKey != "" {
		log.Println("[NEXA] Alchemy price feed: active")
	} else {
		log.Println("[NEXA] Alchemy price feed: disabled (no API key)")
	}

	accountH := api.NewAccountHandler(userStore, withdrawalSvc, apiKeyStore, priceH)
	accountH.SetPnLService(pnlSvc)

	adminH := api.NewAdminHandler(withdrawalSvc, riskEng, exchange)

	mgr := auth.NewJWTManager(cfg.JWTSecret, cfg.JWTIssuer)
	authH := api.NewAuthHandler(mgr, userStore)

	hub := websocket.NewHub()
	go hub.Run()
	wsH := api.NewWSHandler(hub, mgr)

	// ── WebSocket bridge (engine events -> hub) ──
	bridge := wsbridge.NewBridge(hub, exchangeEnginesMap(exchange), orderStore)
	bridge.SetSettler(withdrawalSvc)
	bridge.SetCandleRecorder(candleSvc)
	bridge.SetRiskPriceUpdater(riskEng)
	bridge.SetPnLRecorder(pnlSvc)
	bridge.Start()

	// ── Health checks ──
	health := observability.NewHealthCollector()
	if db != nil {
		health.Register(observability.PostgresCheck(db))
	}
	health.Register(observability.ExchangeHealthCheck(exchange))
	health.Register(observability.SimpleCheck("wallet", func() error {
		if walletStore == nil {
			return nil
		}
		_, err := walletStore.GetWallet("__healthcheck__", "USDT")
		return err
	}))

	// ── HTTP router ──
	router := api.NewRouter(orderH, authH, wsH, priceH, walletH, accountH, adminH, futuresH,
		authH.AuthMiddleware(), api.APIKeyMiddleware(apiKeyStore), cfg, health)
	staticDir := os.Getenv("STATIC_DIR")
	if staticDir == "" {
		staticDir = "./frontend/dist"
	}
	if fi, err := os.Stat(staticDir); err == nil && fi.IsDir() {
		router.SetStaticDir(staticDir)
		log.Printf("[NEXA] serving static files from %s", staticDir)
	}
	srv := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      router.Setup(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// ── Graceful shutdown ──
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		s := <-sig
		log.Printf("[NEXA] received %v, shutting down...", s)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		bridge.Stop()
		exchange.Stop()
		_ = srv.Shutdown(ctx)
	}()

	log.Printf("[NEXA] listening on %s", cfg.ListenAddr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("[NEXA] server failed: %v", err)
	}
	log.Println("[NEXA] api-gateway stopped")
}

// exchangeEnginesMap exposes the per-pair engines so the WebSocket bridge can
// subscribe to trade events. The exchange facade remains the canonical owner.
func exchangeEnginesMap(ex *matching.ExchangeEngine) map[string]*matching.MatchingEngine {
	out := make(map[string]*matching.MatchingEngine)
	for _, pair := range []string{"BTC/USDT", "ETH/USDT", "SOL/USDT", "BNB/USDT", "ADA/USDT"} {
		if e := ex.Get(pair); e != nil {
			out[pair] = e
		}
	}
	return out
}

func defaultFeeSchedule() wallet.FeeSchedule {
	return &wallet.StaticFeeSchedule{
		Default: wallet.FeeConfig{
			TakerRate: big.NewFloat(0.001),
			MakerRate: big.NewFloat(0.001),
		},
		Pairs: map[string]wallet.FeeConfig{
			"BTC/USDT": {TakerRate: big.NewFloat(0.001), MakerRate: big.NewFloat(0.0005)},
			"ETH/USDT": {TakerRate: big.NewFloat(0.001), MakerRate: big.NewFloat(0.0005)},
			"SOL/USDT": {TakerRate: big.NewFloat(0.0015), MakerRate: big.NewFloat(0.0005)},
			"BNB/USDT": {TakerRate: big.NewFloat(0.001), MakerRate: big.NewFloat(0.0005)},
			"ADA/USDT": {TakerRate: big.NewFloat(0.0015), MakerRate: big.NewFloat(0.0005)},
		},
	}
}

func defaultPairRisk(pair string) *risk.PairConfig {
	return &risk.PairConfig{
		Pair:                pair,
		MinNotional:         big.NewFloat(5),
		MaxNotional:         big.NewFloat(1_000_000),
		MinQty:              big.NewFloat(0.0001),
		MaxQty:              big.NewFloat(1000),
		TickSize:            big.NewFloat(0.01),
		LotSize:             big.NewFloat(0.0001),
		PriceBandPct:        big.NewFloat(0.05),
		CircuitBreakerPct:   big.NewFloat(0.10),
		ReferencePrice:      big.NewFloat(0),
		MarketOrdersEnabled: true,
		TradingEnabled:      true,
	}
}
