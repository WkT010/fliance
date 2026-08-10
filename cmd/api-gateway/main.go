package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/WkT010/nexa-exchange/internal/amm"
	"github.com/WkT010/nexa-exchange/internal/api"
	"github.com/WkT010/nexa-exchange/internal/audit"
	"github.com/WkT010/nexa-exchange/internal/auth"
	"github.com/WkT010/nexa-exchange/internal/cache"
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
	cfg := config.Load()
	observability.Setup(cfg)
	slog.Info("api-gateway starting", "env", cfg.Environment, "version", version)

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
			slog.Warn("postgres connection failed; running in best-effort mode", "err", err)
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
			slog.Info("postgres connected")
		}
	}

	// ── Admin audit trail ──
	// Asynchronous: entries buffer and flush in the background so auditing
	// never blocks admin requests. Without Postgres the logger degrades to
	// the local process log instead of dropping entries.
	var auditStore audit.AuditStore
	if db != nil {
		auditStore = store.NewPGAuditStore(db)
	}
	auditLog := audit.NewLogger(auditStore)
	defer auditLog.Close()

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
			slog.Error("failed to register trading pair", "pair", pair, "err", err)
			os.Exit(1)
		}
		slog.Info("registered pair", "pair", pair)
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
					slog.Error("snapshot failed", "err", err)
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
		slog.Warn("AMM bootstrap failed (price feed will be empty until pools exist)", "err", err)
	} else if seeded {
		slog.Info("AMM pools seeded with initial liquidity")
	}

	// AMM price feed: a fully self-contained price source derived from pool
	// reserves. This is the primary source for tickers/depth/trades — no
	// external dependency on Binance/Uniswap/Alchemy.
	ammFeed := market.NewAMMPriceFeed(ammSvc)
	ammFeed.SetTradeRecorder(candleSvc) // simulator trades -> K-lines
	if err := ammFeed.Reload(); err != nil {
		slog.Warn("AMM feed reload failed", "err", err)
	} else {
		slog.Info("AMM feed loaded", "pools", len(ammFeed.Pairs()))
	}

	ammH := api.NewAmmHandler(ammSvc)

	// ── Binance market data (primary market-data source) ──
	// Combined WebSocket streams feed the price handler's cache and are also
	// bridged into the platform WS hub (wired below, once the hub exists) so
	// the orderbook/trades panels show live Binance activity with the exact
	// message shapes the frontend already consumes from the matching engine.
	binanceWS := market.NewBinanceWSClient(cfg.BinanceWSURL, cfg.TradingPairs)
	// Persist live Binance kline updates into the candle store so the
	// in-progress bucket is always current and /klines reads never need a REST
	// backfill for the newest bar. Throttled per (pair, interval) — the kline
	// streams tick on every trade and the store is a remote database.
	var (
		klinePersistMu   sync.Mutex
		klinePersistLast = make(map[string]time.Time)
	)
	binanceWS.SetKlineHandler(func(cd *matching.Candle) {
		if cd == nil {
			return
		}
		key := cd.Pair + "|" + cd.Interval
		klinePersistMu.Lock()
		last, ok := klinePersistLast[key]
		klinePersistMu.Unlock()
		if ok && time.Since(last) < 10*time.Second {
			return
		}
		if err := candleSvc.SaveCandle(cd); err == nil {
			klinePersistMu.Lock()
			klinePersistLast[key] = time.Now()
			klinePersistMu.Unlock()
		}
	})

	// ── Handlers ──
	orderH := api.NewOrderHandlerWithExchange(exchange, orderStore, riskEng)
	orderH.SetWallet(withdrawalSvc, withdrawalSvc)
	orderH.SetCandleStore(candleSvc)

	walletH := api.NewWalletHandler(withdrawalSvc, clients)
	// Admin deposits may target another user; the lookup validates the
	// target exists before any balance is credited.
	walletH.SetUserLookup(userStore)
	walletH.SetAuditLogger(auditLog)

	priceH := api.NewPriceHandler(cfg.AlchemyAPIKey, cfg.BinanceRESTURLs)
	// Source chain: Binance WS cache -> Binance REST poller -> AMM pool feed.
	// Cached sources are freshness-gated (MARKET_DATA_STALENESS); the AMM
	// feed is only used as a fallback and its pool reserves are never
	// overwritten by external market data.
	priceH.SetBinanceWS(binanceWS)
	priceH.SetStaleness(cfg.MarketDataStaleness)
	priceH.StartPolling(cfg.BinancePollInterval)
	binanceWS.Start()
	priceH.SetAMMFeed(ammFeed)
	// Historical candle backfill for /klines when the local store is empty.
	orderH.SetKlineFetcher(priceH.BinanceFeed())
	// Warm every (pair x interval) series in the background at boot so the
	// first chart request never blocks on the external mirror (cold-start 500s).
	orderH.StartKlinePrewarm(cfg.TradingPairs)
	// Order book / recent-trades fallback for the matching engine (which is
	// empty on a fresh deployment with no resting orders) resolves through
	// the same chain: Binance depth/trades first, AMM synthesis last.
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
		slog.Info("Alchemy price feed active")
	} else {
		slog.Info("Alchemy price feed disabled (no API key)")
	}

	accountH := api.NewAccountHandler(userStore, withdrawalSvc, apiKeyStore, priceH)
	accountH.SetPnLService(pnlSvc)

	adminH := api.NewAdminHandler(withdrawalSvc, riskEng, exchange)
	adminH.SetAuditLogger(auditLog)

	// ── Shared cache (Redis with in-memory fallback) ──
	// Backs the login lockout counters, the JWT token blacklist and the WS
	// connection rate limiter. When Redis is unreachable the gateway degrades
	// to an in-memory cache instead of failing startup.
	var sharedCache cache.Cache
	redisOK := false
	redisCfg := cache.DefaultConfig()
	redisCfg.Addr = cfg.RedisAddr
	redisCfg.Pass = cfg.RedisPass
	redisCfg.DB = cfg.RedisDB
	rc := cache.New(redisCfg)
	pingCtx, pingCancel := context.WithTimeout(context.Background(), 3*time.Second)
	if err := rc.Ping(pingCtx); err == nil {
		sharedCache = rc
		redisOK = true
		slog.Info("cache: redis connected")
	} else {
		_ = rc.Close()
		sharedCache = cache.NewMemoryCache(0)
		slog.Warn("cache: redis unavailable; falling back to in-memory cache", "err", err)
	}
	pingCancel()
	defer sharedCache.Close()

	mgr := auth.NewJWTManager(cfg.JWTSecret, cfg.JWTIssuer)
	authH := api.NewAuthHandler(mgr, userStore)
	// Wire the shared cache so login lockout and the token blacklist operate
	// distributedly; must happen before router.Setup() publishes the blacklist
	// to the WebSocket handler.
	authH.SetCache(sharedCache)
	// Apply the configured account-lockout policy (env knobs
	// ACCOUNT_LOCKOUT_THRESHOLD / ACCOUNT_LOCKOUT_DURATION_MIN); the handler
	// ignores non-positive values and keeps its defaults.
	authH.SetLockoutPolicy(cfg.AccountLockoutThreshold, time.Duration(cfg.AccountLockoutDurationMin)*time.Minute)

	hub := websocket.NewHub()
	go hub.Run()
	wsH := api.NewWSHandler(hub, mgr)

	// Bridge Binance depth/trade events into the hub (see wireBinanceHubBridge).
	wireBinanceHubBridge(hub, binanceWS)

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
		// A missing row for the synthetic user is expected — it only proves the
		// DB round-trip works. Only real errors mark the check unhealthy.
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		return nil
	}))

	// ── HTTP router ──
	router := api.NewRouter(orderH, authH, wsH, priceH, walletH, accountH, adminH, futuresH, ammH,
		authH.AuthMiddleware(), api.APIKeyMiddleware(apiKeyStore), cfg, health)
	router.SetCache(sharedCache)
	// Global per-IP HTTP rate limit. ENABLE_REDIS_RATE_LIMIT=true uses the
	// distributed Redis limiter (only when Redis actually connected; a closed
	// client would refuse every request); otherwise the in-memory limiter.
	if cfg.EnableRedisRateLimit && redisOK {
		router.SetRateLimiter(api.RedisRateLimiter(rc, cfg.RateLimitPerSec, time.Second))
		slog.Info("rate limiter: redis-backed", "req_per_sec", cfg.RateLimitPerSec)
	} else {
		if cfg.EnableRedisRateLimit && !redisOK {
			slog.Warn("rate limiter: ENABLE_REDIS_RATE_LIMIT=true but redis is down; using in-memory limiter")
		}
		router.SetRateLimiter(api.RateLimiter(cfg.RateLimitPerSec, time.Second))
	}
	staticDir := os.Getenv("STATIC_DIR")
	if staticDir == "" {
		staticDir = "./frontend/dist"
	}
	if fi, err := os.Stat(staticDir); err == nil && fi.IsDir() {
		router.SetStaticDir(staticDir)
		slog.Info("serving static files", "dir", staticDir)
	}
	srv := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      router.Setup(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// ── AMM market simulator ──
	// Optionally runs small swaps against every pool so AMM-derived prices
	// move on their own. Disabled by default: Binance real market data is the
	// primary source and the simulator would only perturb AMM pool reserves.
	// Admins can still toggle it at runtime via the admin endpoints.
	simInterval := 3 * time.Second
	if v := os.Getenv("MARKET_SIM_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			simInterval = d
		}
	}
	simulator := market.NewSimulator(ammFeed, simInterval)
	if cfg.EnableMarketSimulator {
		simulator.Start()
		slog.Info("AMM market simulator started (opt-in)")
	} else {
		slog.Info("AMM market simulator disabled (ENABLE_MARKET_SIMULATOR=false)")
	}
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
		slog.Info("shutdown signal received", "signal", s.String())
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		simulator.Stop()
		binanceWS.Stop()
		priceH.StopPolling()
		bridge.Stop()
		exchange.Stop()
		_ = srv.Shutdown(ctx)
	}()

	slog.Info("listening", "addr", cfg.ListenAddr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("server failed", "err", err)
		os.Exit(1)
	}
	slog.Info("api-gateway stopped")
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

// wireBinanceHubBridge forwards Binance depth snapshots and trades into the
// platform WS hub using the exact message shapes wsbridge produces for
// matching-engine events, so the frontend needs zero changes.
func wireBinanceHubBridge(hub *websocket.Hub, ws *market.BinanceWSClient) {
	var (
		seq      uint64
		lastMu   sync.Mutex
		lastSent = make(map[string]time.Time)
	)
	// depth10@100ms arrives ~10x/s per pair; throttle broadcasts to ~4x/s so
	// the hub fan-out stays cheap while the book still feels live.
	const depthMinInterval = 250 * time.Millisecond
	ws.SetDepthHandler(func(pair string, d *market.Depth) {
		lastMu.Lock()
		if time.Since(lastSent[pair]) < depthMinInterval {
			lastMu.Unlock()
			return
		}
		lastSent[pair] = time.Now()
		lastMu.Unlock()
		seq++
		bids := make([]matching.PriceLevel, 0, len(d.Bids))
		for _, lv := range d.Bids {
			bids = append(bids, matching.PriceLevel{Price: lv.Price, Quantity: lv.Quantity, Count: 1})
		}
		asks := make([]matching.PriceLevel, 0, len(d.Asks))
		for _, lv := range d.Asks {
			asks = append(asks, matching.PriceLevel{Price: lv.Price, Quantity: lv.Quantity, Count: 1})
		}
		data, _ := json.Marshal(map[string]interface{}{
			"type": "snapshot", "pair": pair,
			"bids": bids, "asks": asks, "seq": seq,
		})
		hub.BroadcastToRoom(websocket.ChannelOrderbook+":"+pair, data)
	})
	ws.SetTradeHandler(func(pair string, t market.RecentTrade) {
		side := "sell"
		if t.IsBuyer {
			side = "buy"
		}
		data, _ := json.Marshal(map[string]interface{}{
			"type": "trade", "pair": pair,
			// Field names mirror the frontend's Trade type (quantity, not qty).
			"price": t.Price.String(), "quantity": t.Quantity.String(), "qty": t.Quantity.String(),
			"side": side,
			// Frontend formatTime() divides by 1e6: convert Binance ms -> ns.
			"time": t.Time * int64(time.Millisecond),
		})
		hub.BroadcastToRoom(websocket.ChannelTrades+":"+pair, data)
	})
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
	Pair               string
	Token0, Token1     string
	Reserve0, Reserve1 float64
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
