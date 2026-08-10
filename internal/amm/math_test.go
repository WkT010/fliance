package amm

import (
	"errors"
	"math/big"
	"testing"
)

// almostEqual reports whether a and b are within relTol relative difference.
func almostEqual(a, b *big.Float, relTol float64) bool {
	if a == nil || b == nil {
		return false
	}
	diff := new(big.Float).Sub(a, b)
	diff.Abs(diff)
	scale := new(big.Float).Abs(b)
	if scale.Sign() == 0 {
		return diff.Sign() == 0
	}
	ratio, _ := new(big.Float).Quo(diff, scale).Float64()
	return ratio <= relTol
}

func newTestPool(r0, r1 float64, feeRate float64) *Pool {
	p := NewPool("pool_test", "A/B", "AAA", "BBB", big.NewFloat(feeRate))
	p.Reserve0 = big.NewFloat(r0)
	p.Reserve1 = big.NewFloat(r1)
	p.LPShares = big.NewFloat(0)
	return p
}

// ── QuoteSwap ──────────────────────────────────────────────────────────────

func TestQuoteSwapNormal(t *testing.T) {
	pool := newTestPool(1000, 4000, 0.003)
	amountOut, fee, amountInWithFee, err := QuoteSwap(pool, "AAA", big.NewFloat(10))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// fee = 10 * 0.003 = 0.03
	if !almostEqual(fee, big.NewFloat(0.03), 1e-12) {
		t.Errorf("fee = %s, want 0.03", textF(fee))
	}
	// amountInWithFee = 9.97
	if !almostEqual(amountInWithFee, big.NewFloat(9.97), 1e-12) {
		t.Errorf("amountInWithFee = %s, want 9.97", textF(amountInWithFee))
	}
	// amountOut = 4000 * 9.97 / (1000 + 9.97) ≈ 39.48632
	if !almostEqual(amountOut, big.NewFloat(39.48632633), 1e-6) {
		t.Errorf("amountOut = %s, want ≈39.48632633", textF(amountOut))
	}
	// Output must always be strictly less than the output reserve.
	if amountOut.Cmp(pool.Reserve1) >= 0 {
		t.Errorf("amountOut must be < reserveOut")
	}
}

func TestQuoteSwapReverseDirection(t *testing.T) {
	pool := newTestPool(1000, 4000, 0.003)
	amountOut, _, _, err := QuoteSwap(pool, "BBB", big.NewFloat(40))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Symmetric to TestQuoteSwapNormal: 1000 * 39.88 / (4000 + 39.88) ≈ 9.8715803
	if !almostEqual(amountOut, big.NewFloat(9.8715803440), 1e-6) {
		t.Errorf("amountOut = %s, want ≈9.8715803", textF(amountOut))
	}
}

func TestQuoteSwapTinyInput(t *testing.T) {
	pool := newTestPool(1000, 4000, 0.003)
	in := new(big.Float).SetFloat64(1e-9)
	amountOut, _, _, err := QuoteSwap(pool, "AAA", in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// For tiny inputs the output approaches the fee-adjusted spot rate:
	// 4 * (1 - fee) * amountIn ≈ 3.988 * amountIn.
	want := new(big.Float).Mul(in, big.NewFloat(3.988))
	if !almostEqual(amountOut, want, 1e-6) {
		t.Errorf("amountOut = %s, want ≈%s", textF(amountOut), textF(want))
	}
	if amountOut.Sign() <= 0 {
		t.Errorf("amountOut must be positive for tiny inputs")
	}
}

func TestQuoteSwapHugeInput(t *testing.T) {
	pool := newTestPool(1000, 4000, 0.003)
	amountOut, _, _, err := QuoteSwap(pool, "AAA", big.NewFloat(1e12))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if amountOut.Sign() <= 0 {
		t.Fatalf("amountOut must be positive")
	}
	// A huge input saturates the pool: output approaches reserveOut but never reaches it.
	if amountOut.Cmp(pool.Reserve1) >= 0 {
		t.Errorf("amountOut = %s must stay below reserveOut = %s", textF(amountOut), textF(pool.Reserve1))
	}
	if !almostEqual(amountOut, big.NewFloat(3999.99999996), 1e-6) {
		t.Errorf("amountOut = %s, want ≈4000", textF(amountOut))
	}
}

func TestQuoteSwapRejectsZeroAndNegative(t *testing.T) {
	pool := newTestPool(1000, 4000, 0.003)
	for _, tc := range []*big.Float{nil, big.NewFloat(0), big.NewFloat(-1)} {
		if _, _, _, err := QuoteSwap(pool, "AAA", tc); !errors.Is(err, ErrInvalidAmount) {
			t.Errorf("QuoteSwap(%v) error = %v, want ErrInvalidAmount", tc, err)
		}
	}
}

func TestQuoteSwapInvalidTokenAndPaused(t *testing.T) {
	pool := newTestPool(1000, 4000, 0.003)
	if _, _, _, err := QuoteSwap(pool, "ZZZ", big.NewFloat(1)); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("error = %v, want ErrInvalidToken", err)
	}
	pool.Status = "paused"
	if _, _, _, err := QuoteSwap(pool, "AAA", big.NewFloat(1)); !errors.Is(err, ErrPoolPaused) {
		t.Errorf("error = %v, want ErrPoolPaused", err)
	}
}

func TestQuoteSwapEmptyPool(t *testing.T) {
	pool := newTestPool(0, 0, 0.003)
	if _, _, _, err := QuoteSwap(pool, "AAA", big.NewFloat(1)); !errors.Is(err, ErrEmptyPool) {
		t.Errorf("error = %v, want ErrEmptyPool", err)
	}
}

// ── ApplySwap: fee must NOT be injected into reserves (Uniswap-style fix) ──

func TestApplySwapReservesExcludeFee(t *testing.T) {
	pool := newTestPool(1000, 4000, 0.003)
	amountOut, fee, amountInWithFee, err := QuoteSwap(pool, "AAA", big.NewFloat(10))
	if err != nil {
		t.Fatal(err)
	}
	ApplySwap(pool, "AAA", big.NewFloat(10), amountOut)

	// Reserve-in grows by the fee-deducted input only; the fee accrues to the
	// protocol, not to the pool (LP holders).
	wantR0 := new(big.Float).Add(big.NewFloat(1000), amountInWithFee)
	if !almostEqual(pool.Reserve0, wantR0, 1e-12) {
		t.Errorf("Reserve0 = %s, want 1000 + amountInWithFee = %s (fee %s must not enter reserves)",
			textF(pool.Reserve0), textF(wantR0), textF(fee))
	}
	wantR1 := new(big.Float).Sub(big.NewFloat(4000), amountOut)
	if !almostEqual(pool.Reserve1, wantR1, 1e-12) {
		t.Errorf("Reserve1 = %s, want %s", textF(pool.Reserve1), textF(wantR1))
	}
}

func TestApplySwapKInvariant(t *testing.T) {
	pool := newTestPool(1000, 4000, 0.003)
	kBefore := new(big.Float).Mul(pool.Reserve0, pool.Reserve1)

	amountOut, _, _, err := QuoteSwap(pool, "AAA", big.NewFloat(10))
	if err != nil {
		t.Fatal(err)
	}
	ApplySwap(pool, "AAA", big.NewFloat(10), amountOut)
	kAfter := new(big.Float).Mul(pool.Reserve0, pool.Reserve1)

	// With the fee kept out of the reserves, x*y must be preserved (up to
	// floating-point rounding). The fee only ever increases k, never shrinks it.
	if !almostEqual(kAfter, kBefore, 1e-9) {
		t.Errorf("k changed from %s to %s; fee-injection or accounting bug", textF(kBefore), textF(kAfter))
	}
	// And k must never decrease across a swap.
	if kAfter.Cmp(kBefore) < 0 {
		t.Errorf("k decreased from %s to %s", textF(kBefore), textF(kAfter))
	}

	// Repeated swaps must not inflate k (regression: pre-fix, adding the full
	// amountIn made k grow on every swap, diluting LP value).
	for i := 0; i < 10; i++ {
		out, _, _, err := QuoteSwap(pool, "AAA", big.NewFloat(5))
		if err != nil {
			t.Fatal(err)
		}
		ApplySwap(pool, "AAA", big.NewFloat(5), out)
	}
	kFinal := new(big.Float).Mul(pool.Reserve0, pool.Reserve1)
	if !almostEqual(kFinal, kBefore, 1e-8) {
		t.Errorf("k drifted from %s to %s after repeated swaps", textF(kBefore), textF(kFinal))
	}
}

// ── AddLiquidity ───────────────────────────────────────────────────────────

func TestAddLiquidityFirstDeposit(t *testing.T) {
	pool := newTestPool(0, 0, 0.003)
	finalAmount1, shares, err := AddLiquidity(pool, big.NewFloat(100), big.NewFloat(400))
	if err != nil {
		t.Fatal(err)
	}
	if !almostEqual(finalAmount1, big.NewFloat(400), 1e-12) {
		t.Errorf("finalAmount1 = %s, want 400", textF(finalAmount1))
	}
	// shares = sqrt(100 * 400) = 200
	if !almostEqual(shares, big.NewFloat(200), 1e-9) {
		t.Errorf("shares = %s, want 200", textF(shares))
	}
}

func TestAddLiquidityProportional(t *testing.T) {
	pool := newTestPool(1000, 4000, 0.003)
	pool.LPShares = big.NewFloat(2000)

	// Providing more amount1 than needed: capped to the proportional amount.
	finalAmount1, shares, err := AddLiquidity(pool, big.NewFloat(100), big.NewFloat(1000))
	if err != nil {
		t.Fatal(err)
	}
	// amount1Needed = 100 * 4000 / 1000 = 400
	if !almostEqual(finalAmount1, big.NewFloat(400), 1e-12) {
		t.Errorf("finalAmount1 = %s, want 400", textF(finalAmount1))
	}
	// shares = 100 * 2000 / 1000 = 200
	if !almostEqual(shares, big.NewFloat(200), 1e-12) {
		t.Errorf("shares = %s, want 200", textF(shares))
	}

	// Providing less amount1 than needed: user's amount1 is used as-is.
	finalAmount1, _, err = AddLiquidity(pool, big.NewFloat(100), big.NewFloat(250))
	if err != nil {
		t.Fatal(err)
	}
	if !almostEqual(finalAmount1, big.NewFloat(250), 1e-12) {
		t.Errorf("finalAmount1 = %s, want 250", textF(finalAmount1))
	}
}

func TestAddLiquidityRejectsBadInput(t *testing.T) {
	pool := newTestPool(1000, 4000, 0.003)
	pool.LPShares = big.NewFloat(2000)
	for _, tc := range [][2]*big.Float{
		{nil, big.NewFloat(1)},
		{big.NewFloat(0), big.NewFloat(1)},
		{big.NewFloat(-1), big.NewFloat(1)},
		{big.NewFloat(1), nil},
		{big.NewFloat(1), big.NewFloat(0)},
	} {
		if _, _, err := AddLiquidity(pool, tc[0], tc[1]); !errors.Is(err, ErrInvalidAmount) {
			t.Errorf("AddLiquidity(%v,%v) error = %v, want ErrInvalidAmount", tc[0], tc[1], err)
		}
	}
}

// ── RemoveLiquidity ────────────────────────────────────────────────────────

func TestRemoveLiquidityProportional(t *testing.T) {
	pool := newTestPool(1000, 4000, 0.003)
	pool.LPShares = big.NewFloat(2000)
	a0, a1, err := RemoveLiquidity(pool, big.NewFloat(500))
	if err != nil {
		t.Fatal(err)
	}
	if !almostEqual(a0, big.NewFloat(250), 1e-12) {
		t.Errorf("amount0 = %s, want 250", textF(a0))
	}
	if !almostEqual(a1, big.NewFloat(1000), 1e-12) {
		t.Errorf("amount1 = %s, want 1000", textF(a1))
	}
}

func TestRemoveLiquidityErrors(t *testing.T) {
	pool := newTestPool(1000, 4000, 0.003)
	pool.LPShares = big.NewFloat(2000)

	if _, _, err := RemoveLiquidity(pool, big.NewFloat(0)); !errors.Is(err, ErrInvalidAmount) {
		t.Errorf("error = %v, want ErrInvalidAmount", err)
	}
	if _, _, err := RemoveLiquidity(pool, big.NewFloat(3000)); !errors.Is(err, ErrInsufficientLiquidity) {
		t.Errorf("error = %v, want ErrInsufficientLiquidity", err)
	}
	empty := newTestPool(0, 0, 0.003)
	if _, _, err := RemoveLiquidity(empty, big.NewFloat(1)); !errors.Is(err, ErrEmptyPool) {
		t.Errorf("error = %v, want ErrEmptyPool", err)
	}
}

// ── sqrtBigFloat ───────────────────────────────────────────────────────────

func TestSqrtBigFloat(t *testing.T) {
	cases := []struct {
		in   float64
		want float64
	}{
		{0, 0},
		{1, 1},
		{4, 2},
		{2, 1.41421356237},
		{100, 10},
		{1e20, 1e10},
	}
	for _, tc := range cases {
		got := sqrtBigFloat(big.NewFloat(tc.in))
		if !almostEqual(got, big.NewFloat(tc.want), 1e-9) {
			t.Errorf("sqrt(%v) = %s, want %v", tc.in, textF(got), tc.want)
		}
	}
	if sqrtBigFloat(big.NewFloat(-5)).Sign() != 0 {
		t.Errorf("sqrt of negative must be 0")
	}
}
