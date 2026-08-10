package pnl

import (
	"fmt"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/WkT010/nexa-exchange/internal/matching"
)

// bf parses a decimal string into a *big.Float, failing the test on error.
func bf(t *testing.T, s string) *big.Float {
	t.Helper()
	f, ok := new(big.Float).SetString(s)
	if !ok {
		t.Fatalf("bad test constant %q", s)
	}
	return f
}

// mustBf is the non-test variant used inside helpers.
func mustBf(s string) *big.Float {
	f, ok := new(big.Float).SetString(s)
	if !ok {
		panic("bad test constant: " + s)
	}
	return f
}

// assertEq compares a *big.Float against a decimal string.
func assertEq(t *testing.T, label string, got *big.Float, want string) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s: got nil, want %s", label, want)
	}
	w := mustBf(want)
	if got.Cmp(w) != 0 {
		t.Errorf("%s = %s, want %s", label, got.Text('f', 10), want)
	}
}

func mkFill(taker, maker, pair string, side matching.Side, price, qty string) *matching.FillNotification {
	return &matching.FillNotification{
		TakerUserID: taker,
		MakerUserID: maker,
		Pair:        pair,
		Side:        side,
		Price:       mustBf(price),
		Quantity:    mustBf(qty),
	}
}

// posOf fetches a user's position for an asset (nil when absent).
func posOf(s *Service, userID, asset string) *Position {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.positions[userID+":"+asset]
}

// TestLongLifecycle walks a long position through open -> add -> partial
// close -> full close and verifies realized/unrealized PnL, AvgCost,
// quantity and the daily ledger at every step.
func TestLongLifecycle(t *testing.T) {
	s := NewService()
	u, cp := "long-user", "counterparty"

	// Open: buy 1 @ 100.
	s.RecordFill(mkFill(u, cp, "BTC/USDT", matching.Buy, "100", "1"))
	p := posOf(s, u, "BTC")
	if p == nil {
		t.Fatal("position not created")
	}
	assertEq(t, "open qty", p.Qty, "1")
	assertEq(t, "open avg cost", p.AvgCost, "100")
	assertEq(t, "open realized", p.RealizedPnL, "0")

	// Add: buy 1 @ 110 -> weighted avg 105.
	s.RecordFill(mkFill(u, cp, "BTC/USDT", matching.Buy, "110", "1"))
	assertEq(t, "add qty", p.Qty, "2")
	assertEq(t, "add avg cost", p.AvgCost, "105")
	assertEq(t, "add realized", p.RealizedPnL, "0")

	// Partial close: sell 1 @ 120 -> realized (120-105)*1 = 15.
	s.RecordFill(mkFill(u, cp, "BTC/USDT", matching.Sell, "120", "1"))
	assertEq(t, "partial qty", p.Qty, "1")
	assertEq(t, "partial realized", p.RealizedPnL, "15")
	// Reducing a position must keep the original cost basis.
	assertEq(t, "partial avg cost", p.AvgCost, "105")

	// Summary: unrealized against ref 130 = (130-105)*1 = 25.
	sum := s.Summary(u, map[string]*big.Float{"BTC": mustBf("130")})
	assertEq(t, "summary unrealized", sum.Unrealized, "25")
	assertEq(t, "summary total realized", sum.TotalRealized, "15")
	assertEq(t, "summary portfolio value", sum.PortfolioValue, "130")
	assertEq(t, "summary today realized", sum.TodayRealized, "15")

	// Full close: sell 1 @ 130 -> realized 15 + 25 = 40.
	s.RecordFill(mkFill(u, cp, "BTC/USDT", matching.Sell, "130", "1"))
	assertEq(t, "final qty", p.Qty, "0")
	assertEq(t, "final avg cost", p.AvgCost, "0")
	assertEq(t, "final realized", p.RealizedPnL, "40")

	// Cash-flow cross-check: -100 -110 +120 +130 = +40.
	sum = s.Summary(u, nil)
	assertEq(t, "cross-checked realized", sum.TotalRealized, "40")

	// Daily ledger: everything happened today.
	assertEq(t, "today realized", sum.TodayRealized, "40")
	hist := s.History(u, 7)
	if len(hist) != 7 {
		t.Fatalf("history length = %d, want 7", len(hist))
	}
	assertEq(t, "history last day", hist[len(hist)-1].Realized, "40")
	wantToday := time.Now().UTC().Format("2006-01-02")
	if hist[len(hist)-1].Date != wantToday {
		t.Errorf("history last date = %s, want %s", hist[len(hist)-1].Date, wantToday)
	}
}

// TestShortLifecycle verifies that shorting deeper never corrupts the sign of
// AvgCost (an audit concern) and that short PnL signs are correct: profit
// when the price falls below the average short entry.
func TestShortLifecycle(t *testing.T) {
	s := NewService()
	u, cp := "short-user", "counterparty"

	// Open short: sell 1 @ 100 -> qty -1, AvgCost must stay positive.
	s.RecordFill(mkFill(u, cp, "ETH/USDT", matching.Sell, "100", "1"))
	p := posOf(s, u, "ETH")
	if p == nil {
		t.Fatal("position not created")
	}
	assertEq(t, "short qty", p.Qty, "-1")
	assertEq(t, "short avg cost", p.AvgCost, "100")
	if p.AvgCost.Sign() < 0 {
		t.Fatalf("AvgCost went negative on short open: %s", p.AvgCost.Text('f', 10))
	}

	// Short deeper: sell 1 @ 90 -> qty -2, weighted avg (100+90)/2 = 95.
	s.RecordFill(mkFill(u, cp, "ETH/USDT", matching.Sell, "90", "1"))
	assertEq(t, "deeper qty", p.Qty, "-2")
	assertEq(t, "deeper avg cost", p.AvgCost, "95")
	if p.AvgCost.Sign() < 0 {
		t.Fatalf("AvgCost went negative on short-to-shorter: %s", p.AvgCost.Text('f', 10))
	}
	assertEq(t, "deeper realized", p.RealizedPnL, "0")

	// Partial cover: buy 1 @ 80 -> profit (95-80)*1 = 15.
	s.RecordFill(mkFill(u, cp, "ETH/USDT", matching.Buy, "80", "1"))
	assertEq(t, "cover qty", p.Qty, "-1")
	assertEq(t, "cover avg cost", p.AvgCost, "95")
	assertEq(t, "cover realized", p.RealizedPnL, "15")

	// Cover at a loss: buy 1 @ 100 -> 15 + (95-100) = 10.
	s.RecordFill(mkFill(u, cp, "ETH/USDT", matching.Buy, "100", "1"))
	assertEq(t, "final qty", p.Qty, "0")
	assertEq(t, "final realized", p.RealizedPnL, "10")

	// Cash-flow cross-check: +100 +90 -80 -100 = +10.
	sum := s.Summary(u, nil)
	assertEq(t, "cross-checked realized", sum.TotalRealized, "10")

	// Unrealized on a short: ref 90 vs avg 95 with qty -1 -> +5 while open.
	s2 := NewService()
	s2.RecordFill(mkFill(u, cp, "ETH/USDT", matching.Sell, "95", "1"))
	sum2 := s2.Summary(u, map[string]*big.Float{"ETH": mustBf("90")})
	assertEq(t, "short unrealized", sum2.Unrealized, "5")
}

// TestFlipLongToShort sells more than the long size: the closed part must
// realize PnL and the remainder must open at the fill price.
func TestFlipLongToShort(t *testing.T) {
	s := NewService()
	u, cp := "flip-user", "counterparty"

	s.RecordFill(mkFill(u, cp, "BTC/USDT", matching.Buy, "100", "1"))
	// Sell 2 @ 110: closes 1 long (profit 10) and opens 1 short @ 110.
	s.RecordFill(mkFill(u, cp, "BTC/USDT", matching.Sell, "110", "2"))

	p := posOf(s, u, "BTC")
	assertEq(t, "flipped qty", p.Qty, "-1")
	assertEq(t, "flipped realized", p.RealizedPnL, "10")
	// The new short leg opened at 110, not at a cost polluted by realized PnL.
	assertEq(t, "flipped avg cost", p.AvgCost, "110")

	// Cover the short @ 110 -> no extra PnL. Total must equal the cash flow
	// -100 +220 -110 = +10.
	s.RecordFill(mkFill(u, cp, "BTC/USDT", matching.Buy, "110", "1"))
	assertEq(t, "after cover realized", p.RealizedPnL, "10")
	assertEq(t, "after cover qty", p.Qty, "0")
}

// TestFlipShortToLong mirrors the flip test from the short side.
func TestFlipShortToLong(t *testing.T) {
	s := NewService()
	u, cp := "flip2-user", "counterparty"

	s.RecordFill(mkFill(u, cp, "SOL/USDT", matching.Sell, "100", "1"))
	// Buy 2 @ 90: covers the short (profit 10) and opens 1 long @ 90.
	s.RecordFill(mkFill(u, cp, "SOL/USDT", matching.Buy, "90", "2"))

	p := posOf(s, u, "SOL")
	assertEq(t, "flipped qty", p.Qty, "1")
	assertEq(t, "flipped realized", p.RealizedPnL, "10")
	assertEq(t, "flipped avg cost", p.AvgCost, "90")

	// Sell the long @ 95 -> +5; total matches cash flow +100 -180 +95 = +15.
	s.RecordFill(mkFill(u, cp, "SOL/USDT", matching.Sell, "95", "1"))
	assertEq(t, "final realized", p.RealizedPnL, "15")
	assertEq(t, "final qty", p.Qty, "0")
}

// TestRecordFillDefensive exercises nil/malformed inputs and zero edges.
func TestRecordFillDefensive(t *testing.T) {
	s := NewService()

	// None of these may panic or create state.
	s.RecordFill(nil)
	s.RecordFill(&matching.FillNotification{Pair: "BTC/USDT", Price: nil, Quantity: mustBf("1")})
	s.RecordFill(&matching.FillNotification{Pair: "BTC/USDT", Price: mustBf("1"), Quantity: nil})
	s.RecordFill(&matching.FillNotification{Pair: "BTCUSDT", Price: mustBf("1"), Quantity: mustBf("1")}) // no slash
	s.RecordFill(mkFill("", "", "BTC/USDT", matching.Buy, "100", "1"))                                   // both ids empty

	if len(s.positions) != 0 {
		t.Fatalf("defensive inputs created %d positions", len(s.positions))
	}

	// Zero quantity: nothing moves.
	u := "zero-user"
	s.RecordFill(mkFill(u, "cp", "BTC/USDT", matching.Buy, "100", "0"))
	p := posOf(s, u, "BTC")
	if p == nil {
		t.Fatal("position entry missing after zero-qty fill")
	}
	assertEq(t, "zero qty", p.Qty, "0")
	assertEq(t, "zero qty realized", p.RealizedPnL, "0")

	// Zero price on open: cost basis 0, no panic.
	s.RecordFill(mkFill(u, "cp", "BTC/USDT", matching.Buy, "0", "1"))
	assertEq(t, "zero price qty", p.Qty, "1")
	assertEq(t, "zero price avg cost", p.AvgCost, "0")
}

// TestRecordFeeAndPortfolioValue covers fee accumulation and the
// balance-based portfolio valuation.
func TestRecordFeeAndPortfolioValue(t *testing.T) {
	s := NewService()

	s.RecordFee("u1", "USDT", mustBf("0.5"))
	s.RecordFee("u1", "USDT", mustBf("1.25"))
	s.RecordFee("u1", "USDT", nil)            // ignored
	s.RecordFee("u1", "USDT", new(big.Float)) // zero, ignored
	s.RecordFee("", "USDT", mustBf("9"))      // empty user, ignored

	sum := s.Summary("u1", nil)
	assertEq(t, "total fees", sum.TotalFees, "1.75")

	refs := map[string]*big.Float{"BTC": mustBf("50000")}
	balances := map[string]*big.Float{
		"USDT": mustBf("1000"),
		"BTC":  mustBf("0.5"),
		"XYZ":  mustBf("7"), // no reference price -> skipped
		"NIL":  nil,         // nil balance -> skipped
	}
	pv := s.PortfolioValue("u1", balances, refs)
	assertEq(t, "portfolio value", pv, "26000") // 1000 + 0.5*50000
}

// TestHistoryClamping verifies the days-window clamping rules.
func TestHistoryClamping(t *testing.T) {
	s := NewService()
	if got := s.History("u", 0); len(got) != 30 {
		t.Errorf("days<=0 should default to 30, got %d", len(got))
	}
	if got := s.History("u", -5); len(got) != 30 {
		t.Errorf("negative days should default to 30, got %d", len(got))
	}
	if got := s.History("u", 1000); len(got) != 365 {
		t.Errorf("days>365 should clamp to 365, got %d", len(got))
	}
	hist := s.History("u", 3)
	for i, d := range hist {
		if d.Realized.Sign() != 0 {
			t.Errorf("day %d realized = %s, want 0", i, d.Realized.Text('f', 10))
		}
	}
}

// TestMakerSideAccounting checks the counterparty leg of a fill: a taker buy
// is a maker sell and vice versa.
func TestMakerSideAccounting(t *testing.T) {
	s := NewService()
	taker, maker := "taker", "maker"

	// Seed the maker with a long bought at 90.
	s.RecordFill(mkFill(maker, "seed", "BTC/USDT", matching.Buy, "90", "1"))
	// Taker buys 1 @ 100 -> maker sells 1 @ 100, realizing +10.
	s.RecordFill(mkFill(taker, maker, "BTC/USDT", matching.Buy, "100", "1"))

	mp := posOf(s, maker, "BTC")
	assertEq(t, "maker qty", mp.Qty, "0")
	assertEq(t, "maker realized", mp.RealizedPnL, "10")

	tp := posOf(s, taker, "BTC")
	assertEq(t, "taker qty", tp.Qty, "1")
	assertEq(t, "taker avg cost", tp.AvgCost, "100")
}

// TestConcurrentRecordFill hammers the service from many goroutines. The
// service documents itself as safe for concurrent use; run with -race to
// verify the locking. Final state must be exact.
func TestConcurrentRecordFill(t *testing.T) {
	s := NewService()
	const goroutines = 32
	const fillsPerG = 25

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(g int) {
			defer wg.Done()
			for i := 0; i < fillsPerG; i++ {
				s.RecordFill(mkFill(
					fmt.Sprintf("user-%d", g%4), "cp",
					"BTC/USDT", matching.Buy, "100", "0.125",
				))
			}
		}(g)
	}
	wg.Wait()

	for u := 0; u < 4; u++ {
		p := posOf(s, fmt.Sprintf("user-%d", u), "BTC")
		if p == nil {
			t.Fatalf("user-%d position missing", u)
		}
		// 8 goroutines/user * 25 fills * 0.125 = 25 (exact in binary float).
		assertEq(t, fmt.Sprintf("user-%d qty", u), p.Qty, "25")
		assertEq(t, fmt.Sprintf("user-%d avg cost", u), p.AvgCost, "100")
	}
}
