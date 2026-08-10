package main

import (
	"context"
	"encoding/json"
	"log/slog"
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

	"google.golang.org/grpc/reflection"
)

const version = "3.0.0"

func main() {
	cfg := config.Load()
	observability.Setup(cfg)
	slog.Info("matching-engine starting", "env", cfg.Environment, "version", version)

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
			slog.Error("failed to register trading pair", "pair", pair, "err", err)
			os.Exit(1)
		}
		engines[pair] = e
		slog.Info("engine started", "pair", pair)
	}
	slog.Info("all engines running", "count", len(engines))

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
					slog.Error("snapshot failed", "err", err)
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
		w.Write([]byte(`{"status":"ok","service":"fliance-matching","version":"` + version + `"}`))
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
		slog.Info("matching-engine monitor starting", "addr", monitor.Addr)
		if err := monitor.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("monitor failed", "err", err)
		}
	}()

	// gRPC service for external order entry / market data.
	grpcAddr := cfg.GRPCAddr
	if grpcAddr == "" {
		grpcAddr = ":50051"
	}
	lis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		slog.Error("failed to listen grpc", "addr", grpcAddr, "err", err)
		os.Exit(1)
	}
	// Secured gRPC server: shared-token auth (GRPC_SHARED_TOKEN) and optional
	// TLS (GRPC_TLS_CERT/GRPC_TLS_KEY) are enforced by the internal/grpc
	// package; outside development a missing token aborts startup. Reflection
	// is only exposed in development.
	grpcServer := grpc.NewSecuredServer()
	pb.RegisterExchangeServiceServer(grpcServer, grpc.NewMatchingServer(engines))
	if cfg.DevMode {
		reflection.Register(grpcServer)
	}
	go func() {
		slog.Info("grpc server starting", "addr", grpcAddr)
		if err := grpcServer.Serve(lis); err != nil {
			slog.Error("grpc server stopped", "err", err)
		}
	}()

	// Wait for shutdown signal.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig
	slog.Info("shutdown signal received", "signal", s.String(), "engines", len(engines))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	exchange.Stop()
	grpcServer.GracefulStop()
	_ = monitor.Shutdown(ctx)

	slog.Info("matching-engine stopped gracefully")
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
