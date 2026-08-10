package matching

import (
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ExchangeEngine manages one MatchingEngine per trading pair. It is the
// top-level facade used by the API gateway and gRPC services.
type ExchangeEngine struct {
	mu          sync.RWMutex
	engines     map[string]*MatchingEngine
	risk        RiskChecker
	walDir      string
	snapshotDir string
}

// RiskChecker is the minimal interface the exchange needs from the risk engine.
type RiskChecker interface {
	Check(req OrderRequest) error
}

// NewExchangeEngine creates an exchange with optional risk checker, WAL
// directory and snapshot directory. Empty directories disable the corresponding
// persistence feature.
func NewExchangeEngine(risk RiskChecker, walDir, snapshotDir string) *ExchangeEngine {
	return &ExchangeEngine{
		engines:     make(map[string]*MatchingEngine),
		risk:        risk,
		walDir:      walDir,
		snapshotDir: snapshotDir,
	}
}

// RegisterPair creates (or returns) the matching engine for a trading pair.
func (ex *ExchangeEngine) RegisterPair(pair string, ringCap uint64) (*MatchingEngine, error) {
	pair = normalizePair(pair)
	ex.mu.Lock()
	defer ex.mu.Unlock()
	if e, ok := ex.engines[pair]; ok {
		return e, nil
	}
	e := NewMatchingEngine(pair, ringCap)
	if ex.walDir != "" {
		walPath := filepath.Join(ex.walDir, walFileName(pair))
		w, err := NewWALWriter(walPath)
		if err != nil {
			return nil, fmt.Errorf("open wal for %s: %w", pair, err)
		}
		e.SetWAL(w)
	}
	if err := ex.recoverPair(e); err != nil {
		slog.Error("exchange recovery failed", "pair", pair, "err", err)
	}
	e.Start()
	ex.engines[pair] = e
	return e, nil
}

// Get returns the engine for a pair, or nil.
func (ex *ExchangeEngine) Get(pair string) *MatchingEngine {
	pair = normalizePair(pair)
	ex.mu.RLock()
	defer ex.mu.RUnlock()
	return ex.engines[pair]
}

// Engines returns a copy of the registered engine map.
func (ex *ExchangeEngine) Engines() map[string]*MatchingEngine {
	ex.mu.RLock()
	defer ex.mu.RUnlock()
	out := make(map[string]*MatchingEngine, len(ex.engines))
	for k, v := range ex.engines {
		out[k] = v
	}
	return out
}

// EngineCount returns the number of registered trading pairs.
func (ex *ExchangeEngine) EngineCount() int {
	ex.mu.RLock()
	defer ex.mu.RUnlock()
	return len(ex.engines)
}

// SubmitOrder validates risk, persists to WAL, and submits to the matching
// engine for the order's pair.
func (ex *ExchangeEngine) SubmitOrder(o *Order) error {
	if o == nil {
		return fmt.Errorf("nil order")
	}
	o.Pair = normalizePair(o.Pair)
	if ex.risk != nil {
		req := OrderRequest{
			UserID:   o.UserID,
			Pair:     o.Pair,
			Side:     o.Side,
			Type:     o.Type,
			Price:    o.Price,
			Quantity: o.Quantity,
		}
		if err := ex.risk.Check(req); err != nil {
			o.Status = Rejected
			return err
		}
	}
	e, err := ex.RegisterPair(o.Pair, 1<<20)
	if err != nil {
		return err
	}
	if !e.SubmitOrder(o) {
		return ErrEngineBusy
	}
	return nil
}

// SubmitOrderSync is the synchronous variant of SubmitOrder.
func (ex *ExchangeEngine) SubmitOrderSync(o *Order) (*Order, error) {
	if err := ex.SubmitOrder(o); err != nil {
		return nil, err
	}
	e := ex.Get(o.Pair)
	if e == nil {
		return nil, ErrEngineStopped
	}
	return e.SubmitOrderSync(o)
}

// CancelOrder cancels an order on the appropriate engine.
func (ex *ExchangeEngine) CancelOrder(orderID, userID, pair string) (*Order, error) {
	pair = normalizePair(pair)
	e := ex.Get(pair)
	if e == nil {
		return nil, ErrOrderNotFound
	}
	return e.Cancel(orderID, userID)
}

// Snapshot writes the current order-book state for all pairs to the snapshot
// directory. This is used for fast restarts.
func (ex *ExchangeEngine) Snapshot(dir string) error {
	if dir == "" {
		dir = ex.snapshotDir
	}
	if dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}
	ex.mu.RLock()
	engines := make(map[string]*MatchingEngine, len(ex.engines))
	for k, v := range ex.engines {
		engines[k] = v
	}
	ex.mu.RUnlock()

	for pair, e := range engines {
		path := filepath.Join(dir, snapshotFileName(pair))
		s, err := e.Snapshot()
		if err != nil {
			return fmt.Errorf("snapshot %s: %w", pair, err)
		}
		if err := SaveSnapshot(path, s); err != nil {
			return fmt.Errorf("save snapshot %s: %w", pair, err)
		}
	}
	return nil
}

// SnapshotAll writes snapshots to the configured snapshot directory.
func (ex *ExchangeEngine) SnapshotAll() error { return ex.Snapshot("") }

// recoverPair restores an engine from its latest snapshot and replays WAL
// records written after the snapshot was taken. Recovery errors are logged but
// do not prevent startup so the engine can recover from partial corruption.
func (ex *ExchangeEngine) recoverPair(e *MatchingEngine) error {
	pair := e.Pair
	if ex.snapshotDir != "" {
		path := filepath.Join(ex.snapshotDir, snapshotFileName(pair))
		if s, err := LoadSnapshot(path); err == nil {
			if err := e.Restore(s); err != nil {
				slog.Error("exchange restore snapshot failed", "pair", pair, "err", err)
			} else {
				slog.Info("exchange restored snapshot",
					"pair", pair, "seq", s.OrderBook.SeqNo, "orders", len(s.OrderBook.Orders), "stops", len(s.OrderBook.Stops))
			}
		}
	}
	if ex.walDir == "" {
		return nil
	}
	walPath := filepath.Join(ex.walDir, walFileName(pair))
	lastSeq := uint64(0)
	if s, err := LoadSnapshot(filepath.Join(ex.snapshotDir, snapshotFileName(pair))); err == nil {
		lastSeq = s.LastWALSeq
	}
	return ex.replayWAL(e, walPath, lastSeq)
}

// replayWAL re-applies order/cancel records written after lastSeq. Records up
// to and including lastSeq are assumed to be already reflected in the snapshot.
func (ex *ExchangeEngine) replayWAL(e *MatchingEngine, walPath string, lastSeq uint64) error {
	reader := NewWALReader(walPath)
	swapped := e.wal
	e.wal = nil // do not re-journal during replay
	defer func() { e.wal = swapped }()

	var replayed, skipped uint64
	err := reader.Replay(func(rec WALRecord) error {
		if rec.Seq <= lastSeq {
			skipped++
			return nil
		}
		switch rec.Op {
		case "order":
			if o := orderFromWALRecord(rec); o != nil {
				e.processOrder(o)
				replayed++
			}
		case "cancel":
			op := &cancelOp{orderID: rec.OrderID, userID: rec.UserID, resp: make(chan cancelResult, 1)}
			e.processCancel(op)
			replayed++
		}
		return nil
	})
	if err != nil {
		return err
	}
	if replayed > 0 || skipped > 0 {
		slog.Info("exchange replayed wal", "pair", e.Pair, "skipped", skipped, "replayed", replayed)
	}
	return nil
}

func orderFromWALRecord(rec WALRecord) *Order {
	price, _, _ := new(big.Float).Parse(rec.Price, 10)
	stopPrice, _, _ := new(big.Float).Parse(rec.StopPrice, 10)
	qty, _, _ := new(big.Float).Parse(rec.Quantity, 10)
	if rec.Quantity != "" && (qty == nil || qty.Sign() <= 0) {
		return nil
	}
	o := NewOrder(rec.UserID, rec.Pair, Side(rec.Side), OrderType(rec.Type), price, qty)
	o.ID = rec.OrderID
	o.ClientOrderID = rec.ClientOrderID
	o.StopPrice = stopPrice
	o.TimeInForce = TimeInForce(rec.TimeInForce)
	o.STP = SelfTradePreventionMode(rec.STP)
	o.CreatedAt = rec.Timestamp
	o.UpdatedAt = rec.Timestamp
	return o
}

func snapshotFileName(pair string) string {
	return strings.ReplaceAll(pair, "/", "-") + ".snapshot"
}

// Tickers returns a 24h ticker for every registered pair.
func (ex *ExchangeEngine) Tickers() map[string]*Ticker {
	ex.mu.RLock()
	defer ex.mu.RUnlock()
	out := make(map[string]*Ticker, len(ex.engines))
	for pair, e := range ex.engines {
		var bid, ask *big.Float
		if b := e.OrderBook.BestBid(); b != nil {
			bid = new(big.Float).Set(b.Price)
		}
		if a := e.OrderBook.BestAsk(); a != nil {
			ask = new(big.Float).Set(a.Price)
		}
		out[pair] = e.MD.Ticker(bid, ask)
	}
	return out
}

// Stop gracefully shuts down all engines and closes their WAL files.
func (ex *ExchangeEngine) Stop() {
	ex.mu.Lock()
	engines := make([]*MatchingEngine, 0, len(ex.engines))
	for _, e := range ex.engines {
		engines = append(engines, e)
	}
	ex.mu.Unlock()
	for _, e := range engines {
		e.Stop()
		if e.wal != nil {
			_ = e.wal.Close()
		}
	}
}

func normalizePair(pair string) string {
	return strings.ToUpper(strings.ReplaceAll(pair, "-", "/"))
}

func walFileName(pair string) string {
	return strings.ReplaceAll(pair, "/", "-") + ".wal"
}

// OrderRequest is the normalized input used by the risk engine and the exchange
// facade. Defined here to avoid an import cycle with the risk engine.
type OrderRequest struct {
	UserID   string
	Pair     string
	Side     Side
	Type     OrderType
	Price    *big.Float
	Quantity *big.Float
}

// Notional returns price * quantity, or quantity for market orders where price
// is unknown.
func (r *OrderRequest) Notional() *big.Float {
	q := new(big.Float).SetPrec(128)
	if r.Quantity != nil {
		q.Set(r.Quantity)
	}
	if r.Price != nil && r.Price.Sign() > 0 {
		return new(big.Float).Mul(r.Price, q)
	}
	return q
}
