package wallet

import (
	"errors"
	"math/big"
	"sync"
	"testing"
	"time"
)

// fakeLimitStore wraps the in-memory wallet store with the same atomic
// reserve + daily-usage contract PGWalletStore implements (single critical
// section stands in for the single Postgres transaction).
type fakeLimitStore struct {
	*memWalletStore
	mu    sync.Mutex
	usage map[string]*big.Float
}

func newFakeLimitStore() *fakeLimitStore {
	return &fakeLimitStore{
		memWalletStore: newMemWalletStore(),
		usage:          make(map[string]*big.Float),
	}
}

func (f *fakeLimitStore) dayKey(userID, asset string) string {
	return userID + ":" + asset + ":" + time.Now().UTC().Format("2006-01-02")
}

func (f *fakeLimitStore) ReserveWithDailyLimit(userID, asset string, amount, usdtEquiv, limit *big.Float) (*Wallet, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := f.dayKey(userID, asset)
	used := f.usage[key]
	if used == nil {
		used = big.NewFloat(0)
	}
	if new(big.Float).Add(used, usdtEquiv).Cmp(limit) > 0 {
		return nil, ErrDailyLimitExceeded
	}
	w, err := f.memWalletStore.ReserveForOrder(userID, asset, amount)
	if err != nil {
		return nil, err
	}
	f.usage[key] = new(big.Float).Add(used, usdtEquiv)
	return w, nil
}

func (f *fakeLimitStore) ReleaseDailyUsage(userID, asset string, usdtEquiv *big.Float) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := f.dayKey(userID, asset)
	used := f.usage[key]
	if used == nil {
		return nil
	}
	next := new(big.Float).Sub(used, usdtEquiv)
	if next.Sign() < 0 {
		next = big.NewFloat(0)
	}
	f.usage[key] = next
	return nil
}

func (f *fakeLimitStore) totalUsage(userID, asset string) *big.Float {
	f.mu.Lock()
	defer f.mu.Unlock()
	if v := f.usage[f.dayKey(userID, asset)]; v != nil {
		return new(big.Float).Copy(v)
	}
	return big.NewFloat(0)
}

// fixedPriceGetter prices pairs from a static table.
type fixedPriceGetter map[string]float64

func (f fixedPriceGetter) BestPrice(pair string) (*big.Float, string, error) {
	if p, ok := f[pair]; ok {
		return big.NewFloat(p), "test", nil
	}
	return nil, "", errors.New("no price for " + pair)
}

// fixedLimitLoader serves a static KYC-tier limit table.
type fixedLimitLoader map[int]float64

func (f fixedLimitLoader) LoadPlatformLimits() (map[int]*big.Float, error) {
	out := make(map[int]*big.Float, len(f))
	for level, v := range f {
		out[level] = big.NewFloat(v)
	}
	return out, nil
}

// newLimitFixture builds a withdrawal service over the fake atomic store:
// BTC @ 10000 USDT, level-0 daily limit 30000 USDT.
func newLimitFixture(t *testing.T) (*WithdrawalService, *fakeLimitStore) {
	t.Helper()
	store := newFakeLimitStore()
	store.seed("u", "BTC", 100)
	svc := NewService(store, map[string]BlockchainClient{"BTC": NewMockBlockchainClient("BTC")}, nil)
	ws := NewWithdrawalService(svc)
	ws.SetReviewThreshold(big.NewFloat(1e9)) // keep requests in pending
	ws.AddAddress(AddressBookEntry{UserID: "u", Asset: "BTC", Address: validTestBTCAddr}, "admin")
	ws.SetPriceGetter(fixedPriceGetter{"BTC/USDT": 10000})
	ws.SetPlatformLimitLoader(fixedLimitLoader{0: 30000})
	if err := ws.ReloadPlatformLimits(); err != nil {
		t.Fatalf("reload platform limits: %v", err)
	}
	return ws, store
}

// TestDailyLimitConcurrentCap hammers RequestWithdrawal from goroutines and
// verifies the atomic reserve+usage path never lets total usage exceed the
// KYC-tier limit: 5 x 1 BTC (=10000 USDT) against a 30000 USDT cap must
// admit exactly 3 and refuse 2 with ErrDailyLimitExceeded.
func TestDailyLimitConcurrentCap(t *testing.T) {
	ws, store := newLimitFixture(t)

	const workers = 5
	results := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := ws.RequestWithdrawal("u", "BTC", validTestBTCAddr, big.NewFloat(1))
			results <- err
		}()
	}
	wg.Wait()
	close(results)

	admitted, refused := 0, 0
	for err := range results {
		switch {
		case err == nil:
			admitted++
		case errors.Is(err, ErrDailyLimitExceeded):
			refused++
		default:
			t.Fatalf("unexpected withdrawal error: %v", err)
		}
	}
	if admitted != 3 || refused != 2 {
		t.Fatalf("admitted=%d refused=%d, want 3/2", admitted, refused)
	}
	used := store.totalUsage("u", "BTC")
	if used.Cmp(big.NewFloat(30000)) > 0 {
		t.Fatalf("daily usage %s exceeds the 30000 USDT cap", used.Text('f', 2))
	}
}

// TestDailyLimitReleaseOnReject verifies a rejected withdrawal credits the
// daily meter back so the capacity becomes usable again.
func TestDailyLimitReleaseOnReject(t *testing.T) {
	ws, store := newLimitFixture(t)

	tx, err := ws.RequestWithdrawal("u", "BTC", validTestBTCAddr, big.NewFloat(1)) // 10000 USDT
	if err != nil {
		t.Fatalf("first withdrawal: %v", err)
	}
	if used := store.totalUsage("u", "BTC"); used.Cmp(big.NewFloat(10000)) != 0 {
		t.Fatalf("usage after first withdrawal = %s, want 10000", used.Text('f', 2))
	}
	if err := ws.RejectWithdrawal(tx.ID); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if used := store.totalUsage("u", "BTC"); used.Sign() != 0 {
		t.Fatalf("usage after reject = %s, want 0", used.Text('f', 2))
	}
	// Full remaining capacity is available again.
	if _, err := ws.RequestWithdrawal("u", "BTC", validTestBTCAddr, big.NewFloat(3)); err != nil { // 30000 USDT = cap
		t.Fatalf("withdrawal after reject should fit the cap: %v", err)
	}
	if _, err := ws.RequestWithdrawal("u", "BTC", validTestBTCAddr, big.NewFloat(0.5)); !errors.Is(err, ErrDailyLimitExceeded) {
		t.Fatalf("over-cap withdrawal = %v, want ErrDailyLimitExceeded", err)
	}
}

// TestDailyLimitFailClosedOnMissingPrice verifies that when the price source
// cannot price an asset the withdrawal is refused (fail-closed), not admitted
// against an unknown USDT equivalent.
func TestDailyLimitFailClosedOnMissingPrice(t *testing.T) {
	store := newFakeLimitStore()
	store.seed("u", "ETH", 100)
	svc := NewService(store, map[string]BlockchainClient{"ETH": NewMockBlockchainClient("ETH")}, nil)
	ws := NewWithdrawalService(svc)
	ws.SetReviewThreshold(big.NewFloat(1e9))
	addr := "0x71c7656ec7ab88b098defb751b7401b5f6d8976f"
	ws.AddAddress(AddressBookEntry{UserID: "u", Asset: "ETH", Address: addr}, "admin")
	ws.SetPriceGetter(fixedPriceGetter{}) // no prices at all
	ws.SetPlatformLimitLoader(fixedLimitLoader{0: 30000})
	if err := ws.ReloadPlatformLimits(); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.RequestWithdrawal("u", "ETH", addr, big.NewFloat(1)); err == nil {
		t.Fatal("withdrawal admitted without a price source; want fail-closed refusal")
	}
}
