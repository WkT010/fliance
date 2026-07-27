package main

import (
	"context"
	"log"
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
	"github.com/WkT010/nexa-exchange/internal/wsbridge"
	"github.com/WkT010/nexa-exchange/pkg/websocket"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	cfg := config.Load()
	log.Printf("[NEXA] api-gateway (env=%s)", cfg.Environment)

	engines := map[string]*matching.MatchingEngine{"BTC/USDT": matching.NewMatchingEngine("BTC/USDT", 1_000_000)}
	engines["BTC/USDT"].Start()

	hub := websocket.NewHub(); go hub.Run()
	bridge := wsbridge.NewBridge(hub, engines); bridge.Start()
	log.Println("[NEXA] engine + ws started")

	if db, err := store.NewPG(cfg.PostgresDSN); err == nil { defer db.Close(); store.NewPGWalletStore(db); store.NewPGOrderStore(db); store.NewPGUserStore(db) }

	mgr := auth.NewJWTManager(cfg.JWTSecret, cfg.JWTIssuer)
	authH := api.NewAuthHandler(mgr, nil)
	priceH := api.NewPriceHandler(api.NewPriceCache())
	log.Println("[NEXA] Binance price feed: active")

	r := api.NewRouter(api.NewOrderHandler(engines["BTC/USDT"], nil), authH, api.NewWSHandler(hub), priceH, authH.AuthMiddleware()).Setup()
	srv := &http.Server{Addr: cfg.ListenAddr, Handler: r, ReadTimeout: 15*time.Second, WriteTimeout: 15*time.Second, IdleTimeout: 60*time.Second}

	go func() {
		c := make(chan os.Signal,1); signal.Notify(c, syscall.SIGINT, syscall.SIGTERM); <-c
		log.Println("[NEXA] shutting down...")
		bridge.Stop(); engines["BTC/USDT"].Stop()
		srv.Shutdown(context.Background())
	}()
	log.Printf("[NEXA] listening on %s", cfg.ListenAddr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed { log.Fatalf("failed: %v", err) }
}
