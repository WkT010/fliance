package matching

import (
	"math/big"
	"runtime"
	"sync/atomic"
	"time"
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
	head := rb.head.Load()
	tail := rb.tail.Load()
	if tail-head >= rb.mask {
		return false
	}
	rb.buffer[tail&rb.mask] = order
	rb.tail.Store(tail + 1)
	return true
}

func (rb *RingBuffer) Drain() []*Order {
	head := rb.head.Load()
	tail := rb.tail.Load()
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
	// notify signals the consumer that an order was enqueued so the matching
	// loop can block instead of busy-waiting. Capacity 1 is enough: a single
	// pending signal guarantees the consumer will wake and drain everything.
	notify chan struct{}
}

func NewMPRingBuffer(capacity uint64) *MPRingBuffer {
	cap := uint64(1)
	for cap < capacity {
		cap <<= 1
	}
	return &MPRingBuffer{
		buffer: make([]atomic.Pointer[Order], cap),
		mask:   cap - 1,
		notify: make(chan struct{}, 1),
	}
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
			// Wake the consumer. Non-blocking: a signal already pending still
			// guarantees a wake-up and a full drain.
			select {
			case rb.notify <- struct{}{}:
			default:
			}
			return true
		}
	}
}

// Notify returns a channel that receives a signal whenever an order is
// enqueued. Consumers select on it to sleep when the buffer is empty; wakeup
// latency after Enqueue is bounded by the channel send (sub-microsecond).
func (rb *MPRingBuffer) Notify() <-chan struct{} { return rb.notify }

// drainSpinBudget bounds how long Drain spins on a single reserved-but-
// unpublished slot before treating the producer as slow and stopping.
const drainSpinBudget = 64

// Drain returns queued orders in FIFO order. A producer reserves a slot by
// advancing tail and then publishes the value, so the first slot may be
// reserved but not yet published. Drain spins briefly (drainSpinBudget yields)
// on such a slot; if it is still unpublished, Drain STOPS there: it returns
// only the prefix it could collect and leaves head pointing AT the unpublished
// slot, so the next Drain call retries it. Orders are never skipped — a slow
// producer's order cannot be silently lost. The consumer (engine loop)
// re-drains immediately and, while a slot stays unpublished, yields the CPU
// (see drainStallYield) so the stalled producer can run and publish; since
// every successful Enqueue eventually stores its value, the stall is always
// transient and no deadlock can occur.
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
		for spin := 0; spin < drainSpinBudget; spin++ {
			if o = rb.buffer[idx].Swap(nil); o != nil {
				break
			}
			runtime.Gosched()
		}
		if o == nil {
			// Producer is still between reservation and publication. Stop the
			// drain BEFORE this slot: head only advances past collected orders,
			// so nothing is ever skipped. The next drain retries this slot.
			break
		}
		orders = append(orders, o)
	}
	if n := uint64(len(orders)); n > 0 {
		rb.head.Store(head + n)
	}
	return orders
}

// DrainStalled reports whether a reserved slot is currently unpublished, i.e.
// the last Drain stopped early waiting for a slow producer. Consumers use it
// to pace retries while stalled (see drainStallYield).
func (rb *MPRingBuffer) DrainStalled() bool {
	head := rb.head.Load()
	tail := rb.tail.Load()
	return tail > head && rb.buffer[head&rb.mask].Load() == nil
}

// drainStallYield is how long the consumer sleeps between drain attempts while
// DrainStalled, so it does not busy-spin a whole core waiting for a slow
// producer. The value is tiny: normal publish latency is nanoseconds, and the
// engine's notify channel / idle timeout remain the safety net.
const drainStallYield = 100 * time.Microsecond

func (rb *MPRingBuffer) Len() uint64 {
	return rb.tail.Load() - rb.head.Load()
}

func BigFloatFromString(s string) *big.Float {
	f := new(big.Float)
	f.SetString(s)
	return f
}
