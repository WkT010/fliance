package matching

import (
	"log"
	"math/big"
)

type MatchingEngine struct {
	Pair      string
	OrderBook *OrderBook
	Inbound   *MPRingBuffer
	Trades    chan *Trade
	Fills     chan *FillNotification
	done      chan struct{}
}

type FillNotification struct {
	TakerOrderID, MakerOrderID, Pair string
	Side Side
	Price, Quantity, RemainingQty *big.Float
	Filled bool
}

func NewMatchingEngine(pair string, ringCap uint64) *MatchingEngine {
	return &MatchingEngine{
		Pair: pair, OrderBook: NewOrderBook(pair),
		Inbound: NewMPRingBuffer(ringCap),
		Trades: make(chan *Trade, 10000),
		Fills:  make(chan *FillNotification, 10000),
		done:   make(chan struct{}),
	}
}

func (e *MatchingEngine) Start() { go e.run() }
func (e *MatchingEngine) Stop() { close(e.done) }
func (e *MatchingEngine) SubmitOrder(order *Order) bool { return e.Inbound.Enqueue(order) }

func (e *MatchingEngine) run() {
	log.Printf("[engine-%s] started", e.Pair)
	for {
		select {
		case <-e.done: return
		default:
			for _, o := range e.Inbound.Drain() { e.processOrder(o) }
		}
	}
}

func (e *MatchingEngine) processOrder(order *Order) {
	switch order.Type {
	case Market: e.matchMarket(order)
	case Limit: e.matchLimit(order)
	case FillOrKill: e.matchFOK(order)
	case ImmediateOrCancel: e.matchIOC(order)
	case Iceberg: e.matchIceberg(order)
	default: e.matchLimit(order)
	}
}

func (e *MatchingEngine) matchLimit(order *Order) {
	for order.RemainingQty.Sign() > 0 {
		var best *Order
		if order.Side == Buy { best = e.OrderBook.BestAsk(); if best == nil || best.Price.Cmp(order.Price) > 0 { break } } else { best = e.OrderBook.BestBid(); if best == nil || best.Price.Cmp(order.Price) < 0 { break } }
		e.fill(order, best)
	}
	if order.RemainingQty.Sign() > 0 && order.TimeInForce == GTC { e.OrderBook.Add(order) }
}

func (e *MatchingEngine) matchMarket(order *Order) {
	for order.RemainingQty.Sign() > 0 {
		var best *Order
		if order.Side == Buy { best = e.OrderBook.BestAsk() } else { best = e.OrderBook.BestBid() }
		if best == nil { break }
		e.fill(order, best)
	}
}

func (e *MatchingEngine) matchFOK(order *Order) {
	available := big.NewFloat(0)
	if order.Side == Buy { for _, o := range e.OrderBook.asks.orders { available.Add(available, o.RemainingQty) } } else { for _, o := range e.OrderBook.bids.orders { available.Add(available, o.RemainingQty) } }
	if available.Cmp(order.Quantity) < 0 { order.Status = Rejected; return }
	e.matchLimit(order)
}

func (e *MatchingEngine) matchIOC(order *Order) {
	e.matchMarket(order)
	if order.RemainingQty.Sign() > 0 { order.Status = Cancelled }
}

func (e *MatchingEngine) matchIceberg(order *Order) {
	for order.RemainingQty.Sign() > 0 {
		vis := new(big.Float).Copy(order.VisibleQty)
		if vis == nil || vis.Sign() <= 0 { break }
		visOrder := &Order{ID: order.ID + ":ice", UserID: order.UserID, Pair: order.Pair, Side: order.Side, Type: Limit, Price: order.Price, Quantity: vis, RemainingQty: vis, FilledQty: big.NewFloat(0), TimeInForce: IOC, Status: New, CreatedAt: order.CreatedAt}
		e.matchIOC(visOrder)
		order.FilledQty.Add(order.FilledQty, visOrder.FilledQty)
		order.RemainingQty.Sub(order.RemainingQty, visOrder.FilledQty)
		if order.RemainingQty.Sign() > 0 {
			if order.RemainingQty.Cmp(order.IcebergQty) >= 0 { order.VisibleQty = new(big.Float).Copy(order.IcebergQty) } else { order.VisibleQty = new(big.Float).Copy(order.RemainingQty) }
		}
	}
	if order.RemainingQty.Sign() == 0 { order.Status = Filled }
}

func (e *MatchingEngine) fill(taker, maker *Order) {
	fillQty := new(big.Float)
	if taker.RemainingQty.Cmp(maker.RemainingQty) >= 0 { fillQty.Copy(maker.RemainingQty) } else { fillQty.Copy(taker.RemainingQty) }
	taker.FilledQty.Add(taker.FilledQty, fillQty)
	taker.RemainingQty.Sub(taker.RemainingQty, fillQty)
	maker.FilledQty.Add(maker.FilledQty, fillQty)
	maker.RemainingQty.Sub(maker.RemainingQty, fillQty)
	if taker.RemainingQty.Sign() == 0 { taker.Status = Filled } else { taker.Status = PartiallyFilled }
	if maker.RemainingQty.Sign() == 0 { maker.Status = Filled; e.OrderBook.Remove(maker.ID) } else { maker.Status = PartiallyFilled }
	select { case e.Trades <- &Trade{BuyOrderID: taker.ID, SellOrderID: maker.ID, Pair: e.Pair, Price: maker.Price, Quantity: fillQty, CreatedAt: taker.CreatedAt}: default: }
}