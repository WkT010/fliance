package matching

import (
	"fmt"
	"log/slog"
	"math/big"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

type FillNotification struct {
	TakerOrderID, MakerOrderID, TakerUserID, MakerUserID, Pair string
	Side                                                       Side
	Price, Quantity, RemainingQty                              *big.Float
	TakerFilled, MakerFilled                                   bool
}

type FeeSchedule interface {
	TakerRate(pair string) *big.Float
	MakerRate(pair string) *big.Float
}

type EngineStats struct {
	OrdersReceived, OrdersMatched, OrdersRejected, OrdersCancelled, TradesExecuted atomic.Uint64
	// TradesDropped / FillsDropped count events dropped because the outbound
	// channels were full (bounded emit policy, see emitTrade/emitFill).
	// IMPORTANT: every drop is a potential loss of funds (a trade executed in
	// the book but never settled/broadcast). Operators MUST wire these
	// counters (and the matching CRITICAL log lines) to alerting.
	TradesDropped, FillsDropped atomic.Uint64
	// WALErrors counts write-ahead-log append failures. A non-zero value means
	// accepted orders/cancels could not be journaled and were rejected to
	// preserve durability; alert immediately.
	WALErrors atomic.Uint64
}

type MatchingEngine struct {
	Pair      string
	OrderBook *OrderBook
	Inbound   *MPRingBuffer
	Trades    chan *Trade
	Fills     chan *FillNotification
	Stats     EngineStats
	MD        *MarketDataRecorder
	cancels   chan *cancelOp
	done      chan struct{}
	stp       SelfTradePreventionMode
	wal       *WALWriter
	// degraded is latched true after any WAL append failure: the engine keeps
	// matching but is no longer durable, so readiness probes must fail.
	degraded atomic.Bool
}

type cancelOp struct {
	orderID, userID string
	resp            chan cancelResult
}

type cancelResult struct {
	order *Order
	err   error
}

func NewMatchingEngine(pair string, ringCap uint64) *MatchingEngine {
	return &MatchingEngine{
		Pair:      pair,
		OrderBook: NewOrderBook(pair),
		Inbound:   NewMPRingBuffer(ringCap),
		Trades:    make(chan *Trade, 100_000),
		Fills:     make(chan *FillNotification, 100_000),
		cancels:   make(chan *cancelOp, 1024),
		done:      make(chan struct{}),
		stp:       STPDisabled,
		MD:        NewMarketDataRecorder(pair),
	}
}

func (e *MatchingEngine) SetSelfTradePrevention(mode SelfTradePreventionMode) { e.stp = mode }
func (e *MatchingEngine) SetWAL(w *WALWriter)                                 { e.wal = w }
func (e *MatchingEngine) Start()                                              { go e.run() }
func (e *MatchingEngine) Stop()                                               { close(e.done) }

func (e *MatchingEngine) SubmitOrder(order *Order) bool {
	if order == nil {
		return false
	}
	e.Stats.OrdersReceived.Add(1)
	return e.Inbound.Enqueue(order)
}

func (e *MatchingEngine) SubmitOrderSync(order *Order) (*Order, error) {
	if order == nil {
		return nil, ErrOrderNotFound
	}
	order.done = make(chan struct{})
	if !e.SubmitOrder(order) {
		return nil, ErrEngineBusy
	}
	select {
	case <-order.done:
		// The engine surfaces processing errors (e.g. WAL append failure) via
		// order.Err; a rejected-by-durability order is never matched.
		if order.Err != nil {
			return nil, order.Err
		}
		return order, nil
	case <-e.done:
		return nil, ErrEngineStopped
	}
}

// IsHealthy reports whether the engine can accept orders durably. It turns
// false after any WAL append failure (durability gap) so /ready probes stop
// routing traffic to this instance until the storage issue is resolved.
func (e *MatchingEngine) IsHealthy() bool { return !e.degraded.Load() }

// WALFailureCount returns the number of WAL append failures observed.
func (e *MatchingEngine) WALFailureCount() uint64 { return e.Stats.WALErrors.Load() }

var ErrEngineBusy = errEngineBusy{}

type errEngineBusy struct{}

func (errEngineBusy) Error() string { return "matching engine buffer full" }

var ErrEngineStopped = errEngineStopped{}

type errEngineStopped struct{}

func (errEngineStopped) Error() string { return "matching engine stopped" }

func (e *MatchingEngine) Cancel(orderID, userID string) (*Order, error) {
	op := &cancelOp{orderID: orderID, userID: userID, resp: make(chan cancelResult, 1)}
	select {
	case e.cancels <- op:
		select {
		case r := <-op.resp:
			return r.order, r.err
		case <-e.done:
			return nil, ErrEngineStopped
		}
	case <-e.done:
		return nil, ErrEngineStopped
	}
}

// CancelAll cancels every live order belonging to userID and returns the number
// of orders cancelled.
func (e *MatchingEngine) CancelAll(userID string) int {
	orders := e.OrderBook.GetOrdersByUser(userID)
	count := 0
	for _, o := range orders {
		if _, err := e.Cancel(o.ID, userID); err == nil {
			count++
		}
	}
	return count
}

func (e *MatchingEngine) RecentTrades(limit int) []*Trade {
	if e.MD == nil {
		return nil
	}
	return e.MD.RecentTrades(limit)
}

// ── Main loop ──

// engineIdleTimeout is the fallback wake-up interval for the idle matching
// loop. It only guards against lost signals; normal wake-ups happen via the
// Inbound notify channel with sub-millisecond latency.
const engineIdleTimeout = 250 * time.Millisecond

func (e *MatchingEngine) run() {
	slog.Info("engine started", "pair", e.Pair)
	idle := time.NewTimer(engineIdleTimeout)
	defer idle.Stop()
	for {
		select {
		case <-e.done:
			slog.Info("engine stopped", "pair", e.Pair)
			return
		case op := <-e.cancels:
			e.processCancel(op)
			continue
		default:
		}
		orders := e.Inbound.Drain()
		if len(orders) > 0 {
			for _, o := range orders {
				if o != nil {
					e.processOrder(o)
				}
			}
			e.triggerStops()
			continue
		}
		// Drain stopped early on a reserved-but-unpublished slot (slow
		// producer). Yield briefly so the producer can run and publish; the
		// next loop iteration retries the same slot and nothing is skipped.
		if e.Inbound.DrainStalled() {
			time.Sleep(drainStallYield)
			continue
		}
		// Idle: block (near-zero CPU) until an order signal arrives, a cancel
		// is requested, the engine stops, or the fallback timeout fires.
		if !idle.Stop() {
			select {
			case <-idle.C:
			default:
			}
		}
		idle.Reset(engineIdleTimeout)
		select {
		case <-e.done:
			slog.Info("engine stopped", "pair", e.Pair)
			return
		case op := <-e.cancels:
			e.processCancel(op)
		case <-e.Inbound.Notify():
		case <-idle.C:
		}
	}
}

func (e *MatchingEngine) processCancel(op *cancelOp) {
	if err := e.wal.AppendCancel(op.orderID, op.userID); err != nil {
		// Durability gap: refuse the cancel instead of applying it only in
		// memory (it would vanish on recovery and the user would see a
		// cancelled order resurrect after restart).
		e.markWALFailure("cancel", op.orderID, err)
		op.resp <- cancelResult{err: err}
		return
	}
	ob := e.OrderBook
	ob.mu.Lock()
	defer ob.mu.Unlock()
	o, ok := ob.orders[op.orderID]
	if !ok {
		op.resp <- cancelResult{err: ErrOrderNotFound}
		return
	}
	if op.userID != "" && o.UserID != op.userID {
		op.resp <- cancelResult{err: ErrOrderNotOwned}
		return
	}
	ob.removeLocked(o.ID) // updates status, seqNo and the per-user index
	e.Stats.OrdersCancelled.Add(1)
	op.resp <- cancelResult{order: o}
}

func (e *MatchingEngine) processOrder(order *Order) {
	if err := e.wal.AppendOrder(order); err != nil {
		// Durability gap: the order was accepted into the inbound queue, but
		// journaling it failed. Matching it anyway would silently lose it on
		// crash/recovery, so reject and surface the error to the submitter
		// (via order.Err / SubmitOrderSync).
		e.markWALFailure("order", order.ID, err)
		order.Status = Rejected
		order.Err = fmt.Errorf("wal append failed: %w", err)
		e.Stats.OrdersRejected.Add(1)
		order.UpdatedAt = nowNanos()
		if order.done != nil {
			close(order.done)
		}
		return
	}
	ob := e.OrderBook
	ob.mu.Lock()
	defer ob.mu.Unlock()
	if order.Quantity == nil || order.Quantity.Sign() <= 0 {
		order.Status = Rejected
		e.Stats.OrdersRejected.Add(1)
		return
	}
	if order.RemainingQty == nil || order.RemainingQty.Sign() <= 0 {
		order.RemainingQty = newBigFloatCopy(order.Quantity)
	}
	switch order.Type {
	case Market:
		e.matchMarketLocked(order)
	case Limit:
		e.matchLimitLocked(order)
	case FillOrKill:
		e.matchFOKLocked(order)
	case ImmediateOrCancel:
		e.matchIOCLocked(order)
	case Iceberg:
		e.matchIcebergLocked(order)
	case StopLoss:
		e.matchStopLocked(order, false)
	case StopLimit:
		e.matchStopLocked(order, true)
	case PostOnly:
		e.matchPostOnlyLocked(order)
	default:
		e.matchLimitLocked(order)
	}
	if order.Status == Filled || order.Status == PartiallyFilled {
		e.Stats.OrdersMatched.Add(1)
	}
	order.UpdatedAt = nowNanos()
	if order.done != nil {
		close(order.done)
	}
}

// markWALFailure records a WAL append failure: CRITICAL log, WALErrors counter
// and the degraded latch so IsHealthy() (and thus /ready) reports unhealthy.
func (e *MatchingEngine) markWALFailure(op string, id string, err error) {
	e.Stats.WALErrors.Add(1)
	e.degraded.Store(true)
	slog.Error("CRITICAL: wal append failed, operation rejected (durability gap)",
		"pair", e.Pair, "op", op, "order_id", id, "severity", "CRITICAL", "err", err)
}

// ── Matching strategies ──

func (e *MatchingEngine) matchLimitLocked(order *Order) {
	for order.RemainingQty.Sign() > 0 {
		best := e.bestOppositeLocked(order.Side)
		if best == nil {
			break
		}
		if order.Side == Buy && best.Price.Cmp(order.Price) > 0 {
			break
		}
		if order.Side == Sell && best.Price.Cmp(order.Price) < 0 {
			break
		}
		if e.isSelfTrade(order, best) {
			if !e.applySTP(order, best) {
				break
			}
			continue
		}
		e.fillLocked(order, best)
	}
	if order.RemainingQty.Sign() > 0 && order.TimeInForce == GTC {
		e.OrderBook.addLocked(order)
		if order.Status != PartiallyFilled {
			order.Status = New
		}
	}
}

func (e *MatchingEngine) matchMarketLocked(order *Order) {
	for order.RemainingQty.Sign() > 0 {
		best := e.bestOppositeLocked(order.Side)
		if best == nil {
			break
		}
		if e.isSelfTrade(order, best) {
			if !e.applySTP(order, best) {
				break
			}
			continue
		}
		e.fillLocked(order, best)
	}
	if order.RemainingQty.Sign() > 0 {
		if order.FilledQty.Sign() > 0 {
			order.Status = PartiallyFilled
		} else {
			order.Status = Cancelled
		}
	}
}

func (e *MatchingEngine) matchFOKLocked(order *Order) {
	available := newBigFloat()
	for _, o := range e.collectOppositeLocked(order.Side) {
		if order.Price != nil {
			if (order.Side == Buy && o.Price.Cmp(order.Price) > 0) || (order.Side == Sell && o.Price.Cmp(order.Price) < 0) {
				continue
			}
		}
		if e.isSelfTrade(order, o) {
			continue
		}
		available.Add(available, o.RemainingQty)
	}
	if available.Cmp(order.Quantity) < 0 {
		order.Status = Rejected
		e.Stats.OrdersRejected.Add(1)
		return
	}
	e.matchLimitLocked(order)
	if order.RemainingQty.Sign() > 0 {
		order.Status = Rejected
	}
}

func (e *MatchingEngine) matchIOCLocked(order *Order) {
	for order.RemainingQty.Sign() > 0 {
		best := e.bestOppositeLocked(order.Side)
		if best == nil {
			break
		}
		if order.Price != nil {
			if (order.Side == Buy && best.Price.Cmp(order.Price) > 0) || (order.Side == Sell && best.Price.Cmp(order.Price) < 0) {
				break
			}
		}
		if e.isSelfTrade(order, best) {
			if !e.applySTP(order, best) {
				break
			}
			continue
		}
		e.fillLocked(order, best)
	}
	if order.RemainingQty.Sign() > 0 {
		if order.FilledQty.Sign() > 0 {
			order.Status = PartiallyFilled
		} else {
			order.Status = Cancelled
		}
	}
}

func (e *MatchingEngine) matchPostOnlyLocked(order *Order) {
	if order.Price == nil || order.Price.Sign() <= 0 {
		order.Status = Rejected
		e.Stats.OrdersRejected.Add(1)
		return
	}
	best := e.bestOppositeLocked(order.Side)
	if best != nil {
		wouldCross := (order.Side == Buy && best.Price.Cmp(order.Price) <= 0) || (order.Side == Sell && best.Price.Cmp(order.Price) >= 0)
		if wouldCross {
			order.Status = Rejected
			e.Stats.OrdersRejected.Add(1)
			return
		}
	}
	e.OrderBook.addLocked(order)
	order.Status = New
}

func (e *MatchingEngine) matchStopLocked(order *Order, isLimit bool) {
	if order.StopPrice == nil || order.StopPrice.Sign() <= 0 {
		order.Status = Rejected
		e.Stats.OrdersRejected.Add(1)
		return
	}
	if e.triggerStopLocked(order) {
		if isLimit {
			order.Type = Limit
			e.matchLimitLocked(order)
		} else {
			order.Type = Market
			e.matchMarketLocked(order)
		}
		return
	}
	e.OrderBook.addStopLocked(order)
	order.Status = New
}

func (e *MatchingEngine) matchIcebergLocked(order *Order) {
	for order.RemainingQty.Sign() > 0 {
		if order.VisibleQty == nil || order.VisibleQty.Sign() <= 0 {
			break
		}
		vis := &Order{ID: order.ID + ":ice", UserID: order.UserID, Pair: order.Pair, Side: order.Side, Type: Limit, Price: newBigFloatCopy(order.Price), Quantity: newBigFloatCopy(order.VisibleQty), RemainingQty: newBigFloatCopy(order.VisibleQty), FilledQty: newBigFloat(), TimeInForce: IOC, Status: New, CreatedAt: order.CreatedAt, STP: order.STP}
		e.matchIOCLocked(vis)
		order.FilledQty.Add(order.FilledQty, vis.FilledQty)
		order.RemainingQty.Sub(order.RemainingQty, vis.FilledQty)
		if order.RemainingQty.Sign() > 0 && order.IcebergQty != nil && order.IcebergQty.Sign() > 0 {
			nv := newBigFloat()
			if order.RemainingQty.Cmp(order.IcebergQty) >= 0 {
				nv.Set(order.IcebergQty)
			} else {
				nv.Set(order.RemainingQty)
			}
			order.VisibleQty = nv
		}
	}
	if order.RemainingQty.Sign() == 0 {
		order.Status = Filled
	} else if order.FilledQty.Sign() > 0 {
		order.Status = PartiallyFilled
	} else {
		order.Status = Cancelled
	}
}

func (e *MatchingEngine) triggerStopLocked(order *Order) bool {
	last := e.OrderBook.LastTradePrice()
	if last == nil || last.Sign() <= 0 {
		return false
	}
	if order.Side == Buy {
		return last.Cmp(order.StopPrice) >= 0
	}
	return last.Cmp(order.StopPrice) <= 0
}

func (e *MatchingEngine) triggerStops() {
	ob := e.OrderBook
	ob.mu.Lock()
	defer ob.mu.Unlock()
	if len(ob.stops) == 0 {
		return
	}
	last := ob.LastTradePrice()
	if last == nil || last.Sign() <= 0 {
		return
	}
	var triggered, pending []*Order
	for _, o := range ob.stops {
		fire := (o.Side == Buy && last.Cmp(o.StopPrice) >= 0) || (o.Side == Sell && last.Cmp(o.StopPrice) <= 0)
		if fire {
			triggered = append(triggered, o)
		} else {
			pending = append(pending, o)
		}
	}
	ob.stops = pending
	for _, o := range triggered {
		if o.Type == StopLoss {
			o.Type = Market
			e.matchMarketLocked(o)
		} else {
			o.Type = Limit
			e.matchLimitLocked(o)
		}
	}
}

func (e *MatchingEngine) bestOppositeLocked(side Side) *Order {
	if side == Buy {
		return e.OrderBook.bestAskLocked()
	}
	return e.OrderBook.bestBidLocked()
}

func (e *MatchingEngine) collectOppositeLocked(side Side) []*Order {
	if side == Buy {
		return e.OrderBook.asks.orders
	}
	return e.OrderBook.bids.orders
}

func (e *MatchingEngine) isSelfTrade(taker, maker *Order) bool {
	if taker.STP == STPDisabled && e.stp == STPDisabled {
		return false
	}
	return taker.UserID == maker.UserID && taker.UserID != ""
}

func (e *MatchingEngine) applySTP(taker, maker *Order) bool {
	mode := taker.STP
	if mode == STPDisabled {
		mode = e.stp
	}
	switch mode {
	case STPCancelTaker:
		taker.Status = Cancelled
		return false
	case STPCancelMaker:
		e.OrderBook.removeLocked(maker.ID)
		return true
	case STPCancelBoth:
		e.OrderBook.removeLocked(maker.ID)
		taker.Status = Cancelled
		return false
	default:
		return true
	}
}

// ── Fill execution ──

func (e *MatchingEngine) fillLocked(taker, maker *Order) {
	fillQty := newBigFloat()
	if taker.RemainingQty.Cmp(maker.RemainingQty) >= 0 {
		fillQty.Set(maker.RemainingQty)
	} else {
		fillQty.Set(taker.RemainingQty)
	}
	if fillQty.Sign() <= 0 {
		return
	}
	taker.FilledQty.Add(taker.FilledQty, fillQty)
	taker.RemainingQty.Sub(taker.RemainingQty, fillQty)
	maker.FilledQty.Add(maker.FilledQty, fillQty)
	maker.RemainingQty.Sub(maker.RemainingQty, fillQty)
	if taker.RemainingQty.Sign() == 0 {
		taker.Status = Filled
	} else {
		taker.Status = PartiallyFilled
	}
	makerFilled := false
	if maker.RemainingQty.Sign() == 0 {
		maker.Status = Filled
		e.OrderBook.removeLocked(maker.ID)
		makerFilled = true
	} else {
		maker.Status = PartiallyFilled
	}
	maker.UpdatedAt = nowNanos()
	execPrice := newBigFloatCopy(maker.Price)
	e.OrderBook.setLastTradePrice(execPrice)
	buyOrder, sellOrder := taker, maker
	if taker.Side == Sell {
		buyOrder, sellOrder = maker, taker
	}
	trade := &Trade{ID: uuid.NewString(), BuyOrderID: buyOrder.ID, SellOrderID: sellOrder.ID, BuyerID: buyOrder.UserID, SellerID: sellOrder.UserID, Pair: e.Pair, Price: newBigFloatCopy(execPrice), Quantity: newBigFloatCopy(fillQty), TakerSide: taker.Side, Fee: newBigFloat(), CreatedAt: nowNanos()}
	e.Stats.TradesExecuted.Add(1)
	if e.MD != nil {
		e.MD.RecordTrade(trade)
	}
	e.emitTrade(trade)
	e.emitFill(taker, maker, fillQty, makerFilled)
}

// ── Bounded non-blocking emit ──
// Trades/Fills are fixed-capacity channels. When full, events are dropped with
// a CRITICAL log entry and a drop counter increment instead of spawning
// unbounded goroutines, so the matching loop is never blocked and goroutine
// count stays constant under high-frequency fills.
//
// ALERTING CONTRACT: a dropped trade/fill means an execution happened in the
// book but was never settled/broadcast — potential loss of funds. Drops are
// logged at CRITICAL level and counted in Stats.TradesDropped/Stats.FillsDropped;
// both MUST be wired to operator alerting.

func (e *MatchingEngine) emitTrade(trade *Trade) {
	select {
	case e.Trades <- trade:
	default:
		e.Stats.TradesDropped.Add(1)
		slog.Error("CRITICAL: trade dropped (trade channel full)",
			"pair", e.Pair, "trade_id", trade.ID, "severity", "CRITICAL")
	}
}

func (e *MatchingEngine) emitFill(taker, maker *Order, qty *big.Float, makerFilled bool) {
	notif := &FillNotification{
		TakerOrderID: taker.ID, MakerOrderID: maker.ID,
		TakerUserID: taker.UserID, MakerUserID: maker.UserID,
		Pair: e.Pair, Side: taker.Side,
		Price: newBigFloatCopy(maker.Price), Quantity: newBigFloatCopy(qty),
		RemainingQty: newBigFloatCopy(taker.RemainingQty),
		TakerFilled:  taker.Status == Filled, MakerFilled: makerFilled,
	}
	select {
	case e.Fills <- notif:
	default:
		e.Stats.FillsDropped.Add(1)
		slog.Error("CRITICAL: fill dropped (fill channel full)",
			"pair", e.Pair, "taker_order_id", taker.ID, "severity", "CRITICAL")
	}
}
