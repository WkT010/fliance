package amm

import (
	"errors"
	"math/big"
	"testing"
)

// fakeStore is an in-memory Store for service-level tests.
type fakeStore struct {
	pools     map[string]*Pool
	positions map[string]*LPPosition
	swaps     []*Swap
}

func newFakeStore() *fakeStore {
	return &fakeStore{pools: map[string]*Pool{}, positions: map[string]*LPPosition{}}
}

func (f *fakeStore) SavePool(p *Pool) error { f.pools[p.ID] = p; return nil }
func (f *fakeStore) GetPool(id string) (*Pool, error) {
	p, ok := f.pools[id]
	if !ok {
		return nil, ErrPoolNotFound
	}
	return p, nil
}
func (f *fakeStore) GetPoolByPair(pair string) (*Pool, error) {
	for _, p := range f.pools {
		if p.Pair == pair {
			return p, nil
		}
	}
	return nil, ErrPoolNotFound
}
func (f *fakeStore) ListPools() ([]*Pool, error) {
	out := make([]*Pool, 0, len(f.pools))
	for _, p := range f.pools {
		out = append(out, p)
	}
	return out, nil
}
func (f *fakeStore) UpdatePoolReserves(id, r0, r1, lp string) error { return nil }
func (f *fakeStore) UpdatePoolProtocolFees(id, fee0, fee1 string) error {
	p, ok := f.pools[id]
	if !ok {
		return ErrPoolNotFound
	}
	p.ProtocolFees0.Parse(fee0, 10)
	p.ProtocolFees1.Parse(fee1, 10)
	return nil
}
func (f *fakeStore) SavePosition(p *LPPosition) error               { f.positions[p.ID] = p; return nil }
func (f *fakeStore) GetPosition(id, userID string) (*LPPosition, error) {
	return f.positions[id], nil
}
func (f *fakeStore) GetPositionByPool(userID, poolID string) (*LPPosition, error) {
	for _, p := range f.positions {
		if p.UserID == userID && p.PoolID == poolID {
			return p, nil
		}
	}
	return nil, nil
}
func (f *fakeStore) ListPositionsByUser(userID string) ([]*LPPosition, error) { return nil, nil }
func (f *fakeStore) ListPositionsByPool(poolID string) ([]*LPPosition, error) { return nil, nil }
func (f *fakeStore) SaveSwap(sw *Swap) error                                  { f.swaps = append(f.swaps, sw); return nil }
func (f *fakeStore) ListSwaps(poolID string, limit, offset int) ([]*Swap, error) {
	return f.swaps, nil
}

func newSwapTestService(t *testing.T) (*Service, *fakeStore) {
	t.Helper()
	store := newFakeStore()
	pool := newTestPool(1000, 4000, 0.003)
	pool.ID = "pool_1"
	store.pools[pool.ID] = pool
	return NewService(store, nil), store
}

func TestExecuteSwapBackwardCompatible(t *testing.T) {
	svc, store := newSwapTestService(t)
	sw, err := svc.ExecuteSwap("u1", "pool_1", "AAA", big.NewFloat(10))
	if err != nil {
		t.Fatalf("ExecuteSwap failed: %v", err)
	}
	if !almostEqual(sw.AmountOut, big.NewFloat(39.48632633), 1e-6) {
		t.Errorf("AmountOut = %s, want ≈39.48632633", textF(sw.AmountOut))
	}
	pool := store.pools["pool_1"]
	// Fee must not be injected into reserves: 1000 + 9.97 = 1009.97.
	if !almostEqual(pool.Reserve0, big.NewFloat(1009.97), 1e-9) {
		t.Errorf("Reserve0 = %s, want 1009.97", textF(pool.Reserve0))
	}
	// Ledger conservation: the wallet was debited the full amountIn, so the
	// reserve increment plus the protocol fee must equal amountIn exactly.
	reserveDelta := new(big.Float).Sub(pool.Reserve0, big.NewFloat(1000))
	sum := new(big.Float).Add(reserveDelta, pool.ProtocolFees0)
	if !almostEqual(sum, big.NewFloat(10), 1e-12) {
		t.Errorf("reserveDelta(%s) + protocolFee(%s) = %s, want amountIn 10",
			textF(reserveDelta), textF(pool.ProtocolFees0), textF(sum))
	}
}

func TestExecuteSwapAccumulatesProtocolFees(t *testing.T) {
	svc, store := newSwapTestService(t)

	// Swap 10 AAA (token0) → fee 0.03 AAA accrues to ProtocolFees0.
	if _, err := svc.ExecuteSwap("u1", "pool_1", "AAA", big.NewFloat(10)); err != nil {
		t.Fatalf("swap AAA failed: %v", err)
	}
	pool := store.pools["pool_1"]
	if !almostEqual(pool.ProtocolFees0, big.NewFloat(0.03), 1e-12) {
		t.Errorf("ProtocolFees0 = %s, want 0.03", textF(pool.ProtocolFees0))
	}
	if pool.ProtocolFees1.Sign() != 0 {
		t.Errorf("ProtocolFees1 = %s, want 0 (fee is denominated in tokenIn)", textF(pool.ProtocolFees1))
	}

	// Swap 40 BBB (token1) → fee 0.12 BBB accrues to ProtocolFees1.
	if _, err := svc.ExecuteSwap("u1", "pool_1", "BBB", big.NewFloat(40)); err != nil {
		t.Fatalf("swap BBB failed: %v", err)
	}
	if !almostEqual(pool.ProtocolFees0, big.NewFloat(0.03), 1e-12) {
		t.Errorf("ProtocolFees0 = %s, want 0.03 unchanged", textF(pool.ProtocolFees0))
	}
	if !almostEqual(pool.ProtocolFees1, big.NewFloat(0.12), 1e-12) {
		t.Errorf("ProtocolFees1 = %s, want 0.12", textF(pool.ProtocolFees1))
	}

	// Fees accumulate across swaps in the same direction.
	if _, err := svc.ExecuteSwap("u1", "pool_1", "AAA", big.NewFloat(10)); err != nil {
		t.Fatalf("second AAA swap failed: %v", err)
	}
	if !almostEqual(pool.ProtocolFees0, big.NewFloat(0.06), 1e-12) {
		t.Errorf("ProtocolFees0 = %s, want 0.06 cumulative", textF(pool.ProtocolFees0))
	}
}

func TestGetAndCollectProtocolFees(t *testing.T) {
	svc, store := newSwapTestService(t)
	if _, err := svc.ExecuteSwap("u1", "pool_1", "AAA", big.NewFloat(10)); err != nil {
		t.Fatalf("swap failed: %v", err)
	}

	fee0, fee1, err := svc.GetProtocolFees("pool_1")
	if err != nil {
		t.Fatalf("GetProtocolFees failed: %v", err)
	}
	if !almostEqual(fee0, big.NewFloat(0.03), 1e-12) || fee1.Sign() != 0 {
		t.Errorf("GetProtocolFees = (%s, %s), want (0.03, 0)", textF(fee0), textF(fee1))
	}

	collected0, collected1, err := svc.CollectProtocolFees("pool_1")
	if err != nil {
		t.Fatalf("CollectProtocolFees failed: %v", err)
	}
	if !almostEqual(collected0, big.NewFloat(0.03), 1e-12) || collected1.Sign() != 0 {
		t.Errorf("collected = (%s, %s), want (0.03, 0)", textF(collected0), textF(collected1))
	}

	// Accumulators reset after collection; reserves must be untouched.
	pool := store.pools["pool_1"]
	if pool.ProtocolFees0.Sign() != 0 || pool.ProtocolFees1.Sign() != 0 {
		t.Errorf("protocol fees must be zeroed after collect: (%s, %s)",
			textF(pool.ProtocolFees0), textF(pool.ProtocolFees1))
	}
	fee0, _, err = svc.GetProtocolFees("pool_1")
	if err != nil || fee0.Sign() != 0 {
		t.Errorf("GetProtocolFees after collect = %s, %v; want 0", textF(fee0), err)
	}
}

func TestExecuteSwapWithMinOutSlippageProtection(t *testing.T) {
	svc, store := newSwapTestService(t)

	// minAmountOut above the achievable output → rejected before any state change.
	_, err := svc.ExecuteSwapWithMinOut("u1", "pool_1", "AAA", big.NewFloat(10), big.NewFloat(40))
	if !errors.Is(err, ErrSlippageExceeded) {
		t.Fatalf("error = %v, want ErrSlippageExceeded", err)
	}
	pool := store.pools["pool_1"]
	if !almostEqual(pool.Reserve0, big.NewFloat(1000), 1e-12) ||
		!almostEqual(pool.Reserve1, big.NewFloat(4000), 1e-12) {
		t.Errorf("reserves must be untouched after slippage rejection: %s / %s",
			textF(pool.Reserve0), textF(pool.Reserve1))
	}
	if len(store.swaps) != 0 {
		t.Errorf("no swap record must be saved on slippage rejection")
	}

	// minAmountOut exactly reachable → success.
	sw, err := svc.ExecuteSwapWithMinOut("u1", "pool_1", "AAA", big.NewFloat(10), big.NewFloat(39))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !almostEqual(sw.AmountOut, big.NewFloat(39.48632633), 1e-6) {
		t.Errorf("AmountOut = %s, want ≈39.48632633", textF(sw.AmountOut))
	}

	// nil / zero / negative minAmountOut disable the check (backward compatible).
	if _, err := svc.ExecuteSwapWithMinOut("u1", "pool_1", "AAA", big.NewFloat(1), nil); err != nil {
		t.Errorf("nil minAmountOut must not fail: %v", err)
	}
	if _, err := svc.ExecuteSwapWithMinOut("u1", "pool_1", "AAA", big.NewFloat(1), big.NewFloat(0)); err != nil {
		t.Errorf("zero minAmountOut must not fail: %v", err)
	}
}
