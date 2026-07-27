package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/WkT010/nexa-exchange/internal/api"
	"github.com/WkT010/nexa-exchange/internal/auth"
	"github.com/WkT010/nexa-exchange/internal/cache"
	"github.com/WkT010/nexa-exchange/internal/config"
	"github.com/WkT010/nexa-exchange/internal/matching"
	"github.com/WkT010/nexa-exchange/internal/store"
	"github.com/WkT010/nexa-exchange/internal/wallet"
)

func main() {
	cfg := config.Load()
	log.Printf("NEXA Exchange starting (env=%s listen=%s tls=%v)", cfg.Environment, cfg.ListenAddr, cfg.HasTLS())

	// ── Database ──────────────────────────────────────────────────────
	pgStore, err := store.NewPostgresStore(cfg.PostgresDSN)
	if err != nil {
		log.Fatalf("postgres: %v", err)
	}
	defer pgStore.Close()

	// ── Redis Cache ────────────────────────────────────────────────────
	redisCache := cache.New(&cache.Config{
		Addr: cfg.RedisAddr, Pass: cfg.RedisPass, DB: cfg.RedisDB,
		Prefix: "nexa:", TTL: 5 * time.Second,
	})
	if err := redisCache.Ping(context.Background()); err != nil {
		log.Printf("[WARN] Redis not reachable: %v (rate limiting will fall back to in-memory)", err)
		redisCache = nil
	} else {
		log.Println("Redis connected")
	}
	defer func() {
		if redisCache != nil { redisCache.Close() }
	}()

	// ── Matching Engine ───────────────────────────────────────────────
	engine := matching.NewEngine(matching.EngineConfig{
		TradingPairs: cfg.TradingPairs,
	})
	go engine.Start()

	// ── Wallet Service ────────────────────────────────────────────────
	walletSvc, err := wallet.NewService(&wallet.Config{
		EthURL:     cfg.AlchemyEthURL,
		PolygonURL: cfg.AlchemyPolygonURL,
		Store:      pgStore,
	})
	if err != nil {
		log.Printf("[WARN] wallet service init: %v", err)
		walletSvc = nil
	}

	// ── Dependencies ──────────────────────────────────────────────────
	authSvc := auth.NewService(cfg.JWTSecret, cfg.JWTIssuer)
	orderHandler := api.NewOrderHandler(engine, pgStore, redisCache)
	authHandler := api.NewAuthHandler(authSvc, pgStore)
	wsHandler := api.NewWSHandler(engine)
	walletHandler := api.NewWalletHandler(walletSvc)
	accountHandler := api.NewAccountHandler(pgStore)
	feeHandler := api.NewFeeHandler()

	// ── Router ────────────────────────────────────────────────────────
	router := api.NewRouter(
		orderHandler, authHandler, wsHandler, walletHandler,
		accountHandler, legalH, feeHandler,
		authSvc.AuthMiddleware(), pgStore, redisCache, cfg,
	).Setup()

	// ── HTTP Server ──────────────────────────────────────────────────
	srv := &http.Server{Addr: cfg.ListenAddr, Handler: router}

	go func() {
		log.Printf("HTTP server listening on %s", cfg.ListenAddr)
		if cfg.HasTLS() {
			if cfg.TLSAutoCert {
				log.Fatal("Auto TLS (Let's Encrypt) requires additional setup; use TLS_CERT_FILE/TLS_KEY_FILE instead")
			}
			if err := srv.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile); err != nil && err != http.ErrServerClosed {
				log.Fatalf("HTTPS serve: %v", err)
			}
		} else {
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatalf("HTTP serve: %v", err)
			}
		}
	}()

	// ── Graceful Shutdown ─────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("forced shutdown: %v", err)
	}
	engine.Stop()
	fmt.Println("Server exited gracefully")
}