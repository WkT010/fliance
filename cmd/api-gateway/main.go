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
	log.Printf("[NEXA] api-gateway starting (env=%s, version=2.0.0)", cfg.Environment)

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

	var orderStore api.OrderStore
	var userStore api.UserStore

	if dsn := cfg.PostgresDSN; dsn != "" {
		db, err := store.NewPG(dsn)
		if err != nil {
			log.Printf("[NEXA] postgres connection failed: %v (running without persistence)", err)
		} else {
			defer db.Close()
			orderStore = store.NewPGOrderStore(db)
			userStore = store.NewPGUserStore(db)
			log.Println("[NEXA] postgres connected")
		}
	}

	bridge := wsbridge.NewBridge(hub, engines, orderStore)
	bridge.Start()

	mgr := auth.NewJWTManager(cfg.JWTSecret, cfg.JWTIssuer)
	ah := api.NewAuthHandler(mgr, userStore)
	oh := api.NewOrderHandler(engines, orderStore)
	wh := api.NewWSHandler(hub)

	router := api.NewRouter(oh, ah, wh, ah.AuthMiddleware()).Setup()
	srv := &http.Server{
		Addr: cfg.ListenAddr, Handler: router,
		ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second,
	}

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		log.Println("[NEXA] shutting down...")
		bridge.Stop()
		for _, e := range engines { e.Stop() }
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
