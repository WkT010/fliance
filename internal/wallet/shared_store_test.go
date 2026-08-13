package wallet

import (
	"context"
	"errors"
	"math/big"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// In-memory RedisLike fake
// ---------------------------------------------------------------------------

// fakeRedis is a process-local RedisLike used to exercise the L2 semantics
// (including cross-instance behaviour) without a real server. failAll makes
// every operation error out, simulating an unavailable Redis.
type fakeRedis struct {
	mu      sync.Mutex
	m       map[string]string
	failAll bool
}

func newFakeRedis() *fakeRedis { return &fakeRedis{m: make(map[string]string)} }

var _ RedisLike = (*fakeRedis)(nil)

func (f *fakeRedis) SetNX(_ context.Context, key, value string, _ time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failAll {
		return false, errors.New("redis down")
	}
	if _, ok := f.m[key]; ok {
		return false, nil
	}
	f.m[key] = value
	return true, nil
}

func (f *fakeRedis) Get(_ context.Context, key string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failAll {
		return "", errors.New("redis down")
	}
	v, ok := f.m[key]
	if !ok {
		return "", ErrSharedKeyMissing
	}
	return v, nil
}

func (f *fakeRedis) Set(_ context.Context, key, value string, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failAll {
		return errors.New("redis down")
	}
	f.m[key] = value
	return nil
}

func (f *fakeRedis) Del(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failAll {
		return errors.New("redis down")
	}
	delete(f.m, key)
	return nil
}

func (f *fakeRedis) Expire(_ context.Context, key string, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failAll {
		return errors.New("redis down")
	}
	if _, ok := f.m[key]; !ok {
		return ErrSharedKeyMissing
	}
	return nil
}

// twoInstances builds two wallet services that share one store (the DB) and
// one L2 fake redis — the horizontal-scaling topology.
func twoInstances(t *testing.T) (a, b *Service, store *memWalletStore, rs *fakeRedis) {
	t.Helper()
	store = newMemWalletStore()
	store.seed("taker", "USDT", 1000)
	store.seed("maker", "BTC", 10)
	rs = newFakeRedis()
	fees := &StaticFeeSchedule{Default: FeeConfig{TakerRate: big.NewFloat(0.001), MakerRate: big.NewFloat(0.0005)}}
	a = NewService(store, nil, fees)
	a.SetSharedStore(rs)
	b = NewService(store, nil, fees)
	b.SetSharedStore(rs)
	return a, b, store, rs
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestSettleFillCrossInstanceDedup: two instances consuming the same fill
// settle it exactly once, via the L2 SetNX claim.
func TestSettleFillCrossInstanceDedup(t *testing.T) {
	a, b, store, rs := twoInstances(t)

	if err := a.SettleFill("fill-ha", "BTC/USDT", 1, "oT", "oM", "taker", "maker", big.NewFloat(100), big.NewFloat(2)); err != nil {
		t.Fatalf("instance A settle: %v", err)
	}
	// Instance B never saw the fill in its L1 cache, but L2 blocks it.
	if err := b.SettleFill("fill-ha", "BTC/USDT", 1, "oT", "oM", "taker", "maker", big.NewFloat(100), big.NewFloat(2)); err != nil {
		t.Fatalf("instance B duplicate settle must be a no-op, got: %v", err)
	}
	if len(store.settleLog) != 1 {
		t.Fatalf("settle executed %d times across instances, want 1", len(store.settleLog))
	}
	// The claim is recorded in L2 under the namespaced key.
	if _, err := rs.Get(context.Background(), sharedFillPrefix+"fill-ha"); err != nil {
		t.Fatalf("L2 fill claim missing: %v", err)
	}
	// Instance B caches the dedup locally too (L1 fast path on replay).
	if err := b.SettleFill("fill-ha", "BTC/USDT", 1, "oT", "oM", "taker", "maker", big.NewFloat(100), big.NewFloat(2)); err != nil {
		t.Fatal(err)
	}
	if len(store.settleLog) != 1 {
		t.Fatal("local replay of a claimed fill settled again")
	}
}

// TestReservationCrossInstanceRelease: a reservation created on instance A
// can be released by instance B via the L2 write-through.
func TestReservationCrossInstanceRelease(t *testing.T) {
	a, b, store, rs := twoInstances(t)

	if err := a.ReserveOrder("ord-ha", "taker", "BTC/USDT", 1, 0, big.NewFloat(100), big.NewFloat(2)); err != nil {
		t.Fatalf("reserve on A: %v", err)
	}
	assertNear(t, store.wallets["taker/USDT/spot"].Locked, 200.2, "locked after reserve")
	// L2 holds the reservation record.
	if _, err := rs.Get(context.Background(), sharedResPrefix+"ord-ha"); err != nil {
		t.Fatalf("L2 reservation missing: %v", err)
	}

	// Instance B (empty L1) releases it.
	if err := b.ReleaseOrder("ord-ha", "taker"); err != nil {
		t.Fatalf("release on B: %v", err)
	}
	assertNear(t, store.wallets["taker/USDT/spot"].Locked, 0, "locked after cross-instance release")
	if _, err := rs.Get(context.Background(), sharedResPrefix+"ord-ha"); !errors.Is(err, ErrSharedKeyMissing) {
		t.Fatalf("L2 reservation not removed after release: %v", err)
	}
}

// TestSettleFillWritesReservationsThrough: fill settlement updates propagate
// to L2 so other instances see the remaining reservation.
func TestSettleFillWritesReservationsThrough(t *testing.T) {
	a, b, store, rs := twoInstances(t)
	_ = a.ReserveOrder("oT", "taker", "BTC/USDT", 1, 0, big.NewFloat(100), big.NewFloat(2))
	_ = a.ReserveOrder("oM", "maker", "BTC/USDT", -1, 0, big.NewFloat(100), big.NewFloat(2))

	if err := a.SettleFill("fill-res", "BTC/USDT", 1, "oT", "oM", "taker", "maker", big.NewFloat(100), big.NewFloat(1)); err != nil {
		t.Fatalf("partial fill: %v", err)
	}
	// Both reservations partially consumed: records still in L2 with updated
	// remaining amounts.
	data, err := rs.Get(context.Background(), sharedResPrefix+"oT")
	if err != nil {
		t.Fatalf("buyer reservation missing from L2: %v", err)
	}
	r, err := loadReservationL2(rs, "oT")
	if err != nil || r == nil || r.asset != "USDT" {
		t.Fatalf("buyer reservation decode failed: %+v (%v) [%s]", r, err, data)
	}
	assertNear(t, r.remaining, 200.2-100.1, "buyer remaining after partial fill")
	// The fully-settled path: settle the second unit; seller exhausted -> removed.
	if err := b.SettleFill("fill-res-2", "BTC/USDT", 1, "oT", "oM", "taker", "maker", big.NewFloat(100), big.NewFloat(1)); err != nil {
		t.Fatalf("second fill: %v", err)
	}
	if _, err := rs.Get(context.Background(), sharedResPrefix+"oM"); !errors.Is(err, ErrSharedKeyMissing) {
		t.Fatalf("exhausted seller reservation must be removed from L2: %v", err)
	}
	// Reservations never call Settle themselves: exactly two fill batches.
	if len(store.settleLog) != 2 {
		t.Fatalf("settle batches = %d, want 2", len(store.settleLog))
	}
	// Cross-instance hydration worked: seller locked base fully released.
	assertNear(t, store.wallets["maker/BTC/spot"].Locked, 0, "seller locked after cross-instance fills")
}

// TestSettleFillFailureReleasesL2Claim: when the DB rejects a settlement, the
// L2 claim is released so another instance (or a retry) can settle the fill.
func TestSettleFillFailureReleasesL2Claim(t *testing.T) {
	a, b, store, _ := twoInstances(t)

	store.mu.Lock()
	store.settleErr = errors.New("db down")
	store.mu.Unlock()
	if err := a.SettleFill("fill-retry", "BTC/USDT", 1, "oT", "oM", "taker", "maker", big.NewFloat(100), big.NewFloat(2)); err == nil {
		t.Fatal("expected settle failure")
	}
	store.mu.Lock()
	store.settleErr = nil
	store.mu.Unlock()

	// The claim must be gone: instance B can settle the same fill now.
	if err := b.SettleFill("fill-retry", "BTC/USDT", 1, "oT", "oM", "taker", "maker", big.NewFloat(100), big.NewFloat(2)); err != nil {
		t.Fatalf("retry on instance B after failure: %v", err)
	}
	if len(store.settleLog) != 1 {
		t.Fatalf("settle batches = %d, want 1", len(store.settleLog))
	}
}

// TestSharedStoreDegradation: when the L2 store is unreachable the service
// keeps working with local-only semantics (graceful degradation).
func TestSharedStoreDegradation(t *testing.T) {
	store := newMemWalletStore()
	store.seed("taker", "USDT", 1000)
	store.seed("maker", "BTC", 10)
	svc := NewService(store, nil, nil)
	rs := newFakeRedis()
	rs.failAll = true
	svc.SetSharedStore(rs)

	if err := svc.SettleFill("fill-deg", "BTC/USDT", 1, "oT", "oM", "taker", "maker", big.NewFloat(100), big.NewFloat(2)); err != nil {
		t.Fatalf("settle must succeed despite L2 outage: %v", err)
	}
	// Local dedup still applies.
	if err := svc.SettleFill("fill-deg", "BTC/USDT", 1, "oT", "oM", "taker", "maker", big.NewFloat(100), big.NewFloat(2)); err != nil {
		t.Fatal(err)
	}
	if len(store.settleLog) != 1 {
		t.Fatalf("settle batches = %d, want 1 (local dedup intact)", len(store.settleLog))
	}
	// Reservation paths also degrade without erroring.
	if err := svc.ReserveOrder("ord-deg", "taker", "BTC/USDT", 1, 0, big.NewFloat(10), big.NewFloat(1)); err != nil {
		t.Fatalf("reserve during L2 outage: %v", err)
	}
	if err := svc.ReleaseOrder("ord-deg", "taker"); err != nil {
		t.Fatalf("release during L2 outage: %v", err)
	}
}

// TestNoSharedStoreKeepsLocalBehaviour: without injection nothing changes.
func TestNoSharedStoreKeepsLocalBehaviour(t *testing.T) {
	store := newMemWalletStore()
	store.seed("taker", "USDT", 1000)
	store.seed("maker", "BTC", 10)
	svc := NewService(store, nil, nil) // no SetSharedStore, no env
	if err := svc.SettleFill("fill-local", "BTC/USDT", 1, "oT", "oM", "taker", "maker", big.NewFloat(100), big.NewFloat(2)); err != nil {
		t.Fatal(err)
	}
	if err := svc.SettleFill("fill-local", "BTC/USDT", 1, "oT", "oM", "taker", "maker", big.NewFloat(100), big.NewFloat(2)); err != nil {
		t.Fatal(err)
	}
	if len(store.settleLog) != 1 {
		t.Fatalf("settle batches = %d, want 1", len(store.settleLog))
	}
}
