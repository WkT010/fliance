package matching

import (
	"math/big"
	"sync"
	"testing"
)

func setupEngine() *MatchingEngine {
	e := NewMatchingEngine("BTC/USDT", 100000)
	e.Start(); return e
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
	select { case tr := <-e.Trades:
		if tr.Price.Cmp(big.NewFloat(50000)) != 0 { t.Errorf("price bad: %s", tr.Price.String()) }
		if tr.Quantity.Cmp(big.NewFloat(1.0)) != 0 { t.Errorf("qty bad: %s", tr.Quantity.String()) }
	default: t.Error("no trade")
	}
}

func TestPriceTimePriority(t *testing.T) {
	e := setupEngine(); defer e.Stop()
	s1, s2, s3 := createLimitOrder(Sell, "50000", "1.0"), createLimitOrder(Sell, "50000", "1.0"), createLimitOrder(Sell, "50000", "1.0")
	e.SubmitOrder(s1); e.SubmitOrder(s2); e.SubmitOrder(s3)
	e.SubmitOrder(createLimitOrder(Buy, "50000", "3.0"))
	trs := drainTrades(e.Trades)
	if len(trs) != 3 { t.Fatalf("expected 3 trades, got %d", len(trs)) }
	if trs[0].SellOrderID != s1.ID { t.Error("expected s1 first") }
	if trs[1].SellOrderID != s2.ID { t.Error("expected s2 second") }
}

func TestMarketOrder(t *testing.T) {
	e := setupEngine(); defer e.Stop()
	e.SubmitOrder(createLimitOrder(Sell, "49000", "0.5"))
	e.SubmitOrder(createLimitOrder(Sell, "50000", "0.5"))
	e.SubmitOrder(createMarketOrder(Buy, "1.0"))
	trs := drainTrades(e.Trades)
	if len(trs) != 2 { t.Fatalf("expected 2 trades, got %d", len(trs)) }
}

func TestFOKRejected(t *testing.T) {
	e := setupEngine(); defer e.Stop()
	e.SubmitOrder(createLimitOrder(Sell, "50000", "1.0"))
	fok := createLimitOrder(Buy, "50000", "2.0"); fok.TimeInForce = FOK; fok.Type = FillOrKill
	e.SubmitOrder(fok)
	if fok.Status != Rejected { t.Error("FOK should reject") }
}

func BenchmarkMatchingEngine(b *testing.B) {
	e := NewMatchingEngine("BTC/USDT", 1_000_000)
	e.Start(); defer e.Stop()
	prices := []string{"50000","50001","49999"}
	side := Buy; b.ResetTimer(); b.ReportAllocs()
	for i := 0; i < b.N; i++ { e.SubmitOrder(createLimitOrder(side, prices[i%3], "1.0")); side = 1-side }
}

func BenchmarkMPRingBuffer(b *testing.B) {
	rb := NewMPRingBuffer(10_000_000)
	o := createLimitOrder(Buy, "50000", "1.0")
	var wg sync.WaitGroup
	for p := 0; p < 4; p++ { wg.Add(1); go func() { defer wg.Done(); for i := 0; i < b.N/4; i++ { rb.Enqueue(o) } }() }
	go func() { for { rb.Drain() } }()
	wg.Wait()
}

func drainTrades(ch <-chan *Trade) []*Trade {
	var t []*Trade
	for { select { case x := <-ch: t = append(t, x); default: return t } }
}
