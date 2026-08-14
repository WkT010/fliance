package wallet

import (
	"errors"
	"math/big"
	"strings"
	"sync"
	"testing"
)

// fakeWhitelistStore mimics PGAddressWhitelistStore semantics: addresses are
// matched case-insensitively via a normalised (lower-case) key while the raw
// spelling is preserved for display.
type fakeWhitelistStore struct {
	mu          sync.Mutex
	entries     []AddressBookEntry
	createdBy   []string
	addErr      error
	containsErr error
}

func (f *fakeWhitelistStore) ListWhitelistAddresses(userID, asset string) ([]AddressBookEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []AddressBookEntry
	for _, e := range f.entries {
		if e.UserID != userID {
			continue
		}
		if asset != "" && !strings.EqualFold(e.Asset, asset) {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

func (f *fakeWhitelistStore) AddWhitelistAddress(entry AddressBookEntry, createdBy string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.addErr != nil {
		return false, f.addErr
	}
	for _, e := range f.entries {
		if e.UserID == entry.UserID && strings.EqualFold(e.Asset, entry.Asset) &&
			strings.EqualFold(e.Address, entry.Address) {
			return false, nil // duplicate: friendly, no error
		}
	}
	f.entries = append(f.entries, entry)
	f.createdBy = append(f.createdBy, createdBy)
	return true, nil
}

func (f *fakeWhitelistStore) RemoveWhitelistAddress(userID, asset, address string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, e := range f.entries {
		if e.UserID == userID && strings.EqualFold(e.Asset, asset) && strings.EqualFold(e.Address, address) {
			f.entries = append(f.entries[:i], f.entries[i+1:]...)
			f.createdBy = append(f.createdBy[:i], f.createdBy[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeWhitelistStore) ContainsWhitelistedAddress(userID, asset, address string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.containsErr != nil {
		return false, f.containsErr
	}
	for _, e := range f.entries {
		if e.UserID == userID && strings.EqualFold(e.Asset, asset) && strings.EqualFold(e.Address, address) {
			return true, nil
		}
	}
	return false, nil
}

// TestWhitelistStoreBackedCRUD exercises the DB-backed add/list/remove path,
// including idempotent duplicates and case-insensitive matching.
func TestWhitelistStoreBackedCRUD(t *testing.T) {
	store := newMemWalletStore()
	svc := NewService(store, map[string]BlockchainClient{"BTC": permissiveClient{}}, nil)
	ws := NewWithdrawalService(svc)
	fake := &fakeWhitelistStore{}
	ws.SetAddressWhitelistStore(fake)

	added, err := ws.AddAddress(AddressBookEntry{UserID: "u", Asset: "btc", Address: "0xAbCdEf1234567890AbCdEf1234567890aBcDeF12", Label: "cold"}, "admin1")
	if err != nil || !added {
		t.Fatalf("first add: added=%v err=%v, want true/nil", added, err)
	}
	if len(fake.createdBy) != 1 || fake.createdBy[0] != "admin1" {
		t.Fatalf("created_by not propagated: %v", fake.createdBy)
	}

	// Duplicate (different case) must be friendly, not an error.
	added, err = ws.AddAddress(AddressBookEntry{UserID: "u", Asset: "BTC", Address: "0xabcdef1234567890abcdef1234567890abcdef12"}, "")
	if err != nil || added {
		t.Fatalf("duplicate add: added=%v err=%v, want false/nil", added, err)
	}

	// Lookup is case-insensitive and normalises the asset.
	if !ws.IsWhitelisted("u", "BTC", "0XABCDEF1234567890ABCDEF1234567890ABCDEF12") {
		t.Fatal("case-insensitive whitelist match failed")
	}
	if ws.IsWhitelisted("u", "BTC", "1BvBMSEYstWetqTFn5Au4m4GFg7xJaNVN2") {
		t.Fatal("unlisted address reported whitelisted")
	}

	// List returns the raw (original-case) address for display.
	entries, err := ws.ListAddresses("u", "BTC")
	if err != nil || len(entries) != 1 {
		t.Fatalf("list: %v (err=%v), want 1 entry", entries, err)
	}
	if entries[0].Address != "0xAbCdEf1234567890AbCdEf1234567890aBcDeF12" {
		t.Fatalf("raw spelling lost: %q", entries[0].Address)
	}

	// Remove: missing -> false, present -> true.
	removed, err := ws.RemoveAddress("u", "BTC", "1BvBMSEYstWetqTFn5Au4m4GFg7xJaNVN2")
	if err != nil || removed {
		t.Fatalf("remove missing: removed=%v err=%v, want false/nil", removed, err)
	}
	removed, err = ws.RemoveAddress("u", "btc", "0xabcdef1234567890abcdef1234567890abcdef12")
	if err != nil || !removed {
		t.Fatalf("remove present: removed=%v err=%v, want true/nil", removed, err)
	}
	if ws.IsWhitelisted("u", "BTC", "0xabcdef1234567890abcdef1234567890abcdef12") {
		t.Fatal("address still whitelisted after removal")
	}
}

// TestWhitelistStoreBackedWithdrawal: withdrawals to whitelisted addresses
// succeed, unlisted ones are refused, and store errors fail closed.
func TestWhitelistStoreBackedWithdrawal(t *testing.T) {
	store := newMemWalletStore()
	store.seed("u", "BTC", 10)
	svc := NewService(store, map[string]BlockchainClient{"BTC": permissiveClient{}}, nil)
	ws := NewWithdrawalService(svc)
	fake := &fakeWhitelistStore{}
	ws.SetAddressWhitelistStore(fake)

	addr := validTestBTCAddr

	// Outside the whitelist: refused with the sentinel error the HTTP layer
	// maps to 400 (message text is a frontend contract).
	_, err := ws.RequestWithdrawal("u", "BTC", addr, big.NewFloat(1))
	if !errors.Is(err, ErrWithdrawalAddressNotWhitelisted) {
		t.Fatalf("unlisted withdrawal: got %v, want ErrWithdrawalAddressNotWhitelisted", err)
	}
	if err.Error() != "withdrawal address not whitelisted" {
		t.Fatalf("error message = %q, want %q (frontend depends on it)", err.Error(), "withdrawal address not whitelisted")
	}

	if _, err := ws.AddAddress(AddressBookEntry{UserID: "u", Asset: "BTC", Address: addr}, "admin1"); err != nil {
		t.Fatalf("add: %v", err)
	}
	// Inside the whitelist: accepted.
	if _, err := ws.RequestWithdrawal("u", "BTC", addr, big.NewFloat(1)); err != nil {
		t.Fatalf("whitelisted withdrawal refused: %v", err)
	}

	// Lookup failure must fail closed, never bypass the whitelist.
	fake.mu.Lock()
	fake.containsErr = errors.New("db down")
	fake.mu.Unlock()
	if _, err := ws.RequestWithdrawal("u", "BTC", addr, big.NewFloat(1)); err == nil {
		t.Fatal("withdrawal accepted despite whitelist lookup failure")
	}
	if ws.IsWhitelisted("u", "BTC", addr) {
		t.Fatal("IsWhitelisted must fail closed on lookup errors")
	}
}
