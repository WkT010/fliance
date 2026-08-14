package amm

import (
	"errors"
	"math/big"
	"testing"
)

// TestServiceNilStoreDoesNotPanic guards against the startup crash where a
// failed Postgres connection left the AMM store nil and bootstrap dereferenced
// it (api-gateway issue: bootstrapAMMPools panic). With a nil store the service
// must degrade to ErrStoreUnavailable on every operation instead of panicking.
func TestServiceNilStoreDoesNotPanic(t *testing.T) {
	svc := NewService(nil, nil)

	if _, err := svc.ListPools(); !errors.Is(err, ErrStoreUnavailable) {
		t.Errorf("ListPools err = %v, want ErrStoreUnavailable", err)
	}
	if _, err := svc.GetPool("pool_x"); !errors.Is(err, ErrStoreUnavailable) {
		t.Errorf("GetPool err = %v, want ErrStoreUnavailable", err)
	}
	if _, err := svc.GetPoolByPair("BTC/USDT"); !errors.Is(err, ErrStoreUnavailable) {
		t.Errorf("GetPoolByPair err = %v, want ErrStoreUnavailable", err)
	}
	if _, err := svc.CreatePool("BTC/USDT", "BTC", "USDT", big.NewFloat(0.003)); !errors.Is(err, ErrStoreUnavailable) {
		t.Errorf("CreatePool err = %v, want ErrStoreUnavailable", err)
	}
	if err := svc.SeedPoolReserves("pool_x", big.NewFloat(1), big.NewFloat(1)); !errors.Is(err, ErrStoreUnavailable) {
		t.Errorf("SeedPoolReserves err = %v, want ErrStoreUnavailable", err)
	}
	if _, err := svc.GetPositionByPool("u1", "pool_x"); !errors.Is(err, ErrStoreUnavailable) {
		t.Errorf("GetPositionByPool err = %v, want ErrStoreUnavailable", err)
	}
	if _, err := svc.ListPositionsByUser("u1"); !errors.Is(err, ErrStoreUnavailable) {
		t.Errorf("ListPositionsByUser err = %v, want ErrStoreUnavailable", err)
	}
	if _, err := svc.ListSwaps("pool_x", 10, 0); !errors.Is(err, ErrStoreUnavailable) {
		t.Errorf("ListSwaps err = %v, want ErrStoreUnavailable", err)
	}
	if _, _, _, err := svc.Quote("pool_x", "BTC", big.NewFloat(1)); !errors.Is(err, ErrStoreUnavailable) {
		t.Errorf("Quote err = %v, want ErrStoreUnavailable", err)
	}
	if _, err := svc.ExecuteSwap("u1", "pool_x", "BTC", big.NewFloat(1)); !errors.Is(err, ErrStoreUnavailable) {
		t.Errorf("ExecuteSwap err = %v, want ErrStoreUnavailable", err)
	}
	if _, _, err := svc.GetProtocolFees("pool_x"); !errors.Is(err, ErrStoreUnavailable) {
		t.Errorf("GetProtocolFees err = %v, want ErrStoreUnavailable", err)
	}
}
