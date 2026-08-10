package wallet

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Fake bitcoind JSON-RPC server
// ---------------------------------------------------------------------------

type btcRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type btcRPCResponse struct {
	JSONRPC string       `json:"jsonrpc"`
	ID      int          `json:"id"`
	Result  interface{}  `json:"result,omitempty"`
	Error   *btcRPCError `json:"error,omitempty"`
}

// newBTCTestServer fakes a bitcoind node. It enforces basic auth and routes
// by method; special txids trigger error paths.
func newBTCTestServer(t *testing.T, wantUser, wantPass string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := strings.TrimPrefix(r.Header.Get("Authorization"), "Basic ")
		if dec, err := base64.StdEncoding.DecodeString(auth); err != nil || string(dec) != wantUser+":"+wantPass {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req rpcReq
		if err := json.Unmarshal(body, &req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		write := func(v btcRPCResponse) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(v)
		}
		ok := func(result interface{}) { write(btcRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: result}) }
		fail := func(code int, msg string) { write(btcRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: &btcRPCError{Code: code, Message: msg}}) }

		switch req.Method {
		case "getblockcount":
			ok(800000)
		case "getbestblockhash":
			ok("000000000000000000024bead17f2ada9a1a7e1a3c1b7d1e0a2b3c4d5e6f7a8b")
		case "getbalance":
			ok(12.5)
		case "sendrawtransaction":
			raw, _ := req.Params[0].(string)
			if raw == "badhex" {
				fail(-26, "TX decode failed")
				return
			}
			ok("9b8f6a5e4d3c2b1a0987654321fedcba9b8f6a5e4d3c2b1a0987654321fedcba")
		case "getrawtransaction":
			txid, _ := req.Params[0].(string)
			switch txid {
			case "unknown-txid":
				fail(-5, "No such mempool or blockchain transaction")
			case "mempool-txid":
				ok(map[string]interface{}{"txid": "mempool-txid", "hex": "0200"})
			case "old-block-txid":
				// confirmations absent but block metadata present: forces the
				// (chainHeight - blockHeight + 1) fallback.
				ok(map[string]interface{}{"txid": "old-block-txid", "blockhash": "00aa", "blockheight": 799995, "confirmations": 0})
			default:
				ok(map[string]interface{}{"txid": txid, "hex": "0200", "confirmations": 6, "blockhash": "00aa", "blockheight": 799995})
			}
		default:
			fail(-32601, "Method not found")
		}
	}))
}

func newBTCTestClient(t *testing.T) (*BTCClient, *httptest.Server) {
	t.Helper()
	srv := newBTCTestServer(t, "rpcuser", "rpcpass")
	t.Cleanup(srv.Close)
	return NewBTCClient(srv.URL, "rpcuser", "rpcpass", false), srv
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestBTCGetBlockCount(t *testing.T) {
	c, _ := newBTCTestClient(t)
	n, err := c.GetBlockCount()
	if err != nil || n != 800000 {
		t.Fatalf("GetBlockCount = %d (%v), want 800000", n, err)
	}
}

func TestBTCGetBestBlockHash(t *testing.T) {
	c, _ := newBTCTestClient(t)
	h, err := c.GetBestBlockHash()
	if err != nil || !strings.HasPrefix(h, "000000000000000000024bead") {
		t.Fatalf("GetBestBlockHash = %q (%v)", h, err)
	}
}

func TestBTCGetWalletBalance(t *testing.T) {
	c, _ := newBTCTestClient(t)
	bal, err := c.GetWalletBalance(1)
	if err != nil {
		t.Fatalf("GetWalletBalance: %v", err)
	}
	assertNear(t, bal, 12.5, "watch-only wallet balance")
}

func TestBTCBroadcastTx(t *testing.T) {
	c, _ := newBTCTestClient(t)
	txid, err := c.BroadcastTx("0200000001abcd")
	if err != nil || txid == "" {
		t.Fatalf("BroadcastTx = %q (%v)", txid, err)
	}
	// Error path: node rejects the raw tx.
	if _, err := c.BroadcastTx("badhex"); err == nil || !strings.Contains(err.Error(), "TX decode failed") {
		t.Fatalf("BroadcastTx(badhex) = %v, want RPC error", err)
	}
}

func TestBTCBroadcastSignedTxImplementsColdFlow(t *testing.T) {
	c, _ := newBTCTestClient(t)
	var b SignedTxBroadcaster = c
	txid, err := b.BroadcastSignedTx("0200000001abcd")
	if err != nil || txid == "" {
		t.Fatalf("BroadcastSignedTx = %q (%v)", txid, err)
	}
}

func TestBTCGetRawTransaction(t *testing.T) {
	c, _ := newBTCTestClient(t)
	info, err := c.GetRawTransaction("some-txid")
	if err != nil || info.Confirmations != 6 || info.BlockHash != "00aa" {
		t.Fatalf("GetRawTransaction = %+v (%v)", info, err)
	}
	// Error path: unknown txid.
	if _, err := c.GetRawTransaction("unknown-txid"); err == nil || !strings.Contains(err.Error(), "-5") {
		t.Fatalf("GetRawTransaction(unknown) = %v, want -5 error", err)
	}
}

func TestBTCGetConfirmations(t *testing.T) {
	c, _ := newBTCTestClient(t)
	// Confirmed tx: node-reported confirmations.
	conf, err := c.GetConfirmations("some-txid")
	if err != nil || conf != 6 {
		t.Fatalf("confirmed = %d (%v), want 6", conf, err)
	}
	// Mempool tx: zero confirmations.
	conf, err = c.GetConfirmations("mempool-txid")
	if err != nil || conf != 0 {
		t.Fatalf("mempool = %d (%v), want 0", conf, err)
	}
	// Fallback: head 800000 - blockheight 799995 + 1 = 6.
	conf, err = c.GetConfirmations("old-block-txid")
	if err != nil || conf != 6 {
		t.Fatalf("fallback = %d (%v), want 6", conf, err)
	}
	// Error path propagates.
	if _, err := c.GetConfirmations("unknown-txid"); err == nil {
		t.Fatal("unknown txid must error")
	}
}

func TestBTCGetBalanceInterface(t *testing.T) {
	c, _ := newBTCTestClient(t)
	var bc BlockchainClient = c
	bal, err := bc.GetBalance(validTestBTCAddr)
	if err != nil {
		t.Fatalf("GetBalance: %v", err)
	}
	assertNear(t, bal, 12.5, "interface GetBalance")
}

func TestBTCSigningStillTODO(t *testing.T) {
	c, _ := newBTCTestClient(t)
	if _, err := c.SendTransaction(validTestBTCAddr, big.NewFloat(1)); err == nil {
		t.Fatal("SendTransaction must stay TODO until btcd/HSM milestone")
	}
	if _, err := c.GenerateAddress(); err == nil {
		t.Fatal("GenerateAddress must stay TODO until HD-wallet milestone")
	}
}

func TestBTCRejectsBadAuth(t *testing.T) {
	srv := newBTCTestServer(t, "rpcuser", "rpcpass")
	defer srv.Close()
	c := NewBTCClient(srv.URL, "wrong", "creds", false)
	if _, err := c.GetBlockCount(); err == nil {
		t.Fatal("bad credentials must fail")
	}
}

func TestBTCClientFromEnv(t *testing.T) {
	t.Setenv(EnvBTCRPCURL, "http://127.0.0.1:18332")
	t.Setenv(EnvBTCRPCUser, "u")
	t.Setenv(EnvBTCRPCPass, "p")
	c, err := NewBTCClientFromEnv()
	if err != nil || c == nil || c.rpcURL != "http://127.0.0.1:18332" {
		t.Fatalf("NewBTCClientFromEnv = %+v (%v)", c, err)
	}
	// Missing URL is an error.
	t.Setenv(EnvBTCRPCURL, "")
	if _, err := NewBTCClientFromEnv(); err == nil {
		t.Fatal("missing BTC_RPC_URL must error")
	}
	// Non-http scheme is rejected.
	t.Setenv(EnvBTCRPCURL, "file:///etc/passwd")
	if _, err := NewBTCClientFromEnv(); err == nil {
		t.Fatal("non-http scheme must be rejected")
	}
}

func TestBTCAddressValidation(t *testing.T) {
	c := NewBTCClient("http://localhost", "", "", false)
	if !c.IsValidAddress(validTestBTCAddr) {
		t.Fatal("valid address rejected")
	}
	if c.IsValidAddress("1BvBMSEYstWetqTFn5Au4m4GFg7xJaNVN3") {
		t.Fatal("bad checksum accepted")
	}
}
