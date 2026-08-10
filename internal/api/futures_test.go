package api

import (
	"math/big"
	"testing"
)

// fb parses a decimal string into a *big.Float, failing on bad input.
func fb(t *testing.T, s string) *big.Float {
	t.Helper()
	f, ok := new(big.Float).SetString(s)
	if !ok {
		t.Fatalf("bad test constant %q", s)
	}
	return f
}

// cmpStr compares got against a decimal string using absolute tolerance eps.
func cmpStr(t *testing.T, label string, got *big.Float, want, eps string) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s: got nil, want %s", label, want)
	}
	w := fb(t, want)
	e := fb(t, eps)
	diff := new(big.Float).Sub(got, w)
	diff.Abs(diff)
	if diff.Cmp(e) > 0 {
		t.Errorf("%s = %s, want %s (±%s)", label, got.Text('f', 10), want, eps)
	}
}

func TestCalcFuturesPnL(t *testing.T) {
	cases := []struct {
		name    string
		side    string
		entry   string
		mark    string
		qty     string
		margin  string
		wantPnL string
		wantPct string
	}{
		{"long profit", "long", "100", "110", "2", "20", "20", "100"},
		{"long loss", "long", "100", "90", "1", "10", "-10", "-100"},
		{"short profit", "short", "100", "90", "2", "20", "20", "100"},
		{"short loss", "short", "100", "110", "1", "10", "-10", "-100"},
		{"flat", "long", "100", "100", "5", "50", "0", "0"},
		{"zero margin pct stays 0", "long", "100", "110", "1", "0", "10", "0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pnl, pct := calcFuturesPnL(tc.side, fb(t, tc.entry), fb(t, tc.mark), fb(t, tc.qty), 10, fb(t, tc.margin))
			cmpStr(t, "pnl", pnl, tc.wantPnL, "1e-9")
			cmpStr(t, "pnl_pct", pct, tc.wantPct, "1e-9")
		})
	}

	// nil margin must not panic and must report 0%.
	pnl, pct := calcFuturesPnL("long", fb(t, "100"), fb(t, "110"), fb(t, "1"), 10, nil)
	cmpStr(t, "nil-margin pnl", pnl, "10", "1e-9")
	cmpStr(t, "nil-margin pct", pct, "0", "1e-9")

	// PnL is the notional move; leverage is already embedded in the margin,
	// so changing the leverage argument alone must not change the PnL.
	p1, _ := calcFuturesPnL("long", fb(t, "100"), fb(t, "110"), fb(t, "1"), 5, fb(t, "20"))
	p2, _ := calcFuturesPnL("long", fb(t, "100"), fb(t, "110"), fb(t, "1"), 50, fb(t, "2"))
	if p1.Cmp(p2) != 0 {
		t.Errorf("pnl depends on leverage: %s vs %s", p1.Text('f', 10), p2.Text('f', 10))
	}
}

func TestCalcMargin(t *testing.T) {
	cmpStr(t, "10x", calcMargin(fb(t, "100"), fb(t, "2"), 10), "20", "1e-9")
	cmpStr(t, "1x", calcMargin(fb(t, "100"), fb(t, "2"), 1), "200", "1e-9")
	cmpStr(t, "125x", calcMargin(fb(t, "100"), fb(t, "1"), 125), "0.8", "1e-9")
}

// TestCalcLiqPrice derives the liquidation price from first principles:
// isolated liquidation happens when the loss eats the initial margin minus
// the maintenance margin. For a long:
//
//	(entry - liq) * qty = margin - mm*notional
//	(entry - liq)        = entry/leverage - mm*entry
//	liq                  = entry * (1 - 1/leverage + mm)
//
// and symmetrically liq = entry * (1 + 1/leverage - mm) for a short. This is
// the same formula Binance publishes for isolated margin. At leverage 1 the
// margin covers the whole notional, so liquidation requires an almost total
// collapse: liq = entry * mm = 0.005*entry. That is economically correct —
// not a bug — and is asserted explicitly below.
func TestCalcLiqPrice(t *testing.T) {
	mm := "0.005"
	cases := []struct {
		name string
		side string
		lev  int
		want string // for entry = 100
	}{
		// long: 100*(1 - 1/lev + 0.005)
		{"long 1x boundary", "long", 1, "0.5"}, // 100*0.005 — loses 99.5% before liq
		{"long 2x", "long", 2, "50.5"},         // 100*0.505
		{"long 10x", "long", 10, "90.5"},       // 100*0.905
		{"long 125x", "long", 125, "99.7"},     // 100*(1-0.008+0.005)
		// short: 100*(1 + 1/lev - 0.005)
		{"short 1x boundary", "short", 1, "199.5"},
		{"short 2x", "short", 2, "149.5"},
		{"short 10x", "short", 10, "109.5"},
	}
	entry := fb(t, "100")
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := calcLiqPrice(tc.side, entry, tc.lev)
			cmpStr(t, "liq", got, tc.want, "1e-9")
			if got.Sign() <= 0 {
				t.Errorf("liq price must stay positive, got %s", got.Text('f', 10))
			}
		})
	}

	// Higher leverage must push the liquidation price closer to entry: the
	// trader tolerates a smaller adverse move with less skin in the game.
	prevLong := big.NewFloat(0)
	for _, lev := range []int{1, 2, 5, 10, 50, 125} {
		liq := calcLiqPrice("long", entry, lev)
		if liq.Cmp(prevLong) <= 0 {
			t.Errorf("long liq not increasing with leverage at %dx: %s <= %s", lev, liq.Text('f', 10), prevLong.Text('f', 10))
		}
		prevLong = liq
	}
	prevShort := fb(t, "1e9")
	for _, lev := range []int{1, 2, 5, 10, 50, 125} {
		liq := calcLiqPrice("short", entry, lev)
		if liq.Cmp(prevShort) >= 0 {
			t.Errorf("short liq not decreasing with leverage at %dx", lev)
		}
		prevShort = liq
	}

	// Independent derivation check (no shared formula with calcLiqPrice):
	// the admissible loss is margin - maintenance.
	lev := 10
	margin := calcMargin(entry, fb(t, "1"), lev)                            // 10
	notional := new(big.Float).Copy(entry)                                  // qty=1
	maint := new(big.Float).Mul(notional, fb(t, mm))                        // 0.5
	liqLong := new(big.Float).Sub(entry, new(big.Float).Sub(margin, maint)) // 90.5
	cmpStr(t, "derived long 10x", calcLiqPrice("long", entry, lev), liqLong.Text('f', 10), "1e-9")
}

// TestLiqPriceConsistentWithIsLiquidated asserts that the displayed
// LiqPrice is exactly the price at which the runtime isLiquidated check
// fires. A mismatch means the UI says "liquidated" while the engine
// disagrees (or vice versa).
func TestLiqPriceConsistentWithIsLiquidated(t *testing.T) {
	h := NewFuturesHandler(nil, nil, nil) // memory store, no wallet, no prices

	for _, side := range []string{"long", "short"} {
		for _, lev := range []int{1, 2, 10, 50} {
			entry := fb(t, "100")
			qty := fb(t, "1")
			pos := &FuturesPosition{
				Side: side, Leverage: lev, MarginMode: "isolated",
				EntryPrice: new(big.Float).Copy(entry), Quantity: new(big.Float).Copy(qty),
				Margin: calcMargin(entry, qty, lev),
			}
			liq := calcLiqPrice(side, entry, lev)

			// Just beyond the liquidation price the position must be
			// liquidatable. A small epsilon past the boundary avoids asserting
			// on big.Float rounding of the binary-inexact 0.5% rate exactly
			// at the boundary.
			beyond := new(big.Float).Copy(liq)
			if side == "long" {
				beyond.Sub(beyond, fb(t, "0.01"))
			} else {
				beyond.Add(beyond, fb(t, "0.01"))
			}
			if !h.isLiquidated(pos, beyond) {
				t.Errorf("%s %dx: not liquidated just past displayed liq price %s", side, lev, liq.Text('f', 10))
			}

			// A clearly safer price must not liquidate.
			safe := fb(t, "100") // == entry: zero PnL, full margin remains
			if h.isLiquidated(pos, safe) {
				t.Errorf("%s %dx: liquidated at entry price", side, lev)
			}

			// One step away from liq towards entry must stay alive. The step
			// (0.05) is far above any float rounding yet small enough that
			// the position is still losing roughly its full margin only at
			// very high leverage where mm*tolerance still dominates.
			step := fb(t, "0.05")
			if side == "long" {
				above := new(big.Float).Add(liq, step)
				if h.isLiquidated(pos, above) {
					t.Errorf("%s %dx: liquidated just above liq price", side, lev)
				}
			} else {
				below := new(big.Float).Sub(liq, step)
				if h.isLiquidated(pos, below) {
					t.Errorf("%s %dx: liquidated just below liq price", side, lev)
				}
			}
		}
	}

	// Zero or nil margin is always liquidated.
	broken := &FuturesPosition{Side: "long", Margin: new(big.Float)}
	if !h.isLiquidated(broken, fb(t, "100")) {
		t.Error("zero margin must be liquidated")
	}
	broken.Margin = nil
	if !h.isLiquidated(broken, fb(t, "100")) {
		t.Error("nil margin must be liquidated")
	}
}

// TestMergeSameSidePosition verifies weighted-average entry and margin growth
// when adding to an open position.
func TestMergeSameSidePosition(t *testing.T) {
	h := NewFuturesHandler(nil, nil, nil)
	pos := &FuturesPosition{
		Side: "long", Leverage: 10, MarginMode: "isolated",
		EntryPrice: fb(t, "100"), Quantity: fb(t, "1"), Margin: fb(t, "10"),
	}
	h.mergeSameSidePosition(pos, fb(t, "110"), fb(t, "1"), fb(t, "11"), 10)

	cmpStr(t, "merged qty", pos.Quantity, "2", "1e-9")
	cmpStr(t, "merged entry", pos.EntryPrice, "105", "1e-9") // (100+110)/2
	cmpStr(t, "merged margin", pos.Margin, "21", "1e-9")
	// LiqPrice recomputed from the new blended entry.
	wantLiq := calcLiqPrice("long", fb(t, "105"), 10)
	if pos.LiqPrice.Cmp(wantLiq) != 0 {
		t.Errorf("merged liq = %s, want %s", pos.LiqPrice.Text('f', 10), wantLiq.Text('f', 10))
	}
}

// TestFundingRateDeterministic documents the current pseudo-random funding
// implementation: it is a pure function of the pair name (FNV hash), so it is
// deterministic across calls and instances — but it is not market-driven.
// Recommended improvement (see report): derive the rate from the premium
// between mark and index price plus a clamped interest-rate component.
func TestFundingRateDeterministic(t *testing.T) {
	for _, pair := range []string{"BTC/USDT", "ETH/USDT", "SOL/USDT"} {
		first := fundingRate(pair)
		for i := 0; i < 5; i++ {
			if fundingRate(pair).Cmp(first) != 0 {
				t.Fatalf("fundingRate(%s) not deterministic", pair)
			}
		}
		// Range: (h%200 - 100)/10000 ∈ [-0.01, +0.0099] (±0.01%).
		if first.Cmp(fb(t, "-0.01")) < 0 || first.Cmp(fb(t, "0.01")) > 0 {
			t.Errorf("fundingRate(%s) = %s outside ±0.01", pair, first.Text('f', 10))
		}
	}
	if fnvHash("BTC/USDT") != fnvHash("BTC/USDT") {
		t.Error("fnvHash not deterministic")
	}
	if fnvHash("BTC/USDT") == fnvHash("ETH/USDT") {
		t.Error("fnvHash collision on distinct pairs")
	}
}

func TestFuturesHelpers(t *testing.T) {
	for in, want := range map[string]string{"long": "long", "LONG": "long", "buy": "long", "short": "short", "sell": "short", "Sell": "short"} {
		got, ok := parseFuturesSide(in)
		if !ok || got != want {
			t.Errorf("parseFuturesSide(%q) = %q,%v; want %q,true", in, got, ok, want)
		}
	}
	if _, ok := parseFuturesSide("up"); ok {
		t.Error("parseFuturesSide accepted garbage")
	}

	if got := quoteAsset("BTC/USDT"); got != "USDT" {
		t.Errorf("quoteAsset = %q", got)
	}
	if got := quoteAsset("BTCUSDT"); got != "USDT" {
		t.Errorf("quoteAsset fallback = %q", got)
	}

	if _, ok := parseBigFloat("not-a-number"); ok {
		t.Error("parseBigFloat accepted garbage")
	}
	if f, ok := parseBigFloat("1.5"); !ok || f.Cmp(fb(t, "1.5")) != 0 {
		t.Error("parseBigFloat failed on 1.5")
	}

	if safeFloatStr(nil) != "0" {
		t.Error("safeFloatStr(nil) != \"0\"")
	}
	if got := safeFloatStr(fb(t, "2.5")); got != "2.5" {
		t.Errorf("safeFloatStr = %q", got)
	}
}

// TestMemoryFuturesStore covers the default in-memory store used when no
// persistent store is configured, including per-user isolation.
func TestMemoryFuturesStore(t *testing.T) {
	h := NewFuturesHandler(nil, nil, nil)
	store := h.store

	p1 := &FuturesPosition{ID: "p1", UserID: "alice", Pair: "BTC/USDT", Status: "open"}
	p2 := &FuturesPosition{ID: "p2", UserID: "bob", Pair: "ETH/USDT", Status: "closed"}
	if err := store.SavePosition(p1); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePosition(p2); err != nil {
		t.Fatal(err)
	}

	if _, err := store.GetPosition("p1", "bob"); err == nil {
		t.Error("cross-user GetPosition must fail")
	}
	if got, err := store.GetPosition("p1", "alice"); err != nil || got.ID != "p1" {
		t.Errorf("owner GetPosition failed: %v", err)
	}
	if got, _ := store.ListPositions("alice"); len(got) != 1 {
		t.Errorf("ListPositions(alice) = %d, want 1", len(got))
	}
	if got, _ := store.ListOpenPositions(); len(got) != 1 || got[0].ID != "p1" {
		t.Errorf("ListOpenPositions unexpected: %v", got)
	}

	o := &FuturesOrder{ID: "o1", UserID: "alice", Status: "open"}
	if err := store.SaveOrder(o); err != nil {
		t.Fatal(err)
	}
	open, _ := store.ListOpenOrders()
	if len(open) != 1 {
		t.Fatalf("ListOpenOrders = %d, want 1", len(open))
	}
	if err := store.UpdateOrderStatus("o1", "cancelled"); err != nil {
		t.Fatal(err)
	}
	open, _ = store.ListOpenOrders()
	if len(open) != 0 {
		t.Errorf("open orders after cancel = %d, want 0", len(open))
	}
}
