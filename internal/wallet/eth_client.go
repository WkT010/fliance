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

// ETHClient is a production EVM JSON-RPC client, used for both Ethereum and
// Polygon (same address format). Read/fee/broadcast operations are
// implemented against any standard eth JSON-RPC endpoint (node, Alchemy,
// Infura...). Transaction signing remains a TODO (recommended: KMS/HSM).
// Address validation is strict and complete (see validate.go), including
// EIP-55 checksums.
type ETHClient struct {
	asset      string // "ETH" or "POLYGON"
	rpcURL     string
	chainID    int64
	httpClient *http.Client
}

// NewETHClient constructs the client for an EVM chain. chainID must match the
// target network (1 = Ethereum mainnet, 137 = Polygon mainnet,
// 11155111 = Sepolia, 80002 = Amoy). rpcURL is operator-supplied
// configuration, never user input; redirects are disabled.
func NewETHClient(asset, rpcURL string, chainID int64) *ETHClient {
	return &ETHClient{
		asset:   asset,
		rpcURL:  rpcURL,
		chainID: chainID,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// ETH client environment configuration (plan 2.5).
const EnvETHRPCURL = "ETH_RPC_URL"

// NewETHClientFromEnv builds an ETHClient from ETH_RPC_URL for the given
// asset/chainID. Returns an error when ETH_RPC_URL is unset.
func NewETHClientFromEnv(asset string, chainID int64) (*ETHClient, error) {
	url := os.Getenv(EnvETHRPCURL)
	if url == "" {
		return nil, fmt.Errorf("%s not set", EnvETHRPCURL)
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return nil, fmt.Errorf("%s must use http(s) scheme", EnvETHRPCURL)
	}
	return NewETHClient(asset, url, chainID), nil
}

// call performs one EVM JSON-RPC request.
func (c *ETHClient) call(method string, params []interface{}) (json.RawMessage, error) {
	body, err := json.Marshal(rpcReq{JSONRPC: "2.0", Method: method, Params: params, ID: 1})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, c.rpcURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("eth rpc %s: %w", method, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("eth rpc %s: read: %w", method, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("eth rpc %s: http %d: %s", method, resp.StatusCode, string(respBody))
	}
	var r rpcResp
	if err := json.Unmarshal(respBody, &r); err != nil {
		return nil, fmt.Errorf("eth rpc %s: decode: %w", method, err)
	}
	if r.Error != nil {
		return nil, fmt.Errorf("eth rpc %s: err %d: %s", method, r.Error.Code, r.Error.Message)
	}
	return r.Result, nil
}

// parseHexBig decodes a 0x-prefixed hex quantity as returned by EVM RPCs.
func parseHexBig(s string) (*big.Int, error) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "0x")
	if s == "" {
		return big.NewInt(0), nil
	}
	v, ok := new(big.Int).SetString(s, 16)
	if !ok {
		return nil, fmt.Errorf("invalid hex quantity %q", s)
	}
	return v, nil
}

// BlockNumber returns the latest mined block number.
func (c *ETHClient) BlockNumber() (int64, error) {
	result, err := c.call("eth_blockNumber", nil)
	if err != nil {
		return 0, err
	}
	var hex string
	if err := json.Unmarshal(result, &hex); err != nil {
		return 0, fmt.Errorf("eth_blockNumber decode: %w", err)
	}
	n, err := parseHexBig(hex)
	if err != nil {
		return 0, err
	}
	return n.Int64(), nil
}

// GetBalanceWei returns the account balance in wei at the latest block.
func (c *ETHClient) GetBalanceWei(address string) (*big.Int, error) {
	result, err := c.call("eth_getBalance", []interface{}{address, "latest"})
	if err != nil {
		return nil, err
	}
	var hex string
	if err := json.Unmarshal(result, &hex); err != nil {
		return nil, fmt.Errorf("eth_getBalance decode: %w", err)
	}
	return parseHexBig(hex)
}

// GetTransactionCount returns the account nonce (pending included), used for
// tx construction and replay protection.
func (c *ETHClient) GetTransactionCount(address string) (uint64, error) {
	result, err := c.call("eth_getTransactionCount", []interface{}{address, "pending"})
	if err != nil {
		return 0, err
	}
	var hex string
	if err := json.Unmarshal(result, &hex); err != nil {
		return 0, fmt.Errorf("eth_getTransactionCount decode: %w", err)
	}
	n, err := parseHexBig(hex)
	if err != nil {
		return 0, err
	}
	return n.Uint64(), nil
}

// ETHReceipt is the subset of eth_getTransactionReceipt fields the exchange
// needs for confirmation tracking.
type ETHReceipt struct {
	TransactionHash string `json:"transactionHash"`
	BlockNumber     string `json:"blockNumber"` // hex
	BlockHash       string `json:"blockHash"`
	Status          string `json:"status"` // "0x1" success, "0x0" reverted
	GasUsed         string `json:"gasUsed"`
}

// GetTransactionReceipt returns the receipt for a mined tx, or (nil, nil)
// while the tx is still pending.
func (c *ETHClient) GetTransactionReceipt(txHash string) (*ETHReceipt, error) {
	result, err := c.call("eth_getTransactionReceipt", []interface{}{txHash})
	if err != nil {
		return nil, err
	}
	if len(result) == 0 || string(result) == "null" {
		return nil, nil // pending
	}
	var receipt ETHReceipt
	if err := json.Unmarshal(result, &receipt); err != nil {
		return nil, fmt.Errorf("receipt decode: %w", err)
	}
	return &receipt, nil
}

// SendRawTransaction broadcasts a signed tx and returns its hash.
func (c *ETHClient) SendRawTransaction(signedTxHex string) (string, error) {
	result, err := c.call("eth_sendRawTransaction", []interface{}{signedTxHex})
	if err != nil {
		return "", err
	}
	var hash string
	if err := json.Unmarshal(result, &hash); err != nil {
		return "", fmt.Errorf("eth_sendRawTransaction decode: %w", err)
	}
	return hash, nil
}

// GasPrice returns the legacy gas price in wei.
func (c *ETHClient) GasPrice() (*big.Int, error) {
	result, err := c.call("eth_gasPrice", nil)
	if err != nil {
		return nil, err
	}
	var hex string
	if err := json.Unmarshal(result, &hex); err != nil {
		return nil, fmt.Errorf("eth_gasPrice decode: %w", err)
	}
	return parseHexBig(hex)
}

// MaxPriorityFeePerGas returns the EIP-1559 priority fee in wei. Nodes that
// do not support the method (pre-London chains) fall back to eth_gasPrice.
func (c *ETHClient) MaxPriorityFeePerGas() (*big.Int, error) {
	result, err := c.call("eth_maxPriorityFeePerGas", nil)
	if err != nil {
		// Method unsupported on this endpoint: degrade to legacy pricing.
		if strings.Contains(err.Error(), "not supported") || strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "method") {
			return c.GasPrice()
		}
		return nil, err
	}
	var hex string
	if err := json.Unmarshal(result, &hex); err != nil {
		return nil, fmt.Errorf("eth_maxPriorityFeePerGas decode: %w", err)
	}
	return parseHexBig(hex)
}

// ---------------------------------------------------------------------------
// BlockchainClient implementation
// ---------------------------------------------------------------------------

// GenerateAddress derives a new deposit address.
// TODO (separate milestone): HD-wallet (BIP32/BIP44, coin type 60) key
// derivation; keep private keys out of process memory (KMS/HSM signing).
func (c *ETHClient) GenerateAddress() (string, error) {
	return "", errNotImplemented
}

// GetBalance returns the confirmed balance of an address in native units
// (ETH / POL), i.e. wei divided by 1e18.
// TODO: for token withdrawals also query the relevant ERC-20 balanceOf.
func (c *ETHClient) GetBalance(address string) (*big.Float, error) {
	wei, err := c.GetBalanceWei(address)
	if err != nil {
		return nil, err
	}
	return new(big.Float).Quo(new(big.Float).SetInt(wei), big.NewFloat(1e18)), nil
}

// SendTransaction builds and signs a transfer from the hot wallet.
// TODO (separate milestone): nonce management via GetTransactionCount,
// EIP-1559 gas pricing via GasPrice/MaxPriorityFeePerGas, signing with
// chainID (recommended KMS/HSM — never keep withdrawal keys in-process),
// then SendRawTransaction, plus replacement (speed-up/cancel) logic. Large
// withdrawals already route through the cold signing flow (cold_wallet.go);
// use BroadcastSignedTx for cold-signed payloads.
func (c *ETHClient) SendTransaction(to string, amount *big.Float) (string, error) {
	return "", errSigningNotImplemented
}

// BroadcastSignedTx broadcasts a pre-signed raw transaction (cold flow).
// Implements SignedTxBroadcaster.
func (c *ETHClient) BroadcastSignedTx(signedRawTx string) (string, error) {
	return c.SendRawTransaction(signedRawTx)
}

// GetConfirmations returns the confirmation count for a tx hash:
// latestBlock - receiptBlock + 1. Returns 0 while the tx is pending.
// TODO: re-check receipts within the confirmation window to detect reorgs.
func (c *ETHClient) GetConfirmations(txHash string) (int, error) {
	receipt, err := c.GetTransactionReceipt(txHash)
	if err != nil {
		return 0, err
	}
	if receipt == nil {
		return 0, nil // pending
	}
	txBlock, err := parseHexBig(receipt.BlockNumber)
	if err != nil {
		return 0, err
	}
	head, err := c.BlockNumber()
	if err != nil {
		return 0, err
	}
	conf := big.NewInt(head)
	conf.Sub(conf, txBlock)
	conf.Add(conf, big.NewInt(1))
	if conf.Sign() < 0 {
		return 0, nil // reorg edge case
	}
	return int(conf.Int64()), nil
}

// IsValidAddress uses the strict EVM validator (0x + 40 hex, EIP-55 checksum
// enforced for mixed-case input). Complete; only signing above is TODO.
func (c *ETHClient) IsValidAddress(address string) bool {
	return ValidateETHAddress(address)
}

var _ BlockchainClient = (*ETHClient)(nil)
var _ SignedTxBroadcaster = (*ETHClient)(nil)
