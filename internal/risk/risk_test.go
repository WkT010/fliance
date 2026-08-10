package risk

import (
	"errors"
	"math/big"
	"testing"

	"github.com/WkT010/nexa-exchange/internal/matching"
)

func baseConfig(pair string) *PairConfig {
	return &PairConfig{
		Pair:                pair,
		TradingEnabled:      true,
		MarketOrdersEnabled: true,
	}
}

func limitOrder(user, pair string, price, qty float64) matching.OrderRequest {
	return matching.OrderRequest{
		UserID:   user,
		Pair:     pair,
		Side:     matching.Buy,
		Type:     matching.Limit,
		Price:    big.NewFloat(price),
		Quantity: big.NewFloat(qty),
	}
}

func marketOrder(user, pair string, qty float64) matching.OrderRequest {
	return matching.OrderRequest{
		UserID:   user,
		Pair:     pair,
		Side:     matching.Buy,
		Type:     matching.Market,
		Quantity: big.NewFloat(qty),
	}
}

func TestUnknownPairPassesThrough(t *testing.T) {
	e := NewEngine()
	if err := e.Check(limitOrder("u1", "FOO/BAR", 100, 1)); err != nil {
		t.Errorf("unknown pair must pass, got %v", err)
	}
}

func TestTradingSuspended(t *testing.T) {
	e := NewEngine()
	cfg := baseConfig("BTC/USDT")
	cfg.TradingEnabled = false
	e.SetPairConfig(cfg)
	if err := e.Check(limitOrder("u1", "BTC/USDT", 100, 1)); !errors.Is(err, ErrTradingSuspended) {
		t.Errorf("error = %v, want ErrTradingSuspended", err)
	}
}

func TestMarketOrderDisabled(t *testing.T) {
	e := NewEngine()
	cfg := baseConfig("BTC/USDT")
	cfg.MarketOrdersEnabled = false
	e.SetPairConfig(cfg)
	if err := e.Check(marketOrder("u1", "BTC/USDT", 1)); !errors.Is(err, ErrMarketOrderDisabled) {
		t.Errorf("error = %v, want ErrMarketOrderDisabled", err)
	}
}

func TestPriceBand(t *testing.T) {
	e := NewEngine()
	cfg := baseConfig("BTC/USDT")
	cfg.PriceBandPct = big.NewFloat(0.05)
	cfg.ReferencePrice = big.NewFloat(100)
	e.SetPairConfig(cfg)

	cases := []struct {
		price   float64
		wantErr error
	}{
		{100, nil}, // at reference
		{104, nil}, // within +5%
		{96, nil},  // within -5%
		{105, nil}, // exact upper edge (inclusive)
		{95, nil},  // exact lower edge (inclusive)
		{106, ErrPriceBandBreached},
		{94, ErrPriceBandBreached},
	}
	for i, tc := range cases {
		err := e.Check(limitOrder("u1", "BTC/USDT", tc.price, 1))
		if !errors.Is(err, tc.wantErr) {
			t.Errorf("case %d (price %v): error = %v, want %v", i, tc.price, err, tc.wantErr)
		}
	}

	// Market orders (no price) skip the price band.
	if err := e.Check(marketOrder("u1", "BTC/USDT", 1)); err != nil {
		t.Errorf("market order must skip price band, got %v", err)
	}
}

func TestNotionalLimits(t *testing.T) {
	e := NewEngine()
	cfg := baseConfig("BTC/USDT")
	cfg.MinNotional = big.NewFloat(10)
	cfg.MaxNotional = big.NewFloat(100000)
	e.SetPairConfig(cfg)

	// notional = price * qty
	if err := e.Check(limitOrder("u1", "BTC/USDT", 5, 1)); !errors.Is(err, ErrMinNotional) {
		t.Errorf("error = %v, want ErrMinNotional", err)
	}
	if err := e.Check(limitOrder("u1", "BTC/USDT", 50000, 4)); !errors.Is(err, ErrMaxNotional) {
		t.Errorf("error = %v, want ErrMaxNotional", err)
	}
	if err := e.Check(limitOrder("u1", "BTC/USDT", 100, 1)); err != nil {
		t.Errorf("notional within bounds must pass, got %v", err)
	}
}

func TestOrderRateLimit(t *testing.T) {
	e := NewEngine()
	e.SetPairConfig(baseConfig("BTC/USDT"))
	e.SetUserLimit(&UserLimit{UserID: "u1", OrdersPerMinute: 3})

	for i := 0; i < 3; i++ {
		if err := e.Check(limitOrder("u1", "BTC/USDT", 100, 1)); err != nil {
			t.Fatalf("order %d must pass, got %v", i, err)
		}
	}
	if err := e.Check(limitOrder("u1", "BTC/USDT", 100, 1)); !errors.Is(err, ErrOrderRateLimit) {
		t.Errorf("error = %v, want ErrOrderRateLimit", err)
	}
	// Other users are unaffected.
	if err := e.Check(limitOrder("u2", "BTC/USDT", 100, 1)); err != nil {
		t.Errorf("other user must pass, got %v", err)
	}
}

func TestDailyOrderLimit(t *testing.T) {
	e := NewEngine()
	e.SetPairConfig(baseConfig("BTC/USDT"))
	e.SetUserLimit(&UserLimit{UserID: "u1", OrdersPerDay: 2})

	if err := e.Check(limitOrder("u1", "BTC/USDT", 100, 1)); err != nil {
		t.Fatal(err)
	}
	if err := e.Check(limitOrder("u1", "BTC/USDT", 100, 1)); err != nil {
		t.Fatal(err)
	}
	if err := e.Check(limitOrder("u1", "BTC/USDT", 100, 1)); !errors.Is(err, ErrDailyVolumeLimit) {
		t.Errorf("error = %v, want ErrDailyVolumeLimit", err)
	}
}

func TestPositionLimit(t *testing.T) {
	e := NewEngine()
	e.SetPairConfig(baseConfig("BTC/USDT"))
	e.SetUserLimit(&UserLimit{
		UserID:      "u1",
		MaxPosition: map[string]*big.Float{"BTC/USDT": big.NewFloat(5)},
	})

	if err := e.Check(limitOrder("u1", "BTC/USDT", 100, 10)); !errors.Is(err, ErrMaxPosition) {
		t.Errorf("error = %v, want ErrMaxPosition", err)
	}
	if err := e.Check(limitOrder("u1", "BTC/USDT", 100, 5)); err != nil {
		t.Errorf("qty at limit must pass, got %v", err)
	}
	// Unlisted pair has no position limit.
	if err := e.Check(limitOrder("u1", "ETH/USDT", 100, 10)); err != nil {
		t.Errorf("pair without limit must pass, got %v", err)
	}
}

// Circuit breaker (fix target): CircuitBreakerPct was declared but never
// consulted by Check. After the fix, an order priced more than
// CircuitBreakerPct away from the reference price trips the breaker and all
// further orders for the pair are rejected until ResetCircuitBreaker.
func TestCircuitBreakerTripsOnExtremeDeviation(t *testing.T) {
	e := NewEngine()
	cfg := baseConfig("BTC/USDT")
	cfg.CircuitBreakerPct = big.NewFloat(0.10)
	cfg.ReferencePrice = big.NewFloat(100)
	e.SetPairConfig(cfg)

	// Within ±10%: allowed.
	if err := e.Check(limitOrder("u1", "BTC/USDT", 109, 1)); err != nil {
		t.Errorf("price within breaker threshold must pass, got %v", err)
	}
	if err := e.Check(limitOrder("u1", "BTC/USDT", 91, 1)); err != nil {
		t.Errorf("price within breaker threshold must pass, got %v", err)
	}

	// +12% deviation trips the breaker.
	if err := e.Check(limitOrder("u1", "BTC/USDT", 112, 1)); !errors.Is(err, ErrCircuitBreakerOpen) {
		t.Fatalf("error = %v, want ErrCircuitBreakerOpen", err)
	}
	if !e.IsCircuitOpen("BTC/USDT") {
		t.Errorf("breaker must be open after trip")
	}

	// While tripped, even normally-fine orders are rejected.
	if err := e.Check(limitOrder("u1", "BTC/USDT", 100, 1)); !errors.Is(err, ErrCircuitBreakerOpen) {
		t.Errorf("error = %v, want ErrCircuitBreakerOpen while tripped", err)
	}
	if err := e.Check(marketOrder("u1", "BTC/USDT", 1)); !errors.Is(err, ErrCircuitBreakerOpen) {
		t.Errorf("market orders must also be rejected while tripped, got %v", err)
	}
	// Other pairs are unaffected.
	e.SetPairConfig(baseConfig("ETH/USDT"))
	if err := e.Check(limitOrder("u1", "ETH/USDT", 100, 1)); err != nil {
		t.Errorf("other pair must pass, got %v", err)
	}

	// Manual reset restores trading.
	e.ResetCircuitBreaker("BTC/USDT")
	if e.IsCircuitOpen("BTC/USDT") {
		t.Errorf("breaker must be closed after reset")
	}
	if err := e.Check(limitOrder("u1", "BTC/USDT", 100, 1)); err != nil {
		t.Errorf("order must pass after reset, got %v", err)
	}
}

func TestCircuitBreakerDisabledWhenUnconfigured(t *testing.T) {
	e := NewEngine()
	cfg := baseConfig("BTC/USDT")
	cfg.ReferencePrice = big.NewFloat(100)
	// No CircuitBreakerPct → breaker disabled regardless of deviation.
	e.SetPairConfig(cfg)
	if err := e.Check(limitOrder("u1", "BTC/USDT", 500, 1)); err != nil {
		t.Errorf("breaker must be disabled without CircuitBreakerPct, got %v", err)
	}
	if e.IsCircuitOpen("BTC/USDT") {
		t.Errorf("breaker must never trip when disabled")
	}
}
