package matching

import (
	"math/big"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

const testTimeout = 2 * time.Second

func setupEngine() *MatchingEngine {
	e := NewMatchingEngine("BTC/USDT", 100000)
	e.Start()
	return e
}

func createLimitOrder(side Side, price, qty string) *Order {
	p, _ := new(big.Float).SetString(price)
	q, _ := new(big.Float).SetString(qty)
	return NewOrder("u1", "BTC/USDT", side, Limit, p, q)
}

func createLimitOrderUser(uid string, side Side, price, qty string) *Order {
	p, _ := new(big.Float).SetString(price)
	q, _ := new(big.Float).SetString(qty)
	return NewOrder(uid, "BTC/USDT", side, Limit, p, q)
}

func createMarketOrder(side Side, qty string) *Order {
	q, _ := new(big.Float).SetString(qty)
	return NewOrder("u1", "BTC/USDT", side, Market, nil, q)
}

// waitForTrades drains up to n trades or returns when the deadline elapses.
func waitForTrades(ch <-chan *Trade, n int) []*Trade {
	var trades []*Trade
	deadline := time.Now().Add(testTimeout)
	for len(trades) < n {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return trades
		}
		select {
		case t := <-ch:
			trades = append(trades, t)
		case <-time.After(remaining):
			return trades
		}
	}
	return trades
}

// submitSync submits an order and waits for the engine to finish processing it,
// returning the order with a final, safely-readable status.
func submitSync(e *MatchingEngine, o *Order) *Order {
	out, err := e.SubmitOrderSync(o)
	if err != nil {
		return o
	}
	return out
}

// drainTrades drains the trade channel of any pending trades. Useful between
// engine steps to ensure the channel is empty before assertions.
func drainTrades(ch <-chan *Trade) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

// drainTradesCount drains and returns all pending trades in the channel.
func drainTradesCount(ch <-chan *Trade) []*Trade {
	var out []*Trade
	for {
		select {
		case t := <-ch:
			out = append(out, t)
		default:
			return out
		}
	}
}

func TestSimpleMatch(t *testing.T) {
	e := setupEngine()
	defer e.Stop()
	submitSync(e, createLimitOrder(Sell, "50000", "1.0"))
	submitSync(e, createLimitOrder(Buy, "50000", "1.0"))
	trades := waitForTrades(e.Trades, 1)
	if len(trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(trades))
	}
	if trades[0].Price.Cmp(big.NewFloat(50000)) != 0 {
		t.Errorf("bad price: %s", trades[0].Price.String())
	}
	if trades[0].Quantity.Cmp(big.NewFloat(1.0)) != 0 {
		t.Errorf("bad qty: %s", trades[0].Quantity.String())
	}
	if trades[0].BuyerID != "u1" || trades[0].SellerID != "u1" {
		t.Errorf("trade counter-parties not enriched: buyer=%s seller=%s", trades[0].BuyerID, trades[0].SellerID)
	}
	if trades[0].TakerSide != Buy {
		t.Errorf("expected taker side buy, got %s", trades[0].TakerSide)
	}
}

func TestPriceTimePriority(t *testing.T) {
	e := setupEngine()
	defer e.Stop()
	s1 := submitSync(e, createLimitOrder(Sell, "50000", "1.0"))
	s2 := submitSync(e, createLimitOrder(Sell, "50000", "1.0"))
	submitSync(e, createLimitOrder(Sell, "50000", "1.0"))
	submitSync(e, createLimitOrder(Buy, "50000", "3.0"))
	trades := waitForTrades(e.Trades, 3)
	if len(trades) != 3 {
		t.Fatalf("expected 3 trades, got %d", len(trades))
	}
	if trades[0].SellOrderID != s1.ID {
		t.Error("expected s1 first (time priority)")
	}
	if trades[1].SellOrderID != s2.ID {
		t.Error("expected s2 second (time priority)")
	}
}

func TestPricePriority(t *testing.T) {
	e := setupEngine()
	defer e.Stop()
	submitSync(e, createLimitOrder(Sell, "51000", "1.0"))
	submitSync(e, createLimitOrder(Sell, "50000", "1.0"))
	submitSync(e, createLimitOrder(Sell, "52000", "1.0"))
	submitSync(e, createLimitOrder(Buy, "52000", "3.0"))
	trades := waitForTrades(e.Trades, 3)
	if len(trades) != 3 {
		t.Fatalf("expected 3 trades, got %d", len(trades))
	}
	if trades[0].Price.Cmp(big.NewFloat(50000)) != 0 {
		t.Error("expected first fill at 50000 (best ask)")
	}
	if trades[1].Price.Cmp(big.NewFloat(51000)) != 0 {
		t.Error("expected second fill at 51000")
	}
}

func TestMarketOrder(t *testing.T) {
	e := setupEngine()
	defer e.Stop()
	submitSync(e, createLimitOrder(Sell, "49000", "0.5"))
	submitSync(e, createLimitOrder(Sell, "50000", "0.5"))
	submitSync(e, createMarketOrder(Buy, "1.0"))
	trades := waitForTrades(e.Trades, 2)
	if len(trades) != 2 {
		t.Fatalf("expected 2 trades, got %d", len(trades))
	}
}

func TestFOKAccepted(t *testing.T) {
	e := setupEngine()
	defer e.Stop()
	// Two asks totalling 2.0 at or below 50000 -> FOK buy 2.0 @ 50000 fills.
	submitSync(e, createLimitOrder(Sell, "50000", "1.0"))
	submitSync(e, createLimitOrder(Sell, "50000", "1.0"))
	fok := createLimitOrder(Buy, "50000", "2.0")
	fok.TimeInForce = FOK
	fok.Type = FillOrKill
	out := submitSync(e, fok)
	waitForTrades(e.Trades, 2)
	if out.Status != Filled {
		t.Errorf("FOK should fill when enough liquidity; status=%s", out.Status)
	}
}

func TestFOKRejected(t *testing.T) {
	e := setupEngine()
	defer e.Stop()
	submitSync(e, createLimitOrder(Sell, "50000", "1.0"))
	fok := createLimitOrder(Buy, "50000", "2.0")
	fok.TimeInForce = FOK
	fok.Type = FillOrKill
	out := submitSync(e, fok)
	if out.Status != Rejected {
		t.Errorf("FOK should reject when insufficient liquidity; status=%s", out.Status)
	}
}

func TestIOCPartialFill(t *testing.T) {
	e := setupEngine()
	defer e.Stop()
	submitSync(e, createLimitOrder(Sell, "50000", "0.3"))
	ioc := createLimitOrder(Buy, "50000", "1.0")
	ioc.TimeInForce = IOC
	ioc.Type = ImmediateOrCancel
	out := submitSync(e, ioc)
	trades := waitForTrades(e.Trades, 1)
	if len(trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(trades))
	}
	// IOC partial fill -> the filled portion is recorded and the remainder is
	// cancelled. The overall order status is PartiallyFilled (some filled, some
	// cancelled) which matches Binance/Huobi conventions.
	if out.Status != PartiallyFilled {
		t.Errorf("IOC partial fill should be PartiallyFilled; status=%s", out.Status)
	}
	wantFilled, _ := new(big.Float).SetString("0.3")
	cmpResult := out.FilledQty.Cmp(wantFilled)
	t.Logf("filled=%s (prec=%d) want=%s (prec=%d) cmp=%d",
		out.FilledQty.Text('f', 18), out.FilledQty.Prec(),
		wantFilled.Text('f', 18), wantFilled.Prec(), cmpResult)
	if cmpResult != 0 {
		t.Errorf("expected filled 0.3, got %s", out.FilledQty.Text('f', 8))
	}
	wantRem, _ := new(big.Float).SetString("0.7")
	if out.RemainingQty.Cmp(wantRem) != 0 {
		t.Errorf("IOC should have 0.7 remaining (unfilled portion); got %s", out.RemainingQty.Text('f', 8))
	}
}

func TestOrderBookDepth(t *testing.T) {
	ob := NewOrderBook("BTC/USDT")
	ob.Add(createLimitOrder(Buy, "50000", "1.0"))
	ob.Add(createLimitOrder(Buy, "49900", "2.0"))
	ob.Add(createLimitOrder(Sell, "50100", "1.5"))
	ob.Add(createLimitOrder(Sell, "50200", "0.5"))
	depth := ob.Depth(10)
	if len(depth.Bids) != 2 {
		t.Fatalf("expected 2 bid levels, got %d", len(depth.Bids))
	}
	if len(depth.Asks) != 2 {
		t.Fatalf("expected 2 ask levels, got %d", len(depth.Asks))
	}
	if depth.Bids[0].Price.Cmp(big.NewFloat(50000)) != 0 {
		t.Errorf("expected best bid 50000, got %s", depth.Bids[0].Price.String())
	}
	if depth.Asks[0].Price.Cmp(big.NewFloat(50100)) != 0 {
		t.Errorf("expected best ask 50100, got %s", depth.Asks[0].Price.String())
	}
}

// TestStaleHeapPurge verifies that removing a maker does not leave a stale entry
// at the top of the heap (the original bug caused BestAsk to return a cancelled
// order and the matching loop to spin forever).
func TestStaleHeapPurge(t *testing.T) {
	ob := NewOrderBook("BTC/USDT")
	best := createLimitOrder(Sell, "50000", "1.0")
	ob.Add(best)
	ob.Add(createLimitOrder(Sell, "50100", "1.0"))
	ob.Remove(best.ID)
	got := ob.BestAsk()
	if got == nil {
		t.Fatal("expected BestAsk to skip purged order")
	}
	if got.Price.Cmp(big.NewFloat(50100)) != 0 {
		t.Errorf("expected 50100 after purge, got %s", got.Price.String())
	}
}

func TestConcurrentEnqueue(t *testing.T) {
	rb := NewMPRingBuffer(1_000_000)
	o := createLimitOrder(Buy, "50000", "1.0")
	var wg sync.WaitGroup
	for p := 0; p < 4; p++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 10000; i++ {
				rb.Enqueue(o)
			}
		}()
	}
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			rb.Drain()
			select {
			case <-done:
				return
			default:
			}
			// Honour the stop-and-retry protocol: while a slow producer has not
			// published its reserved slot, pace retries instead of hot-spinning.
			if rb.DrainStalled() {
				time.Sleep(drainStallYield)
			}
		}
	}()
	wg.Wait()
	close(done)
	<-stopped // single-consumer contract: never drain concurrently
	for rb.Len() > 0 {
		rb.Drain()
	}
	if rb.Len() != 0 {
		t.Errorf("expected empty buffer, got %d", rb.Len())
	}
}

func TestCancelViaChannel(t *testing.T) {
	e := setupEngine()
	defer e.Stop()
	o := submitSync(e, createLimitOrder(Buy, "49000", "1.0"))
	if o.Status != New {
		t.Fatalf("expected resting order, got %s", o.Status)
	}
	cancelled, err := e.Cancel(o.ID, "u1")
	if err != nil {
		t.Fatalf("cancel failed: %v", err)
	}
	if cancelled.Status != Cancelled {
		t.Errorf("expected cancelled status, got %s", cancelled.Status)
	}
	if got := e.OrderBook.Get(o.ID); got != nil {
		t.Error("order should be removed from book after cancel")
	}
}

func TestCancelWrongUser(t *testing.T) {
	e := setupEngine()
	defer e.Stop()
	o := submitSync(e, createLimitOrderUser("alice", Buy, "49000", "1.0"))
	if o.Status != New {
		t.Fatalf("expected resting order, got %s", o.Status)
	}
	if _, err := e.Cancel(o.ID, "bob"); err == nil {
		t.Error("expected error when cancelling another user's order")
	}
}

func TestPostOnlyRejectsCrossing(t *testing.T) {
	e := setupEngine()
	defer e.Stop()
	submitSync(e, createLimitOrder(Sell, "50000", "1.0"))
	drainTrades(e.Trades)
	po := createLimitOrder(Buy, "50000", "1.0")
	po.Type = PostOnly
	po.PostOnly = true
	out := submitSync(e, po)
	if out.Status != Rejected {
		t.Errorf("post-only should reject when it would cross; status=%s", out.Status)
	}
}

func TestPostOnlyRests(t *testing.T) {
	e := setupEngine()
	defer e.Stop()
	po := createLimitOrder(Buy, "49000", "1.0")
	po.Type = PostOnly
	po.PostOnly = true
	out := submitSync(e, po)
	if out.Status != New {
		t.Errorf("post-only should rest when not crossing; status=%s", out.Status)
	}
}

func TestSelfTradePrevention(t *testing.T) {
	e := setupEngine()
	e.SetSelfTradePrevention(STPCancelMaker)
	defer e.Stop()
	// Alice rests a sell; Alice then buys against it -> maker cancelled, taker
	// continues (no liquidity so it rests as a new bid).
	maker := submitSync(e, createLimitOrderUser("alice", Sell, "50000", "1.0"))
	if maker.Status != New {
		t.Fatalf("expected maker to rest, got %s", maker.Status)
	}
	taker := createLimitOrderUser("alice", Buy, "50000", "1.0")
	taker.STP = STPCancelMaker
	out := submitSync(e, taker)
	// No trade should have been produced. Drain any pending trades and assert
	// the channel is empty (give the engine a brief moment to flush).
	time.Sleep(10 * time.Millisecond)
	if got := drainTradesCount(e.Trades); len(got) != 0 {
		t.Errorf("STP should prevent self-trade, got %d trades", len(got))
	}
	// Maker should be gone.
	if e.OrderBook.Get(maker.ID) != nil {
		t.Error("maker should have been cancelled by STP")
	}
	// Taker should rest as a new bid (it could not match anything).
	if out.Status != New {
		t.Errorf("taker should rest as new bid after STP, got %s", out.Status)
	}
}

func TestStopLossTrigger(t *testing.T) {
	e := setupEngine()
	defer e.Stop()
	// Establish a last trade price of 50000 via a match.
	submitSync(e, createLimitOrderUser("maker", Sell, "50000", "1.0"))
	submitSync(e, createLimitOrderUser("taker", Buy, "50000", "1.0"))
	waitForTrades(e.Trades, 1)
	// Buy stop at 50100 should NOT trigger yet (last=50000 < 50100).
	stop := createLimitOrderUser("alice", Buy, "0", "1.0")
	stop.Type = StopLoss
	sp, _ := new(big.Float).SetString("50100")
	stop.StopPrice = sp
	stop.Price = nil
	out := submitSync(e, stop)
	if out.Status != New {
		t.Fatalf("expected stop to rest (parked), got %s", out.Status)
	}
	if e.OrderBook.StopCount() != 1 {
		t.Fatalf("expected 1 parked stop, got %d", e.OrderBook.StopCount())
	}
	// Now push last trade price to 50100 to trigger.
	submitSync(e, createLimitOrderUser("maker2", Sell, "50100", "1.0"))
	submitSync(e, createLimitOrderUser("taker2", Buy, "50100", "1.0"))
	// Wait for the stop to be processed (it converts to market and sweeps).
	// The first trade was the maker2/taker2 match, the next is the triggered
	// stop sweeping maker2's remaining liquidity. The stop is a buy market of
	// qty 1.0; maker2 already filled taker2 so there is no remaining ask.
	// Therefore the stop becomes a cancelled/partially filled resting market.
	// We only assert that the stop is no longer parked.
	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if e.OrderBook.StopCount() == 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if e.OrderBook.StopCount() != 0 {
		t.Errorf("expected stop to be triggered and removed, got %d parked", e.OrderBook.StopCount())
	}
}

// TestMarketDataRecorder exercises the ticker / candle aggregation paths so the
// new market-data code is covered.
func TestMarketDataRecorder(t *testing.T) {
	r := NewMarketDataRecorder("BTC/USDT")
	now := time.Now().UnixNano()
	p1, _ := new(big.Float).SetString("50000")
	p2, _ := new(big.Float).SetString("51000")
	p3, _ := new(big.Float).SetString("49000")
	q, _ := new(big.Float).SetString("1.0")
	r.RecordTrade(&Trade{ID: "t1", Pair: "BTC/USDT", Price: p1, Quantity: q, CreatedAt: now})
	r.RecordTrade(&Trade{ID: "t2", Pair: "BTC/USDT", Price: p2, Quantity: q, CreatedAt: now + 1})
	r.RecordTrade(&Trade{ID: "t3", Pair: "BTC/USDT", Price: p3, Quantity: q, CreatedAt: now + 2})
	// Idempotency.
	r.RecordTrade(&Trade{ID: "t1", Pair: "BTC/USDT", Price: p1, Quantity: q, CreatedAt: now})

	bid, _ := new(big.Float).SetString("48999")
	ask, _ := new(big.Float).SetString("49001")
	ticker := r.Ticker(bid, ask)
	if ticker.LastPrice.Cmp(p3) != 0 {
		t.Errorf("expected last %s, got %s", p3, ticker.LastPrice)
	}
	if ticker.High24H.Cmp(p2) != 0 {
		t.Errorf("expected high %s, got %s", p2, ticker.High24H)
	}
	if ticker.Low24H.Cmp(p3) != 0 {
		t.Errorf("expected low %s, got %s", p3, ticker.Low24H)
	}
	if ticker.Volume24H.Cmp(big.NewFloat(3)) != 0 {
		t.Errorf("expected volume 3, got %s", ticker.Volume24H)
	}
	if ticker.Bid.Cmp(bid) != 0 || ticker.Ask.Cmp(ask) != 0 {
		t.Errorf("bid/ask mismatch: bid=%s ask=%s", ticker.Bid, ticker.Ask)
	}

	candles := r.Candles("1m", 100, 0, 0)
	if len(candles) != 1 {
		t.Fatalf("expected 1 candle for 1m, got %d", len(candles))
	}
	c := candles[0]
	if c.Open.Cmp(p1) != 0 || c.Close.Cmp(p3) != 0 {
		t.Errorf("candle open=%s close=%s (want %s %s)", c.Open, c.Close, p1, p3)
	}
	if c.High.Cmp(p2) != 0 || c.Low.Cmp(p3) != 0 {
		t.Errorf("candle high=%s low=%s (want %s %s)", c.High, c.Low, p2, p3)
	}

	recent := r.RecentTrades(2)
	if len(recent) != 2 {
		t.Fatalf("expected 2 recent trades, got %d", len(recent))
	}
	if recent[0].ID != "t3" {
		t.Errorf("expected newest first, got %s", recent[0].ID)
	}
}

// TestEngineTickerAndCandles verifies the engine wires the market-data recorder
// into the trade path so Ticker()/Candles() return real data after matches.
func TestEngineTickerAndCandles(t *testing.T) {
	e := setupEngine()
	defer e.Stop()
	submitSync(e, createLimitOrderUser("m", Sell, "50000", "1.0"))
	submitSync(e, createLimitOrderUser("t", Buy, "50000", "1.0"))
	waitForTrades(e.Trades, 1)

	ticker := e.MD.Ticker(big.NewFloat(50000), big.NewFloat(50001))
	if ticker.LastPrice.Cmp(big.NewFloat(50000)) != 0 {
		t.Errorf("expected last 50000, got %s", ticker.LastPrice)
	}
	if ticker.Volume24H.Cmp(big.NewFloat(1)) != 0 {
		t.Errorf("expected volume 1, got %s", ticker.Volume24H)
	}

	candles := e.MD.Candles("1m", 10, 0, 0)
	if len(candles) != 1 {
		t.Fatalf("expected 1 candle, got %d", len(candles))
	}
	if candles[0].Close.Cmp(big.NewFloat(50000)) != 0 {
		t.Errorf("expected close 50000, got %s", candles[0].Close)
	}

	recent := e.RecentTrades(10)
	if len(recent) != 1 {
		t.Errorf("expected 1 recent trade, got %d", len(recent))
	}
}

// TestWALAppendFailureRejectsOrder verifies the durability-gap policy: when
// the WAL cannot be appended, the engine must reject the operation and report
// the error back to the submitter (never match-and-lose), latch unhealthy,
// and count the failure.
func TestWALAppendFailureRejectsOrder(t *testing.T) {
	e := NewMatchingEngine("BTC/USDT", 1024)
	w, err := NewWALWriter(filepath.Join(t.TempDir(), "BTC-USDT.wal"))
	if err != nil {
		t.Fatalf("open wal: %v", err)
	}
	e.SetWAL(w)
	e.Start()
	defer e.Stop()

	// Healthy path: order rests normally.
	resting := submitSync(e, createLimitOrder(Sell, "50000", "1.0"))
	if resting.Status != New {
		t.Fatalf("expected resting order, got %s", resting.Status)
	}
	if !e.IsHealthy() {
		t.Fatal("engine should be healthy before WAL failure")
	}

	// Break the WAL (simulate disk failure).
	if err := w.file.Close(); err != nil {
		t.Fatalf("close wal file: %v", err)
	}

	// The crossing buy must be rejected with the WAL error, not matched.
	_, err = e.SubmitOrderSync(createLimitOrder(Buy, "50000", "1.0"))
	if err == nil {
		t.Fatal("expected error when WAL append fails")
	}
	if e.IsHealthy() {
		t.Error("engine must report unhealthy after WAL failure")
	}
	if e.WALFailureCount() != 1 {
		t.Errorf("expected 1 WAL failure, got %d", e.WALFailureCount())
	}
	if e.Stats.WALErrors.Load() != 1 {
		t.Errorf("WALErrors counter mismatch: %d", e.Stats.WALErrors.Load())
	}
	// No trade may have been produced from the rejected order.
	time.Sleep(20 * time.Millisecond)
	if got := drainTradesCount(e.Trades); len(got) != 0 {
		t.Errorf("rejected order must not match, got %d trades", len(got))
	}

	// Cancel must also be refused (it cannot be journaled), and the order
	// must remain in the book.
	if _, err := e.Cancel(resting.ID, "u1"); err == nil {
		t.Fatal("expected cancel to fail when WAL is broken")
	}
	if e.OrderBook.Get(resting.ID) == nil {
		t.Error("order must remain in book when cancel is not durable")
	}
}

func BenchmarkMatchingEngine(b *testing.B) {
	e := NewMatchingEngine("BTC/USDT", 1_000_000)
	e.Start()
	defer e.Stop()
	prices := []string{"50000", "50001", "49999"}
	side := Buy
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if side == Sell {
			side = Buy
		} else {
			side = Sell
		}
		e.SubmitOrder(createLimitOrder(side, prices[i%3], "1.0"))
	}
}

func BenchmarkMPRingBuffer(b *testing.B) {
	rb := NewMPRingBuffer(10_000_000)
	o := createLimitOrder(Buy, "50000", "1.0")
	var wg sync.WaitGroup
	for p := 0; p < 4; p++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < b.N/4; i++ {
				rb.Enqueue(o)
			}
		}()
	}
	go func() {
		for {
			rb.Drain()
		}
	}()
	wg.Wait()
}
