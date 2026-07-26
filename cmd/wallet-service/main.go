package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/WkT010/nexa-exchange/internal/config"
	"github.com/WkT010/nexa-exchange/internal/wallet"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	cfg := config.Load()
	log.Printf("[NEXA] wallet-service starting (env=%s, version=2.0.0)", cfg.Environment)

	clients := map[string]wallet.BlockchainClient{
		"BTC":     wallet.NewMockBlockchainClient("BTC"),
		"ETH":     wallet.NewAlchemyClient("ETH", cfg.AlchemyEthURL),
		"POLYGON": wallet.NewAlchemyClient("POLYGON", cfg.AlchemyPolygonURL),
	}
	wallet.NewService(nil, clients)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok","service":"nexa-wallet","version":"2.0.0"}`))
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

func signalNotify() chan os.Signal {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
	return c
}
