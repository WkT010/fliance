package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/WkT010/nexa-exchange/internal/config"
	"github.com/WkT010/nexa-exchange/internal/matching"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	cfg := config.Load()
	log.Printf("[NEXA] matching-engine starting (env=%s, version=2.0.0)", cfg.Environment)

	pairs := cfg.TradingPairs
	if len(pairs) == 0 {
		pairs = []string{"BTC/USDT", "ETH/USDT", "SOL/USDT", "BNB/USDT", "ADA/USDT"}
	}

	engines := make(map[string]*matching.MatchingEngine, len(pairs))
	for _, pair := range pairs {
		e := matching.NewMatchingEngine(pair, 1_000_000)
		e.Start()
		engines[pair] = e
		log.Printf("[NEXA] engine started: %s", pair)
	}
	log.Printf("[NEXA] all %d engines running", len(engines))

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig
	log.Printf("[NEXA] received %v, stopping %d engines", s, len(engines))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for pair, e := range engines {
		e.Stop()
		log.Printf("[NEXA] engine stopped: %s", pair)
		select {
		case <-ctx.Done():
			log.Println("[NEXA] shutdown timeout, exiting")
			return
		default:
		}
	}
	log.Println("[NEXA] matching-engine stopped gracefully")
}
