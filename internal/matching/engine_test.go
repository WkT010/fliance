package matching

import (
	"math/big"
	"sync"
	"testing"
)

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

func createMarketOrder(side Side, qty string) *Order {
	q, _ := new(big.Float).SetString(qty)
	return NewOrder("u1", "BTC/USDT", side, Market, nil, q)
}

func TestSimpleMatch(t *testing.T) {
	e := setupEngine(); defer e.Stop()
	e.SubmitOrder(createLimitOrder(Sell, "50000", "1.0"))
	e.SubmitOrder(createLimitOrder(Buy, "50000", "1.0"))
	select {
	case tr := <-e.Trades:
		if tr.Price.Cmp(big.NewFloat(50000)) != 0 { t.Errorf("bad price: %s", tr.Price.String()) }
		if tr.Quantity.Cmp(big.NewFloat(1.0)) != 0 { t.Errorf("bad qty: %s", tr.Quantity.String()) }
	default:
		t.Error("no trade produced")
	}
}

func TestPriceTimePriority(t *testing.T) {
	e := setupEngine(); defer e.Stop()
	s1 := createLimitOrder(Sell, "50000", "1.0")
	s2 := createLimitOrder(Sell, "50000", "1.0")
	s3 := createLimitOrder(Sell, "50000", "1.0")
	e.SubmitOrder(s1); e.SubmitOrder(s2); e.SubmitOrder(s3)
	e.SubmitOrder(createLimitOrder(Buy, "50000", "3.0"))
	trades := drainTrades(e.Trades)
	if len(trades) != 3 { t.Fatalf("expected 3 trades, got %d", len(trades)) }
	if trades[0].SellOrderID != s1.ID { t.Error("expected s1 first") }
}

func TestPricePriority(t *testing.T) {
	e := setupEngine(); defer e.Stop()
	e.SubmitOrder(createLimitOrder(Sell, "51000", "1.0"))
	e.SubmitOrder(createLimitOrder(Sell, "50000", "1.0"))
	e.SubmitOrder(createLimitOrder(Sell, "52000", "1.0"))
	e.SubmitOrder(createLimitOrder(Buy, "52000", "3.0"))
	trades := drainTrades(e.Trades)
	if len(trades) != 3 { t.Fatalf("expected 3 trades, got %d", len(trades)) }
	if trades[0].Price.Cmp(big.NewFloat(50000)) != 0 { t.Error("expected first fill at 50000") }
}

func TestMarketOrder(t *testing.T) {
	e := setupEngine(); defer e.Stop()
	e.SubmitOrder(createLimitOrder(Sell, "49000", "0.5"))
	e.SubmitOrder(createLimitOrder(Sell, "50000", "0.5"))
	e.SubmitOrder(createMarketOrder(Buy, "1.0"))
	trades := drainTrades(e.Trades)
	if len(trades) != 2 { t.Fatalf("expected 2 trades, got %d", len(trades)) }
}

func TestFOKAccepted(t *testing.T) {
	e := setupEngine(); defer e.Stop()
	e.SubmitOrder(createLimitOrder(Sell, "50000", "1.0"))
	e.SubmitOrder(createLimitOrder(Sell, "50100", "1.0"))
	fok := createLimitOrder(Buy, "50000", "2.0"); fok.TimeInForce = FOK; fok.Type = FillOrKill
	e.SubmitOrder(fok)
	if fok.Status == Rejected { t.Error("FOK should NOT reject when enough liquidity") }
}

func TestFOKRejected(t *testing.T) {
	e := setupEngine(); defer e.Stop()
	e.SubmitOrder(createLimitOrder(Sell, "50000", "1.0"))
	fok := createLimitOrder(Buy, "50000", "2.0"); fok.TimeInForce = FOK; fok.Type = FillOrKill
	e.SubmitOrder(fok)
	if fok.Status != Rejected { t.Error("FOK should reject when insufficient liquidity") }
}

func TestIOCPartialFill(t *testing.T) {
	e := setupEngine(); defer e.Stop()
	e.SubmitOrder(createLimitOrder(Sell, "50000", "0.3"))
	ioc := createLimitOrder(Buy, "50000", "1.0"); ioc.TimeInForce = IOC; ioc.Type = ImmediateOrCancel
	e.SubmitOrder(ioc)
	trades := drainTrades(e.Trades)
	if len(trades) != 1 { t.Fatalf("expected 1 trade, got %d", len(trades)) }
	if ioc.Status != Cancelled { t.Error("IOC should cancel remaining qty") }
}

func TestOrderBookDepth(t *testing.T) {
	ob := NewOrderBook("BTC/USDT")
	ob.Add(createLimitOrder(Buy, "50000", "1.0"))
	ob.Add(createLimitOrder(Buy, "49900", "2.0"))
	ob.Add(createLimitOrder(Sell, "50100", "1.5"))
	ob.Add(createLimitOrder(Sell, "50200", "0.5"))
	depth := ob.Depth(10)
	if len(depth.Bids) != 2 { t.Fatalf("expected 2 bid levels, got %d", len(depth.Bids)) }
	if len(depth.Asks) != 2 { t.Fatalf("expected 2 ask levels, got %d", len(depth.Asks)) }
	if depth.Bids[0].Price.Cmp(big.NewFloat(50000)) != 0 { t.Errorf("expected best bid 50000, got %s", depth.Bids[0].Price.String()) }
	if depth.Asks[0].Price.Cmp(big.NewFloat(50100)) != 0 { t.Errorf("expected best ask 50100, got %s", depth.Asks[0].Price.String()) }
}

func TestConcurrentEnqueue(t *testing.T) {
	rb := NewMPRingBuffer(1_000_000)
	o := createLimitOrder(Buy, "50000", "1.0")
	var wg sync.WaitGroup
	for p := 0; p < 4; p++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 10000; i++ { rb.Enqueue(o) }
		}()
	}
	done := make(chan struct{})
	go func() {
		for {
			rb.Drain()
			select { case <-done: return; default: }
		}
	}()
	wg.Wait(); close(done); rb.Drain()
	if rb.Len() != 0 { t.Errorf("expected empty buffer, got %d", rb.Len()) }
}

func BenchmarkMatchingEngine(b *testing.B) {
	e := NewMatchingEngine("BTC/USDT", 1_000_000)
	e.Start(); defer e.Stop()
	prices := []string{"50000", "50001", "49999"}
	side := Buy
	b.ResetTimer(); b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if side == Sell { side = Buy } else { side = Sell }
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
			for i := 0; i < b.N/4; i++ { rb.Enqueue(o) }
		}()
	}
	go func() { for { rb.Drain() } }()
	wg.Wait()
}

func drainTrades(ch <-chan *Trade) []*Trade {
	var trades []*Trade
	for {
		select { case t := <-ch: trades = append(trades, t); default: return trades }
	}
}
