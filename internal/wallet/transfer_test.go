package wallet

import (
	"errors"
	"math/big"
	"testing"
)

// TestTransferMovesFundsAtomically covers the happy path: a spot->futures
// transfer debits the source, credits the destination and writes one
// type=Transfer ledger entry per leg (debit negative, credit positive).
func TestTransferMovesFundsAtomically(t *testing.T) {
	store := newMemWalletStore()
	store.seed("u1", "USDT", 100)
	svc := newTestService(store)

	if err := svc.Transfer("u1", AccountSpot, AccountFutures, "USDT", big.NewFloat(40)); err != nil {
		t.Fatalf("transfer: %v", err)
	}

	spot := store.wallets["u1/USDT/spot"]
	assertNear(t, spot.Balance, 60, "spot balance after transfer")
	fut, ok := store.wallets["u1/USDT/futures"]
	if !ok {
		t.Fatal("futures wallet row was not created")
	}
	assertNear(t, fut.Balance, 40, "futures balance after transfer")

	// Exactly one settle batch executed both legs together.
	if len(store.settleLog) != 1 {
		t.Fatalf("settle batches = %d, want 1", len(store.settleLog))
	}
	batch := store.settleLog[0]
	if len(batch) != 2 {
		t.Fatalf("ops in batch = %d, want 2", len(batch))
	}

	// Ledger: one Transfer entry per leg.
	var debit, credit int
	for _, tx := range store.txs {
		if tx.Type != Transfer || tx.Asset != "USDT" {
			continue
		}
		switch {
		case tx.AccountType == AccountSpot && tx.Amount.Sign() < 0:
			debit++
			assertNear(t, new(big.Float).Neg(tx.Amount), 40, "debit leg amount")
		case tx.AccountType == AccountFutures && tx.Amount.Sign() > 0:
			credit++
			assertNear(t, tx.Amount, 40, "credit leg amount")
		default:
			t.Errorf("unexpected transfer ledger entry: %+v", tx)
		}
	}
	if debit != 1 || credit != 1 {
		t.Errorf("transfer ledger legs = %d debit / %d credit, want 1/1", debit, credit)
	}
}

// TestTransferInsufficientBalanceIsAtomic: a failing transfer must leave both
// accounts untouched and write no ledger entries (negative-balance guard).
func TestTransferInsufficientBalanceIsAtomic(t *testing.T) {
	store := newMemWalletStore()
	store.seed("u1", "USDT", 50)
	svc := newTestService(store)

	err := svc.Transfer("u1", AccountSpot, AccountFutures, "USDT", big.NewFloat(200))
	if !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("err = %v, want ErrInsufficientBalance", err)
	}

	assertNear(t, store.wallets["u1/USDT/spot"].Balance, 50, "spot balance unchanged")
	if w, ok := store.wallets["u1/USDT/futures"]; ok && w.Balance.Sign() != 0 {
		t.Errorf("futures balance = %s, want 0 (rolled back)", w.Balance.Text('f', 6))
	}
	if len(store.txs) != 0 {
		t.Errorf("ledger entries after failed transfer = %d, want 0", len(store.txs))
	}
	if len(store.settleLog) != 0 {
		t.Errorf("settle batches after failed transfer = %d, want 0", len(store.settleLog))
	}

	// Transferring from an account that never received funds also fails.
	err = svc.Transfer("u1", AccountFunding, AccountSpot, "USDT", big.NewFloat(1))
	if !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("err = %v, want ErrInsufficientBalance for empty source account", err)
	}
}

// TestTransferLockedFundsCannotMove: only the available (balance - locked)
// portion is transferable.
func TestTransferLockedFundsCannotMove(t *testing.T) {
	store := newMemWalletStore()
	store.seed("u1", "USDT", 100)
	svc := newTestService(store)

	if _, err := svc.ReserveForOrder("u1", "USDT", big.NewFloat(80)); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if err := svc.Transfer("u1", AccountSpot, AccountFutures, "USDT", big.NewFloat(30)); !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("err = %v, want ErrInsufficientBalance (only 20 available)", err)
	}
	if err := svc.Transfer("u1", AccountSpot, AccountFutures, "USDT", big.NewFloat(20)); err != nil {
		t.Fatalf("transfer of available portion: %v", err)
	}
	assertNear(t, store.wallets["u1/USDT/futures"].Balance, 20, "futures balance")
}

// TestTransferRejectsBadRequests covers validation: same account, unknown
// accounts, non-positive amounts and empty assets.
func TestTransferRejectsBadRequests(t *testing.T) {
	store := newMemWalletStore()
	store.seed("u1", "USDT", 100)
	svc := newTestService(store)

	cases := []struct {
		name     string
		from, to string
		asset    string
		amount   *big.Float
		want     error
	}{
		{"same account", AccountSpot, AccountSpot, "USDT", big.NewFloat(1), ErrSameAccountTransfer},
		{"invalid source", "margin", AccountFutures, "USDT", big.NewFloat(1), ErrInvalidAccount},
		{"invalid destination", AccountSpot, "margin", "USDT", big.NewFloat(1), ErrInvalidAccount},
		{"zero amount", AccountSpot, AccountFutures, "USDT", big.NewFloat(0), ErrNegativeAmount},
		{"negative amount", AccountSpot, AccountFutures, "USDT", big.NewFloat(-5), ErrNegativeAmount},
		{"nil amount", AccountSpot, AccountFutures, "USDT", nil, ErrNegativeAmount},
		{"empty asset", AccountSpot, AccountFutures, "  ", big.NewFloat(1), ErrUnsupportedAsset},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := svc.Transfer("u1", tc.from, tc.to, tc.asset, tc.amount)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}

	// Nothing moved during the rejected attempts.
	assertNear(t, store.wallets["u1/USDT/spot"].Balance, 100, "spot balance unchanged")
	if len(store.txs) != 0 {
		t.Errorf("ledger entries after rejected transfers = %d, want 0", len(store.txs))
	}
}

// TestTransferAllAccountPairs exercises every valid ordered pair of accounts.
func TestTransferAllAccountPairs(t *testing.T) {
	accts := []string{AccountSpot, AccountFutures, AccountFunding}
	for _, from := range accts {
		for _, to := range accts {
			if from == to {
				continue
			}
			store := newMemWalletStore()
			store.seedAccount("u1", "USDT", from, 10)
			svc := newTestService(store)
			if err := svc.Transfer("u1", from, to, "USDT", big.NewFloat(10)); err != nil {
				t.Fatalf("%s->%s: %v", from, to, err)
			}
			assertNear(t, store.wallets["u1/USDT/"+from].Balance, 0, from+" drained")
			assertNear(t, store.wallets["u1/USDT/"+to].Balance, 10, to+" credited")
		}
	}
}

// TestGetBalanceForAccount verifies account-scoped reads and validation.
func TestGetBalanceForAccount(t *testing.T) {
	store := newMemWalletStore()
	store.seedAccount("u1", "USDT", AccountFutures, 42)
	svc := newTestService(store)

	w, err := svc.GetBalanceForAccount("u1", "USDT", AccountFutures)
	if err != nil {
		t.Fatalf("get futures balance: %v", err)
	}
	assertNear(t, w.Balance, 42, "futures balance")

	if _, err := svc.GetBalanceForAccount("u1", "USDT", "margin"); !errors.Is(err, ErrInvalidAccount) {
		t.Errorf("err = %v, want ErrInvalidAccount", err)
	}
	// Spot row does not exist: the store reports not-found, not zero.
	if _, err := svc.GetBalanceForAccount("u1", "USDT", AccountSpot); err == nil {
		t.Error("spot lookup should fail when only the futures row exists")
	}
}

// TestReserveForAccountScoping: a futures reservation must lock funds in the
// futures account only, never touching the spot wallet.
func TestReserveForAccountScoping(t *testing.T) {
	store := newMemWalletStore()
	store.seedAccount("u1", "USDT", AccountSpot, 100)
	store.seedAccount("u1", "USDT", AccountFutures, 30)
	svc := newTestService(store)

	if _, err := svc.ReserveForAccount("u1", "USDT", AccountFutures, big.NewFloat(25)); err != nil {
		t.Fatalf("reserve futures: %v", err)
	}
	assertNear(t, store.wallets["u1/USDT/spot"].Locked, 0, "spot untouched")
	assertNear(t, store.wallets["u1/USDT/futures"].Locked, 25, "futures locked")

	// Exceeding the futures available balance fails even though spot has funds.
	if _, err := svc.ReserveForAccount("u1", "USDT", AccountFutures, big.NewFloat(10)); !errors.Is(err, ErrInsufficientBalance) {
		t.Errorf("err = %v, want ErrInsufficientBalance", err)
	}
}
