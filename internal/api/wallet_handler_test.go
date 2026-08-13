package api

import (
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/WkT010/nexa-exchange/internal/wallet"
	"github.com/gin-gonic/gin"
)

// ─────────────────────────────────────────────────────────────────────────────
// GetDepositAddress — persistence + idempotency (T54).
//
// The deposit-claim auto-verifier binds an on-chain transfer to the claimant
// exclusively through the spot wallet row's address column (wallets.address).
// These tests pin down that the endpoint (1) writes every generated address
// into that column and (2) never generates a second address once one exists.
// ─────────────────────────────────────────────────────────────────────────────

// fakeAddrStore is an in-memory DepositAddressStore with the same
// first-address-wins semantics as the PG upsert.
type fakeAddrStore struct {
	wallets map[string]*wallet.Wallet // key: userID/asset
	err     error                     // simulate a store outage
}

func newFakeAddrStore() *fakeAddrStore {
	return &fakeAddrStore{wallets: map[string]*wallet.Wallet{}}
}

func (f *fakeAddrStore) GetWallet(userID, asset string) (*wallet.Wallet, error) {
	if f.err != nil {
		return nil, f.err
	}
	if w := f.wallets[userID+"/"+asset]; w != nil {
		return w, nil
	}
	return nil, wallet.ErrWalletNotFound
}

func (f *fakeAddrStore) AssignDepositAddress(userID, asset, address string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	key := userID + "/" + asset
	w := f.wallets[key]
	if w == nil {
		w = &wallet.Wallet{
			ID: "wal_test", UserID: userID, Asset: asset, AccountType: wallet.AccountSpot,
			Balance: big.NewFloat(0), Locked: big.NewFloat(0),
		}
		f.wallets[key] = w
	}
	if w.Address == "" { // the first address ever assigned wins
		w.Address = address
	}
	return w.Address, nil
}

// countingClient counts GenerateAddress calls so tests can prove generation
// is skipped on repeat requests.
type countingClient struct {
	calls int
	next  string
	err   error
}

func (c *countingClient) GenerateAddress() (string, error) {
	c.calls++
	if c.err != nil {
		return "", c.err
	}
	return c.next, nil
}
func (c *countingClient) GetBalance(string) (*big.Float, error) { return big.NewFloat(0), nil }
func (c *countingClient) SendTransaction(string, *big.Float) (string, error) {
	return "0xtx", nil
}
func (c *countingClient) GetConfirmations(string) (int, error) { return 0, nil }
func (c *countingClient) IsValidAddress(string) bool             { return true }

// depositAddrHandler wires the handler with fakes and mounts the endpoint
// behind a stub JWT middleware (X-User header selects the identity).
func depositAddrHandler(clients map[string]wallet.BlockchainClient, store DepositAddressStore) (*WalletHandler, *gin.Engine) {
	gin.SetMode(gin.TestMode)
	h := NewWalletHandler(&fakeWalletService{}, clients)
	if store != nil {
		h.SetDepositAddressStore(store)
	}
	r := gin.New()
	mw := func(c *gin.Context) {
		c.Set("user_id", c.GetHeader("X-User"))
		c.Next()
	}
	r.POST("/api/v2/wallet/deposit/address", mw, h.GetDepositAddress)
	return h, r
}

func postDepositAddress(t *testing.T, r *gin.Engine, user, asset string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v2/wallet/deposit/address",
		strings.NewReader(`{"asset":"`+asset+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User", user)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON response: %v (body=%s)", err, w.Body.String())
	}
	return w, body
}

// TestGetDepositAddressPersistsGeneratedAddress: the generated address is
// written to the user's spot wallet row and returned verbatim.
func TestGetDepositAddressPersistsGeneratedAddress(t *testing.T) {
	store := newFakeAddrStore()
	client := &countingClient{next: "0xGeneratedFirst"}
	_, r := depositAddrHandler(map[string]wallet.BlockchainClient{"USDT": client}, store)

	w, body := postDepositAddress(t, r, "usr_alice", "USDT")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if body["address"] != "0xGeneratedFirst" {
		t.Fatalf("address = %v, want 0xGeneratedFirst", body["address"])
	}
	if body["asset"] != "USDT" || body["user_id"] != "usr_alice" {
		t.Fatalf("unexpected body shape: %v", body)
	}
	persisted, err := store.GetWallet("usr_alice", "USDT")
	if err != nil || persisted.Address != "0xGeneratedFirst" {
		t.Fatalf("wallets.address not persisted: w=%+v err=%v", persisted, err)
	}
}

// TestGetDepositAddressIdempotentNoRegeneration: a repeat call returns the
// previously assigned address and never invokes GenerateAddress again.
func TestGetDepositAddressIdempotentNoRegeneration(t *testing.T) {
	store := newFakeAddrStore()
	client := &countingClient{next: "0xGeneratedFirst"}
	_, r := depositAddrHandler(map[string]wallet.BlockchainClient{"USDT": client}, store)

	_, first := postDepositAddress(t, r, "usr_alice", "USDT")
	// Second generation would return a different value if it ran.
	client.next = "0xGeneratedSecond"
	w, second := postDepositAddress(t, r, "usr_alice", "USDT")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if second["address"] != first["address"] {
		t.Fatalf("second call returned %v, want the same address %v", second["address"], first["address"])
	}
	if client.calls != 1 {
		t.Fatalf("GenerateAddress called %d times, want 1 (repeat must not regenerate)", client.calls)
	}
}

// TestGetDepositAddressReturnsStoreWinner: when the store already holds an
// address (e.g. written concurrently by an earlier request), the endpoint
// returns the persisted winner, never the freshly generated candidate.
func TestGetDepositAddressReturnsStoreWinner(t *testing.T) {
	store := newFakeAddrStore()
	store.wallets["usr_alice/USDT"] = &wallet.Wallet{
		ID: "wal_existing", UserID: "usr_alice", Asset: "USDT", AccountType: wallet.AccountSpot,
		Address: "", Balance: big.NewFloat(0), Locked: big.NewFloat(0),
	}
	// Make the GetWallet lookup fail so the handler takes the generate path,
	// then have the store resolve the race in favour of a pre-existing row.
	client := &countingClient{next: "0xLateCandidate"}
	_, r := depositAddrHandler(map[string]wallet.BlockchainClient{"USDT": client}, winnerAddrStore{inner: store, winner: "0xEarlierWinner"})

	w, body := postDepositAddress(t, r, "usr_alice", "USDT")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if body["address"] != "0xEarlierWinner" {
		t.Fatalf("address = %v, want the persisted winner 0xEarlierWinner", body["address"])
	}
}

// winnerAddrStore wraps a fakeAddrStore but resolves AssignDepositAddress to
// a fixed winner, emulating the PG upsert when a concurrent request inserted
// first.
type winnerAddrStore struct {
	inner  *fakeAddrStore
	winner string
}

func (s winnerAddrStore) GetWallet(userID, asset string) (*wallet.Wallet, error) {
	return nil, wallet.ErrWalletNotFound
}
func (s winnerAddrStore) AssignDepositAddress(userID, asset, address string) (string, error) {
	return s.winner, nil
}

// TestGetDepositAddressEVMPlaceholderForClientlessAsset: USDT has no
// blockchain client; the endpoint issues a random EVM-format address and
// persists it so the verifier has a recipient to bind transfers against.
func TestGetDepositAddressEVMPlaceholderForClientlessAsset(t *testing.T) {
	store := newFakeAddrStore()
	_, r := depositAddrHandler(map[string]wallet.BlockchainClient{}, store)

	w, body := postDepositAddress(t, r, "usr_bob", "USDT")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	addr, _ := body["address"].(string)
	if !strings.HasPrefix(addr, "0x") || len(addr) != 42 {
		t.Fatalf("placeholder address %q is not 0x + 40 hex chars", addr)
	}
	persisted, err := store.GetWallet("usr_bob", "USDT")
	if err != nil || persisted.Address != addr {
		t.Fatalf("placeholder not persisted: w=%+v err=%v", persisted, err)
	}
	// Repeat call is idempotent.
	_, second := postDepositAddress(t, r, "usr_bob", "USDT")
	if second["address"] != addr {
		t.Fatalf("repeat call returned %v, want %v", second["address"], addr)
	}
}

// TestGetDepositAddressClientErrorFallsBackForEVM: an RPC client whose
// GenerateAddress is unimplemented (errNotImplemented) must not 500 for EVM
// assets — the placeholder path takes over.
func TestGetDepositAddressClientErrorFallsBackForEVM(t *testing.T) {
	store := newFakeAddrStore()
	client := &countingClient{err: errors.New("not implemented")}
	_, r := depositAddrHandler(map[string]wallet.BlockchainClient{"ETH": client}, store)

	w, body := postDepositAddress(t, r, "usr_carol", "ETH")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	addr, _ := body["address"].(string)
	if !strings.HasPrefix(addr, "0x") || len(addr) != 42 {
		t.Fatalf("placeholder address %q is not EVM-formatted", addr)
	}
}

// TestGetDepositAddressManualOnlyStaysManual: non-EVM assets without a
// client keep the legacy "manual deposit only" response and persist nothing.
func TestGetDepositAddressManualOnlyStaysManual(t *testing.T) {
	store := newFakeAddrStore()
	_, r := depositAddrHandler(map[string]wallet.BlockchainClient{}, store)

	w, body := postDepositAddress(t, r, "usr_dave", "SOL")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if body["address"] != "" || body["note"] != "manual deposit only" {
		t.Fatalf("expected manual-only response, got %v", body)
	}
	if _, err := store.GetWallet("usr_dave", "SOL"); !errors.Is(err, wallet.ErrWalletNotFound) {
		t.Fatalf("manual-only path must not persist a wallet row, got err=%v", err)
	}
}

// TestGetDepositAddressPersistenceFailure500s: a store outage on the write
// path must fail closed (500) — never hand out an address that was not
// persisted, since the verifier would later be unable to bind claims to it.
func TestGetDepositAddressPersistenceFailure500s(t *testing.T) {
	store := newFakeAddrStore()
	store.err = errors.New("pg connection refused")
	client := &countingClient{next: "0xLostAddress"}
	_, r := depositAddrHandler(map[string]wallet.BlockchainClient{"USDT": client}, store)

	w, _ := postDepositAddress(t, r, "usr_eve", "USDT")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (fail closed on persistence error)", w.Code)
	}
}

// TestGetDepositAddressLegacyBehaviourWithoutStore: with no persistence
// wired the endpoint keeps its pre-T54 shape (generate + return).
func TestGetDepositAddressLegacyBehaviourWithoutStore(t *testing.T) {
	client := &countingClient{next: "BTC_legacy"}
	_, r := depositAddrHandler(map[string]wallet.BlockchainClient{"BTC": client}, nil)

	w, body := postDepositAddress(t, r, "usr_frank", "BTC")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if body["address"] != "BTC_legacy" {
		t.Fatalf("address = %v, want BTC_legacy", body["address"])
	}
}
