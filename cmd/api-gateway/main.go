package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/WkT010/nexa-exchange/internal/amm"
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

	// ── Futures persistence ──
	var futuresStore api.FuturesStore
	if db != nil {
		futuresStore = store.NewPGFuturesStore(db)
	}

	// ── AMM persistence and service ──
	var ammStore amm.Store
	if db != nil {
		ammStore = store.NewPGAmmStore(db)
	}
	ammSvc := amm.NewService(ammStore, walletSvc)

	// Bootstrap AMM pools with seed liquidity so every market has starting depth
	// and a starting price. Idempotent: only creates/seeds when the store is empty.
	seeded, err := bootstrapAMMPools(ammSvc)
	if err != nil {
		log.Printf("[NEXA] AMM bootstrap failed: %v (price feed will be empty until pools exist)", err)
	} else if seeded {
		log.Printf("[NEXA] AMM pools seeded with initial liquidity")
	}

	// AMM price feed: a fully self-contained price source derived from pool
	// reserves. This is the primary source for tickers/depth/trades — no
	// external dependency on Binance/Uniswap/Alchemy.
	ammFeed := market.NewAMMPriceFeed(ammSvc)
	ammFeed.SetTradeRecorder(candleSvc) // simulator trades -> K-lines
	if err := ammFeed.Reload(); err != nil {
		log.Printf("[NEXA] AMM feed reload failed: %v", err)
	} else {
		log.Printf("[NEXA] AMM feed loaded %d pools", len(ammFeed.Pairs()))
	}

	ammH := api.NewAmmHandler(ammSvc)

	// ── Handlers ──
	orderH := api.NewOrderHandlerWithExchange(exchange, orderStore, riskEng)
	orderH.SetWallet(withdrawalSvc, withdrawalSvc)
	orderH.SetCandleStore(candleSvc)

	walletH := api.NewWalletHandler(withdrawalSvc, clients)

	priceH := api.NewPriceHandler(cfg.AlchemyAPIKey)
	// AMM feed is the primary price source. External sources (Binance/Uniswap/
	// Alchemy) are only consulted when ENABLE_EXTERNAL_PRICE_FALLBACK=true AND
	// the AMM feed has no data for a pair — default off keeps the exchange fully
	// self-contained.
	priceH.SetAMMFeed(ammFeed)
	if os.Getenv("ENABLE_EXTERNAL_PRICE_FALLBACK") == "true" {
		priceH.SetExternalFallback(true)
		log.Printf("[NEXA] external price fallback: enabled (opt-in)")
	}
	// Order book / recent-trades fallback for the matching engine (which is
	// empty on a fresh deployment with no resting orders) comes from the same
	// AMM feed, so the trading UI shows live, self-contained depth and tape.
	orderH.SetPriceProvider(priceH)
	futuresH := api.NewFuturesHandler(priceH, walletSvc, futuresStore)

	// ── Futures liquidation / TP-SL / funding / order processing ──
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
				futuresH.ProcessOrders()
				futuresH.CheckTPSL()
				futuresH.CheckLiquidations()
			}
		}
	}()
	go func() {
		t := time.NewTicker(1 * time.Hour)
		defer t.Stop()
		for {
			select {
			case <-futuresLiquidationCtx.Done():
				return
			case <-t.C:
				futuresH.ApplyFunding()
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
	router := api.NewRouter(orderH, authH, wsH, priceH, walletH, accountH, adminH, futuresH, ammH,
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

	// ── AMM market simulator ──
	// Periodically runs small swaps against every pool so prices move even when
	// no real trader is active. This is what makes the exchange's market
	// self-contained: the AMM pools themselves produce all price action.
	simInterval := 3 * time.Second
	if v := os.Getenv("MARKET_SIM_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			simInterval = d
		}
	}
	simulator := market.NewSimulator(ammFeed, simInterval)
	simulator.Start()
	// Expose simulator control + bootstrap to admin endpoints (start/stop/status/seed).
	adminH.SetAMMSimulator(simulator)
	adminH.SetAMMFeed(ammFeed)
	adminH.SetAMMBootstrap(func() error {
		_, err := bootstrapAMMPools(ammSvc)
		if err != nil {
			return err
		}
		return ammFeed.Reload()
	})

	// ── Graceful shutdown ──
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		s := <-sig
		log.Printf("[NEXA] received %v, shutting down...", s)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		simulator.Stop()
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

// ammSeedSpec defines the seed liquidity for a market. token0 is the base
// asset, token1 is USDT; the product reserve0*reserve1 sets the price (price =
// reserve1/reserve0). Reserves are chosen so each pool has meaningful depth
// relative to typical trade sizes.
type ammSeedSpec struct {
	Pair                          string
	Token0, Token1                string
	Reserve0, Reserve1            float64
}

// defaultAMMSeeds is the set of markets bootstrapped on a fresh store. Adding a
// pair here also makes the AMM feed cover it automatically once seeded.
var defaultAMMSeeds = []ammSeedSpec{
	// BTC @ ~63000, ETH @ ~3000, SOL @ ~150, BNB @ ~600, ADA @ ~0.5
	{"BTC/USDT", "BTC", "USDT", 10, 630000},
	{"ETH/USDT", "ETH", "USDT", 100, 300000},
	{"SOL/USDT", "SOL", "USDT", 2000, 300000},
	{"BNB/USDT", "BNB", "USDT", 500, 300000},
	{"ADA/USDT", "ADA", "USDT", 200000, 100000},
}

// bootstrapAMMPools ensures every default market has a pool with seed liquidity.
// It creates missing pools and seeds reserves for pools that have no liquidity
// yet. Returns true if any pool was created or seeded. Idempotent: a fully-seeded
// store returns (false, nil) on subsequent calls.
func bootstrapAMMPools(svc *amm.Service) (bool, error) {
	existing, err := svc.ListPools()
	if err != nil {
		return false, fmt.Errorf("list pools: %w", err)
	}
	byPair := make(map[string]*amm.Pool, len(existing))
	for _, p := range existing {
		byPair[p.Pair] = p
	}
	anyChanged := false
	fee := big.NewFloat(0.003) // 30 bps
	for _, spec := range defaultAMMSeeds {
		pool, ok := byPair[spec.Pair]
		if !ok || pool == nil {
			// Create the pool.
			p, err := svc.CreatePool(spec.Pair, spec.Token0, spec.Token1, fee)
			if err != nil {
				return anyChanged, fmt.Errorf("create pool %s: %w", spec.Pair, err)
			}
			pool = p
			anyChanged = true
		}
		// Seed reserves only if the pool is empty (no liquidity yet). This
		// preserves any real liquidity a user may have added since boot.
		if pool.Reserve0 == nil || pool.Reserve0.Sign() <= 0 ||
			pool.Reserve1 == nil || pool.Reserve1.Sign() <= 0 {
			if err := svc.SeedPoolReserves(pool.ID, big.NewFloat(spec.Reserve0), big.NewFloat(spec.Reserve1)); err != nil {
				return anyChanged, fmt.Errorf("seed pool %s: %w", spec.Pair, err)
			}
			anyChanged = true
		}
	}
	return anyChanged, nil
}
