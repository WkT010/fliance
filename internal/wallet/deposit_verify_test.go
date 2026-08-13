package wallet

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// Alchemy JSON-RPC mock: dispatches on the requested method and returns
// canned result payloads, so the verifier tests run fully offline. The
// verifier only uses standard methods: eth_getTransactionReceipt,
// eth_blockNumber and eth_getTransactionByHash.
// ─────────────────────────────────────────────────────────────────────────────

type alchemyMock struct {
	mu       sync.Mutex
	receipt  json.RawMessage // eth_getTransactionReceipt result (raw JSON, "null" = not found)
	head     string          // eth_blockNumber result, e.g. "0x11"
	tx       json.RawMessage // eth_getTransactionByHash result (raw JSON, "null" = not found)
	rpcError bool            // answer a JSON-RPC error object instead
	status   int             // HTTP status override (0 = 200)
	methods  []string        // observed call order
}

func (m *alchemyMock) server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		m.mu.Lock()
		m.methods = append(m.methods, req.Method)
		m.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if m.status != 0 {
			w.WriteHeader(m.status)
			return
		}
		if m.rpcError {
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"mock failure"}}`))
			return
		}
		var result json.RawMessage
		switch req.Method {
		case "eth_getTransactionReceipt":
			result = m.receipt
		case "eth_blockNumber":
			result = json.RawMessage(`"` + m.head + `"`)
		case "eth_getTransactionByHash":
			result = m.tx
		default:
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":null}`))
			return
		}
		resp := map[string]any{"jsonrpc": "2.0", "id": 1, "result": result}
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

const goodTxID = "0x000000000000000000000000000000000000000000000000000000000000abcd"

// okReceipt builds a successful receipt at block 0x10 with optional logs.
func okReceipt(logs ...map[string]any) json.RawMessage {
	b, _ := json.Marshal(map[string]any{"status": "0x1", "blockNumber": "0x10", "logs": logs})
	return b
}

// ethTx builds an eth_getTransactionByHash result (native transfer).
func ethTx(to, valueWeiHex string) json.RawMessage {
	b, _ := json.Marshal(map[string]any{"to": to, "value": valueWeiHex})
	return b
}

// topic32 left-pads a 20-byte hex address into a 32-byte log topic.
func topic32(addr string) string {
	return "0x000000000000000000000000" + strings.TrimPrefix(strings.ToLower(addr), "0x")
}

// transferLog builds one ERC-20 Transfer event log. valueHex is the raw
// integer token amount in base units (no 0x prefix needed).
func transferLog(contract, to, valueHex string) map[string]any {
	return map[string]any{
		"address": contract,
		"topics": []string{
			erc20TransferTopic,
			topic32("0x1111111111111111111111111111111111111111"),
			topic32(to),
		},
		"data": "0x" + strings.TrimPrefix(strings.ToLower(valueHex), "0x"),
	}
}

func newTestVerifier(t *testing.T, m *alchemyMock) *DepositVerifier {
	t.Helper()
	srv := m.server()
	t.Cleanup(srv.Close)
	return NewDepositVerifier(srv.URL, 5*time.Second)
}

// ─────────────────────────────────────────────────────────────────────────────
// tests
// ─────────────────────────────────────────────────────────────────────────────

func TestVerifyDepositERC20Match(t *testing.T) {
	to := "0x2222222222222222222222222222222222222222"
	// 1000 USDT (6 decimals) = 10^9 = 0x3b9aca00.
	m := &alchemyMock{
		receipt: okReceipt(transferLog(erc20ContractUSDT, to, "3b9aca00")),
		head:    "0x15",
		tx:      ethTx(to, "0x0"),
	}
	v := newTestVerifier(t, m)

	res, err := v.VerifyDeposit(context.Background(), "USDT", goodTxID, big.NewFloat(900))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !res.OK {
		t.Fatalf("result = %+v, want OK", res)
	}
	if res.MatchedAmount == nil || res.MatchedAmount.Cmp(big.NewFloat(1000)) != 0 {
		t.Errorf("matched amount = %v, want 1000", res.MatchedAmount)
	}
	// block 0x10, head 0x15 → 6 confirmations.
	if res.Confirmations != 6 {
		t.Errorf("confirmations = %d, want 6", res.Confirmations)
	}
	if !strings.Contains(res.Note, "recipient address NOT checked") {
		t.Errorf("note = %q, must flag the skipped address check", res.Note)
	}
}

func TestVerifyDepositNativeETH(t *testing.T) {
	to := "0x2222222222222222222222222222222222222222"
	// 2.5 ETH = 2500000000000000000 wei = 0x22b1c8c1227a0000.
	m := &alchemyMock{
		receipt: okReceipt(),
		head:    "0x11",
		tx:      ethTx(to, "0x22b1c8c1227a0000"),
	}
	v := newTestVerifier(t, m)

	res, err := v.VerifyDeposit(context.Background(), "eth", goodTxID, big.NewFloat(2))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !res.OK {
		t.Fatalf("result = %+v, want OK", res)
	}
	if res.MatchedAmount == nil || res.MatchedAmount.Cmp(big.NewFloat(2.5)) != 0 {
		t.Errorf("matched amount = %v, want 2.5", res.MatchedAmount)
	}
}

func TestVerifyDepositNativeETHInsufficient(t *testing.T) {
	to := "0x2222222222222222222222222222222222222222"
	// 0.5 ETH = 500000000000000000 wei = 0x6f05b59d3b20000.
	m := &alchemyMock{
		receipt: okReceipt(),
		head:    "0x11",
		tx:      ethTx(to, "0x6f05b59d3b20000"),
	}
	v := newTestVerifier(t, m)

	res, err := v.VerifyDeposit(context.Background(), "ETH", goodTxID, big.NewFloat(5))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if res.OK {
		t.Fatalf("result = %+v, want NOT OK (tx value below claim)", res)
	}
	if !strings.Contains(res.Note, "below the claimed amount") {
		t.Errorf("note = %q, want insufficiency reason", res.Note)
	}
}

func TestVerifyDepositInsufficientAmount(t *testing.T) {
	to := "0x2222222222222222222222222222222222222222"
	// 100 USDT = 10^8 = 0x5f5e100.
	m := &alchemyMock{
		receipt: okReceipt(transferLog(erc20ContractUSDT, to, "5f5e100")),
		head:    "0x11",
		tx:      ethTx(to, "0x0"),
	}
	v := newTestVerifier(t, m)

	res, err := v.VerifyDeposit(context.Background(), "USDT", goodTxID, big.NewFloat(900))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if res.OK {
		t.Fatalf("result = %+v, want NOT OK (amount below claim)", res)
	}
	if !strings.Contains(res.Note, "below the claimed amount") {
		t.Errorf("note = %q, want insufficiency reason", res.Note)
	}
}

func TestVerifyDepositNoMatchingTransfer(t *testing.T) {
	// Receipt carries no token Transfer log at all.
	m := &alchemyMock{
		receipt: okReceipt(),
		head:    "0x11",
		tx:      ethTx("0x2222222222222222222222222222222222222222", "0x0"),
	}
	v := newTestVerifier(t, m)

	res, err := v.VerifyDeposit(context.Background(), "USDT", goodTxID, big.NewFloat(1))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if res.OK {
		t.Fatalf("result = %+v, want NOT OK (no transfer log)", res)
	}
	if !strings.Contains(res.Note, "no on-chain USDT transfer") {
		t.Errorf("note = %q, want no-transfer reason", res.Note)
	}
}

func TestVerifyDepositReceiptFailed(t *testing.T) {
	m := &alchemyMock{
		receipt: json.RawMessage(`{"status":"0x0","blockNumber":"0x10"}`),
		head:    "0x11",
	}
	v := newTestVerifier(t, m)

	res, err := v.VerifyDeposit(context.Background(), "USDT", goodTxID, big.NewFloat(1))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if res.OK {
		t.Fatalf("result = %+v, want NOT OK (reverted receipt)", res)
	}
	if !strings.Contains(res.Note, "reverted") {
		t.Errorf("note = %q, want revert reason", res.Note)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, method := range m.methods {
		if method == "eth_getTransactionByHash" {
			t.Error("transaction queried despite a failed receipt")
		}
	}
}

func TestVerifyDepositTxNotFound(t *testing.T) {
	m := &alchemyMock{receipt: json.RawMessage(`null`), head: "0x11"}
	v := newTestVerifier(t, m)

	res, err := v.VerifyDeposit(context.Background(), "USDC", goodTxID, big.NewFloat(1))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if res.OK {
		t.Fatalf("result = %+v, want NOT OK (missing tx)", res)
	}
	if !strings.Contains(res.Note, "not found") {
		t.Errorf("note = %q, want not-found reason", res.Note)
	}
}

func TestVerifyDepositWrongTokenContract(t *testing.T) {
	// A USDC transfer cannot satisfy a USDT claim (and vice versa).
	to := "0x2222222222222222222222222222222222222222"
	// 5000 USDC = 5*10^9 = 0x12a05f200.
	m := &alchemyMock{
		receipt: okReceipt(transferLog(erc20ContractUSDC, to, "12a05f200")),
		head:    "0x11",
		tx:      ethTx(to, "0x0"),
	}
	v := newTestVerifier(t, m)

	res, err := v.VerifyDeposit(context.Background(), "USDT", goodTxID, big.NewFloat(10))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if res.OK {
		t.Fatalf("result = %+v, want NOT OK (wrong contract)", res)
	}
}

func TestVerifyDepositDepositAddressEnforced(t *testing.T) {
	// 5 ETH = 5000000000000000000 wei = 0x4563918244f40000, sent to the
	// wrong address.
	m := &alchemyMock{
		receipt: okReceipt(),
		head:    "0x11",
		tx:      ethTx("0x9999999999999999999999999999999999999999", "0x4563918244f40000"),
	}
	v := newTestVerifier(t, m)
	v.DepositAddress = "0x2222222222222222222222222222222222222222"

	res, err := v.VerifyDeposit(context.Background(), "ETH", goodTxID, big.NewFloat(1))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if res.OK {
		t.Fatalf("result = %+v, want NOT OK (wrong destination)", res)
	}
	if !strings.Contains(res.Note, "does not match the platform deposit address") {
		t.Errorf("note = %q, want destination mismatch", res.Note)
	}
}

func TestVerifyDepositERC20DestinationEnforced(t *testing.T) {
	// Large USDT transfer, but to an address that is not the platform's.
	// 10000 USDT = 10^10 = 0x2540be400.
	m := &alchemyMock{
		receipt: okReceipt(transferLog(erc20ContractUSDT, "0x9999999999999999999999999999999999999999", "2540be400")),
		head:    "0x11",
		tx:      ethTx("0x3333333333333333333333333333333333333333", "0x0"),
	}
	v := newTestVerifier(t, m)
	v.DepositAddress = "0x2222222222222222222222222222222222222222"

	res, err := v.VerifyDeposit(context.Background(), "USDT", goodTxID, big.NewFloat(1))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if res.OK {
		t.Fatalf("result = %+v, want NOT OK (wrong ERC-20 recipient)", res)
	}
}

func TestVerifyDepositUnverifiableAsset(t *testing.T) {
	m := &alchemyMock{}
	v := newTestVerifier(t, m)

	res, err := v.VerifyDeposit(context.Background(), "BTC", goodTxID, big.NewFloat(1))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if res.OK {
		t.Fatalf("result = %+v, want NOT OK", res)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.methods) != 0 {
		t.Errorf("RPC calls = %v, want none for unverifiable assets", m.methods)
	}
}

func TestVerifyDepositMalformedTxID(t *testing.T) {
	m := &alchemyMock{}
	v := newTestVerifier(t, m)

	res, err := v.VerifyDeposit(context.Background(), "ETH", "not-a-hash", big.NewFloat(1))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if res.OK {
		t.Fatalf("result = %+v, want NOT OK", res)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.methods) != 0 {
		t.Errorf("RPC calls = %v, want none for malformed txid", m.methods)
	}
}

func TestVerifyDepositRPCErrorIsNotApproval(t *testing.T) {
	m := &alchemyMock{rpcError: true}
	v := newTestVerifier(t, m)

	res, err := v.VerifyDeposit(context.Background(), "USDT", goodTxID, big.NewFloat(1))
	if err == nil {
		t.Fatalf("result = %+v, want error (infra failure → cannot verify)", res)
	}
	if res != nil {
		t.Errorf("result = %+v, want nil alongside the error", res)
	}
}

func TestVerifyDepositHTTPFailureIsNotApproval(t *testing.T) {
	m := &alchemyMock{status: http.StatusInternalServerError}
	v := newTestVerifier(t, m)

	_, err := v.VerifyDeposit(context.Background(), "USDT", goodTxID, big.NewFloat(1))
	if err == nil {
		t.Fatal("want error on HTTP 500, got nil")
	}
}

func TestIsAutoVerifiableAsset(t *testing.T) {
	for asset, want := range map[string]bool{
		"ETH": true, "eth": true, " USDT ": true, "usdc": true,
		"BTC": false, "ADA": false, "SOL": false, "BNB": false, "": false,
	} {
		if got := IsAutoVerifiableAsset(asset); got != want {
			t.Errorf("IsAutoVerifiableAsset(%q) = %v, want %v", asset, got, want)
		}
	}
}
