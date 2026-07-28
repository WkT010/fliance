package matching

import (
	"container/heap"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

// OrderBookSnapshot captures the live state of an order book (resting orders and
// parked stop orders) without pair/version metadata.
type OrderBookSnapshot struct {
	SeqNo  uint64   `json:"seq_no"`
	Orders []*Order `json:"orders"`
	Stops  []*Order `json:"stops"`
}

// Snapshot serialises the current order-book state. The returned orders are deep
// copies so the snapshot is safe to write asynchronously while matching continues.
func (ob *OrderBook) Snapshot() *OrderBookSnapshot {
	ob.mu.RLock()
	defer ob.mu.RUnlock()

	orders := make([]*Order, 0, len(ob.orders))
	for _, o := range ob.orders {
		orders = append(orders, copyOrder(o))
	}
	stops := make([]*Order, 0, len(ob.stops))
	for _, o := range ob.stops {
		stops = append(stops, copyOrder(o))
	}
	return &OrderBookSnapshot{
		SeqNo:  ob.seqNo.Load(),
		Orders: orders,
		Stops:  stops,
	}
}

// Restore replaces the order-book state with the supplied snapshot. It is used
// during crash recovery before the matching engine goroutine starts processing
// new orders.
func (ob *OrderBook) Restore(s *OrderBookSnapshot) error {
	if s == nil {
		return nil
	}
	ob.mu.Lock()
	defer ob.mu.Unlock()

	ob.orders = make(map[string]*Order)
	ob.bids = &OrderHeap{side: Buy}
	ob.asks = &OrderHeap{side: Sell}
	ob.stops = nil
	ob.seqNo.Store(s.SeqNo)

	for _, o := range s.Orders {
		if o == nil || o.ID == "" {
			continue
		}
		if o.RemainingQty == nil {
			o.RemainingQty = newBigFloatCopy(o.Quantity)
		}
		if o.FilledQty == nil {
			o.FilledQty = newBigFloat()
		}
		ob.orders[o.ID] = o
		if o.Side == Buy {
			heapPush(ob.bids, o)
		} else {
			heapPush(ob.asks, o)
		}
	}
	for _, o := range s.Stops {
		if o == nil || o.ID == "" {
			continue
		}
		ob.stops = append(ob.stops, o)
	}
	return nil
}

// MatchingEngineSnapshot is the on-disk format for a per-pair snapshot.
type MatchingEngineSnapshot struct {
	Version     string             `json:"version"`
	Pair        string             `json:"pair"`
	SeqNo       uint64             `json:"seq_no"`
	LastWALSeq  uint64             `json:"last_wal_seq"`
	Timestamp   int64              `json:"timestamp"`
	OrderBook   *OrderBookSnapshot `json:"orderbook"`
	LastTradePrice string          `json:"last_trade_price,omitempty"`
}

// Snapshot returns a serialisable snapshot of the engine state including the
// sequence number of the last WAL record that has been persisted.
func (e *MatchingEngine) Snapshot() (*MatchingEngineSnapshot, error) {
	var lastWALSeq uint64
	if e.wal != nil {
		lastWALSeq = e.wal.Seq()
	}
	lastPrice := ""
	if p := e.OrderBook.LastTradePrice(); p != nil && p.Sign() > 0 {
		lastPrice = p.Text('f', 18)
	}
	return &MatchingEngineSnapshot{
		Version:        "1",
		Pair:           e.Pair,
		SeqNo:          e.OrderBook.Seq(),
		LastWALSeq:     lastWALSeq,
		Timestamp:      time.Now().UnixNano(),
		OrderBook:      e.OrderBook.Snapshot(),
		LastTradePrice: lastPrice,
	}, nil
}

// Restore applies a snapshot to the engine. It must be called before Start.
func (e *MatchingEngine) Restore(s *MatchingEngineSnapshot) error {
	if s == nil || s.OrderBook == nil {
		return nil
	}
	if err := e.OrderBook.Restore(s.OrderBook); err != nil {
		return err
	}
	if s.LastTradePrice != "" {
		if p, err := parseBigFloat(s.LastTradePrice); err == nil {
			e.OrderBook.setLastTradePrice(p)
		}
	}
	return nil
}

func parseBigFloat(s string) (*big.Float, error) {
	f := newBigFloat()
	if _, _, err := f.Parse(s, 10); err != nil {
		return nil, err
	}
	return f, nil
}

// SaveSnapshot writes a snapshot atomically to path (creating the directory if
// needed). The file is written to a temporary name and renamed into place.
func SaveSnapshot(path string, s *MatchingEngineSnapshot) error {
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return fmt.Errorf("create snapshot dir: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0640)
	if err != nil {
		return fmt.Errorf("open snapshot tmp: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("write snapshot: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("sync snapshot: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close snapshot: %w", err)
	}
	return os.Rename(tmp, path)
}

// LoadSnapshot reads a snapshot from path.
func LoadSnapshot(path string) (*MatchingEngineSnapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s MatchingEngineSnapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("unmarshal snapshot: %w", err)
	}
	return &s, nil
}

func copyOrder(o *Order) *Order {
	if o == nil {
		return nil
	}
	return &Order{
		ID:            o.ID,
		ClientOrderID: o.ClientOrderID,
		UserID:        o.UserID,
		Pair:          o.Pair,
		Side:          o.Side,
		Type:          o.Type,
		Price:         newBigFloatCopy(o.Price),
		StopPrice:     newBigFloatCopy(o.StopPrice),
		Quantity:      newBigFloatCopy(o.Quantity),
		FilledQty:     newBigFloatCopy(o.FilledQty),
		RemainingQty:  newBigFloatCopy(o.RemainingQty),
		IcebergQty:    newBigFloatCopy(o.IcebergQty),
		VisibleQty:    newBigFloatCopy(o.VisibleQty),
		TimeInForce:   o.TimeInForce,
		Status:        o.Status,
		CreatedAt:     o.CreatedAt,
		UpdatedAt:     o.UpdatedAt,
		STP:           o.STP,
		PostOnly:      o.PostOnly,
	}
}

func heapPush(h *OrderHeap, o *Order) {
	if h == nil {
		return
	}
	heap.Push(h, o)
}
