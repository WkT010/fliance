package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"
	"time"

	"github.com/WkT010/nexa-exchange/internal/config"
	"github.com/WkT010/nexa-exchange/internal/wallet"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	cfg := config.Load()
	log.Printf("[NEXA] wallet-service starting (env=%s)", cfg.Environment)

	clients := map[string]wallet.BlockchainClient{
		"BTC":     wallet.NewMockBlockchainClient("BTC"),
		"ETH":     wallet.NewAlchemyClient("ETH", cfg.AlchemyEthURL),
		"POLYGON": wallet.NewAlchemyClient("POLYGON", cfg.AlchemyPolygonURL),
	}
	_ = wallet.NewService(nil, clients)
	log.Println("[NEXA] wallet ready (ETH/POLYGON via Alchemy)")

	<-signalNotify()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	<-ctx.Done()
	log.Println("[NEXA] wallet-service stopped")
}

func signalNotify() chan os.Signal { c := make(chan os.Signal, 1); signal.Notify(c, syscall.SIGINT, syscall.SIGTERM); return c }
