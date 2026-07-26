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

	"github.com/WkT010/nexa-exchange/internal/config"
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
	var walletSvc *wallet.Service
	if dsn := cfg.PostgresDSN; dsn != "" {
		if db, err := store.NewPG(dsn); err == nil {
			defer db.Close()
			walletStore := store.NewPGWalletStore(db)
			walletSvc = wallet.NewService(walletStore, clients, defaultFeeSchedule())
			log.Println("[NEXA] wallet-service postgres connected")
		} else {
			log.Printf("[NEXA] wallet-service postgres failed: %v", err)
			walletSvc = wallet.NewService(nil, clients, defaultFeeSchedule())
		}
	} else {
		walletSvc = wallet.NewService(nil, clients, defaultFeeSchedule())
	}
	_ = walletSvc

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok","service":"nexa-wallet","version":"` + version + `"}`))
	})
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ready"}`))
	})

	srv := &http.Server{Addr: cfg.ListenAddr, Handler: mux}

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
	log.Println("[NEXA] wallet-service stopped")
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
