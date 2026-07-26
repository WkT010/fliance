package main

import (
	"context"
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
	"github.com/WkT010/nexa-exchange/internal/matching"
	"github.com/WkT010/nexa-exchange/internal/store"
	"github.com/WkT010/nexa-exchange/internal/wallet"
	"github.com/WkT010/nexa-exchange/internal/wsbridge"
	"github.com/WkT010/nexa-exchange/pkg/websocket"
)

const version = "3.0.0"

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	cfg := config.Load()
	log.Printf("[NEXA] api-gateway starting (env=%s, version=%s)", cfg.Environment, version)

	engines := make(map[string]*matching.MatchingEngine)
	for _, pair := range cfg.TradingPairs {
		e := matching.NewMatchingEngine(pair, 1_000_000)
		e.Start()
		engines[pair] = e
		log.Printf("[NEXA] engine started: %s", pair)
	}
	log.Printf("[NEXA] %d matching engines running", len(engines))

	hub := websocket.NewHub()
	go hub.Run()

	var (
		orderStore api.OrderStore
		userStore  api.UserStore
		walletSvc  *wallet.Service
		apiKeyStore auth.APIKeyStore
	)

	if dsn := cfg.PostgresDSN; dsn != "" {
		db, err := store.NewPG(dsn)
		if err != nil {
			log.Printf("[NEXA] postgres connection failed: %v (running without persistence)", err)
		} else {
			defer db.Close()
			orderStore = store.NewPGOrderStore(db)
			userStore = store.NewPGUserStore(db)
			walletStore := store.NewPGWalletStore(db)
			apiKeyStore = store.NewPGAPIKeyStore(db)
			walletSvc = wallet.NewService(walletStore, blockchainClients(cfg), buildFeeSchedule())
			log.Println("[NEXA] postgres connected")
		}
	}

	bridge := wsbridge.NewBridge(hub, engines, orderStore)
	if walletSvc != nil {
		bridge.SetSettler(walletSvc)
	}
	bridge.Start()

	mgr := auth.NewJWTManager(cfg.JWTSecret, cfg.JWTIssuer)
	ah := api.NewAuthHandler(mgr, userStore)
	oh := api.NewOrderHandler(engines, orderStore)
	if walletSvc != nil {
		oh.SetWallet(walletSvc, walletSvc)
	}
	wh := api.NewWSHandler(hub, mgr)

	var walletH *api.WalletHandler
	var accountH *api.AccountHandler
	var walletSvcIface api.WalletService
	if walletSvc != nil {
		walletSvcIface = walletSvc
		walletH = api.NewWalletHandler(walletSvc, walletSvc.ClientsMap())
	} else {
		walletH = api.NewWalletHandler(nil, nil)
	}
	accountH = api.NewAccountHandler(userStore, walletSvcIface, apiKeyStore)

	router := api.NewRouter(oh, ah, wh, walletH, accountH, ah.AuthMiddleware(), apiKeyStore, cfg).Setup()
	srv := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		log.Println("[NEXA] shutting down...")
		bridge.Stop()
		for _, e := range engines {
			e.Stop()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}()

	log.Printf("[NEXA] listening on %s", cfg.ListenAddr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("[NEXA] server failed: %v", err)
	}
	log.Println("[NEXA] api-gateway stopped")
}

// blockchainClients builds the asset->client map used by the wallet service for
// deposit address generation and on-chain withdrawal. Mock clients are used for
// BTC; Alchemy is used for EVM chains when an API key is configured.
func blockchainClients(cfg *config.Config) map[string]wallet.BlockchainClient {
	clients := map[string]wallet.BlockchainClient{
		"BTC": wallet.NewMockBlockchainClient("BTC"),
	}
	if cfg.AlchemyAPIKey != "" {
		clients["ETH"] = wallet.NewAlchemyClient("ETH", cfg.AlchemyEthURL)
		clients["POLYGON"] = wallet.NewAlchemyClient("POLYGON", cfg.AlchemyPolygonURL)
	}
	return clients
}

// buildFeeSchedule returns a Binance-style fee schedule: 10 bps taker / 10 bps
// maker default, with 5 bps maker discount on majors. In production this would
// be loaded from the fee_schedule table.
func buildFeeSchedule() wallet.FeeSchedule {
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
