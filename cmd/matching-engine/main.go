package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"github.com/WkT010/nexa-exchange/internal/matching"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("[NEXA] matching-engine starting...")
	for _, pair := range []string{"BTC/USDT", "ETH/USDT", "SOL/USDT", "BNB/USDT", "ADA/USDT"} {
		e := matching.NewMatchingEngine(pair, 1_000_000)
		e.Start()
		log.Printf("engine started: %s", pair)
	}
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM); <-quit
	log.Println("[NEXA] matching-engine stopped")
}