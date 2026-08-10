package wallet

import (
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Fake EVM JSON-RPC server
// ---------------------------------------------------------------------------

type ethRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type ethRPCResponse struct {
	JSONRPC string       `json:"jsonrpc"`
	ID      int          `json:"id"`
	Result  interface{}  `json:"result,omitempty"`
	Error   *ethRPCError `json:"error,omitempty"`
}

// newETHTxTestServer fakes an EVM node. headBlock is the chain tip; receipt
// block for "0xaaa..." hashes is receiptBlock. When noPriorityFee is set the
// node rejects eth_maxPriorityFeePerGas (pre-London behaviour) to exercise
// the gasPrice fallback.
func newETHTxTestServer(t *testing.T, headBlock, receiptBlock string, noPriorityFee bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req rpcReq
		if err := json.Unmarshal(body, &req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		write := func(v ethRPCResponse) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(v)
		}
		ok := func(result interface{}) { write(ethRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: result}) }
		fail := func(code int, msg string) { write(ethRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: &ethRPCError{Code: code, Message: msg}}) }

		switch req.Method {
		case "eth_blockNumber":
			ok(headBlock)
		case "eth_getBalance":
			addr, _ := req.Params[0].(string)
			if addr == "0x000000000000000000000000000000000000bad0" {
				fail(-32602, "invalid argument")
				return
			}
			ok("0xde0b6b3a7640000") // 1e18 wei = 1 ETH
		case "eth_getTransactionCount":
			ok("0x2a")
		case "eth_getTransactionReceipt":
			hash, _ := req.Params[0].(string)
			switch hash {
			case "0xpending":
				ok(nil)
			case "0xreverted":
				ok(map[string]interface{}{"transactionHash": hash, "blockNumber": receiptBlock, "blockHash": "0xbb", "status": "0x0", "gasUsed": "0x5208"})
			case "0xunknown":
				fail(-32602, "missing trie node")
			default:
				ok(map[string]interface{}{"transactionHash": hash, "blockNumber": receiptBlock, "blockHash": "0xbb", "status": "0x1", "gasUsed": "0x5208"})
			}
		case "eth_sendRawTransaction":
			raw, _ := req.Params[0].(string)
			if raw == "0xbad" {
				fail(-32000, "nonce too low")
				return
			}
			ok("0x1111111111111111111111111111111111111111111111111111111111111111")
		case "eth_gasPrice":
			ok("0x3b9aca00") // 1e9 wei
		case "eth_maxPriorityFeePerGas":
			if noPriorityFee {
				fail(-32601, "the method eth_maxPriorityFeePerGas does not exist/is not available")
				return
			}
			ok("0x77359400") // 2e9 wei
		default:
			fail(-32601, "Method not found")
		}
	}))
}

func newETHTxTestClient(t *testing.T, headBlock, receiptBlock string, noPriorityFee bool) *ETHClient {
	t.Helper()
	srv := newETHTxTestServer(t, headBlock, receiptBlock, noPriorityFee)
	t.Cleanup(srv.Close)
	return NewETHClient("ETH", srv.URL, 1)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestETHBlockNumber(t *testing.T) {
	c := newETHTxTestClient(t, "0x6e", "0x64", false)
	n, err := c.BlockNumber()
	if err != nil || n != 110 {
		t.Fatalf("BlockNumber = %d (%v), want 110", n, err)
	}
}

func TestETHGetBalance(t *testing.T) {
	c := newETHTxTestClient(t, "0x6e", "0x64", false)
	wei, err := c.GetBalanceWei("0x742d35Cc6634C0532925a3b844Bc454e4438f44e")
	if err != nil {
		t.Fatalf("GetBalanceWei: %v", err)
	}
	if wei.String() != "1000000000000000000" {
		t.Fatalf("wei = %s, want 1e18", wei)
	}
	bal, err := c.GetBalance("0x742d35Cc6634C0532925a3b844Bc454e4438f44e")
	if err != nil {
		t.Fatalf("GetBalance: %v", err)
	}
	assertNear(t, bal, 1.0, "ETH balance")
	// Error path.
	if _, err := c.GetBalanceWei("0x000000000000000000000000000000000000bad0"); err == nil {
		t.Fatal("invalid argument must error")
	}
}

func TestETHGetTransactionCount(t *testing.T) {
	c := newETHTxTestClient(t, "0x6e", "0x64", false)
	n, err := c.GetTransactionCount("0x742d35Cc6634C0532925a3b844Bc454e4438f44e")
	if err != nil || n != 42 {
		t.Fatalf("nonce = %d (%v), want 42", n, err)
	}
}

func TestETHGetTransactionReceipt(t *testing.T) {
	c := newETHTxTestClient(t, "0x6e", "0x64", false)
	// Mined tx.
	receipt, err := c.GetTransactionReceipt("0xabcd")
	if err != nil || receipt == nil || receipt.BlockNumber != "0x64" || receipt.Status != "0x1" {
		t.Fatalf("receipt = %+v (%v)", receipt, err)
	}
	// Pending tx: null result -> (nil, nil).
	receipt, err = c.GetTransactionReceipt("0xpending")
	if err != nil || receipt != nil {
		t.Fatalf("pending receipt = %+v (%v), want nil", receipt, err)
	}
	// Error path.
	if _, err := c.GetTransactionReceipt("0xunknown"); err == nil {
		t.Fatal("unknown receipt must error")
	}
}

func TestETHSendRawTransaction(t *testing.T) {
	c := newETHTxTestClient(t, "0x6e", "0x64", false)
	hash, err := c.SendRawTransaction("0x02f8")
	if err != nil || !strings.HasPrefix(hash, "0x1111") {
		t.Fatalf("SendRawTransaction = %q (%v)", hash, err)
	}
	// Error path (e.g. nonce reuse).
	if _, err := c.SendRawTransaction("0xbad"); err == nil || !strings.Contains(err.Error(), "nonce too low") {
		t.Fatalf("SendRawTransaction(0xbad) = %v, want nonce error", err)
	}
	// Cold-flow interface.
	var b SignedTxBroadcaster = c
	if _, err := b.BroadcastSignedTx("0x02f8"); err != nil {
		t.Fatalf("BroadcastSignedTx: %v", err)
	}
}

func TestETHGasPricing(t *testing.T) {
	c := newETHTxTestClient(t, "0x6e", "0x64", false)
	gas, err := c.GasPrice()
	if err != nil || gas.Int64() != 1_000_000_000 {
		t.Fatalf("GasPrice = %v (%v), want 1e9", gas, err)
	}
	tip, err := c.MaxPriorityFeePerGas()
	if err != nil || tip.Int64() != 2_000_000_000 {
		t.Fatalf("MaxPriorityFeePerGas = %v (%v), want 2e9", tip, err)
	}
}

func TestETHMaxPriorityFeeFallbackToGasPrice(t *testing.T) {
	// Pre-London endpoint: method unsupported -> falls back to eth_gasPrice.
	c := newETHTxTestClient(t, "0x6e", "0x64", true)
	tip, err := c.MaxPriorityFeePerGas()
	if err != nil || tip.Int64() != 1_000_000_000 {
		t.Fatalf("fallback tip = %v (%v), want gasPrice 1e9", tip, err)
	}
}

func TestETHGetConfirmations(t *testing.T) {
	// head 0x6e (110), receipt block 0x64 (100) -> 11 confirmations.
	c := newETHTxTestClient(t, "0x6e", "0x64", false)
	conf, err := c.GetConfirmations("0xabcd")
	if err != nil || conf != 11 {
		t.Fatalf("confirmations = %d (%v), want 11", conf, err)
	}
	// Pending tx -> 0.
	conf, err = c.GetConfirmations("0xpending")
	if err != nil || conf != 0 {
		t.Fatalf("pending confirmations = %d (%v), want 0", conf, err)
	}
	// Error path propagates.
	if _, err := c.GetConfirmations("0xunknown"); err == nil {
		t.Fatal("unknown tx must error")
	}
}

func TestETHSignStillTODO(t *testing.T) {
	c := newETHTxTestClient(t, "0x6e", "0x64", false)
	if _, err := c.SendTransaction("0x742d35Cc6634C0532925a3b844Bc454e4438f44e", big.NewFloat(1)); err == nil {
		t.Fatal("SendTransaction must stay TODO until KMS/HSM milestone")
	}
	if _, err := c.GenerateAddress(); err == nil {
		t.Fatal("GenerateAddress must stay TODO until HD-wallet milestone")
	}
}

func TestETHClientFromEnv(t *testing.T) {
	t.Setenv(EnvETHRPCURL, "http://127.0.0.1:8545")
	c, err := NewETHClientFromEnv("ETH", 1)
	if err != nil || c == nil || c.rpcURL != "http://127.0.0.1:8545" || c.chainID != 1 {
		t.Fatalf("NewETHClientFromEnv = %+v (%v)", c, err)
	}
	t.Setenv(EnvETHRPCURL, "")
	if _, err := NewETHClientFromEnv("ETH", 1); err == nil {
		t.Fatal("missing ETH_RPC_URL must error")
	}
	t.Setenv(EnvETHRPCURL, "ftp://node")
	if _, err := NewETHClientFromEnv("ETH", 1); err == nil {
		t.Fatal("non-http scheme must be rejected")
	}
}

func TestETHAddressValidation(t *testing.T) {
	c := NewETHClient("ETH", "http://localhost", 1)
	if !c.IsValidAddress("0x742d35Cc6634C0532925a3b844Bc454e4438f44e") {
		t.Fatal("valid EIP-55 address rejected")
	}
	if c.IsValidAddress("0x123") {
		t.Fatal("short address accepted")
	}
}
