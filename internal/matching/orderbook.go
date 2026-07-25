package matching

import (
	"container/heap"
	"math/big"
	"sync/atomic"
)

type OrderHeap struct {
	orders []*Order
	side   Side
}

func (h *OrderHeap) Len() int { return len(h.orders) }
func (h *OrderHeap) Less(i, j int) bool {
	a, b := h.orders[i], h.orders[j]
	switch h.side {
	case Buy:
		if c := a.Price.Cmp(b.Price); c != 0 { return c > 0 }
		return a.CreatedAt < b.CreatedAt
	default:
		if c := a.Price.Cmp(b.Price); c != 0 { return c < 0 }
		return a.CreatedAt < b.CreatedAt
	}
}
func (h *OrderHeap) Swap(i, j int) { h.orders[i], h.orders[j] = h.orders[j], h.orders[i] }
func (h *OrderHeap) Push(x any) { h.orders = append(h.orders, x.(*Order)) }
func (h *OrderHeap) Pop() any { old := h.orders; n := len(old); x := old[n-1]; h.orders = old[:n-1]; return x }
func (h *OrderHeap) peek() *Order { if len(h.orders) == 0 { return nil }; return h.orders[0] }

type OrderBook struct {
	Pair   string
	bids   *OrderHeap
	asks   *OrderHeap
	orders map[string]*Order
	seqNo  atomic.Uint64
}

func NewOrderBook(pair string) *OrderBook {
	return &OrderBook{Pair: pair, bids: &OrderHeap{side: Buy}, asks: &OrderHeap{side: Sell}, orders: make(map[string]*Order)}
}

func (ob *OrderBook) Add(order *Order) {
	ob.orders[order.ID] = order
	if order.Side == Buy { heap.Push(ob.bids, order) } else { heap.Push(ob.asks, order) }
	ob.seqNo.Add(1)
}

func (ob *OrderBook) Remove(orderID string) *Order {
	o, ok := ob.orders[orderID]
	if !ok { return nil }
	delete(ob.orders, orderID)
	ob.seqNo.Add(1)
	return o
}

func (ob *OrderBook) Get(orderID string) *Order { return ob.orders[orderID] }
func (ob *OrderBook) BestBid() *Order { return ob.bids.peek() }
func (ob *OrderBook) BestAsk() *Order { return ob.asks.peek() }

func (ob *OrderBook) PopBestBid() *Order {
	for ob.bids.Len() > 0 {
		o := heap.Pop(ob.bids).(*Order)
		if _, ok := ob.orders[o.ID]; ok { delete(ob.orders, o.ID); return o }
	}
	return nil
}

func (ob *OrderBook) PopBestAsk() *Order {
	for ob.asks.Len() > 0 {
		o := heap.Pop(ob.asks).(*Order)
		if _, ok := ob.orders[o.ID]; ok { delete(ob.orders, o.ID); return o }
	}
	return nil
}

func (ob *OrderBook) Depth(levels int) *OrderBookDepth {
	return &OrderBookDepth{Pair: ob.Pair, Bids: aggregateLevels(ob.bids, Buy, levels), Asks: aggregateLevels(ob.asks, Sell, levels), SeqNo: ob.seqNo.Load()}
}

func (ob *OrderBook) Size() int { return len(ob.orders) }

func aggregateLevels(h *OrderHeap, side Side, levels int) []PriceLevel {
	priceMap := make(map[string]*PriceLevel)
	var keys []string
	for _, o := range h.orders {
		k := o.Price.String()
		if _, ok := priceMap[k]; !ok {
			priceMap[k] = &PriceLevel{Price: new(big.Float).Copy(o.Price), Quantity: big.NewFloat(0)}
			keys = append(keys, k)
		}
		priceMap[k].Quantity.Add(priceMap[k].Quantity, o.RemainingQty)
		priceMap[k].Count++
	}
	result := make([]PriceLevel, 0, levels)
	for _, k := range keys { result = append(result, *priceMap[k]) }
	if len(result) > levels { result = result[:levels] }
	return result
}