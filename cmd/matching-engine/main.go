package main

import (
	"context"
	"encoding/json"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/WkT010/nexa-exchange/internal/config"
	"github.com/WkT010/nexa-exchange/internal/grpc"
	"github.com/WkT010/nexa-exchange/internal/matching"
	"github.com/WkT010/nexa-exchange/internal/observability"
	"github.com/WkT010/nexa-exchange/internal/risk"
	pb "github.com/WkT010/nexa-exchange/proto/exchange/v1"

	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

const version = "3.0.0"

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	cfg := config.Load()
	log.Printf("[NEXA] matching-engine starting (env=%s, version=%s)", cfg.Environment, version)

	pairs := cfg.TradingPairs
	if len(pairs) == 0 {
		pairs = []string{"BTC/USDT", "ETH/USDT", "SOL/USDT", "BNB/USDT", "ADA/USDT"}
	}

	// Risk engine with conservative defaults.
	riskEng := risk.NewEngine()
	for _, pair := range pairs {
		riskEng.SetPairConfig(defaultPairRisk(pair))
	}

	// Exchange facade manages per-pair engines, WAL and risk checks.
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

	engines := make(map[string]*matching.MatchingEngine, len(pairs))
	for _, pair := range pairs {
		e, err := exchange.RegisterPair(pair, 1<<20)
		if err != nil {
			log.Fatalf("[NEXA] failed to register %s: %v", pair, err)
		}
		engines[pair] = e
		log.Printf("[NEXA] engine started: %s", pair)
	}
	log.Printf("[NEXA] all %d engines running", len(engines))

	// Periodic snapshots every 60s for fast recovery after a crash.
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

	// Health / metrics HTTP surface.
	health := observability.NewHealthCollector()
	health.Register(observability.ExchangeHealthCheck(exchange))
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok","service":"nexa-matching","version":"` + version + `"}`))
	})
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		status := health.Check(r.Context())
		code := http.StatusOK
		if status.Status != "healthy" {
			code = http.StatusServiceUnavailable
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(status)
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		observability.CollectGoRuntime()
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		observability.Default.WritePrometheus(w)
	})
	monitor := &http.Server{Addr: ":8081", Handler: mux}
	go func() {
		log.Printf("[NEXA] matching-engine monitor on %s", monitor.Addr)
		if err := monitor.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[NEXA] monitor failed: %v", err)
		}
	}()

	// gRPC service for external order entry / market data.
	grpcAddr := cfg.GRPCAddr
	if grpcAddr == "" {
		grpcAddr = ":50051"
	}
	lis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		log.Fatalf("[NEXA] failed to listen grpc %s: %v", grpcAddr, err)
	}
	grpcServer := googlegrpc.NewServer()
	pb.RegisterExchangeServiceServer(grpcServer, grpc.NewMatchingServer(engines))
	reflection.Register(grpcServer)
	go func() {
		log.Printf("[NEXA] grpc server on %s", grpcAddr)
		if err := grpcServer.Serve(lis); err != nil {
			log.Printf("[NEXA] grpc server stopped: %v", err)
		}
	}()

	// Wait for shutdown signal.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig
	log.Printf("[NEXA] received %v, stopping %d engines", s, len(engines))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	exchange.Stop()
	grpcServer.GracefulStop()
	_ = monitor.Shutdown(ctx)

	log.Println("[NEXA] matching-engine stopped gracefully")
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
