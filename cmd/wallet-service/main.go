package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"github.com/WkT010/nexa-exchange/internal/wallet"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("[NEXA] wallet-service starting...")
	key := getEnv("ALCHEMY_API_KEY", "owtgBOQy-6ABQ9Pzd_7Nz")
	clients := map[string]wallet.BlockchainClient{
		"BTC":     wallet.NewMockBlockchainClient("BTC"),
		"ETH":     wallet.NewAlchemyClient("ETH", "https://eth-mainnet.g.alchemy.com/v2/"+key),
		"POLYGON": wallet.NewAlchemyClient("POLYGON", "https://polygon-mainnet.g.alchemy.com/v2/"+key),
	}
	_ = wallet.NewService(nil, clients)
	log.Println("[NEXA] wallet-service ready (ETH/POLYGON via Alchemy)")
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM); <-quit
	log.Println("[NEXA] wallet-service stopped")
}

func getEnv(k, fallback string) string { if v := os.Getenv(k); v != "" { return v }; return fallback }
