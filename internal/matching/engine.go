package matching

import (
	"log"
	"math/big"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// FillNotification is emitted for every (partial) fill so downstream services
// (wallet settlement, websocket, persistence) can react.
type FillNotification struct {
	TakerOrderID string
	MakerOrderID string
	TakerUserID  string
	MakerUserID  string
	Pair         string
	Side         Side // taker side
	Price        *big.Float
	Quantity     *big.Float
	RemainingQty *big.Float
	TakerFilled  bool
	MakerFilled  bool
}

// FeeSchedule computes trading fees. Implementations live in the wallet/settlement
// layer; the engine itself does not charge fees, it only records the taker side.
type FeeSchedule interface {
	TakerRate(pair string) *big.Float
	MakerRate(pair string) *big.Float
}

// EngineStats exposes runtime counters for observability.
type EngineStats struct {
	OrdersReceived atomic.Uint64
	OrdersMatched  atomic.Uint64
	OrdersRejected atomic.Uint64
	OrdersCancelled atomic.Uint64
	TradesExecuted atomic.Uint64
}

// MatchingEngine matches orders for a single trading pair. It owns its OrderBook
// and processes inbound orders from a lock-free MPSC ring buffer on a dedicated
// goroutine. Cancels are routed through the same goroutine via a channel so that
// the book is never mutated concurrently.
type MatchingEngine struct {
	Pair      string
	OrderBook *OrderBook
	Inbound   *MPRingBuffer
	Trades    chan *Trade
	Fills     chan *FillNotification
	Stats     EngineStats
	MD        *MarketDataRecorder

	cancels chan *cancelOp
	done    chan struct{}
	stp     SelfTradePreventionMode
}

type cancelOp struct {
	orderID string
	userID  string
	resp    chan cancelResult
}

type cancelResult struct {
	order *Order
	err   error
}

// NewMatchingEngine constructs a new engine. ringCap is rounded up to a power of
// two. The default STP mode is disabled; callers can opt in per-order via
// Order.STP or globally via SetSelfTradePrevention.
func NewMatchingEngine(pair string, ringCap uint64) *MatchingEngine {
	return &MatchingEngine{
		Pair:      pair,
		OrderBook: NewOrderBook(pair),
		Inbound:   NewMPRingBuffer(ringCap),
		Trades:    make(chan *Trade, 10000),
		Fills:     make(chan *FillNotification, 10000),
		cancels:   make(chan *cancelOp, 1024),
		done:      make(chan struct{}),
		stp:       STPDisabled,
		MD:        NewMarketDataRecorder(pair),
	}
}

// SetSelfTradePrevention configures the default STP mode. Must be called before
// Start.
func (e *MatchingEngine) SetSelfTradePrevention(mode SelfTradePreventionMode) {
	e.stp = mode
}

// Start launches the matching goroutine.
func (e *MatchingEngine) Start() { go e.run() }

// Stop signals the matching goroutine to exit.
func (e *MatchingEngine) Stop() { close(e.done) }

// SubmitOrder enqueues a new order for matching. Returns false if the inbound
// buffer is full.
func (e *MatchingEngine) SubmitOrder(order *Order) bool {
	if order == nil {
		return false
	}
	e.Stats.OrdersReceived.Add(1)
	return e.Inbound.Enqueue(order)
}

// SubmitOrderSync submits an order and blocks until the engine has finished
// processing it, returning the same order pointer with its final Status /
// FilledQty populated. It is safe to read the returned order's fields. If the
// engine stops before processing, ErrEngineStopped is returned.
func (e *MatchingEngine) SubmitOrderSync(order *Order) (*Order, error) {
	if order == nil {
		return nil, ErrOrderNotFound
	}
	order.done = make(chan struct{})
	if !e.SubmitOrder(order) {
		// Buffer full; nothing to close since the engine will never see it.
		return nil, ErrEngineBusy
	}
	select {
	case <-order.done:
		return order, nil
	case <-e.done:
		return nil, ErrEngineStopped
	}
}

// ErrEngineBusy is returned when the inbound ring buffer is full.
var ErrEngineBusy = errEngineBusy{}

type errEngineBusy struct{}

func (errEngineBusy) Error() string { return "matching engine buffer full" }

// Cancel requests cancellation of a resting order. The request is processed on
// the matching goroutine so it is naturally serialised with matching. It returns
// the cancelled order (or an error if not found / not owned).
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

// CancelAll cancels every resting order belonging to userID for this pair.
func (e *MatchingEngine) CancelAll(userID string) int {
	e.OrderBook.mu.Lock()
	defer e.OrderBook.mu.Unlock()
	count := 0
	for id, o := range e.OrderBook.orders {
		if userID != "" && o.UserID != userID {
			continue
		}
		delete(e.OrderBook.orders, id)
		o.Status = Cancelled
		o.UpdatedAt = nowNanos()
		count++
	}
	// Also cancel parked stop orders belonging to the user.
	if len(e.OrderBook.stops) > 0 {
		pending := e.OrderBook.stops[:0]
		for _, o := range e.OrderBook.stops {
			if userID != "" && o.UserID != userID {
				pending = append(pending, o)
				continue
			}
			o.Status = Cancelled
			o.UpdatedAt = nowNanos()
			count++
		}
		e.OrderBook.stops = pending
	}
	if count > 0 {
		e.OrderBook.seqNo.Add(1)
		e.Stats.OrdersCancelled.Add(uint64(count))
	}
	return count
}

// CancelOrder is a fire-and-forget alias for Cancel that does not wait for the
// engine to acknowledge. Useful when the caller does not need the cancelled
// order back (e.g. gRPC fire-and-forget path).
func (e *MatchingEngine) CancelOrder(orderID string) {
	op := &cancelOp{orderID: orderID, resp: make(chan cancelResult, 1)}
	select {
	case e.cancels <- op:
	default:
		// Cancel queue full; best-effort.
	}
}

// RecentTrades returns up to `limit` most recent trades recorded by the engine's
// market-data recorder (newest first).
func (e *MatchingEngine) RecentTrades(limit int) []*Trade {
	if e.MD == nil {
		return nil
	}
	return e.MD.RecentTrades(limit)
}

var ErrEngineStopped = errEngineStopped{}

type errEngineStopped struct{}

func (errEngineStopped) Error() string { return "matching engine stopped" }

func (e *MatchingEngine) run() {
	log.Printf("[engine-%s] started", e.Pair)
	idleSleep := time.Millisecond
	activeSleep := 10 * time.Microsecond
	for {
		// Prioritise stop signal and cancels over new orders.
		select {
		case <-e.done:
			log.Printf("[engine-%s] stopped", e.Pair)
			return
		case op := <-e.cancels:
			e.processCancel(op)
			continue
		default:
		}

		orders := e.Inbound.Drain()
		if len(orders) == 0 {
			time.Sleep(idleSleep)
			continue
		}
		for _, o := range orders {
			if o == nil {
				continue
			}
			e.processOrder(o)
		}
		e.triggerStops()
		if len(orders) < 100 {
			time.Sleep(activeSleep)
		}
	}
}

func (e *MatchingEngine) processCancel(op *cancelOp) {
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
	delete(ob.orders, o.ID)
	o.Status = Cancelled
	o.UpdatedAt = nowNanos()
	ob.seqNo.Add(1)
	e.Stats.OrdersCancelled.Add(1)
	op.resp <- cancelResult{order: o}
}

func (e *MatchingEngine) processOrder(order *Order) {
	ob := e.OrderBook
	ob.mu.Lock()
	defer ob.mu.Unlock()

	// Validate basic preconditions.
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

// matchLimitLocked matches a limit (GTC) order against the book, then rests the
// remainder.
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
			if order.Side == Buy && o.Price.Cmp(order.Price) > 0 {
				continue
			}
			if order.Side == Sell && o.Price.Cmp(order.Price) < 0 {
				continue
			}
		}
		// Skip opposite orders that would be treated as self-trades, since
		// STP would cancel one leg and the FOK would not actually fill.
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
		// FOK must fully fill; cancel any remainder.
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
			if order.Side == Buy && best.Price.Cmp(order.Price) > 0 {
				break
			}
			if order.Side == Sell && best.Price.Cmp(order.Price) < 0 {
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

// matchPostOnlyLocked rejects the order if it would immediately match (cross the
// book); otherwise it rests as a maker order.
func (e *MatchingEngine) matchPostOnlyLocked(order *Order) {
	if order.Price == nil || order.Price.Sign() <= 0 {
		order.Status = Rejected
		e.Stats.OrdersRejected.Add(1)
		return
	}
	best := e.bestOppositeLocked(order.Side)
	if best != nil {
		wouldCross := false
		if order.Side == Buy {
			wouldCross = best.Price.Cmp(order.Price) <= 0
		} else {
			wouldCross = best.Price.Cmp(order.Price) >= 0
		}
		if wouldCross {
			order.Status = Rejected
			e.Stats.OrdersRejected.Add(1)
			return
		}
	}
	e.OrderBook.addLocked(order)
	order.Status = New
}

func (e *MatchingEngine) matchIcebergLocked(order *Order) {
	for order.RemainingQty.Sign() > 0 {
		visQty := order.VisibleQty
		if visQty == nil || visQty.Sign() <= 0 {
			break
		}
		visOrder := &Order{
			ID:           order.ID + ":ice:" + order.FilledQty.Text('f', 0),
			UserID:       order.UserID,
			Pair:         order.Pair,
			Side:         order.Side,
			Type:         Limit,
			Price:        newBigFloatCopy(order.Price),
			Quantity:     newBigFloatCopy(visQty),
			RemainingQty: newBigFloatCopy(visQty),
			FilledQty:    newBigFloat(),
			TimeInForce:  IOC,
			Status:       New,
			CreatedAt:    order.CreatedAt,
			STP:          order.STP,
		}
		e.matchIOCLocked(visOrder)
		order.FilledQty.Add(order.FilledQty, visOrder.FilledQty)
		order.RemainingQty.Sub(order.RemainingQty, visOrder.FilledQty)
		if order.RemainingQty.Sign() > 0 && order.IcebergQty != nil && order.IcebergQty.Sign() > 0 {
			newVis := newBigFloat()
			if order.RemainingQty.Cmp(order.IcebergQty) >= 0 {
				newVis.Set(order.IcebergQty)
			} else {
				newVis.Set(order.RemainingQty)
			}
			order.VisibleQty = newVis
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

// matchStopLocked evaluates a stop order against the current last trade price. If
// triggered, it converts to market (StopLoss) or limit (StopLimit) and matches.
// Otherwise it is parked in a separate stop book.
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
	// Park the stop order. We store it on the book's resting map under a
	// dedicated prefix so triggerStops can find it later, but it is NOT added
	// to the bid/ask heaps (it should not participate in matching until
	// triggered).
	e.OrderBook.addStopLocked(order)
	order.Status = New
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

// triggerStops re-evaluates parked stop orders after a batch of orders has been
// processed. It must be called with the book lock held by the caller path; here
// it takes the lock itself because it runs outside processOrder.
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
	triggered := make([]*Order, 0, len(ob.stops))
	pending := make([]*Order, 0, len(ob.stops))
	for _, o := range ob.stops {
		fire := false
		if o.Side == Buy {
			fire = last.Cmp(o.StopPrice) >= 0
		} else {
			fire = last.Cmp(o.StopPrice) <= 0
		}
		if fire {
			triggered = append(triggered, o)
		} else {
			pending = append(pending, o)
		}
	}
	ob.stops = pending
	for _, o := range triggered {
		switch o.Type {
		case StopLoss:
			o.Type = Market
			e.matchMarketLocked(o)
		case StopLimit:
			o.Type = Limit
			e.matchLimitLocked(o)
		}
	}
}

// bestOppositeLocked returns the best opposite-side live order. Caller must hold
// the book write lock.
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

// applySTP resolves a self-trade. Returns true if the taker should continue
// matching against the next maker, false if the taker should stop.
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

// fillLocked applies a fill between taker and maker. Caller must hold the book
// write lock.
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

	// Build the canonical trade with both counter-parties and taker side.
	buyOrder, sellOrder := taker, maker
	if taker.Side == Sell {
		buyOrder, sellOrder = maker, taker
	}
	trade := &Trade{
		ID:          uuid.NewString(),
		BuyOrderID:  buyOrder.ID,
		SellOrderID: sellOrder.ID,
		BuyerID:     buyOrder.UserID,
		SellerID:    sellOrder.UserID,
		Pair:        e.Pair,
		Price:       newBigFloatCopy(execPrice),
		Quantity:    newBigFloatCopy(fillQty),
		TakerSide:   taker.Side,
		Fee:         newBigFloat(),
		CreatedAt:   nowNanos(),
	}
	e.Stats.TradesExecuted.Add(1)
	if e.MD != nil {
		e.MD.RecordTrade(trade)
	}
	e.emitTrade(trade)
	e.emitFill(taker, maker, fillQty, makerFilled)
}

func (e *MatchingEngine) emitTrade(trade *Trade) {
	select {
	case e.Trades <- trade:
	default:
		log.Printf("[engine-%s] trades channel full, dropping trade %s", e.Pair, trade.ID)
	}
}

func (e *MatchingEngine) emitFill(taker, maker *Order, qty *big.Float, makerFilled bool) {
	notif := &FillNotification{
		TakerOrderID: taker.ID,
		MakerOrderID: maker.ID,
		TakerUserID:  taker.UserID,
		MakerUserID:  maker.UserID,
		Pair:         e.Pair,
		Side:         taker.Side,
		Price:        newBigFloatCopy(maker.Price),
		Quantity:     newBigFloatCopy(qty),
		RemainingQty: newBigFloatCopy(taker.RemainingQty),
		TakerFilled:  taker.Status == Filled,
		MakerFilled:  makerFilled,
	}
	select {
	case e.Fills <- notif:
	default:
		log.Printf("[engine-%s] fills channel full, dropping fill notification", e.Pair)
	}
}
