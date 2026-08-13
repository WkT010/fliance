package market

import (
	"math/big"
	"testing"
)

// TestApplyTickerCopiesAndAdjusts verifies the adjustment is applied to a
// COPY of the ticker: the cached original must stay untouched while the
// returned view carries mult/offset on every price field.
func TestApplyTickerCopiesAndAdjusts(t *testing.T) {
	adj := NewPriceAdjuster(nil)
	mult, off := big.NewFloat(1.05), big.NewFloat(10)
	if err := adj.Upsert(&PriceAdjustment{Pair: "BTC/USDT", Multiplier: mult, Offset: off}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	orig := &Ticker{
		Pair:      "BTC/USDT",
		Last:      big.NewFloat(100),
		Bid:       big.NewFloat(99),
		Ask:       big.NewFloat(101),
		Spread:    big.NewFloat(2),
		High24h:   big.NewFloat(110),
		Low24h:    big.NewFloat(90),
		Open24h:   big.NewFloat(95),
		Change24h: big.NewFloat(5),
		Timestamp: 123,
	}
	out := adj.ApplyTicker(orig)
	if out == orig {
		t.Fatal("ApplyTicker must return a copy when an adjustment exists")
	}
	// Cache pollution check: none of the original values may change.
	check := map[string][2]*big.Float{
		"Last":    {orig.Last, big.NewFloat(100)},
		"Bid":     {orig.Bid, big.NewFloat(99)},
		"Ask":     {orig.Ask, big.NewFloat(101)},
		"Spread":  {orig.Spread, big.NewFloat(2)},
		"High24h": {orig.High24h, big.NewFloat(110)},
		"Low24h":  {orig.Low24h, big.NewFloat(90)},
		"Open24h": {orig.Open24h, big.NewFloat(95)},
	}
	for name, c := range check {
		if c[0].Cmp(c[1]) != 0 {
			t.Fatalf("cache mutated: %s = %s, want %s", name, c[0].Text('f', 4), c[1].Text('f', 4))
		}
	}
	// Adjusted fields: v*mult + offset.
	want := func(v float64) *big.Float {
		return new(big.Float).Add(new(big.Float).Mul(big.NewFloat(v), mult), off)
	}
	for name, c := range map[string][2]*big.Float{
		"Last":    {out.Last, want(100)},
		"Bid":     {out.Bid, want(99)},
		"Ask":     {out.Ask, want(101)},
		"High24h": {out.High24h, want(110)},
		"Low24h":  {out.Low24h, want(90)},
		"Open24h": {out.Open24h, want(95)},
	} {
		if c[0] == nil || c[0].Cmp(c[1]) != 0 {
			t.Fatalf("adjusted %s = %s, want %s", name, c[0].Text('f', 4), c[1].Text('f', 4))
		}
	}
	// Derived fields are recomputed from the adjusted values.
	if out.Spread == nil || out.Spread.Cmp(new(big.Float).Sub(out.Ask, out.Bid)) != 0 {
		t.Fatalf("spread not re-derived: %v", out.Spread)
	}
	if out.Change24h == nil || out.Change24h.Sign() <= 0 {
		t.Fatalf("change24h not re-derived: %v", out.Change24h)
	}
	if out.ChangePct24h == nil || out.ChangePct24h.Sign() <= 0 {
		t.Fatalf("changePct24h not re-derived: %v", out.ChangePct24h)
	}
	if out.Timestamp != orig.Timestamp || out.Pair != orig.Pair {
		t.Fatal("non-price fields must pass through unchanged")
	}
}

// TestApplyTickerIdentity verifies pairs without an adjustment (or with an
// identity adjustment) are returned as-is: same pointer, zero allocations on
// the hot path.
func TestApplyTickerIdentity(t *testing.T) {
	adj := NewPriceAdjuster(nil)
	if err := adj.Upsert(&PriceAdjustment{Pair: "ETH/USDT", Multiplier: big.NewFloat(1), Offset: big.NewFloat(0)}); err != nil {
		t.Fatal(err)
	}
	eth := &Ticker{Pair: "ETH/USDT", Last: big.NewFloat(3000)}
	if out := adj.ApplyTicker(eth); out != eth {
		t.Fatal("identity adjustment must return the original pointer")
	}
	sol := &Ticker{Pair: "SOL/USDT", Last: big.NewFloat(150)}
	if out := adj.ApplyTicker(sol); out != sol {
		t.Fatal("pair without adjustment must return the original pointer")
	}
	var nilAdj *PriceAdjuster
	if out := nilAdj.ApplyTicker(sol); out != sol {
		t.Fatal("nil adjuster must return the original pointer")
	}
}

// TestPriceAdjusterMemoryRoundTrip covers Upsert/Get/All and the nil-db
// degradation (memory-only, LoadAll no-op).
func TestPriceAdjusterMemoryRoundTrip(t *testing.T) {
	adj := NewPriceAdjuster(nil)
	if err := adj.LoadAll(); err != nil {
		t.Fatalf("nil-db LoadAll: %v", err)
	}
	if got := adj.Get("BTC/USDT"); got != nil {
		t.Fatalf("Get on empty adjuster = %+v, want nil", got)
	}
	if err := adj.Upsert(&PriceAdjustment{Pair: "btc/usdt", Multiplier: big.NewFloat(0.9), Offset: big.NewFloat(-5)}); err != nil {
		t.Fatal(err)
	}
	got := adj.Get("BTC/USDT") // case-insensitive lookup
	if got == nil || got.Multiplier.Cmp(big.NewFloat(0.9)) != 0 || got.Offset.Cmp(big.NewFloat(-5)) != 0 {
		t.Fatalf("Get = %+v, want multiplier 0.9 offset -5", got)
	}
	// Returned copies must not alias internal state.
	got.Multiplier.SetFloat64(99)
	if again := adj.Get("BTC/USDT"); again.Multiplier.Cmp(big.NewFloat(0.9)) != 0 {
		t.Fatal("Get returned an alias of internal state")
	}
	if all := adj.All(); len(all) != 1 || all[0].Pair != "BTC/USDT" {
		t.Fatalf("All = %+v, want one BTC/USDT entry", all)
	}
}
