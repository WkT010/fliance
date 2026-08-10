package wallet

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strings"
	"time"
)

// errSigningNotImplemented marks transaction construction/signing, a separate
// milestone (btcd/btcutil for BTC, KMS/HSM for EVM). errNotImplemented keeps
// the legacy skeleton's sentinel name for the same purpose.
var (
	errSigningNotImplemented = fmt.Errorf("wallet: tx construction/signing not implemented (requires btcd/btcutil or HSM integration)")
	errNotImplemented        = errSigningNotImplemented
)

// BTCClient is a production Bitcoin RPC client speaking bitcoind-style
// JSON-RPC with HTTP basic auth. Read/broadcast operations are implemented;
// transaction construction & signing remain TODOs (see the markers below).
// Address validation is strict and complete (see validate.go).
type BTCClient struct {
	rpcURL     string
	rpcUser    string
	rpcPass    string
	testnet    bool
	httpClient *http.Client
}

// NewBTCClient constructs the client. rpcURL points at a trusted full node or
// hosted RPC endpoint (operator-supplied configuration, never user input);
// testnet selects the testnet address formats. Redirects are disabled so the
// endpoint cannot be silently swapped to another host.
func NewBTCClient(rpcURL, rpcUser, rpcPass string, testnet bool) *BTCClient {
	return &BTCClient{
		rpcURL:  rpcURL,
		rpcUser: rpcUser,
		rpcPass: rpcPass,
		testnet: testnet,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// BTC client environment configuration (plan 2.5).
const (
	EnvBTCRPCURL  = "BTC_RPC_URL"
	EnvBTCRPCUser = "BTC_RPC_USER"
	EnvBTCRPCPass = "BTC_RPC_PASS"
	EnvBTCTestnet = "BTC_TESTNET"
)

// NewBTCClientFromEnv builds a BTCClient from BTC_RPC_URL / BTC_RPC_USER /
// BTC_RPC_PASS (and optional BTC_TESTNET=1). Returns an error when
// BTC_RPC_URL is unset.
func NewBTCClientFromEnv() (*BTCClient, error) {
	url := os.Getenv(EnvBTCRPCURL)
	if url == "" {
		return nil, fmt.Errorf("%s not set", EnvBTCRPCURL)
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return nil, fmt.Errorf("%s must use http(s) scheme", EnvBTCRPCURL)
	}
	testnet := os.Getenv(EnvBTCTestnet) == "1" || os.Getenv(EnvBTCTestnet) == "true"
	return NewBTCClient(url, os.Getenv(EnvBTCRPCUser), os.Getenv(EnvBTCRPCPass), testnet), nil
}

// call performs one bitcoind JSON-RPC request with basic auth.
func (c *BTCClient) call(method string, params []interface{}) (json.RawMessage, error) {
	body, err := json.Marshal(rpcReq{JSONRPC: "2.0", Method: method, Params: params, ID: 1})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, c.rpcURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.rpcUser != "" {
		req.SetBasicAuth(c.rpcUser, c.rpcPass)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("btc rpc %s: %w", method, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("btc rpc %s: read: %w", method, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// bitcoind encodes RPC errors both in the JSON body and via status.
		var r rpcResp
		if json.Unmarshal(respBody, &r) == nil && r.Error != nil {
			return nil, fmt.Errorf("btc rpc %s: err %d: %s", method, r.Error.Code, r.Error.Message)
		}
		return nil, fmt.Errorf("btc rpc %s: http %d: %s", method, resp.StatusCode, string(respBody))
	}
	var r rpcResp
	if err := json.Unmarshal(respBody, &r); err != nil {
		return nil, fmt.Errorf("btc rpc %s: decode: %w", method, err)
	}
	if r.Error != nil {
		return nil, fmt.Errorf("btc rpc %s: err %d: %s", method, r.Error.Code, r.Error.Message)
	}
	return r.Result, nil
}

// GetBlockCount returns the height of the most-work fully extended chain.
func (c *BTCClient) GetBlockCount() (int64, error) {
	result, err := c.call("getblockcount", nil)
	if err != nil {
		return 0, err
	}
	var height int64
	if err := json.Unmarshal(result, &height); err != nil {
		return 0, fmt.Errorf("getblockcount decode: %w", err)
	}
	return height, nil
}

// GetBestBlockHash returns the hash of the current chain tip.
func (c *BTCClient) GetBestBlockHash() (string, error) {
	result, err := c.call("getbestblockhash", nil)
	if err != nil {
		return "", err
	}
	var hash string
	if err := json.Unmarshal(result, &hash); err != nil {
		return "", fmt.Errorf("getbestblockhash decode: %w", err)
	}
	return hash, nil
}

// GetWalletBalance returns the node wallet balance via getbalance.
//
// WATCH-ONLY SEMANTICS: the exchange should import deposit addresses into the
// node wallet with `importaddress <addr> <label> false` (rescan off), making
// the node wallet watch-only (it never holds private keys). getbalance then
// reports the total watch-only balance across ALL imported addresses — it is
// NOT a per-user figure; per-user attribution must come from the deposit
// crediting pipeline. minConf sets the minimum confirmation depth (0 includes
// untrusted).
func (c *BTCClient) GetWalletBalance(minConf int) (*big.Float, error) {
	result, err := c.call("getbalance", []interface{}{"*", minConf})
	if err != nil {
		return nil, err
	}
	var f float64
	if err := json.Unmarshal(result, &f); err != nil {
		return nil, fmt.Errorf("getbalance decode: %w", err)
	}
	if f < 0 {
		return nil, fmt.Errorf("getbalance: negative balance %v", f)
	}
	return big.NewFloat(f), nil
}

// BroadcastTx submits a fully-signed raw transaction to the network and
// returns the txid.
func (c *BTCClient) BroadcastTx(rawTxHex string) (string, error) {
	result, err := c.call("sendrawtransaction", []interface{}{rawTxHex})
	if err != nil {
		return "", err
	}
	var txid string
	if err := json.Unmarshal(result, &txid); err != nil {
		return "", fmt.Errorf("sendrawtransaction decode: %w", err)
	}
	return txid, nil
}

// BTCRawTxInfo is the decoded shape of getrawtransaction (verbose mode).
type BTCRawTxInfo struct {
	TxID          string `json:"txid"`
	Hex           string `json:"hex"`
	Confirmations int64  `json:"confirmations"`
	BlockHash     string `json:"blockhash"`
	BlockHeight   int64  `json:"blockheight"`
}

// GetRawTransaction fetches a transaction in verbose mode. Unconfirmed
// transactions return Confirmations == 0 and an empty BlockHash.
func (c *BTCClient) GetRawTransaction(txid string) (*BTCRawTxInfo, error) {
	result, err := c.call("getrawtransaction", []interface{}{txid, true})
	if err != nil {
		return nil, err
	}
	var info BTCRawTxInfo
	if err := json.Unmarshal(result, &info); err != nil {
		return nil, fmt.Errorf("getrawtransaction decode: %w", err)
	}
	return &info, nil
}

// ---------------------------------------------------------------------------
// BlockchainClient implementation
// ---------------------------------------------------------------------------

// GenerateAddress derives a new deposit address.
// TODO (separate milestone): integrate HD-wallet (BIP32/BIP44) derivation or
// the node's getnewaddress RPC; never reuse addresses across users.
func (c *BTCClient) GenerateAddress() (string, error) {
	return "", errNotImplemented
}

// GetBalance returns the node wallet balance (see GetWalletBalance for the
// watch-only semantics: this is the aggregate watch-only balance, not a
// per-address figure). The address argument is accepted for interface
// conformance; per-address queries need listunspent/scantxoutset.
// TODO (separate milestone): per-address balance via scantxoutset.
func (c *BTCClient) GetBalance(address string) (*big.Float, error) {
	return c.GetWalletBalance(1)
}

// SendTransaction builds, signs and broadcasts a hot-wallet payment.
// TODO (separate milestone): transaction construction requires btcd/btcutil
// (UTXO selection, fee estimation, change handling) and signing via an HSM or
// an air-gapped signer; large withdrawals already route through the cold
// signing flow (cold_wallet.go) instead. For cold-signed payloads use
// BroadcastSignedTx.
func (c *BTCClient) SendTransaction(to string, amount *big.Float) (string, error) {
	return "", errSigningNotImplemented
}

// BroadcastSignedTx broadcasts a pre-signed raw transaction (cold flow).
// Implements SignedTxBroadcaster.
func (c *BTCClient) BroadcastSignedTx(signedRawTx string) (string, error) {
	return c.BroadcastTx(signedRawTx)
}

// GetConfirmations returns the confirmation count for a txid, computed from
// getrawtransaction (verbose): confirmations reported by the node, falling
// back to (chainHeight - blockHeight + 1) when only block metadata is given.
func (c *BTCClient) GetConfirmations(txHash string) (int, error) {
	info, err := c.GetRawTransaction(txHash)
	if err != nil {
		return 0, err
	}
	if info.Confirmations > 0 {
		if info.Confirmations > int64(^uint(0)>>1) {
			return 0, fmt.Errorf("confirmations overflow: %d", info.Confirmations)
		}
		return int(info.Confirmations), nil
	}
	if info.BlockHash == "" {
		return 0, nil // still in mempool
	}
	head, err := c.GetBlockCount()
	if err != nil {
		return 0, err
	}
	if info.BlockHeight <= 0 {
		return 1, nil // mined but node omitted blockheight
	}
	conf := head - info.BlockHeight + 1
	if conf < 0 {
		conf = 0 // reorg edge case
	}
	return int(conf), nil
}

// IsValidAddress uses the strict chain-specific validator (base58check +
// bech32). This part is complete; signing/construction above is TODO.
func (c *BTCClient) IsValidAddress(address string) bool {
	return ValidateBTCAddress(address)
}

var _ BlockchainClient = (*BTCClient)(nil)
var _ SignedTxBroadcaster = (*BTCClient)(nil)
