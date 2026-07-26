package wsbridge

import (
	"encoding/json"
	"log"
	"sync"

	"github.com/WkT010/nexa-exchange/internal/matching"
	"github.com/WkT010/nexa-exchange/pkg/websocket"
)

type Bridge struct {
	hub     *websocket.Hub
	engines map[string]*matching.MatchingEngine
	done    chan struct{}
	wg      sync.WaitGroup
}

func NewBridge(hub *websocket.Hub, engines map[string]*matching.MatchingEngine) *Bridge {
	return &Bridge{hub: hub, engines: engines, done: make(chan struct{})}
}

func (b *Bridge) Start() {
	for pair, e := range b.engines {
		b.wg.Add(2)
		go b.consumeTrades(pair, e)
		go b.consumeFills(pair, e)
	}
	log.Printf("[wsbridge] started (%d engines)", len(b.engines))
}

func (b *Bridge) Stop() { close(b.done); b.wg.Wait(); log.Println("[wsbridge] stopped") }

func (b *Bridge) consumeTrades(pair string, e *matching.MatchingEngine) {
	defer b.wg.Done()
	for {
		select {
		case <-b.done: return
		case t := <-e.Trades:
			data, _ := json.Marshal(map[string]interface{}{"type":"trade","pair":pair,"price":t.Price.String(),"qty":t.Quantity.String(),"time":t.CreatedAt})
			b.hub.BroadcastToRoom(websocket.ChannelTrades+":"+pair, data)
		}
	}
}

func (b *Bridge) consumeFills(pair string, e *matching.MatchingEngine) {
	defer b.wg.Done()
	for {
		select {
		case <-b.done: return
		case <-e.Fills:
			depth := e.OrderBook.Depth(10)
			data, _ := json.Marshal(map[string]interface{}{"type":"snapshot","pair":pair,"bids":depth.Bids,"asks":depth.Asks,"seq":depth.SeqNo})
			b.hub.BroadcastToRoom(websocket.ChannelOrderbook+":"+pair, data)
		}
	}
}
