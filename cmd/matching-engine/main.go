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
	log.Printf("[NEXA] matching-engine starting (env=%s)", cfg.Environment)

	pairs := []string{"BTC/USDT", "ETH/USDT", "SOL/USDT", "BNB/USDT", "ADA/USDT"}
	engines := make(map[string]*matching.MatchingEngine)
	for _, pair := range pairs {
		e := matching.NewMatchingEngine(pair, 1_000_000)
		e.Start()
		engines[pair] = e
	}

	sig := <-signalNotify()
	log.Printf("[NEXA] received %v, stopping %d engines", sig, len(engines))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for pair, e := range engines {
		e.Stop()
		select { case <-ctx.Done(): os.Exit(1); default: }
	}
	log.Println("[NEXA] matching-engine stopped")
}

func signalNotify() chan os.Signal { c := make(chan os.Signal, 1); signal.Notify(c, syscall.SIGINT, syscall.SIGTERM); return c }
