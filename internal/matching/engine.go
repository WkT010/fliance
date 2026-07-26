package matching

import (
	"log"
	"math/big"
	"time"
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
	TakerOrderID string
	MakerOrderID string
	Pair         string
	Side         Side
	Price        *big.Float
	Quantity     *big.Float
	RemainingQty *big.Float
	Filled       bool
}

func NewMatchingEngine(pair string, ringCap uint64) *MatchingEngine {
	return &MatchingEngine{
		Pair: pair, OrderBook: NewOrderBook(pair),
		Inbound: NewMPRingBuffer(ringCap),
		Trades:  make(chan *Trade, 10000),
		Fills:   make(chan *FillNotification, 10000),
		done:    make(chan struct{}),
	}
}

func (e *MatchingEngine) Start() { go e.run() }
func (e *MatchingEngine) Stop()  { close(e.done) }
func (e *MatchingEngine) SubmitOrder(order *Order) bool { return e.Inbound.Enqueue(order) }

func (e *MatchingEngine) run() {
	log.Printf("[engine-%s] started", e.Pair)
	for {
		select {
		case <-e.done:
			log.Printf("[engine-%s] stopped", e.Pair)
			return
		default:
			orders := e.Inbound.Drain()
			if len(orders) == 0 {
				time.Sleep(1 * time.Millisecond)
				continue
			}
			for _, o := range orders {
				if o == nil {
					continue
				}
				e.processOrder(o)
			}
			if len(orders) < 100 {
				time.Sleep(10 * time.Microsecond)
			}
		}
	}
}

func (e *MatchingEngine) processOrder(order *Order) {
	switch order.Type {
	case Market:
		e.matchMarket(order)
	case Limit:
		e.matchLimit(order)
	case FillOrKill:
		e.matchFOK(order)
	case ImmediateOrCancel:
		e.matchIOC(order)
	case Iceberg:
		e.matchIceberg(order)
	case StopLoss:
		e.matchStopLoss(order)
	case StopLimit:
		e.matchStopLimit(order)
	default:
		e.matchLimit(order)
	}
}

func (e *MatchingEngine) matchLimit(order *Order) {
	for order.RemainingQty.Sign() > 0 {
		var best *Order
		if order.Side == Buy {
			best = e.OrderBook.BestAsk()
			if best == nil || best.Price.Cmp(order.Price) > 0 {
				break
			}
		} else {
			best = e.OrderBook.BestBid()
			if best == nil || best.Price.Cmp(order.Price) < 0 {
				break
			}
		}
		e.fill(order, best)
	}
	if order.RemainingQty.Sign() > 0 && order.TimeInForce == GTC {
		e.OrderBook.Add(order)
		order.Status = New
	}
}

func (e *MatchingEngine) matchMarket(order *Order) {
	for order.RemainingQty.Sign() > 0 {
		var best *Order
		if order.Side == Buy {
			best = e.OrderBook.BestAsk()
		} else {
			best = e.OrderBook.BestBid()
		}
		if best == nil {
			break
		}
		e.fill(order, best)
	}
	if order.RemainingQty.Sign() > 0 {
		order.Status = Cancelled
	}
}

func (e *MatchingEngine) matchFOK(order *Order) {
	available := big.NewFloat(0)
	if order.Side == Buy {
		for _, o := range e.OrderBook.asks.orders {
			if o.RemainingQty.Sign() <= 0 {
				continue
			}
			if order.Price != nil && o.Price.Cmp(order.Price) > 0 {
				continue
			}
			available.Add(available, o.RemainingQty)
		}
	} else {
		for _, o := range e.OrderBook.bids.orders {
			if o.RemainingQty.Sign() <= 0 {
				continue
			}
			if order.Price != nil && o.Price.Cmp(order.Price) < 0 {
				continue
			}
			available.Add(available, o.RemainingQty)
		}
	}
	if available.Cmp(order.Quantity) < 0 {
		order.Status = Rejected
		return
	}
	e.matchLimit(order)
}

func (e *MatchingEngine) matchIOC(order *Order) {
	for order.RemainingQty.Sign() > 0 {
		var best *Order
		if order.Side == Buy {
			best = e.OrderBook.BestAsk()
		} else {
			best = e.OrderBook.BestBid()
		}
		if best == nil {
			break
		}
		if order.Price != nil {
			if (order.Side == Buy && best.Price.Cmp(order.Price) > 0) ||
				(order.Side == Sell && best.Price.Cmp(order.Price) < 0) {
				break
			}
		}
		e.fill(order, best)
	}
	if order.RemainingQty.Sign() > 0 {
		order.Status = Cancelled
	}
}

func (e *MatchingEngine) matchIceberg(order *Order) {
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
			Price:        order.Price,
			Quantity:     new(big.Float).Copy(visQty),
			RemainingQty: new(big.Float).Copy(visQty),
			FilledQty:    big.NewFloat(0),
			TimeInForce:  IOC,
			Status:       New,
			CreatedAt:    order.CreatedAt,
		}
		e.matchIOC(visOrder)
		order.FilledQty.Add(order.FilledQty, visOrder.FilledQty)
		order.RemainingQty.Sub(order.RemainingQty, visOrder.FilledQty)
		if order.RemainingQty.Sign() > 0 {
			newVis := new(big.Float)
			if order.RemainingQty.Cmp(order.IcebergQty) >= 0 {
				newVis.Copy(order.IcebergQty)
			} else {
				newVis.Copy(order.RemainingQty)
			}
			order.VisibleQty = newVis
		}
	}
	if order.RemainingQty.Sign() == 0 {
		order.Status = Filled
	}
}

func (e *MatchingEngine) matchStopLoss(order *Order) {
	if order.StopPrice == nil || order.StopPrice.Sign() <= 0 {
		order.Status = Rejected
		return
	}
	triggered := false
	if order.Side == Buy {
		if bestAsk := e.OrderBook.BestAsk(); bestAsk != nil && bestAsk.Price.Cmp(order.StopPrice) >= 0 {
			triggered = true
		}
	} else {
		if bestBid := e.OrderBook.BestBid(); bestBid != nil && bestBid.Price.Cmp(order.StopPrice) <= 0 {
			triggered = true
		}
	}
	if triggered {
		order.Type = Market
		e.matchMarket(order)
	} else {
		e.OrderBook.Add(order)
		order.Status = New
	}
}

func (e *MatchingEngine) matchStopLimit(order *Order) {
	if order.StopPrice == nil || order.StopPrice.Sign() <= 0 {
		order.Status = Rejected
		return
	}
	triggered := false
	if order.Side == Buy {
		if bestAsk := e.OrderBook.BestAsk(); bestAsk != nil && bestAsk.Price.Cmp(order.StopPrice) >= 0 {
			triggered = true
		}
	} else {
		if bestBid := e.OrderBook.BestBid(); bestBid != nil && bestBid.Price.Cmp(order.StopPrice) <= 0 {
			triggered = true
		}
	}
	if triggered {
		order.Type = Limit
		e.matchLimit(order)
	} else {
		e.OrderBook.Add(order)
		order.Status = New
	}
}

func (e *MatchingEngine) fill(taker, maker *Order) {
	fillQty := new(big.Float)
	if taker.RemainingQty.Cmp(maker.RemainingQty) >= 0 {
		fillQty.Copy(maker.RemainingQty)
	} else {
		fillQty.Copy(taker.RemainingQty)
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
	if maker.RemainingQty.Sign() == 0 {
		maker.Status = Filled
		e.OrderBook.Remove(maker.ID)
	} else {
		maker.Status = PartiallyFilled
	}
	trade := &Trade{
		BuyOrderID:  taker.ID,
		SellOrderID: maker.ID,
		Pair:        e.Pair,
		Price:       new(big.Float).Copy(maker.Price),
		Quantity:    new(big.Float).Copy(fillQty),
		CreatedAt:   time.Now().UnixNano(),
	}
	select {
	case e.Trades <- trade:
	default:
		log.Printf("[engine-%s] trades channel full, dropping trade", e.Pair)
	}
	select {
	case e.Fills <- &FillNotification{
		TakerOrderID: taker.ID, MakerOrderID: maker.ID,
		Pair: e.Pair, Side: taker.Side,
		Price: new(big.Float).Copy(maker.Price), Quantity: new(big.Float).Copy(fillQty),
		RemainingQty: new(big.Float).Copy(taker.RemainingQty),
		Filled:       taker.Status == Filled,
	}:
	default:
	}
}
