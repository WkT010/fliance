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

// depositAddr is the claimant's platform deposit address used across tests.
const depositAddr = "0x2222222222222222222222222222222222222222"

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

// newTestVerifier builds an eth-mainnet-only verifier against one mock.
// head 0x1c over receipt block 0x10 → 13 confirmations (above the floor).
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
	// 1000 USDT (6 decimals) = 10^9 = 0x3b9aca00, sent to the deposit addr.
	m := &alchemyMock{
		receipt: okReceipt(transferLog(erc20Contract(networkTokens, NetworkEthMainnet, "USDT"), depositAddr, "3b9aca00")),
		head:    "0x1c",
		tx:      ethTx(depositAddr, "0x0"),
	}
	v := newTestVerifier(t, m)

	res, err := v.VerifyDeposit(context.Background(), "eth-mainnet", "USDT", goodTxID, big.NewFloat(900), depositAddr)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !res.OK {
		t.Fatalf("result = %+v, want OK", res)
	}
	if res.MatchedAmount == nil || res.MatchedAmount.Cmp(big.NewFloat(1000)) != 0 {
		t.Errorf("matched amount = %v, want 1000", res.MatchedAmount)
	}
	// block 0x10, head 0x1c → 13 confirmations.
	if res.Confirmations != 13 {
		t.Errorf("confirmations = %d, want 13", res.Confirmations)
	}
	if !strings.Contains(res.Note, "deposit address") {
		t.Errorf("note = %q, must mention the recipient binding", res.Note)
	}
}

// TestVerifyDepositAddressMatchCaseInsensitive: the on-chain recipient is
// lowercase while the platform address uses mixed EIP-55 casing — they must
// still match (case never matters for funds attribution).
func TestVerifyDepositAddressMatchCaseInsensitive(t *testing.T) {
	mixedCase := "0x2222222222222222222222222222222222222222"
	// 2.5 ETH = 0x22b1c8c1227a0000 sent to the LOWERCASE form.
	m := &alchemyMock{
		receipt: okReceipt(),
		head:    "0x1c",
		tx:      ethTx(strings.ToLower(mixedCase), "0x22b1c8c1227a0000"),
	}
	v := newTestVerifier(t, m)

	res, err := v.VerifyDeposit(context.Background(), "", "eth", goodTxID, big.NewFloat(2), strings.ToUpper(mixedCase))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !res.OK {
		t.Fatalf("result = %+v, want OK (case-insensitive address match, default network)", res)
	}
}

func TestVerifyDepositNativeETH(t *testing.T) {
	// 2.5 ETH = 2500000000000000000 wei = 0x22b1c8c1227a0000.
	m := &alchemyMock{
		receipt: okReceipt(),
		head:    "0x1c",
		tx:      ethTx(depositAddr, "0x22b1c8c1227a0000"),
	}
	v := newTestVerifier(t, m)

	res, err := v.VerifyDeposit(context.Background(), "eth-mainnet", "eth", goodTxID, big.NewFloat(2), depositAddr)
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
	// 0.5 ETH = 500000000000000000 wei = 0x6f05b59d3b20000.
	m := &alchemyMock{
		receipt: okReceipt(),
		head:    "0x1c",
		tx:      ethTx(depositAddr, "0x6f05b59d3b20000"),
	}
	v := newTestVerifier(t, m)

	res, err := v.VerifyDeposit(context.Background(), "eth-mainnet", "ETH", goodTxID, big.NewFloat(5), depositAddr)
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
	// 100 USDT = 10^8 = 0x5f5e100.
	m := &alchemyMock{
		receipt: okReceipt(transferLog(erc20Contract(networkTokens, NetworkEthMainnet, "USDT"), depositAddr, "5f5e100")),
		head:    "0x1c",
		tx:      ethTx(depositAddr, "0x0"),
	}
	v := newTestVerifier(t, m)

	res, err := v.VerifyDeposit(context.Background(), "eth-mainnet", "USDT", goodTxID, big.NewFloat(900), depositAddr)
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
		head:    "0x1c",
		tx:      ethTx(depositAddr, "0x0"),
	}
	v := newTestVerifier(t, m)

	res, err := v.VerifyDeposit(context.Background(), "eth-mainnet", "USDT", goodTxID, big.NewFloat(1), depositAddr)
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
		head:    "0x1c",
	}
	v := newTestVerifier(t, m)

	res, err := v.VerifyDeposit(context.Background(), "eth-mainnet", "USDT", goodTxID, big.NewFloat(1), depositAddr)
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
	m := &alchemyMock{receipt: json.RawMessage(`null`), head: "0x1c"}
	v := newTestVerifier(t, m)

	res, err := v.VerifyDeposit(context.Background(), "eth-mainnet", "USDC", goodTxID, big.NewFloat(1), depositAddr)
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
	// 5000 USDC = 5*10^9 = 0x12a05f200.
	m := &alchemyMock{
		receipt: okReceipt(transferLog(erc20Contract(networkTokens, NetworkEthMainnet, "USDC"), depositAddr, "12a05f200")),
		head:    "0x1c",
		tx:      ethTx(depositAddr, "0x0"),
	}
	v := newTestVerifier(t, m)

	res, err := v.VerifyDeposit(context.Background(), "eth-mainnet", "USDT", goodTxID, big.NewFloat(10), depositAddr)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if res.OK {
		t.Fatalf("result = %+v, want NOT OK (wrong contract)", res)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// security invariants (T52): mandatory recipient binding, confirmation floor,
// network whitelist, multi-chain routing
// ─────────────────────────────────────────────────────────────────────────────

func TestVerifyDepositDepositAddressEnforced(t *testing.T) {
	// 5 ETH = 5000000000000000000 wei = 0x4563918244f40000, sent to the
	// WRONG address — must never approve even though amount and confirmations
	// are fine.
	m := &alchemyMock{
		receipt: okReceipt(),
		head:    "0x1c",
		tx:      ethTx("0x9999999999999999999999999999999999999999", "0x4563918244f40000"),
	}
	v := newTestVerifier(t, m)

	res, err := v.VerifyDeposit(context.Background(), "eth-mainnet", "ETH", goodTxID, big.NewFloat(1), depositAddr)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if res.OK {
		t.Fatalf("result = %+v, want NOT OK (wrong destination)", res)
	}
	if !strings.Contains(res.Note, "does not match the user's platform deposit address") {
		t.Errorf("note = %q, want destination mismatch", res.Note)
	}
}

func TestVerifyDepositERC20DestinationEnforced(t *testing.T) {
	// Large USDT transfer, but to an address that is not the claimant's.
	// 10000 USDT = 10^10 = 0x2540be400.
	m := &alchemyMock{
		receipt: okReceipt(transferLog(erc20Contract(networkTokens, NetworkEthMainnet, "USDT"), "0x9999999999999999999999999999999999999999", "2540be400")),
		head:    "0x1c",
		tx:      ethTx("0x3333333333333333333333333333333333333333", "0x0"),
	}
	v := newTestVerifier(t, m)

	res, err := v.VerifyDeposit(context.Background(), "eth-mainnet", "USDT", goodTxID, big.NewFloat(1), depositAddr)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if res.OK {
		t.Fatalf("result = %+v, want NOT OK (wrong ERC-20 recipient)", res)
	}
	if !strings.Contains(res.Note, "does not match the user's platform deposit address") {
		t.Errorf("note = %q, want destination mismatch", res.Note)
	}
}

// TestVerifyDepositNoDepositAddressRefused: without the claimant's platform
// deposit address the verifier must refuse — this closes the pre-T52 hole
// where any amount-matching mainnet transfer could be claimed.
func TestVerifyDepositNoDepositAddressRefused(t *testing.T) {
	m := &alchemyMock{receipt: okReceipt(), head: "0x1c", tx: ethTx(depositAddr, "0x4563918244f40000")}
	v := newTestVerifier(t, m)

	res, err := v.VerifyDeposit(context.Background(), "eth-mainnet", "ETH", goodTxID, big.NewFloat(1), "")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if res.OK {
		t.Fatalf("result = %+v, want NOT OK (empty deposit address)", res)
	}
	if !strings.Contains(res.Note, "no platform deposit address") {
		t.Errorf("note = %q, want missing-address reason", res.Note)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.methods) != 0 {
		t.Errorf("RPC calls = %v, want none without a deposit address", m.methods)
	}
}

// TestVerifyDepositInsufficientConfirmations: below the floor → pending
// (definitive negative), and the note records the current count.
func TestVerifyDepositInsufficientConfirmations(t *testing.T) {
	// block 0x10, head 0x11 → 2 confirmations < 12.
	m := &alchemyMock{
		receipt: okReceipt(),
		head:    "0x11",
		tx:      ethTx(depositAddr, "0x4563918244f40000"),
	}
	v := newTestVerifier(t, m)

	res, err := v.VerifyDeposit(context.Background(), "eth-mainnet", "ETH", goodTxID, big.NewFloat(1), depositAddr)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if res.OK {
		t.Fatalf("result = %+v, want NOT OK (only 2 confirmations)", res)
	}
	if res.Confirmations != 2 {
		t.Errorf("confirmations = %d, want 2", res.Confirmations)
	}
	if !strings.Contains(res.Note, "insufficient confirmations") || !strings.Contains(res.Note, "2") || !strings.Contains(res.Note, "12") {
		t.Errorf("note = %q, want confirmation floor reason with counts", res.Note)
	}
}

// TestVerifyDepositUnknownNetworkRefused: networks outside the whitelist are
// a definitive negative and must not trigger any RPC traffic.
func TestVerifyDepositUnknownNetworkRefused(t *testing.T) {
	m := &alchemyMock{}
	v := newTestVerifier(t, m)

	res, err := v.VerifyDeposit(context.Background(), "bsc-mainnet", "USDT", goodTxID, big.NewFloat(1), depositAddr)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if res.OK {
		t.Fatalf("result = %+v, want NOT OK (unknown network)", res)
	}
	if !strings.Contains(res.Note, "not supported for auto-verification") {
		t.Errorf("note = %q, want unsupported-network reason", res.Note)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.methods) != 0 {
		t.Errorf("RPC calls = %v, want none for unknown networks", m.methods)
	}
}

// TestVerifyDepositMultiChainRouting: a polygon-mainnet claim is answered by
// the polygon endpoint using POLYGON's USDT contract, while the eth endpoint
// stays untouched — and the Ethereum USDT contract must NOT match on polygon.
func TestVerifyDepositMultiChainRouting(t *testing.T) {
	polyUSDT := erc20Contract(networkTokens, NetworkPolygonMainnet, "USDT")
	ethUSDT := erc20Contract(networkTokens, NetworkEthMainnet, "USDT")

	// 1000 USDT on polygon, to the deposit address.
	polyMock := &alchemyMock{
		receipt: okReceipt(transferLog(polyUSDT, depositAddr, "3b9aca00")),
		head:    "0x1c",
		tx:      ethTx(depositAddr, "0x0"),
	}
	ethMock := &alchemyMock{receipt: json.RawMessage(`null`), head: "0x1c"}
	polySrv, ethSrv := polyMock.server(), ethMock.server()
	t.Cleanup(polySrv.Close)
	t.Cleanup(ethSrv.Close)

	v := NewMultiChainDepositVerifier(map[string]string{
		NetworkEthMainnet:     ethSrv.URL,
		NetworkPolygonMainnet: polySrv.URL,
	}, 5*time.Second)

	res, err := v.VerifyDeposit(context.Background(), "polygon-mainnet", "USDT", goodTxID, big.NewFloat(900), depositAddr)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !res.OK {
		t.Fatalf("result = %+v, want OK on polygon-mainnet", res)
	}
	if res.Network != NetworkPolygonMainnet {
		t.Errorf("result network = %q, want polygon-mainnet", res.Network)
	}
	polyMock.mu.Lock()
	polyCalls := len(polyMock.methods)
	polyMock.mu.Unlock()
	ethMock.mu.Lock()
	ethCalls := len(ethMock.methods)
	ethMock.mu.Unlock()
	if polyCalls == 0 {
		t.Error("polygon endpoint never called")
	}
	if ethCalls != 0 {
		t.Errorf("eth endpoint called %d times, want 0 for a polygon claim", ethCalls)
	}

	// Same claim but the receipt only carries the ETHEREUM USDT contract:
	// that contract is meaningless on polygon → must not approve.
	polyMock.mu.Lock()
	polyMock.receipt = okReceipt(transferLog(ethUSDT, depositAddr, "3b9aca00"))
	polyMock.methods = nil
	polyMock.mu.Unlock()

	res, err = v.VerifyDeposit(context.Background(), "polygon-mainnet", "USDT", goodTxID, big.NewFloat(900), depositAddr)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if res.OK {
		t.Fatalf("result = %+v, want NOT OK (ethereum USDT contract on polygon)", res)
	}
}

// TestVerifyDepositBaseUSDTUnverifiable: Base has no canonical USDT, so the
// combination falls back to manual review instead of erroring.
func TestVerifyDepositBaseUSDTUnverifiable(t *testing.T) {
	m := &alchemyMock{receipt: okReceipt(), head: "0x1c"}
	srv := m.server()
	t.Cleanup(srv.Close)
	v := NewMultiChainDepositVerifier(map[string]string{NetworkBaseMainnet: srv.URL}, 5*time.Second)

	res, err := v.VerifyDeposit(context.Background(), "base-mainnet", "USDT", goodTxID, big.NewFloat(1), depositAddr)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if res.OK {
		t.Fatalf("result = %+v, want NOT OK (no USDT on Base)", res)
	}
	if !strings.Contains(res.Note, "no canonical contract") {
		t.Errorf("note = %q, want no-contract reason", res.Note)
	}
}

// TestVerifyDepositMinConfirmationsEnvOverride: DEPOSIT_MIN_CONFIRMATIONS
// tunes the floor (values < 1 are ignored).
func TestVerifyDepositMinConfirmationsEnvOverride(t *testing.T) {
	t.Setenv(EnvDepositMinConfirmations, "3")
	if got := MinConfirmations(); got != 3 {
		t.Errorf("MinConfirmations() = %d, want 3", got)
	}
	t.Setenv(EnvDepositMinConfirmations, "0")
	if got := MinConfirmations(); got != DefaultMinConfirmations {
		t.Errorf("MinConfirmations() = %d, want default %d for invalid override", got, DefaultMinConfirmations)
	}
}

func TestVerifyDepositUnverifiableAsset(t *testing.T) {
	m := &alchemyMock{}
	v := newTestVerifier(t, m)

	res, err := v.VerifyDeposit(context.Background(), "eth-mainnet", "BTC", goodTxID, big.NewFloat(1), depositAddr)
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

	res, err := v.VerifyDeposit(context.Background(), "eth-mainnet", "ETH", "not-a-hash", big.NewFloat(1), depositAddr)
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

	res, err := v.VerifyDeposit(context.Background(), "eth-mainnet", "USDT", goodTxID, big.NewFloat(1), depositAddr)
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

	_, err := v.VerifyDeposit(context.Background(), "eth-mainnet", "USDT", goodTxID, big.NewFloat(1), depositAddr)
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

func TestNetworkWhitelist(t *testing.T) {
	for network, want := range map[string]bool{
		NetworkEthMainnet: true, NetworkPolygonMainnet: true,
		NetworkArbitrumMainnet: true, NetworkOptimismMainnet: true,
		NetworkBaseMainnet: true,
		"bsc-mainnet":      false, "eth-sepolia": false, "": false, "ETH-MAINNET": false,
	} {
		if got := IsValidNetwork(network); got != want {
			t.Errorf("IsValidNetwork(%q) = %v, want %v", network, got, want)
		}
	}
	if got := NormalizeNetwork("  Polygon-Mainnet "); got != NetworkPolygonMainnet {
		t.Errorf("NormalizeNetwork = %q, want polygon-mainnet", got)
	}
	if got := NormalizeNetwork(""); got != DefaultNetwork {
		t.Errorf("NormalizeNetwork(\"\") = %q, want %q", got, DefaultNetwork)
	}
}

// erc20Contract is a test helper pulling the first canonical contract for
// asset on network out of the production table (fails the test when absent).
func erc20Contract(table map[string]map[string]tokenSpec, network, asset string) string {
	spec, ok := table[network][asset]
	if !ok || len(spec.contracts) == 0 {
		panic("test: no contract for " + network + "/" + asset)
	}
	return spec.contracts[0]
}
