package matching

import (
	"container/heap"
	"errors"
	"math/big"
	"sort"
	"sync"
	"sync/atomic"
)

// Sentinel errors for order operations.
var (
	ErrOrderNotFound = errors.New("order not found")
	ErrOrderNotOwned = errors.New("order does not belong to user")
)

// OrderHeap is a price-time priority heap. For bids it is a max-heap (best/highest
// price on top); for asks it is a min-heap (best/lowest price on top). Ties are
// broken by bookSeq, a monotonic sequence assigned when the order enters the
// book, giving strict FIFO (time) priority independent of clock resolution.
type OrderHeap struct {
	orders []*Order
	side   Side
}

func (h *OrderHeap) Len() int { return len(h.orders) }

func (h *OrderHeap) Less(i, j int) bool {
	a, b := h.orders[i], h.orders[j]
	if c := a.Price.Cmp(b.Price); c != 0 {
		switch h.side {
		case Buy:
			return c > 0
		default:
			return c < 0
		}
	}
	return a.bookSeq < b.bookSeq
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

// OrderBook is the limit-order book for a single trading pair.
//
// Concurrency model: the book is owned by the matching engine goroutine during
// matching, but it is also read/mutated by HTTP handlers (depth snapshots,
// cancels, ticker). A mutex therefore guards all public operations. The matching
// engine takes the same lock while processing an order so that cancels issued
// from the API layer cannot observe a partially-applied match.
type OrderBook struct {
	Pair   string
	bids   *OrderHeap
	asks   *OrderHeap
	orders map[string]*Order
	// userOrders indexes resting orders by user (userID -> set of order IDs).
	// It is maintained in lockstep with the orders map on every add/remove
	// path (addLocked, removeLocked, PopBest*, snapshot Restore, cancels) so
	// GetOrdersByUser is O(user orders) instead of a full book scan.
	userOrders map[string]map[string]struct{}
	seqNo      atomic.Uint64
	mu         sync.RWMutex

	// lastTradePrice is maintained atomically for stop-order triggering and
	// ticker computation without taking the write lock.
	lastTradePrice atomic.Pointer[big.Float]

	// stops holds parked stop orders (StopLoss / StopLimit) that have not yet
	// triggered. These are NOT part of the bid/ask heaps.
	stops []*Order
}

func NewOrderBook(pair string) *OrderBook {
	ob := &OrderBook{
		Pair:       pair,
		bids:       &OrderHeap{side: Buy},
		asks:       &OrderHeap{side: Sell},
		orders:     make(map[string]*Order),
		userOrders: make(map[string]map[string]struct{}),
	}
	ob.lastTradePrice.Store(big.NewFloat(0))
	return ob
}

// indexAddLocked registers an order in the per-user index. Caller must hold
// the write lock.
func (ob *OrderBook) indexAddLocked(o *Order) {
	if o == nil {
		return
	}
	ids := ob.userOrders[o.UserID]
	if ids == nil {
		ids = make(map[string]struct{})
		ob.userOrders[o.UserID] = ids
	}
	ids[o.ID] = struct{}{}
}

// indexRemoveLocked unregisters an order from the per-user index. Caller must
// hold the write lock.
func (ob *OrderBook) indexRemoveLocked(o *Order) {
	if o == nil {
		return
	}
	if ids, ok := ob.userOrders[o.UserID]; ok {
		delete(ids, o.ID)
		if len(ids) == 0 {
			delete(ob.userOrders, o.UserID)
		}
	}
}

// addStopLocked parks a stop order. Caller must hold the write lock. The order
// gets its bookSeq here so that, if it later triggers and rests, it keeps its
// original time priority.
func (ob *OrderBook) addStopLocked(order *Order) {
	ob.stops = append(ob.stops, order)
	if order.bookSeq == 0 {
		order.bookSeq = ob.seqNo.Add(1)
	} else {
		ob.seqNo.Add(1)
	}
}

// RemoveStop removes a parked stop order by id. Caller must hold the write lock.
func (ob *OrderBook) RemoveStop(orderID string) *Order {
	ob.mu.Lock()
	defer ob.mu.Unlock()
	for i, o := range ob.stops {
		if o.ID == orderID {
			ob.stops = append(ob.stops[:i], ob.stops[i+1:]...)
			o.Status = Cancelled
			o.UpdatedAt = nowNanos()
			ob.seqNo.Add(1)
			return o
		}
	}
	return nil
}

// StopCount returns the number of parked stop orders.
func (ob *OrderBook) StopCount() int {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	return len(ob.stops)
}

// Add inserts a resting order into the book.
func (ob *OrderBook) Add(order *Order) {
	ob.mu.Lock()
	defer ob.mu.Unlock()
	ob.addLocked(order)
}

func (ob *OrderBook) addLocked(order *Order) {
	// bookSeq is pre-assigned for orders parked as stops first (preserves
	// their original time priority); everyone else gets the next sequence.
	if order.bookSeq == 0 {
		order.bookSeq = ob.seqNo.Add(1)
	} else {
		ob.seqNo.Add(1)
	}
	ob.orders[order.ID] = order
	ob.indexAddLocked(order)
	if order.Side == Buy {
		heap.Push(ob.bids, order)
	} else {
		heap.Push(ob.asks, order)
	}
}

// Remove marks an order as cancelled. It only deletes the order from the lookup
// map; the actual heap entry is purged lazily by bestBidLocked/bestAskLocked so
// that we never have to search the heap (O(n)) on cancel.
func (ob *OrderBook) Remove(orderID string) *Order {
	ob.mu.Lock()
	defer ob.mu.Unlock()
	return ob.removeLocked(orderID)
}

func (ob *OrderBook) removeLocked(orderID string) *Order {
	o, ok := ob.orders[orderID]
	if !ok {
		return nil
	}
	delete(ob.orders, orderID)
	ob.indexRemoveLocked(o)
	o.Status = Cancelled
	o.UpdatedAt = nowNanos()
	ob.seqNo.Add(1)
	return o
}

// Get returns a resting order by id, or nil.
func (ob *OrderBook) Get(orderID string) *Order {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	return ob.orders[orderID]
}

// GetOrdersByUser returns a snapshot of all live orders belonging to userID.
// Backed by the per-user index, so it runs in O(user orders) instead of
// scanning the whole book.
func (ob *OrderBook) GetOrdersByUser(userID string) []*Order {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	ids := ob.userOrders[userID]
	out := make([]*Order, 0, len(ids))
	for id := range ids {
		if o, ok := ob.orders[id]; ok {
			out = append(out, o)
		}
	}
	return out
}

// BestBid returns the best (highest priced) live bid, lazily purging stale
// heap entries left behind by Remove.
func (ob *OrderBook) BestBid() *Order {
	ob.mu.Lock()
	defer ob.mu.Unlock()
	return ob.bestBidLocked()
}

func (ob *OrderBook) bestBidLocked() *Order {
	for ob.bids.Len() > 0 {
		top := ob.bids.peek()
		if top == nil {
			return nil
		}
		if _, ok := ob.orders[top.ID]; !ok || top.RemainingQty.Sign() <= 0 {
			heap.Pop(ob.bids)
			continue
		}
		return top
	}
	return nil
}

// BestAsk returns the best (lowest priced) live ask, lazily purging stale
// heap entries.
func (ob *OrderBook) BestAsk() *Order {
	ob.mu.Lock()
	defer ob.mu.Unlock()
	return ob.bestAskLocked()
}

func (ob *OrderBook) bestAskLocked() *Order {
	for ob.asks.Len() > 0 {
		top := ob.asks.peek()
		if top == nil {
			return nil
		}
		if _, ok := ob.orders[top.ID]; !ok || top.RemainingQty.Sign() <= 0 {
			heap.Pop(ob.asks)
			continue
		}
		return top
	}
	return nil
}

// PopBestBid removes and returns the best live bid (used by market sweeps and
// tests). Stale entries are skipped.
func (ob *OrderBook) PopBestBid() *Order {
	ob.mu.Lock()
	defer ob.mu.Unlock()
	for ob.bids.Len() > 0 {
		o := heap.Pop(ob.bids).(*Order)
		if _, ok := ob.orders[o.ID]; ok && o.RemainingQty.Sign() > 0 {
			delete(ob.orders, o.ID)
			ob.indexRemoveLocked(o)
			ob.seqNo.Add(1)
			return o
		}
	}
	return nil
}

func (ob *OrderBook) PopBestAsk() *Order {
	ob.mu.Lock()
	defer ob.mu.Unlock()
	for ob.asks.Len() > 0 {
		o := heap.Pop(ob.asks).(*Order)
		if _, ok := ob.orders[o.ID]; ok && o.RemainingQty.Sign() > 0 {
			delete(ob.orders, o.ID)
			ob.indexRemoveLocked(o)
			ob.seqNo.Add(1)
			return o
		}
	}
	return nil
}

// Depth returns the aggregated top-of-book levels.
func (ob *OrderBook) Depth(levels int) *OrderBookDepth {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	return &OrderBookDepth{
		Pair:  ob.Pair,
		Bids:  aggregateLevelsFromMap(ob.orders, Buy, levels),
		Asks:  aggregateLevelsFromMap(ob.orders, Sell, levels),
		SeqNo: ob.seqNo.Load(),
	}
}

// Size returns the number of resting orders.
func (ob *OrderBook) Size() int {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	return len(ob.orders)
}

// LastTradePrice returns the most recent trade price (atomic, lock-free read).
func (ob *OrderBook) LastTradePrice() *big.Float {
	return ob.lastTradePrice.Load()
}

func (ob *OrderBook) setLastTradePrice(p *big.Float) {
	ob.lastTradePrice.Store(new(big.Float).Copy(p))
}

// Seq returns the current sequence number (atomic).
func (ob *OrderBook) Seq() uint64 { return ob.seqNo.Load() }

func aggregateLevelsFromMap(orders map[string]*Order, side Side, levels int) []PriceLevel {
	if levels <= 0 {
		levels = 100
	}
	priceMap := make(map[string]*PriceLevel)
	priceKeys := make([]string, 0)
	for _, o := range orders {
		if o.Side != side {
			continue
		}
		if o.RemainingQty.Sign() <= 0 {
			continue
		}
		k := o.Price.Text('f', 18)
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
	result := make([]PriceLevel, 0, minInt(levels, len(priceKeys)))
	for _, k := range priceKeys {
		result = append(result, *priceMap[k])
		if len(result) >= levels {
			break
		}
	}
	return result
}

// sortPriceLevels orders price keys best-first: bids descending (highest
// first), asks ascending (lowest first). Uses sort.Slice (O(n log n)); the
// parsed values travel with their keys so comparisons stay consistent.
func sortPriceLevels(keys []string, side Side) {
	type parsedKey struct {
		key    string
		parsed *big.Float
	}
	items := make([]parsedKey, len(keys))
	for i, k := range keys {
		f, _, _ := big.ParseFloat(k, 10, 256, big.ToNearestEven)
		if f == nil {
			f = big.NewFloat(0)
		}
		items[i] = parsedKey{key: k, parsed: f}
	}
	sort.Slice(items, func(i, j int) bool {
		cmp := items[i].parsed.Cmp(items[j].parsed)
		if side == Buy {
			return cmp > 0
		}
		return cmp < 0
	})
	for i := range items {
		keys[i] = items[i].key
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func nowNanos() int64 { return timeNowUnixNano() }
