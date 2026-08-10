package matching

import (
	"sync"
	"testing"
	"time"
)

// TestMPDrainStopsAtUnpublishedSlot simulates a slow producer: the slot is
// reserved (tail advanced) but the value is not published yet. Drain must NOT
// skip the slot — it stops, returns the already-collected prefix, and leaves
// head pointing at the unpublished slot. Publishing the value afterwards and
// re-draining must return the order (nothing lost).
func TestMPDrainStopsAtUnpublishedSlot(t *testing.T) {
	rb := NewMPRingBuffer(8)

	// Reserve slot 0 the same way Enqueue does, but do not publish yet.
	tail := rb.tail.Load()
	if !rb.tail.CompareAndSwap(tail, tail+1) {
		t.Fatal("failed to reserve slot")
	}

	// Drain while the slot is unpublished: must return nothing and must NOT
	// advance head past the reserved slot.
	if got := rb.Drain(); len(got) != 0 {
		t.Fatalf("expected empty drain while slot unpublished, got %d", len(got))
	}
	if rb.head.Load() != tail {
		t.Fatalf("head must stay at the unpublished slot: head=%d want=%d", rb.head.Load(), tail)
	}
	if rb.Len() != 1 {
		t.Fatalf("reserved order must still be counted, len=%d", rb.Len())
	}
	if !rb.DrainStalled() {
		t.Fatal("DrainStalled must report the stalled slot")
	}

	// Now the slow producer publishes. The next drain must return the order.
	rb.buffer[tail&rb.mask].Store(createLimitOrder(Buy, "50000", "1.0"))
	got := rb.Drain()
	if len(got) != 1 {
		t.Fatalf("expected the delayed order on re-drain, got %d", len(got))
	}
	if rb.Len() != 0 {
		t.Fatalf("buffer must be empty, len=%d", rb.Len())
	}
	if rb.DrainStalled() {
		t.Fatal("DrainStalled must be false after publish")
	}
}

// TestMPDrainPrefixThenStall verifies a mixed scenario: published orders ahead
// of an unpublished slot are returned, the drain stops at the unpublished slot
// and the remaining order survives to the next drain.
func TestMPDrainPrefixThenStall(t *testing.T) {
	rb := NewMPRingBuffer(8)
	if !rb.Enqueue(createLimitOrder(Buy, "50000", "1.0")) {
		t.Fatal("enqueue 1 failed")
	}
	if !rb.Enqueue(createLimitOrder(Buy, "50001", "1.0")) {
		t.Fatal("enqueue 2 failed")
	}
	// Reserve a third slot without publishing it.
	tail := rb.tail.Load()
	if !rb.tail.CompareAndSwap(tail, tail+1) {
		t.Fatal("failed to reserve slot 3")
	}

	got := rb.Drain()
	if len(got) != 2 {
		t.Fatalf("expected prefix of 2 published orders, got %d", len(got))
	}
	if rb.Len() != 1 {
		t.Fatalf("unpublished order must remain pending, len=%d", rb.Len())
	}

	rb.buffer[tail&rb.mask].Store(createLimitOrder(Buy, "50002", "1.0"))
	got = rb.Drain()
	if len(got) != 1 {
		t.Fatalf("expected the delayed order, got %d", len(got))
	}
	if got[0].Price.String() != "50002" {
		t.Errorf("wrong order returned: %s", got[0].Price)
	}
}

// TestMPDrainSlowProducerNoLoss exercises the full engine loop with concurrent
// producers and a consumer that honours the stop-and-retry drain protocol. It
// is the regression test for the silent order loss: under the old skip-based
// drain, orders whose producers were slow between reservation and publication
// were dropped forever. Every successfully enqueued order must be drained.
func TestMPDrainSlowProducerNoLoss(t *testing.T) {
	rb := NewMPRingBuffer(1024)
	const producers = 4
	const perProducer = 2000

	var enqueued [producers]int
	var wg sync.WaitGroup
	for p := 0; p < producers; p++ {
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			for i := 0; i < perProducer; i++ {
				for !rb.Enqueue(createLimitOrder(Buy, "50000", "1.0")) {
					time.Sleep(time.Microsecond)
				}
				enqueued[p]++
			}
		}(p)
	}

	var drained int
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			got := rb.Drain()
			drained += len(got)
			if rb.DrainStalled() {
				time.Sleep(drainStallYield)
				continue
			}
			select {
			case <-done:
				return
			case <-rb.Notify():
			default:
				time.Sleep(time.Millisecond)
			}
		}
	}()

	wg.Wait()
	// Let the consumer finish draining everything the producers published.
	deadline := time.Now().Add(testTimeout)
	for rb.Len() != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	close(done)
	<-stopped // single-consumer contract: never drain concurrently
	// Final sweep.
	for rb.Len() > 0 {
		drained += len(rb.Drain())
	}

	total := 0
	for _, n := range enqueued {
		total += n
	}
	if drained != total {
		t.Fatalf("orders lost: drained=%d enqueued=%d", drained, total)
	}
	if rb.Len() != 0 {
		t.Fatalf("buffer not empty: %d left", rb.Len())
	}
}

// TestEngineSlowProducerNoLoss is the engine-level counterpart: orders must
// actually be matched (not dropped) even when producers interleave with the
// engine's drain loop under contention.
func TestEngineSlowProducerNoLoss(t *testing.T) {
	e := NewMatchingEngine("BTC/USDT", 1024)
	e.Start()
	defer e.Stop()

	const perSide = 500
	var wg sync.WaitGroup
	// Resting sells first, then crossing buys; every buy should match one sell.
	for p := 0; p < 2; p++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perSide; i++ {
				for !e.SubmitOrder(createLimitOrderUser("m", Sell, "50000", "1.0")) {
					time.Sleep(time.Microsecond)
				}
			}
		}()
	}
	wg.Wait()
	// Wait for sells to settle into the book.
	deadline := time.Now().Add(testTimeout)
	for len(e.OrderBook.GetOrdersByUser("m")) < perSide*2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	for p := 0; p < 2; p++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perSide; i++ {
				for !e.SubmitOrder(createLimitOrderUser("t", Buy, "50000", "1.0")) {
					time.Sleep(time.Microsecond)
				}
			}
		}()
	}
	wg.Wait()

	trades := waitForTrades(e.Trades, perSide*2)
	if len(trades) != perSide*2 {
		t.Fatalf("expected %d trades, got %d (orders lost?)", perSide*2, len(trades))
	}
}
