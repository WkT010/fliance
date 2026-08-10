package wallet

import (
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"
)

// validTestBTCAddr passes the strict base58check validator (see validate.go).
const validTestBTCAddr = "1BvBMSEYstWetqTFn5Au4m4GFg7xJaNVN2"

// ---------------------------------------------------------------------------
// Policy tests
// ---------------------------------------------------------------------------

func TestColdWalletPolicyThresholds(t *testing.T) {
	p := NewColdWalletPolicy()
	// Default threshold 10000 applies to assets without an override.
	if !p.RequiresCold("ETH", big.NewFloat(10000)) {
		t.Error("amount == default threshold must require cold")
	}
	if p.RequiresCold("ETH", big.NewFloat(9999)) {
		t.Error("amount below default threshold must stay hot")
	}
	// Per-asset override.
	p.SetThreshold("BTC", big.NewFloat(0.5))
	if !p.RequiresCold("BTC", big.NewFloat(0.5)) {
		t.Error("BTC per-asset threshold not applied")
	}
	if p.RequiresCold("BTC", big.NewFloat(0.49)) {
		t.Error("BTC below threshold must stay hot")
	}
}

func TestColdWalletPolicyFromEnv(t *testing.T) {
	t.Setenv("COLD_WALLET_THRESHOLD", "500")
	t.Setenv("COLD_WALLET_THRESHOLD_BTC", "2")
	p := ColdWalletPolicyFromEnv()
	if got := p.ThresholdFor("ETH").String(); got != "500" {
		t.Errorf("default threshold = %s, want 500", got)
	}
	if got := p.ThresholdFor("BTC").String(); got != "2" {
		t.Errorf("BTC threshold = %s, want 2", got)
	}
	if !p.RequiresCold("BTC", big.NewFloat(2)) || p.RequiresCold("BTC", big.NewFloat(1.9)) {
		t.Error("env BTC threshold boundary wrong")
	}
}

// ---------------------------------------------------------------------------
// FileBasedColdSigner tests
// ---------------------------------------------------------------------------

func TestFileBasedColdSignerQueueAndStatus(t *testing.T) {
	pending := filepath.Join(t.TempDir(), "pending")
	signed := filepath.Join(t.TempDir(), "signed")
	signer, err := NewFileBasedColdSigner(pending, signed)
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}

	refID, err := signer.Queue(ColdTxDesc{
		WithdrawID:  "wd_1",
		Asset:       "BTC",
		ToAddress:   validTestBTCAddr,
		Amount:      "1.5",
		FeeStrategy: "satvbyte=auto",
	})
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	// Unsigned description must be on disk for the offline process.
	data, err := os.ReadFile(filepath.Join(pending, refID+".json"))
	if err != nil {
		t.Fatalf("pending file missing: %v", err)
	}
	var desc ColdTxDesc
	if err := json.Unmarshal(data, &desc); err != nil || desc.Asset != "BTC" || desc.Amount != "1.5" {
		t.Fatalf("pending file content wrong: %+v (%v)", desc, err)
	}

	// Not signed yet.
	st, err := signer.Status(refID)
	if err != nil || st.Status != ColdQueued {
		t.Fatalf("status = %+v (%v), want queued", st, err)
	}

	// Simulate the offline HSM: drop the signed payload.
	payload, _ := json.Marshal(SignedColdTx{RefID: refID, SignedRawTx: "0200000001abcd", SignerID: "hsm-1", SignedAt: 123})
	if err := os.WriteFile(filepath.Join(signed, refID+".json"), payload, 0o640); err != nil {
		t.Fatal(err)
	}
	st, err = signer.Status(refID)
	if err != nil || st.Status != ColdSignedOk || st.Signed == nil || st.Signed.SignedRawTx != "0200000001abcd" {
		t.Fatalf("status = %+v (%v), want signed with payload", st, err)
	}

	// Failure payload is reported as failed.
	failRef := "cold_fail"
	failPayload, _ := json.Marshal(SignedColdTx{RefID: failRef, Error: "policy: address not whitelisted"})
	if err := os.WriteFile(filepath.Join(signed, failRef+".json"), failPayload, 0o640); err != nil {
		t.Fatal(err)
	}
	st, err = signer.Status(failRef)
	if err != nil || st.Status != ColdSignFailed {
		t.Fatalf("status = %+v (%v), want failed", st, err)
	}
}

func TestFileBasedColdSignerRejectsTraversal(t *testing.T) {
	signer, err := NewFileBasedColdSigner(filepath.Join(t.TempDir(), "p"), filepath.Join(t.TempDir(), "s"))
	if err != nil {
		t.Fatal(err)
	}
	for _, evil := range []string{"../escape", `..\escape`, "a/b", ".."} {
		if _, err := signer.Status(evil); err == nil {
			t.Errorf("Status(%q) must be rejected", evil)
		}
		if _, err := signer.Queue(ColdTxDesc{RefID: evil}); err == nil {
			t.Errorf("Queue with ref %q must be rejected", evil)
		}
	}
}

// ---------------------------------------------------------------------------
// Withdrawal flow integration tests
// ---------------------------------------------------------------------------

// coldBroadcastClient records hot vs cold broadcasts.
type coldBroadcastClient struct {
	hotSends     int
	broadcasts   []string
	confirmations int
}

func (c *coldBroadcastClient) GenerateAddress() (string, error)      { return "", nil }
func (c *coldBroadcastClient) GetBalance(string) (*big.Float, error) { return big.NewFloat(0), nil }
func (c *coldBroadcastClient) SendTransaction(string, *big.Float) (string, error) {
	c.hotSends++
	return "hot_txhash", nil
}
func (c *coldBroadcastClient) GetConfirmations(string) (int, error) { return c.confirmations, nil }
func (c *coldBroadcastClient) IsValidAddress(string) bool           { return true }
func (c *coldBroadcastClient) BroadcastSignedTx(raw string) (string, error) {
	c.broadcasts = append(c.broadcasts, raw)
	return "cold_txhash", nil
}

func newColdFlowFixture(t *testing.T, threshold float64) (*WithdrawalService, *coldBroadcastClient, ColdSigner, string, string) {
	t.Helper()
	store := newMemWalletStore()
	store.seed("u", "BTC", 1000)
	cli := &coldBroadcastClient{confirmations: 12}
	svc := NewService(store, map[string]BlockchainClient{"BTC": cli}, nil)
	ws := NewWithdrawalService(svc)
	ws.AddAddress(AddressBookEntry{UserID: "u", Asset: "BTC", Address: validTestBTCAddr})
	policy := NewColdWalletPolicy()
	policy.SetThreshold("BTC", big.NewFloat(threshold))
	pending := filepath.Join(t.TempDir(), "pending")
	signedDir := filepath.Join(t.TempDir(), "signed")
	signer, err := NewFileBasedColdSigner(pending, signedDir)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	ws.SetColdSigner(signer, policy)
	return ws, cli, signer, pending, signedDir
}

// simulateOfflineSigning drops a signed payload for refID, as the offline HSM
// would do.
func simulateOfflineSigning(t *testing.T, signedDir, refID, rawTx string) {
	t.Helper()
	payload, _ := json.Marshal(SignedColdTx{RefID: refID, SignedRawTx: rawTx, SignerID: "hsm-1", SignedAt: 456})
	if err := os.WriteFile(filepath.Join(signedDir, refID+".json"), payload, 0o640); err != nil {
		t.Fatal(err)
	}
}

// TestColdWithdrawalFullFlow: a large withdrawal goes
// approved -> cold_signing -> cold_signed -> broadcast -> completed, and the
// hot wallet is never touched.
func TestColdWithdrawalFullFlow(t *testing.T) {
	ws, cli, _, pending, signedDir := newColdFlowFixture(t, 50)

	tx, err := ws.RequestWithdrawal("u", "BTC", validTestBTCAddr, big.NewFloat(60))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if err := ws.ApproveWithdrawal(tx.ID); err != nil {
		t.Fatalf("approve: %v", err)
	}

	// Broadcast routes to the cold signer instead of the hot wallet.
	if err := ws.BroadcastWithdrawal(tx.ID); err != nil {
		t.Fatalf("broadcast: %v", err)
	}
	tx, _ = ws.GetWithdrawal(tx.ID)
	if tx.Status != WithdrawalColdSigning {
		t.Fatalf("status = %s, want cold_signing", tx.Status)
	}
	if tx.ColdRef == "" {
		t.Fatal("cold ref not recorded")
	}
	if cli.hotSends != 0 {
		t.Fatalf("hot wallet used %d times for large withdrawal", cli.hotSends)
	}
	if _, err := os.Stat(filepath.Join(pending, tx.ColdRef+".json")); err != nil {
		t.Fatalf("pending description missing: %v", err)
	}

	// Not signed yet: polling keeps the withdrawal in cold_signing.
	if err := ws.ProcessColdWithdrawals(10); err != nil {
		t.Fatalf("process cold: %v", err)
	}
	tx, _ = ws.GetWithdrawal(tx.ID)
	if tx.Status != WithdrawalColdSigning {
		t.Fatalf("status = %s, want still cold_signing", tx.Status)
	}

	// Offline signer produces the payload; the next poll advances and
	// broadcasts it.
	simulateOfflineSigning(t, signedDir, tx.ColdRef, "0200000001feedface")
	if err := ws.ProcessColdWithdrawals(10); err != nil {
		t.Fatalf("process cold after signing: %v", err)
	}
	tx, _ = ws.GetWithdrawal(tx.ID)
	if tx.Status != WithdrawalBroadcast {
		t.Fatalf("status = %s, want broadcast", tx.Status)
	}
	if tx.TxHash != "cold_txhash" {
		t.Fatalf("txhash = %s, want cold broadcast hash", tx.TxHash)
	}
	if len(cli.broadcasts) != 1 || cli.broadcasts[0] != "0200000001feedface" {
		t.Fatalf("signed payload not broadcast: %v", cli.broadcasts)
	}

	// Confirmations finalise the withdrawal and debit the balance.
	if err := ws.ProcessBroadcastWithdrawals(10); err != nil {
		t.Fatalf("process broadcast: %v", err)
	}
	tx, _ = ws.GetWithdrawal(tx.ID)
	if tx.Status != WithdrawalCompleted {
		t.Fatalf("status = %s, want completed", tx.Status)
	}
	w, _ := ws.store.GetWallet("u", "BTC")
	assertNear(t, w.Balance, 940, "balance after cold withdrawal")
}

// TestHotWithdrawalUnchanged: amounts below the cold threshold keep the
// original hot-wallet path.
func TestHotWithdrawalUnchanged(t *testing.T) {
	ws, cli, _, _, _ := newColdFlowFixture(t, 50)

	tx, err := ws.RequestWithdrawal("u", "BTC", validTestBTCAddr, big.NewFloat(5))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if err := ws.ApproveWithdrawal(tx.ID); err != nil {
		t.Fatal(err)
	}
	if err := ws.BroadcastWithdrawal(tx.ID); err != nil {
		t.Fatalf("broadcast: %v", err)
	}
	tx, _ = ws.GetWithdrawal(tx.ID)
	if tx.Status != WithdrawalBroadcast || tx.TxHash != "hot_txhash" {
		t.Fatalf("status=%s txhash=%s, want hot broadcast", tx.Status, tx.TxHash)
	}
	if cli.hotSends != 1 {
		t.Fatalf("hot sends = %d, want 1", cli.hotSends)
	}
}

// TestColdWithdrawalSignFailure: a rejected offline signature fails the
// withdrawal and releases the reserved funds.
func TestColdWithdrawalSignFailure(t *testing.T) {
	ws, _, _, _, signedDir := newColdFlowFixture(t, 50)

	tx, err := ws.RequestWithdrawal("u", "BTC", validTestBTCAddr, big.NewFloat(80))
	if err != nil {
		t.Fatal(err)
	}
	_ = ws.ApproveWithdrawal(tx.ID)
	if err := ws.BroadcastWithdrawal(tx.ID); err != nil {
		t.Fatal(err)
	}
	tx, _ = ws.GetWithdrawal(tx.ID)

	failPayload, _ := json.Marshal(SignedColdTx{RefID: tx.ColdRef, Error: "rejected by policy"})
	if err := os.WriteFile(filepath.Join(signedDir, tx.ColdRef+".json"), failPayload, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := ws.ProcessColdWithdrawals(10); err != nil {
		t.Fatal(err)
	}
	tx, _ = ws.GetWithdrawal(tx.ID)
	if tx.Status != WithdrawalFailed {
		t.Fatalf("status = %s, want failed", tx.Status)
	}
	w, _ := ws.store.GetWallet("u", "BTC")
	assertNear(t, w.Locked, 0, "reserved funds released after cold failure")
	assertNear(t, w.Balance, 1000, "balance untouched after cold failure")
}

// TestNoColdSignerKeepsHotPath: without a configured signer everything stays
// on the hot path even for large amounts (legacy behaviour).
func TestNoColdSignerKeepsHotPath(t *testing.T) {
	store := newMemWalletStore()
	store.seed("u", "BTC", 1000)
	cli := &coldBroadcastClient{confirmations: 12}
	svc := NewService(store, map[string]BlockchainClient{"BTC": cli}, nil)
	ws := NewWithdrawalService(svc)
	ws.AddAddress(AddressBookEntry{UserID: "u", Asset: "BTC", Address: validTestBTCAddr})

	tx, err := ws.RequestWithdrawal("u", "BTC", validTestBTCAddr, big.NewFloat(500))
	if err != nil {
		t.Fatal(err)
	}
	_ = ws.ApproveWithdrawal(tx.ID)
	if err := ws.BroadcastWithdrawal(tx.ID); err != nil {
		t.Fatalf("broadcast: %v", err)
	}
	tx, _ = ws.GetWithdrawal(tx.ID)
	if tx.Status != WithdrawalBroadcast {
		t.Fatalf("status = %s, want broadcast (hot fallback)", tx.Status)
	}
}

// TestRejectColdSigningWithdrawal: a queued cold withdrawal can still be
// rejected before anything is broadcast.
func TestRejectColdSigningWithdrawal(t *testing.T) {
	ws, _, _, _, _ := newColdFlowFixture(t, 50)
	tx, err := ws.RequestWithdrawal("u", "BTC", validTestBTCAddr, big.NewFloat(60))
	if err != nil {
		t.Fatal(err)
	}
	_ = ws.ApproveWithdrawal(tx.ID)
	if err := ws.BroadcastWithdrawal(tx.ID); err != nil {
		t.Fatal(err)
	}
	if err := ws.RejectWithdrawal(tx.ID); err != nil {
		t.Fatalf("reject cold-signing withdrawal: %v", err)
	}
	tx, _ = ws.GetWithdrawal(tx.ID)
	if tx.Status != WithdrawalRejected {
		t.Fatalf("status = %s, want rejected", tx.Status)
	}
	w, _ := ws.store.GetWallet("u", "BTC")
	assertNear(t, w.Locked, 0, "locked released on cold rejection")
}

// TestColdBroadcastRequiresBroadcaster: clients without SignedTxBroadcaster
// refuse the cold flow rather than falling back to the hot wallet.
func TestColdBroadcastRequiresBroadcaster(t *testing.T) {
	store := newMemWalletStore()
	store.seed("u", "BTC", 1000)
	svc := NewService(store, map[string]BlockchainClient{"BTC": permissiveClient{}}, nil)
	ws := NewWithdrawalService(svc)
	ws.AddAddress(AddressBookEntry{UserID: "u", Asset: "BTC", Address: validTestBTCAddr})
	policy := NewColdWalletPolicy()
	policy.SetThreshold("BTC", big.NewFloat(50))
	signedDir := filepath.Join(t.TempDir(), "s")
	signer, err := NewFileBasedColdSigner(filepath.Join(t.TempDir(), "p"), signedDir)
	if err != nil {
		t.Fatal(err)
	}
	ws.SetColdSigner(signer, policy)

	tx, _ := ws.RequestWithdrawal("u", "BTC", validTestBTCAddr, big.NewFloat(60))
	_ = ws.ApproveWithdrawal(tx.ID)
	if err := ws.BroadcastWithdrawal(tx.ID); err != nil {
		t.Fatal(err)
	}
	tx, _ = ws.GetWithdrawal(tx.ID)
	simulateOfflineSigning(t, signedDir, tx.ColdRef, "02ff")
	err = ws.AdvanceColdWithdrawal(tx.ID) // -> cold_signed
	if err != nil {
		t.Fatalf("advance to cold_signed: %v", err)
	}
	if err := ws.AdvanceColdWithdrawal(tx.ID); err == nil {
		t.Fatal("cold broadcast must fail without a SignedTxBroadcaster client")
	} else if errors.Is(err, ErrColdTxNotSignedYet) {
		t.Fatalf("unexpected error: %v", err)
	}
}
