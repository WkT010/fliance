package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/WkT010/nexa-exchange/internal/api"
	"github.com/WkT010/nexa-exchange/internal/auth"
	"github.com/WkT010/nexa-exchange/internal/config"
	"github.com/WkT010/nexa-exchange/internal/matching"
	"github.com/WkT010/nexa-exchange/pkg/websocket"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	cfg := config.Load()
	log.Printf("[NEXA] api-gateway starting (env=%s)", cfg.Environment)

	engine := matching.NewMatchingEngine("BTC/USDT", 1_000_000)
	engine.Start()

	hub := websocket.NewHub()
	go hub.Run()

	mgr := auth.NewJWTManager(cfg.JWTSecret, cfg.JWTIssuer)
	authH := api.NewAuthHandler(mgr, nil)
	orderH := api.NewOrderHandler(engine, nil)
	wsH := api.NewWSHandler(hub)
	r := api.NewRouter(orderH, authH, wsH, authH.AuthMiddleware()).Setup()

	srv := &http.Server{Addr: cfg.ListenAddr, Handler: r, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}

	go func() {
		<-signalNotify()
		log.Println("[NEXA] shutting down...")
		engine.Stop()
		srv.Shutdown(context.Background())
	}()

	log.Printf("[NEXA] listening on %s", cfg.ListenAddr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("[NEXA] failed: %v", err)
	}
}

func signalNotify() chan os.Signal { c := make(chan os.Signal, 1); signal.Notify(c, syscall.SIGINT, syscall.SIGTERM); return c }
