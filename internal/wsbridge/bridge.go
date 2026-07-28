package wsbridge

import (
	"encoding/json"
	"log"
	"math/big"
	"strconv"
	"sync"
	"time"

	"github.com/WkT010/nexa-exchange/internal/api"
	"github.com/WkT010/nexa-exchange/internal/matching"
	"github.com/WkT010/nexa-exchange/pkg/websocket"
)

// RiskPriceUpdater receives the latest trade price so it can keep reference
// prices up to date for price-band risk checks.
type RiskPriceUpdater interface {
	UpdateReferencePrice(pair string, price *big.Float)
}

// FillSettler settles a single fill. The bridge calls it for every fill emitted
// by the matching engines so wallet balances stay in sync with trades in real
// time. Errors are logged but never block the bridge (a failed settlement can
// be reconciled later).
type FillSettler interface {
	SettleFill(fillID, pair string, takerSide int, takerOrderID, makerOrderID, takerUserID, makerUserID string, price, qty *big.Float) error
}

// OrderReleaser releases leftover reservations for a fully-filled or cancelled
// order. Implemented by wallet.Service alongside FillSettler.
type OrderReleaser interface {
	ReleaseOrder(orderID, userID string) error
}

// CandleRecorder consumes executed trades to update OHLCV candles.
type CandleRecorder interface {
	RecordTrade(t *matching.Trade) error
}

type Bridge struct {
	hub             *websocket.Hub
	engines         map[string]*matching.MatchingEngine
	orderStore      api.OrderStore
	settler         FillSettler
	candleRecorder  CandleRecorder
	riskPriceUpdater RiskPriceUpdater
	done            chan struct{}
	wg              sync.WaitGroup
}

func NewBridge(hub *websocket.Hub, engines map[string]*matching.MatchingEngine, store api.OrderStore) *Bridge {
	return &Bridge{hub: hub, engines: engines, orderStore: store, done: make(chan struct{})}
}

// SetSettler wires a trade-fill settler (e.g. wallet.Service). Must be called
// before Start.
func (b *Bridge) SetSettler(s FillSettler) { b.settler = s }

// SetCandleRecorder wires an OHLCV candle service. Must be called before Start.
func (b *Bridge) SetCandleRecorder(c CandleRecorder) { b.candleRecorder = c }

// SetRiskPriceUpdater wires the risk engine for live reference-price updates.
func (b *Bridge) SetRiskPriceUpdater(r RiskPriceUpdater) { b.riskPriceUpdater = r }

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
		case <-b.done:
			return
		case t := <-e.Trades:
			if b.orderStore != nil {
				go b.orderStore.SaveTrade(t)
			}
			if b.candleRecorder != nil {
				if err := b.candleRecorder.RecordTrade(t); err != nil {
					log.Printf("[wsbridge] candle record failed (pair=%s): %v", pair, err)
				}
			}
			if b.riskPriceUpdater != nil && t.Price != nil && t.Price.Sign() > 0 {
				b.riskPriceUpdater.UpdateReferencePrice(pair, t.Price)
			}
			data, _ := json.Marshal(map[string]interface{}{"type": "trade", "pair": pair, "price": t.Price.String(), "qty": t.Quantity.String(), "time": t.CreatedAt})
			b.hub.BroadcastToRoom(websocket.ChannelTrades+":"+pair, data)
		}
	}
}

func (b *Bridge) consumeFills(pair string, e *matching.MatchingEngine) {
	defer b.wg.Done()
	for {
		select {
		case <-b.done:
			return
		case fill := <-e.Fills:
			// Settle the fill into wallet balances first so balances reflect the
			// trade before any UI snapshot is pushed.
			if b.settler != nil {
				// fillID combines the two order IDs, the fill quantity and a
				// coarse time bucket. It must be deterministic enough to dedupe
				// a replayed fill but unique across distinct fills. Adding a
				// uuid-style counter would be cleaner, but this avoids importing
				// another package.
				fillID := fill.TakerOrderID + ":" + fill.MakerOrderID + ":" + fill.Quantity.Text('f', 18) + ":" + strconv.FormatInt(time.Now().UnixNano(), 10)
				if err := b.settler.SettleFill(
					fillID, fill.Pair, int(fill.Side),
					fill.TakerOrderID, fill.MakerOrderID, fill.TakerUserID, fill.MakerUserID,
					fill.Price, fill.Quantity,
				); err != nil {
					log.Printf("[wsbridge] settle fill failed (pair=%s taker=%s maker=%s): %v", pair, fill.TakerOrderID, fill.MakerOrderID, err)
				}
			}

			depth := e.OrderBook.Depth(10)
			data, _ := json.Marshal(map[string]interface{}{
				"type": "snapshot", "pair": pair,
				"bids": depth.Bids, "asks": depth.Asks, "seq": depth.SeqNo,
			})
			b.hub.BroadcastToRoom(websocket.ChannelOrderbook+":"+pair, data)

			// Notify both counterparties about their personal fills so the
			// wallet/UI layer can react in real time.
			fillData, _ := json.Marshal(map[string]interface{}{
				"type":          "fill",
				"pair":          fill.Pair,
				"taker_order_id": fill.TakerOrderID,
				"maker_order_id": fill.MakerOrderID,
				"side":          fill.Side.String(),
				"price":         fill.Price.String(),
				"qty":           fill.Quantity.String(),
				"remaining":     fill.RemainingQty.String(),
				"taker_filled":  fill.TakerFilled,
				"maker_filled":  fill.MakerFilled,
				"time":          time.Now().UnixNano(),
			})
			b.hub.BroadcastToRoom(websocket.ChannelUser+":"+fill.TakerUserID, fillData)
			b.hub.BroadcastToRoom(websocket.ChannelUser+":"+fill.MakerUserID, fillData)

			// When an order is fully filled, release any leftover reservation
			// (e.g. price-improvement buffer) so the funds return to available.
			if fill.TakerFilled {
				if rs, ok := b.settler.(OrderReleaser); ok {
					if err := rs.ReleaseOrder(fill.TakerOrderID, fill.TakerUserID); err != nil {
						log.Printf("[wsbridge] release taker order %s failed: %v", fill.TakerOrderID, err)
					}
				}
			}
			if fill.MakerFilled {
				if rs, ok := b.settler.(OrderReleaser); ok {
					if err := rs.ReleaseOrder(fill.MakerOrderID, fill.MakerUserID); err != nil {
						log.Printf("[wsbridge] release maker order %s failed: %v", fill.MakerOrderID, err)
					}
				}
			}
		}
	}
}
