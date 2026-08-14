package wallet

import (
	"errors"
	"fmt"
	"math/big"
	"sort"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// In-memory WalletStore implementation (no database required)
// ---------------------------------------------------------------------------

// memWalletStore emulates the atomicity contract of the real Postgres store:
// ReserveForOrder checks availability and locks in one step; Settle applies
// all ops or returns an error without mutating anything.
type memWalletStore struct {
	mu        sync.Mutex
	wallets   map[string]*Wallet // key: userID/asset/accountType
	byID      map[string]*Wallet
	txs       map[string]*Transaction
	settleLog [][]SettleOp // recorded Settle batches, for assertions
	settleErr error        // injected Settle failure
}

var _ WalletStore = (*memWalletStore)(nil)

func newMemWalletStore() *memWalletStore {
	return &memWalletStore{
		wallets: make(map[string]*Wallet),
		byID:    make(map[string]*Wallet),
		txs:     make(map[string]*Transaction),
	}
}

func memKey(userID, asset, accountType string) string {
	return userID + "/" + asset + "/" + NormalizeAccountType(accountType)
}

func (m *memWalletStore) seed(userID, asset string, balance float64) *Wallet {
	return m.seedAccount(userID, asset, AccountSpot, balance)
}

func (m *memWalletStore) seedAccount(userID, asset, accountType string, balance float64) *Wallet {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UnixNano()
	acct := NormalizeAccountType(accountType)
	w := &Wallet{
		ID:          fmt.Sprintf("wal_%s_%s_%s", userID, asset, acct),
		UserID:      userID,
		Asset:       asset,
		AccountType: acct,
		Balance:     big.NewFloat(balance),
		Locked:      big.NewFloat(0),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	m.wallets[memKey(userID, asset, acct)] = w
	m.byID[w.ID] = w
	return w
}

func (m *memWalletStore) wallet(userID, asset string) (*Wallet, bool) {
	w, ok := m.wallets[memKey(userID, asset, AccountSpot)]
	return w, ok
}

func (m *memWalletStore) walletForAccount(userID, asset, accountType string) (*Wallet, bool) {
	w, ok := m.wallets[memKey(userID, asset, accountType)]
	return w, ok
}

func (m *memWalletStore) GetWallet(userID, asset string) (*Wallet, error) {
	return m.GetWalletForAccount(userID, asset, AccountSpot)
}

func (m *memWalletStore) GetWalletForAccount(userID, asset, accountType string) (*Wallet, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.walletForAccount(userID, asset, accountType)
	if !ok {
		return nil, ErrWalletNotFound
	}
	return w, nil
}

func (m *memWalletStore) GetWallets(userID string) ([]*Wallet, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*Wallet
	for _, w := range m.wallets {
		if w.UserID == userID {
			out = append(out, w)
		}
	}
	return out, nil
}

func (m *memWalletStore) SaveWallet(w *Wallet) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	w.AccountType = NormalizeAccountType(w.AccountType)
	m.wallets[memKey(w.UserID, w.Asset, w.AccountType)] = w
	m.byID[w.ID] = w
	return nil
}

func (m *memWalletStore) UpdateBalance(id string, delta *big.Float) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.byID[id]
	if !ok {
		return ErrWalletNotFound
	}
	w.Balance = new(big.Float).Add(w.Balance, delta)
	return nil
}

func (m *memWalletStore) LockBalance(id string, amt *big.Float) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.byID[id]
	if !ok {
		return ErrWalletNotFound
	}
	w.Locked = new(big.Float).Add(w.Locked, amt)
	return nil
}

func (m *memWalletStore) UnlockBalance(id string, amt *big.Float) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.byID[id]
	if !ok {
		return ErrWalletNotFound
	}
	w.Locked = new(big.Float).Sub(w.Locked, amt)
	return nil
}

func (m *memWalletStore) SaveTx(tx *Transaction) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.txs[tx.ID] = tx
	return nil
}

func (m *memWalletStore) GetTx(id string) (*Transaction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	tx, ok := m.txs[id]
	if !ok {
		return nil, errors.New("tx not found")
	}
	return tx, nil
}

func (m *memWalletStore) ListTx(userID string, limit, offset int) ([]*Transaction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*Transaction
	for _, tx := range m.txs {
		if tx.UserID == userID {
			out = append(out, tx)
		}
	}
	return paginate(out, limit, offset), nil
}

func (m *memWalletStore) UpdateTxStatus(id string, status TxStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	tx, ok := m.txs[id]
	if !ok {
		return errors.New("tx not found")
	}
	tx.Status = status
	return nil
}

func (m *memWalletStore) UpdateTxStatusFrom(id string, from []TxStatus, to TxStatus) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	tx, ok := m.txs[id]
	if !ok {
		return false, nil
	}
	for _, st := range from {
		if tx.Status == st {
			tx.Status = to
			return true, nil
		}
	}
	return false, nil
}

func (m *memWalletStore) ListTxByStatus(status TxStatus, limit, offset int) ([]*Transaction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*Transaction
	for _, tx := range m.txs {
		if tx.Status == status {
			out = append(out, tx)
		}
	}
	return paginate(out, limit, offset), nil
}

func (m *memWalletStore) ReserveForOrder(userID, asset string, amt *big.Float) (*Wallet, error) {
	return m.ReserveForAccount(userID, asset, AccountSpot, amt)
}

func (m *memWalletStore) ReserveForAccount(userID, asset, accountType string, amt *big.Float) (*Wallet, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if amt == nil || amt.Sign() <= 0 {
		return nil, ErrNegativeAmount
	}
	w, ok := m.walletForAccount(userID, asset, accountType)
	if !ok {
		return nil, ErrWalletNotFound
	}
	available := new(big.Float).Sub(w.Balance, w.Locked)
	if available.Cmp(amt) < 0 {
		return nil, ErrInsufficientBalance
	}
	w.Locked = new(big.Float).Add(w.Locked, amt)
	return w, nil
}

func (m *memWalletStore) Settle(ops []SettleOp, txns []*Transaction) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.settleErr != nil {
		return m.settleErr
	}
	// Resolve (and auto-create) every wallet row this batch touches. Credit
	// legs (e.g. the buyer's base asset) may target wallets that do not exist
	// yet; auto-create them like the real store's upsert behaviour.
	var created []string
	get := func(userID, asset, accountType string) *Wallet {
		key := memKey(userID, asset, accountType)
		w, ok := m.wallets[key]
		if !ok {
			acct := NormalizeAccountType(accountType)
			w = &Wallet{ID: fmt.Sprintf("wal_%s_%s_%s", userID, asset, acct), UserID: userID, Asset: asset, AccountType: acct, Balance: big.NewFloat(0), Locked: big.NewFloat(0)}
			m.wallets[key] = w
			m.byID[w.ID] = w
			created = append(created, key)
		}
		return w
	}
	// rollbackCreated removes wallet rows this batch auto-created, mirroring
	// the Postgres transaction rollback of the pre-upserts.
	rollbackCreated := func() {
		for _, key := range created {
			if w, ok := m.wallets[key]; ok {
				delete(m.byID, w.ID)
				delete(m.wallets, key)
			}
		}
	}
	// Dry-run on copies first: any failing op (e.g. a debit below the
	// available balance) aborts the whole batch without mutating state,
	// mirroring the Postgres transaction rollback.
	type sim struct{ bal, lock *big.Float }
	state := make(map[string]*sim)
	row := func(key string, w *Wallet) *sim {
		s, ok := state[key]
		if !ok {
			s = &sim{bal: new(big.Float).Copy(w.Balance), lock: new(big.Float).Copy(w.Locked)}
			state[key] = s
		}
		return s
	}
	for _, op := range ops {
		key := memKey(op.UserID, op.Asset, op.AccountType)
		s := row(key, get(op.UserID, op.Asset, op.AccountType))
		if op.Unlock != nil && op.Unlock.Sign() != 0 {
			s.lock.Sub(s.lock, op.Unlock)
		}
		if op.Delta != nil && op.Delta.Sign() != 0 {
			if op.Delta.Sign() < 0 {
				need := new(big.Float).Neg(op.Delta)
				if new(big.Float).Sub(s.bal, s.lock).Cmp(need) < 0 {
					rollbackCreated()
					return fmt.Errorf("insufficient balance for %s %s: %w", op.UserID, op.Asset, ErrInsufficientBalance)
				}
			}
			s.bal.Add(s.bal, op.Delta)
		}
	}
	// Commit the simulation back to the wallet rows.
	for key, s := range state {
		w := m.wallets[key]
		w.Balance = s.bal
		w.Locked = s.lock
	}
	for _, tx := range txns {
		m.txs[tx.ID] = tx
	}
	cp := make([]SettleOp, len(ops))
	copy(cp, ops)
	m.settleLog = append(m.settleLog, cp)
	return nil
}

func paginate(txs []*Transaction, limit, offset int) []*Transaction {
	sort.Slice(txs, func(i, j int) bool { return txs[i].CreatedAt > txs[j].CreatedAt })
	if offset >= len(txs) {
		return nil
	}
	txs = txs[offset:]
	if limit < len(txs) {
		txs = txs[:limit]
	}
	return txs
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// newTestService builds a Service with 10 bps taker / 5 bps maker fees.
func newTestService(store *memWalletStore) *Service {
	fees := &StaticFeeSchedule{Default: FeeConfig{
		TakerRate: big.NewFloat(0.001),
		MakerRate: big.NewFloat(0.0005),
	}}
	return NewService(store, nil, fees)
}

func assertNear(t *testing.T, got *big.Float, want float64, label string) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s: nil balance", label)
	}
	diff := new(big.Float).Sub(got, big.NewFloat(want))
	diff.Abs(diff)
	if diff.Cmp(big.NewFloat(1e-9)) > 0 {
		t.Errorf("%s = %s, want %v", label, got.Text('f', 10), want)
	}
}

// netDelta sums the Delta of all ops touching user/asset in one settle batch.
func netDelta(ops []SettleOp, user, asset string) *big.Float {
	sum := new(big.Float)
	for _, op := range ops {
		if op.UserID == user && op.Asset == asset && op.Delta != nil {
			sum.Add(sum, op.Delta)
		}
	}
	return sum
}

// permissiveClient accepts any address, proving that the service-level strict
// validation is independent of (and stronger than) the client's check.
type permissiveClient struct{}

func (permissiveClient) GenerateAddress() (string, error)                   { return "x", nil }
func (permissiveClient) GetBalance(string) (*big.Float, error)              { return big.NewFloat(0), nil }
func (permissiveClient) SendTransaction(string, *big.Float) (string, error) { return "0xdead", nil }
func (permissiveClient) GetConfirmations(string) (int, error)               { return 0, nil }
func (permissiveClient) IsValidAddress(string) bool                         { return true }

// ---------------------------------------------------------------------------
// Settlement tests
// ---------------------------------------------------------------------------

// TestSettleFillIdempotent: settling the same fillID twice applies the
// settlement exactly once.
func TestSettleFillIdempotent(t *testing.T) {
	store := newMemWalletStore()
	store.seed("taker", "USDT", 1000)
	store.seed("maker", "BTC", 10)
	svc := newTestService(store)

	// price 100, qty 2 -> notional 200; taker buys (limit), maker sells.
	if err := svc.ReserveOrder("oT", "taker", "BTC/USDT", 1, 0, big.NewFloat(100), big.NewFloat(2)); err != nil {
		t.Fatalf("reserve taker: %v", err)
	}
	if err := svc.ReserveOrder("oM", "maker", "BTC/USDT", -1, 0, big.NewFloat(100), big.NewFloat(2)); err != nil {
		t.Fatalf("reserve maker: %v", err)
	}

	settle := func() error {
		return svc.SettleFill("fill-1", "BTC/USDT", 1, "oT", "oM", "taker", "maker", big.NewFloat(100), big.NewFloat(2))
	}
	if err := settle(); err != nil {
		t.Fatalf("first settle: %v", err)
	}
	if err := settle(); err != nil {
		t.Fatalf("duplicate settle should be a no-op, got: %v", err)
	}
	if len(store.settleLog) != 1 {
		t.Fatalf("settle executed %d times, want exactly 1", len(store.settleLog))
	}

	// taker: -200.2 USDT (notional + taker fee), +2 BTC
	// maker: -2 BTC, +199.9 USDT (notional - maker fee)
	assertNear(t, store.wallets["taker/USDT/spot"].Balance, 799.8, "taker USDT balance")
	assertNear(t, store.wallets["taker/USDT/spot"].Locked, 0, "taker USDT locked")
	assertNear(t, store.wallets["taker/BTC/spot"].Balance, 2, "taker BTC balance")
	assertNear(t, store.wallets["maker/BTC/spot"].Balance, 8, "maker BTC balance")
	assertNear(t, store.wallets["maker/BTC/spot"].Locked, 0, "maker BTC locked")
	assertNear(t, store.wallets["maker/USDT/spot"].Balance, 199.9, "maker USDT balance")
}

// TestSettleFillFeeDeduction verifies maker/taker fee math on both legs.
func TestSettleFillFeeDeduction(t *testing.T) {
	store := newMemWalletStore()
	store.seed("taker", "USDT", 1000)
	store.seed("maker", "BTC", 10)
	svc := newTestService(store)
	_ = svc.ReserveOrder("oT", "taker", "BTC/USDT", 1, 0, big.NewFloat(100), big.NewFloat(2))
	_ = svc.ReserveOrder("oM", "maker", "BTC/USDT", -1, 0, big.NewFloat(100), big.NewFloat(2))

	if err := svc.SettleFill("fill-fee", "BTC/USDT", 1, "oT", "oM", "taker", "maker", big.NewFloat(100), big.NewFloat(2)); err != nil {
		t.Fatalf("settle: %v", err)
	}
	ops := store.settleLog[0]
	// Buyer (taker) debited notional + takerFee = 200 + 0.2.
	assertNear(t, new(big.Float).Neg(netDelta(ops, "taker", "USDT")), 200.2, "buyer quote debit")
	// Seller (maker) credited notional - makerFee = 200 - 0.1.
	assertNear(t, netDelta(ops, "maker", "USDT"), 199.9, "seller quote credit")
	// Base leg is fee-free: exact qty transfer.
	assertNear(t, netDelta(ops, "taker", "BTC"), 2, "buyer base credit")
	assertNear(t, new(big.Float).Neg(netDelta(ops, "maker", "BTC")), 2, "seller base debit")
}

// TestSettleFillTakerSell: when the taker sells, fee roles flip — the maker
// (buyer) pays the maker fee, the taker (seller) pays the taker fee.
func TestSettleFillTakerSell(t *testing.T) {
	store := newMemWalletStore()
	store.seed("maker", "USDT", 1000)
	store.seed("taker", "BTC", 10)
	svc := newTestService(store)
	_ = svc.ReserveOrder("oM", "maker", "BTC/USDT", 1, 0, big.NewFloat(100), big.NewFloat(2))
	_ = svc.ReserveOrder("oT", "taker", "BTC/USDT", -1, 0, big.NewFloat(100), big.NewFloat(2))

	if err := svc.SettleFill("fill-sell", "BTC/USDT", -1, "oT", "oM", "taker", "maker", big.NewFloat(100), big.NewFloat(2)); err != nil {
		t.Fatalf("settle: %v", err)
	}
	// maker (buyer) debited 200 + makerFee(0.1) = 200.1
	assertNear(t, store.wallets["maker/USDT/spot"].Balance, 799.9, "maker (buyer) USDT")
	assertNear(t, store.wallets["maker/BTC/spot"].Balance, 2, "maker BTC credit")
	// taker (seller) credited 200 - takerFee(0.2) = 199.8
	assertNear(t, store.wallets["taker/USDT/spot"].Balance, 199.8, "taker (seller) USDT")
	assertNear(t, store.wallets["taker/BTC/spot"].Balance, 8, "taker BTC debit")
	assertNear(t, store.wallets["taker/BTC/spot"].Locked, 0, "taker BTC locked")
	// The buyer reserved at the taker rate but filled as maker: the leftover
	// fee buffer stays locked until the order is released/completed.
	if err := svc.ReleaseOrder("oM", "maker"); err != nil {
		t.Fatalf("release maker: %v", err)
	}
	assertNear(t, store.wallets["maker/USDT/spot"].Locked, 0, "maker USDT locked after release")
	assertNear(t, store.wallets["maker/USDT/spot"].Balance, 799.9, "maker USDT balance after release")
}

// TestReserveAndReleaseOrder covers reservation boundaries.
func TestReserveAndReleaseOrder(t *testing.T) {
	store := newMemWalletStore()
	store.seed("u", "USDT", 500)
	store.seed("u", "BTC", 3)
	svc := newTestService(store)

	// Limit buy locks price*qty*(1+takerRate) = 100*2*1.001 = 200.2.
	if err := svc.ReserveOrder("ord1", "u", "BTC/USDT", 1, 0, big.NewFloat(100), big.NewFloat(2)); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	assertNear(t, store.wallets["u/USDT/spot"].Locked, 200.2, "limit buy locked")

	// Release on cancel unlocks the full remaining reservation.
	if err := svc.ReleaseOrder("ord1", "u"); err != nil {
		t.Fatalf("release: %v", err)
	}
	assertNear(t, store.wallets["u/USDT/spot"].Locked, 0, "locked after release")
	assertNear(t, store.wallets["u/USDT/spot"].Balance, 500, "balance unchanged by release")

	// Second release is a safe no-op.
	if err := svc.ReleaseOrder("ord1", "u"); err != nil {
		t.Fatalf("second release: %v", err)
	}

	// Insufficient available balance is rejected.
	if err := svc.ReserveOrder("ord2", "u", "BTC/USDT", 1, 0, big.NewFloat(300), big.NewFloat(2)); !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("want ErrInsufficientBalance, got %v", err)
	}

	// Market buy does not pre-lock anything.
	if err := svc.ReserveOrder("ord3", "u", "BTC/USDT", 1, 1, nil, big.NewFloat(2)); err != nil {
		t.Fatalf("market buy reserve: %v", err)
	}
	assertNear(t, store.wallets["u/USDT/spot"].Locked, 0, "market buy locks nothing")
	if err := svc.ReleaseOrder("ord3", "u"); err != nil {
		t.Fatalf("market buy release: %v", err)
	}

	// Sell locks the base qty exactly.
	if err := svc.ReserveOrder("ord4", "u", "BTC/USDT", -1, 0, big.NewFloat(100), big.NewFloat(3)); err != nil {
		t.Fatalf("sell reserve: %v", err)
	}
	assertNear(t, store.wallets["u/BTC/spot"].Locked, 3, "sell locks base qty")

	// Oversell is rejected.
	if err := svc.ReserveOrder("ord5", "u", "BTC/USDT", -1, 0, big.NewFloat(100), big.NewFloat(0.5)); !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("want ErrInsufficientBalance on oversell, got %v", err)
	}
}

// TestSettleFillFailureRestoresReservations: if the store rejects a settle,
// reservation trackers are restored so funds are not silently lost.
func TestSettleFillFailureRestoresReservations(t *testing.T) {
	store := newMemWalletStore()
	store.seed("taker", "USDT", 1000)
	store.seed("maker", "BTC", 10)
	svc := newTestService(store)
	_ = svc.ReserveOrder("oT", "taker", "BTC/USDT", 1, 0, big.NewFloat(100), big.NewFloat(2))
	_ = svc.ReserveOrder("oM", "maker", "BTC/USDT", -1, 0, big.NewFloat(100), big.NewFloat(2))

	store.mu.Lock()
	store.settleErr = errors.New("db down")
	store.mu.Unlock()

	err := svc.SettleFill("fill-x", "BTC/USDT", 1, "oT", "oM", "taker", "maker", big.NewFloat(100), big.NewFloat(2))
	if err == nil {
		t.Fatal("expected settle failure")
	}
	store.mu.Lock()
	store.settleErr = nil
	store.mu.Unlock()

	// Reservation trackers must be restored so ReleaseOrder still works.
	if err := svc.ReleaseOrder("oT", "taker"); err != nil {
		t.Fatalf("release after failed settle: %v", err)
	}
	assertNear(t, store.wallets["taker/USDT/spot"].Locked, 0, "taker locked restored+released")

	// The fillID must NOT be marked processed: a retry succeeds.
	if err := svc.SettleFill("fill-x", "BTC/USDT", 1, "oT", "oM", "taker", "maker", big.NewFloat(100), big.NewFloat(2)); err != nil {
		t.Fatalf("retry settle: %v", err)
	}
	// settleLog holds the ReleaseOrder unlock batch plus the retried fill.
	if len(store.settleLog) != 2 {
		t.Fatalf("settle batches = %d, want 2 (release + retried fill)", len(store.settleLog))
	}
}

// TestProcessedFillsLRUEviction: once the LRU overflows, only the
// least-recently-used fillIDs are evicted; recent ones stay deduplicated.
func TestProcessedFillsLRUEviction(t *testing.T) {
	store := newMemWalletStore()
	store.seed("taker", "USDT", 1e9)
	store.seed("maker", "BTC", 1e9)
	svc := newTestService(store)
	svc.SetProcessedFillCapacity(3)

	for i := 0; i < 4; i++ {
		if err := svc.SettleFill(fmt.Sprintf("f%d", i), "BTC/USDT", 1, "oT", "oM", "taker", "maker", big.NewFloat(1), big.NewFloat(1)); err != nil {
			t.Fatalf("settle f%d: %v", i, err)
		}
	}
	if len(store.settleLog) != 4 {
		t.Fatalf("settle batches = %d, want 4", len(store.settleLog))
	}

	// f0 was evicted (oldest): replaying it settles again.
	if err := svc.SettleFill("f0", "BTC/USDT", 1, "oT", "oM", "taker", "maker", big.NewFloat(1), big.NewFloat(1)); err != nil {
		t.Fatal(err)
	}
	if len(store.settleLog) != 5 {
		t.Fatalf("evicted fill replayed: batches = %d, want 5", len(store.settleLog))
	}

	// f3 is still cached: replay is a no-op.
	if err := svc.SettleFill("f3", "BTC/USDT", 1, "oT", "oM", "taker", "maker", big.NewFloat(1), big.NewFloat(1)); err != nil {
		t.Fatal(err)
	}
	if len(store.settleLog) != 5 {
		t.Fatalf("recent fill must stay deduplicated: batches = %d, want 5", len(store.settleLog))
	}
}

// TestProcessedFillTTLExpiry: dedup records older than the TTL expire.
func TestProcessedFillTTLExpiry(t *testing.T) {
	store := newMemWalletStore()
	store.seed("taker", "USDT", 1e9)
	store.seed("maker", "BTC", 1e9)
	svc := newTestService(store)
	svc.SetProcessedFillTTL(20 * time.Millisecond)

	settle := func() error {
		return svc.SettleFill("ttl-fill", "BTC/USDT", 1, "oT", "oM", "taker", "maker", big.NewFloat(1), big.NewFloat(1))
	}
	if err := settle(); err != nil {
		t.Fatal(err)
	}
	if err := settle(); err != nil {
		t.Fatal(err)
	}
	if len(store.settleLog) != 1 {
		t.Fatalf("immediate replay must be deduplicated, batches = %d", len(store.settleLog))
	}
	time.Sleep(40 * time.Millisecond)
	if err := settle(); err != nil {
		t.Fatal(err)
	}
	if len(store.settleLog) != 2 {
		t.Fatalf("expired record must allow re-settlement, batches = %d", len(store.settleLog))
	}
}

// ---------------------------------------------------------------------------
// Withdrawal address validation in the withdrawal flow
// ---------------------------------------------------------------------------

// TestWithdrawRejectsMalformedAddress: strict validation applies even when the
// registered client's own check is permissive.
func TestWithdrawRejectsMalformedAddress(t *testing.T) {
	store := newMemWalletStore()
	store.seed("u", "BTC", 10)
	svc := NewService(store, map[string]BlockchainClient{"BTC": permissiveClient{}}, nil)

	bad := []string{"", "not-an-address", "1BvBMSEYstWetqTFn5Au4m4GFg7xJaNVN3", "BTC_deadbeef"}
	for _, addr := range bad {
		if err := svc.Withdraw("u", "BTC", addr, big.NewFloat(1)); !errors.Is(err, ErrInvalidAddress) {
			t.Errorf("Withdraw(%q) = %v, want ErrInvalidAddress", addr, err)
		}
	}
	assertNear(t, store.wallets["u/BTC/spot"].Locked, 0, "nothing locked after rejected withdrawals")

	// A structurally valid address passes the format gate.
	if err := svc.Withdraw("u", "BTC", "1BvBMSEYstWetqTFn5Au4m4GFg7xJaNVN2", big.NewFloat(1)); err != nil {
		t.Fatalf("valid address rejected: %v", err)
	}
	assertNear(t, store.wallets["u/BTC/spot"].Locked, 1, "withdrawal locked funds")
}

// TestRequestWithdrawalValidatesAddress: the full withdrawal workflow rejects
// malformed addresses even when they are whitelisted.
func TestRequestWithdrawalValidatesAddress(t *testing.T) {
	store := newMemWalletStore()
	store.seed("u", "BTC", 10)
	svc := NewService(store, map[string]BlockchainClient{"BTC": permissiveClient{}}, nil)
	ws := NewWithdrawalService(svc)

	bad := "1BvBMSEYstWetqTFn5Au4m4GFg7xJaNVN3"
	ws.AddAddress(AddressBookEntry{UserID: "u", Asset: "BTC", Address: bad}, "admin")
	if _, err := ws.RequestWithdrawal("u", "BTC", bad, big.NewFloat(1)); !errors.Is(err, ErrInvalidAddress) {
		t.Fatalf("whitelisted malformed address: got %v, want ErrInvalidAddress", err)
	}

	good := "1BvBMSEYstWetqTFn5Au4m4GFg7xJaNVN2"
	ws.AddAddress(AddressBookEntry{UserID: "u", Asset: "BTC", Address: good}, "admin")
	tx, err := ws.RequestWithdrawal("u", "BTC", good, big.NewFloat(1))
	if err != nil {
		t.Fatalf("valid whitelisted withdrawal: %v", err)
	}
	if tx.Status != WithdrawalPending {
		t.Fatalf("status = %v, want pending", tx.Status)
	}
}
