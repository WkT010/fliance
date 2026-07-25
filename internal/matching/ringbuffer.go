package matching

import (
	"math/big"
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
	for cap < capacity { cap <<= 1 }
	return &RingBuffer{buffer: make([]*Order, cap), mask: cap - 1}
}

func (rb *RingBuffer) Enqueue(order *Order) bool {
	head := rb.head.Load(); tail := rb.tail.Load()
	if tail-head >= rb.mask { return false }
	rb.buffer[tail&rb.mask] = order
	rb.tail.Store(tail + 1)
	return true
}

func (rb *RingBuffer) Dequeue() *Order {
	head := rb.head.Load(); tail := rb.tail.Load()
	if head >= tail { return nil }
	o := rb.buffer[head&rb.mask]
	rb.buffer[head&rb.mask] = nil
	rb.head.Store(head + 1)
	return o
}

func (rb *RingBuffer) Drain() []*Order {
	head := rb.head.Load(); tail := rb.tail.Load()
	count := tail - head
	if count == 0 { return nil }
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
	for cap < capacity { cap <<= 1 }
	return &MPRingBuffer{buffer: make([]atomic.Pointer[Order], cap), mask: cap - 1}
}

func (rb *MPRingBuffer) Enqueue(order *Order) bool {
	for {
		head := rb.head.Load(); tail := rb.tail.Load()
		if tail-head >= rb.mask { return false }
		if rb.tail.CompareAndSwap(tail, tail+1) {
			rb.buffer[tail&rb.mask].Store(order)
			return true
		}
	}
}

func (rb *MPRingBuffer) Drain() []*Order {
	head := rb.head.Load(); tail := rb.tail.Load()
	count := tail - head
	if count == 0 { return nil }
	orders := make([]*Order, 0, count)
	for i := uint64(0); i < count; i++ {
		if o := rb.buffer[(head+i)&rb.mask].Swap(nil); o != nil {
			orders = append(orders, o)
		}
	}
	rb.head.Store(tail)
	return orders
}

func (rb *MPRingBuffer) Len() uint64 { return rb.tail.Load() - rb.head.Load() }

func BigFloatFromString(s string) *big.Float { f := new(big.Float); f.SetString(s); return f }