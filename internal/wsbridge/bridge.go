package wsbridge

import (
	"encoding/json"
	"log/slog"
	"math/big"
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

// PnLRecorder consumes fill notifications to track realized profit/loss.
type PnLRecorder interface {
	RecordFill(fill *matching.FillNotification)
}

type Bridge struct {
	hub              *websocket.Hub
	engines          map[string]*matching.MatchingEngine
	orderStore       api.OrderStore
	settler          FillSettler
	candleRecorder   CandleRecorder
	riskPriceUpdater RiskPriceUpdater
	pnlRecorder      PnLRecorder
	done             chan struct{}
	wg               sync.WaitGroup
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

// SetPnLRecorder wires the profit/loss tracker.
func (b *Bridge) SetPnLRecorder(p PnLRecorder) { b.pnlRecorder = p }

func (b *Bridge) Start() {
	for pair, e := range b.engines {
		b.wg.Add(2)
		go b.consumeTrades(pair, e)
		go b.consumeFills(pair, e)
	}
	slog.Info("wsbridge started", "engines", len(b.engines))
}

func (b *Bridge) Stop() { close(b.done); b.wg.Wait(); slog.Info("wsbridge stopped") }

// fillIDOf derives the deterministic settlement ID of a fill. The same fill
// always maps to the same ID (no wall-clock or random input), so wallet-side
// dedupe turns replays into no-ops. Format:
//
//	<pair>:<takerOrderID>:<makerOrderID>:<price fixed 18>:<qty fixed 18>
//
// Price/quantity are rendered with big.Float.Text('f', 18), i.e. exact
// fixed-point with 18 decimal places. The (taker, maker) pair identifies one
// execution leg; a maker leaves the book when exhausted and its remaining
// quantity shrinks between partial fills, so equal keys across distinct
// executions cannot occur in practice.
func fillIDOf(fill *matching.FillNotification) string {
	return fill.Pair + ":" + fill.TakerOrderID + ":" + fill.MakerOrderID + ":" +
		fixed18(fill.Price) + ":" + fixed18(fill.Quantity)
}

func fixed18(f *big.Float) string {
	if f == nil {
		return ""
	}
	return f.Text('f', 18)
}

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
					slog.Warn("candle record failed", "pair", pair, "err", err)
				}
			}
			if b.riskPriceUpdater != nil && t.Price != nil && t.Price.Sign() > 0 {
				b.riskPriceUpdater.UpdateReferencePrice(pair, t.Price)
			}
			// Field names mirror the frontend's Trade type (quantity, not qty;
			// `qty` is kept as an alias for older consumers).
			data, _ := json.Marshal(map[string]interface{}{"type": "trade", "pair": pair, "price": t.Price.String(), "quantity": t.Quantity.String(), "qty": t.Quantity.String(), "side": t.TakerSide.String(), "time": t.CreatedAt})
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
				// fillID must be DETERMINISTIC: wallet.Service dedupes replays via
				// its processedFills LRU keyed on this ID, so any time/random
				// component would make every replay a "new" fill and settle it
				// twice (double settlement = loss of funds). FillNotification has
				// no engine-assigned unique ID, so we derive one from the fill's
				// immutable identity: pair, both order IDs and the exact
				// fixed-point (18 decimals) price and quantity.
				fillID := fillIDOf(fill)
				if err := b.settler.SettleFill(
					fillID, fill.Pair, int(fill.Side),
					fill.TakerOrderID, fill.MakerOrderID, fill.TakerUserID, fill.MakerUserID,
					fill.Price, fill.Quantity,
				); err != nil {
					slog.Error("settle fill failed", "pair", pair, "taker_order_id", fill.TakerOrderID, "maker_order_id", fill.MakerOrderID, "err", err)
				}
			}

			if b.pnlRecorder != nil {
				b.pnlRecorder.RecordFill(fill)
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
				"type":           "fill",
				"pair":           fill.Pair,
				"taker_order_id": fill.TakerOrderID,
				"maker_order_id": fill.MakerOrderID,
				"side":           fill.Side.String(),
				"price":          fill.Price.String(),
				"qty":            fill.Quantity.String(),
				"remaining":      fill.RemainingQty.String(),
				"taker_filled":   fill.TakerFilled,
				"maker_filled":   fill.MakerFilled,
				"time":           time.Now().UnixNano(),
			})
			b.hub.BroadcastToRoom(websocket.ChannelUser+":"+fill.TakerUserID, fillData)
			b.hub.BroadcastToRoom(websocket.ChannelUser+":"+fill.MakerUserID, fillData)

			// When an order is fully filled, release any leftover reservation
			// (e.g. price-improvement buffer) so the funds return to available.
			if fill.TakerFilled {
				if rs, ok := b.settler.(OrderReleaser); ok {
					if err := rs.ReleaseOrder(fill.TakerOrderID, fill.TakerUserID); err != nil {
						slog.Warn("release taker order failed", "order_id", fill.TakerOrderID, "err", err)
					}
				}
			}
			if fill.MakerFilled {
				if rs, ok := b.settler.(OrderReleaser); ok {
					if err := rs.ReleaseOrder(fill.MakerOrderID, fill.MakerUserID); err != nil {
						slog.Warn("release maker order failed", "order_id", fill.MakerOrderID, "err", err)
					}
				}
			}
		}
	}
}
