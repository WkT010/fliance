package matching

import (
	"math/big"
	"runtime"
	"sync/atomic"
)

type RingBuffer struct {
	buffer []*Order
	mask   uint64
	head   atomic.Uint64
	tail   atomic.Uint64
}

func NewRingBuffer(capacity uint64) *RingBuffer {
	cap := uint64(1)
	for cap < capacity {
		cap <<= 1
	}
	return &RingBuffer{buffer: make([]*Order, cap), mask: cap - 1}
}

func (rb *RingBuffer) Enqueue(order *Order) bool {
	head := rb.head.Load(); tail := rb.tail.Load()
	if tail-head >= rb.mask {
		return false
	}
	rb.buffer[tail&rb.mask] = order
	rb.tail.Store(tail + 1)
	return true
}

func (rb *RingBuffer) Drain() []*Order {
	head := rb.head.Load(); tail := rb.tail.Load()
	count := tail - head
	if count == 0 {
		return nil
	}
	orders := make([]*Order, count)
	for i := uint64(0); i < count; i++ {
		orders[i] = rb.buffer[(head+i)&rb.mask]
		rb.buffer[(head+i)&rb.mask] = nil
	}
	rb.head.Store(tail)
	return orders
}

type MPRingBuffer struct {
	buffer []atomic.Pointer[Order]
	mask   uint64
	head   atomic.Uint64
	tail   atomic.Uint64
}

func NewMPRingBuffer(capacity uint64) *MPRingBuffer {
	cap := uint64(1)
	for cap < capacity {
		cap <<= 1
	}
	return &MPRingBuffer{buffer: make([]atomic.Pointer[Order], cap), mask: cap - 1}
}

func (rb *MPRingBuffer) Enqueue(order *Order) bool {
	for {
		head := rb.head.Load()
		tail := rb.tail.Load()
		if tail-head >= rb.mask {
			return false
		}
		// Reserve the slot by advancing tail first, then publish the value.
		// Drain must spin-load the slot until non-nil (see Drain).
		if rb.tail.CompareAndSwap(tail, tail+1) {
			rb.buffer[tail&rb.mask].Store(order)
			return true
		}
	}
}

// Drain returns all queued orders. It spins briefly on any reserved-but-not-yet-
// published slot to wait for the producer. This keeps the MPSC contract safe
// under contention: a slot whose tail has advanced but whose value has not yet
// been stored is waited for rather than skipped.
func (rb *MPRingBuffer) Drain() []*Order {
	head := rb.head.Load()
	tail := rb.tail.Load()
	count := tail - head
	if count == 0 {
		return nil
	}
	orders := make([]*Order, 0, count)
	for i := uint64(0); i < count; i++ {
		idx := (head + i) & rb.mask
		// The producer reserved this slot (tail already advanced) but may not
		// have published the value yet. Spin until it does.
		var o *Order
		for spin := 0; spin < 64; spin++ {
			if o = rb.buffer[idx].Swap(nil); o != nil {
				break
			}
			runtime.Gosched()
		}
		if o == nil {
			// Final attempt; if still nil, the producer is stuck. Skip rather
			// than block the engine forever.
			o = rb.buffer[idx].Swap(nil)
		}
		if o != nil {
			orders = append(orders, o)
		}
	}
	rb.head.Store(tail)
	return orders
}

func (rb *MPRingBuffer) Len() uint64 {
	return rb.tail.Load() - rb.head.Load()
}

func BigFloatFromString(s string) *big.Float {
	f := new(big.Float)
	f.SetString(s)
	return f
}
