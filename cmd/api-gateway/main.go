package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"github.com/WkT010/nexa-exchange/internal/api"
	"github.com/WkT010/nexa-exchange/internal/auth"
	"github.com/WkT010/nexa-exchange/internal/matching"
	"github.com/WkT010/nexa-exchange/pkg/websocket"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("[NEXA] api-gateway starting...")
	engine := matching.NewMatchingEngine("BTC/USDT", 1_000_000)
	engine.Start()
	hub := websocket.NewHub(); go hub.Run()
	mgr := auth.NewJWTManager(getEnv("JWT_SECRET", "nexa-dev-secret"), "nexa-exchange")
	authH := api.NewAuthHandler(mgr, nil)
	orderH := api.NewOrderHandler(engine, nil)
	wsH := api.NewWSHandler(hub)
	r := api.NewRouter(orderH, authH, wsH, authH.AuthMiddleware()).Setup()
	go func() { <-signalNotify(); engine.Stop(); os.Exit(0) }()
	log.Fatal(r.Run(getEnv("LISTEN_ADDR", ":8080")))
}

func getEnv(k, fallback string) string { if v := os.Getenv(k); v != "" { return v }; return fallback }
func signalNotify() chan os.Signal { c := make(chan os.Signal, 1); signal.Notify(c, syscall.SIGINT, syscall.SIGTERM); return c }
