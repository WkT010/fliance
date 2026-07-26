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
		if c := a.Price.Cmp(b.Price); c != 0 {
			return c > 0
		}
		return a.CreatedAt < b.CreatedAt
	default:
		if c := a.Price.Cmp(b.Price); c != 0 {
			return c < 0
		}
		return a.CreatedAt < b.CreatedAt
	}
}

func (h *OrderHeap) Swap(i, j int) {
	h.orders[i], h.orders[j] = h.orders[j], h.orders[i]
}

func (h *OrderHeap) Push(x any) {
	h.orders = append(h.orders, x.(*Order))
}

func (h *OrderHeap) Pop() any {
	old := h.orders
	n := len(old)
	x := old[n-1]
	h.orders = old[:n-1]
	return x
}

func (h *OrderHeap) peek() *Order {
	if len(h.orders) == 0 {
		return nil
	}
	return h.orders[0]
}

type OrderBook struct {
	Pair   string
	bids   *OrderHeap
	asks   *OrderHeap
	orders map[string]*Order
	seqNo  atomic.Uint64
}

func NewOrderBook(pair string) *OrderBook {
	return &OrderBook{
		Pair:   pair,
		bids:   &OrderHeap{side: Buy},
		asks:   &OrderHeap{side: Sell},
		orders: make(map[string]*Order),
	}
}

func (ob *OrderBook) Add(order *Order) {
	ob.orders[order.ID] = order
	if order.Side == Buy {
		heap.Push(ob.bids, order)
	} else {
		heap.Push(ob.asks, order)
	}
	ob.seqNo.Add(1)
}

func (ob *OrderBook) Remove(orderID string) *Order {
	o, ok := ob.orders[orderID]
	if !ok {
		return nil
	}
	delete(ob.orders, orderID)
	ob.seqNo.Add(1)
	return o
}

func (ob *OrderBook) Get(orderID string) *Order {
	return ob.orders[orderID]
}

func (ob *OrderBook) BestBid() *Order {
	return ob.bids.peek()
}

func (ob *OrderBook) BestAsk() *Order {
	return ob.asks.peek()
}

func (ob *OrderBook) PopBestBid() *Order {
	for ob.bids.Len() > 0 {
		o := heap.Pop(ob.bids).(*Order)
		if _, ok := ob.orders[o.ID]; ok {
			delete(ob.orders, o.ID)
			return o
		}
	}
	return nil
}

func (ob *OrderBook) PopBestAsk() *Order {
	for ob.asks.Len() > 0 {
		o := heap.Pop(ob.asks).(*Order)
		if _, ok := ob.orders[o.ID]; ok {
			delete(ob.orders, o.ID)
			return o
		}
	}
	return nil
}

func (ob *OrderBook) Depth(levels int) *OrderBookDepth {
	return &OrderBookDepth{
		Pair:  ob.Pair,
		Bids:  aggregateLevelsFromMap(ob.orders, Buy, levels),
		Asks:  aggregateLevelsFromMap(ob.orders, Sell, levels),
		SeqNo: ob.seqNo.Load(),
	}
}

func (ob *OrderBook) Size() int {
	return len(ob.orders)
}

func aggregateLevelsFromMap(orders map[string]*Order, side Side, levels int) []PriceLevel {
	priceMap := make(map[string]*PriceLevel)
	priceKeys := make([]string, 0)
	for _, o := range orders {
		if o.Side != side {
			continue
		}
		if o.RemainingQty.Sign() <= 0 {
			continue
		}
		k := o.Price.String()
		pl, exists := priceMap[k]
		if !exists {
			pl = &PriceLevel{
				Price:    new(big.Float).Copy(o.Price),
				Quantity: big.NewFloat(0),
			}
			priceMap[k] = pl
			priceKeys = append(priceKeys, k)
		}
		pl.Quantity.Add(pl.Quantity, o.RemainingQty)
		pl.Count++
	}
	sortPriceLevels(priceKeys, side)
	result := make([]PriceLevel, 0, min(levels, len(priceKeys)))
	for _, k := range priceKeys {
		result = append(result, *priceMap[k])
		if len(result) >= levels {
			break
		}
	}
	return result
}

func sortPriceLevels(keys []string, side Side) {
	n := len(keys)
	for i := 0; i < n; i++ {
		swapped := false
		for j := 0; j < n-i-1; j++ {
			a, _ := new(big.Float).SetString(keys[j])
			b, _ := new(big.Float).SetString(keys[j+1])
			cmp := a.Cmp(b)
			less := (side == Buy && cmp < 0) || (side == Sell && cmp > 0)
			if less {
				keys[j], keys[j+1] = keys[j+1], keys[j]
				swapped = true
			}
		}
		if !swapped {
			break
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
