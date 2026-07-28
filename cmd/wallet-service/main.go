package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/WkT010/nexa-exchange/internal/config"
	"github.com/WkT010/nexa-exchange/internal/observability"
	"github.com/WkT010/nexa-exchange/internal/store"
	"github.com/WkT010/nexa-exchange/internal/wallet"
)

const version = "3.0.0"

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	cfg := config.Load()
	log.Printf("[NEXA] wallet-service starting (env=%s, version=%s)", cfg.Environment, version)

	clients := map[string]wallet.BlockchainClient{
		"BTC": wallet.NewMockBlockchainClient("BTC"),
	}
	if cfg.AlchemyAPIKey != "" {
		clients["ETH"] = wallet.NewAlchemyClient("ETH", cfg.AlchemyEthURL)
		clients["POLYGON"] = wallet.NewAlchemyClient("POLYGON", cfg.AlchemyPolygonURL)
	}

	// The wallet-service is the on-chain worker (deposit confirmation,
	// withdrawal broadcast). Trade settlement runs in the api-gateway where
	// the matching engines live. Here we construct the service for the
	// on-chain workflows; persistence is wired when postgres is available.
	var (
		walletSvc     *wallet.Service
		withdrawalSvc *wallet.WithdrawalService
		db            *sql.DB
	)
	if dsn := cfg.PostgresDSN; dsn != "" {
		var err error
		db, err = store.NewPG(dsn)
		if err == nil {
			defer db.Close()
			walletStore := store.NewPGWalletStore(db)
			walletSvc = wallet.NewService(walletStore, clients, defaultFeeSchedule())
			withdrawalSvc = wallet.NewWithdrawalService(walletSvc)
			withdrawalSvc.SetReviewThreshold(big.NewFloat(10000))
			log.Println("[NEXA] wallet-service postgres connected")
		} else {
			log.Printf("[NEXA] wallet-service postgres failed: %v", err)
			walletSvc = wallet.NewService(nil, clients, defaultFeeSchedule())
			withdrawalSvc = wallet.NewWithdrawalService(walletSvc)
		}
	} else {
		walletSvc = wallet.NewService(nil, clients, defaultFeeSchedule())
		withdrawalSvc = wallet.NewWithdrawalService(walletSvc)
	}

	// Health / metrics HTTP surface.
	health := observability.NewHealthCollector()
	if db != nil {
		health.Register(observability.PostgresCheck(db))
	}
	health.Register(observability.SimpleCheck("wallet", func() error {
		if walletSvc == nil {
			return fmt.Errorf("wallet service not initialized")
		}
		return nil
	}))

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok","service":"nexa-wallet","version":"` + version + `"}`))
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

	srv := &http.Server{Addr: cfg.ListenAddr, Handler: mux}

	// Background worker: confirm deposits and broadcast approved withdrawals.
	// In production this would poll pending rows or consume Kafka events.
	workerCtx, workerCancel := context.WithCancel(context.Background())
	defer workerCancel()
	go runOnChainWorker(workerCtx, withdrawalSvc, 30*time.Second)

	go func() {
		log.Printf("[NEXA] wallet health endpoint on %s", cfg.ListenAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[NEXA] %v", err)
		}
	}()

	<-signalNotify()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
	workerCancel()
	log.Println("[NEXA] wallet-service stopped")
}

func runOnChainWorker(ctx context.Context, svc *wallet.WithdrawalService, interval time.Duration) {
	if svc == nil {
		log.Println("[wallet-worker] no withdrawal service, worker exiting")
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Broadcast any approved withdrawals to the network.
			if err := svc.ProcessApprovedWithdrawals(100); err != nil {
				log.Printf("[wallet-worker] broadcast batch failed: %v", err)
			}
			// Confirm broadcast withdrawals once they reach the required depth.
			if err := svc.ProcessBroadcastWithdrawals(100); err != nil {
				log.Printf("[wallet-worker] confirm batch failed: %v", err)
			}
		}
	}
}

// defaultFeeSchedule mirrors the api-gateway's fee schedule so the wallet
// service can compute withdrawal / settlement fees consistently.
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

func signalNotify() chan os.Signal {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
	return c
}
