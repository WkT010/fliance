package wallet

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// On-chain deposit verification (Alchemy, Ethereum mainnet).
//
// Verifies a user-submitted deposit claim (txid + asset + amount) against the
// chain using STANDARD JSON-RPC methods only (eth_getTransactionReceipt /
// eth_getTransactionByHash / eth_blockNumber). The Enhanced API
// alchemy_getAssetTransfers is deliberately NOT used: its txHash filter was
// observed to be ignored entirely (responses returned unrelated transfers from
// the whole chain), which would have allowed unrelated transfers to approve
// claims.
//
//   - ETH            → tx.value of the transaction itself
//   - USDT / USDC    → ERC-20 Transfer event logs of the canonical contract
//     inside the transaction's receipt
//
// Every other asset (BTC/ADA/SOL/BNB, …) is NOT verifiable through this
// component and stays on the manual-review path.
//
// The verifier never panics and never credits anything itself: it only
// reports an outcome. Infrastructure failures (network, RPC errors, parse
// problems) surface as a Go error, which callers must treat as "cannot
// verify → keep the claim pending" — never as an approval.
// ─────────────────────────────────────────────────────────────────────────────

// Canonical Ethereum mainnet token contracts used for asset matching.
const (
	erc20ContractUSDT = "0xdAC17F958D2ee523a2206206994597C13D831ec7"
	erc20ContractUSDC = "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
)

// Token decimals (ETH mainnet USDT and USDC both use 6).
const (
	erc20DecimalsUSDT = 6
	erc20DecimalsUSDC = 6
)

// erc20TransferTopic is keccak256("Transfer(address,address,uint256)").
const erc20TransferTopic = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"

// depositVerifyRPCID is the fixed JSON-RPC request id; the verifier issues
// one request per call so a constant id is fine.
const depositVerifyRPCID = 1

// IsAutoVerifiableAsset reports whether a claim asset can be checked against
// Ethereum mainnet via Alchemy (ETH native + the two stablecoin ERC-20s).
func IsAutoVerifiableAsset(asset string) bool {
	switch strings.ToUpper(strings.TrimSpace(asset)) {
	case "ETH", "USDT", "USDC":
		return true
	}
	return false
}

// VerifyResult is the outcome of one on-chain verification attempt. OK=true
// means the transaction satisfies every check; Note always carries a
// human-readable explanation (match details or the failure reason).
type VerifyResult struct {
	OK            bool
	Note          string
	MatchedAmount *big.Float // transfer value found on chain (OK only)
	Confirmations int        // best-effort, -1 when unknown
	TxHash        string
}

// DepositVerifier checks deposit claims against Ethereum mainnet through an
// Alchemy JSON-RPC endpoint (standard eth_* methods only).
type DepositVerifier struct {
	rpcURL string
	client *http.Client
	// DepositAddress, when non-empty, additionally constrains the transfer
	// destination (compared case-insensitively). Empty = address not
	// checked; the outcome note records that.
	DepositAddress string
}

// NewDepositVerifier builds a verifier rooted at rpcURL (e.g.
// https://eth-mainnet.g.alchemy.com/v2/<key>). timeout bounds every HTTP
// round-trip (10s is a sensible default; ctx may tighten it further).
func NewDepositVerifier(rpcURL string, timeout time.Duration) *DepositVerifier {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &DepositVerifier{
		rpcURL: rpcURL,
		client: &http.Client{Timeout: timeout},
	}
}

type depositVerifyRPCReq struct {
	JSONRPC string        `json:"jsonrpc"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
	ID      int           `json:"id"`
}

type depositVerifyRPCResp struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// rpcCall performs one JSON-RPC round-trip. json.Number is preserved for
// numeric fields (big.Float parsing later) via a UseNumber decoder.
func (v *DepositVerifier) rpcCall(ctx context.Context, method string, params []interface{}) (json.RawMessage, error) {
	body, err := json.Marshal(depositVerifyRPCReq{
		JSONRPC: "2.0", Method: method, Params: params, ID: depositVerifyRPCID,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal rpc request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.rpcURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build rpc request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := v.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rpc %s: %w", method, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("rpc %s: read body: %w", method, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rpc %s: http %d", method, resp.StatusCode)
	}
	var r depositVerifyRPCResp
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&r); err != nil {
		return nil, fmt.Errorf("rpc %s: decode: %w", method, err)
	}
	if r.Error != nil {
		return nil, fmt.Errorf("rpc %s: alchemy error %d: %s", method, r.Error.Code, r.Error.Message)
	}
	return r.Result, nil
}

// eth_getTransactionReceipt result — only the fields the verifier needs.
type verifyReceipt struct {
	Status      string      `json:"status"`
	BlockNumber string      `json:"blockNumber"`
	Logs        []verifyLog `json:"logs"`
}

// verifyLog is one receipt log; only the ERC-20 Transfer shape matters.
type verifyLog struct {
	Address string   `json:"address"`
	Topics  []string `json:"topics"`
	Data    string   `json:"data"`
}

// eth_getTransactionByHash result — only the fields the verifier needs.
type verifyTx struct {
	Value string `json:"value"` // hex wei of the native transfer
	To    string `json:"to"`    // native transfer recipient
}

// VerifyDeposit checks the claimed deposit against the chain. It returns:
//   - (result, nil) with result.OK=true  → safe to auto-approve
//   - (result, nil) with result.OK=false → definitive negative (tx missing,
//     failed receipt, no matching transfer) — keep pending for manual review
//   - (nil, err)                         → infrastructure/parse failure, i.e.
//     "cannot verify" — keep pending, never approve
func (v *DepositVerifier) VerifyDeposit(ctx context.Context, asset, txid string, amount *big.Float) (*VerifyResult, error) {
	asset = strings.ToUpper(strings.TrimSpace(asset))
	if !IsAutoVerifiableAsset(asset) {
		return &VerifyResult{OK: false, Note: "asset " + asset + " is not auto-verifiable on Ethereum mainnet"}, nil
	}
	if amount == nil || amount.Sign() <= 0 {
		return &VerifyResult{OK: false, Note: "invalid claimed amount"}, nil
	}
	txid = strings.TrimSpace(txid)
	if !strings.HasPrefix(strings.ToLower(txid), "0x") || len(txid) != 66 {
		return &VerifyResult{OK: false, Note: "txid is not a 32-byte Ethereum hash"}, nil
	}

	// 1. The transaction must be mined and its receipt successful.
	rawReceipt, err := v.rpcCall(ctx, "eth_getTransactionReceipt", []interface{}{txid})
	if err != nil {
		return nil, fmt.Errorf("get receipt: %w", err)
	}
	var receipt verifyReceipt
	if len(rawReceipt) == 0 || string(rawReceipt) == "null" {
		return &VerifyResult{
			OK: false, Note: "transaction not found or not yet mined on Ethereum mainnet",
			TxHash: txid, Confirmations: 0,
		}, nil
	}
	if err := json.Unmarshal(rawReceipt, &receipt); err != nil {
		return nil, fmt.Errorf("parse receipt: %w", err)
	}
	if receipt.Status != "0x1" {
		return &VerifyResult{
			OK: false, Note: "transaction reverted on chain (receipt status " + receipt.Status + ")",
			TxHash: txid,
		}, nil
	}

	// 2. Best-effort confirmation count for the audit note.
	confirmations := -1
	if rawHead, err := v.rpcCall(ctx, "eth_blockNumber", nil); err == nil {
		var head string
		if json.Unmarshal(rawHead, &head) == nil {
			headN, okHead := new(big.Int).SetString(strings.TrimPrefix(head, "0x"), 16)
			txN, okTx := new(big.Int).SetString(strings.TrimPrefix(receipt.BlockNumber, "0x"), 16)
			if okHead && okTx {
				confirmations = int(new(big.Int).Add(new(big.Int).Sub(headN, txN), big.NewInt(1)).Int64())
			}
		}
	}

	// 3. Match the claimed asset inside the transaction itself.
	switch asset {
	case "ETH":
		return v.verifyNativeETH(ctx, txid, amount, confirmations)
	default: // USDT / USDC
		return v.verifyERC20(asset, receipt.Logs, amount, confirmations, txid)
	}
}

// verifyNativeETH checks the transaction's own native value (tx.value) —
// exactly what the sender transferred.
func (v *DepositVerifier) verifyNativeETH(ctx context.Context, txid string, amount *big.Float, confirmations int) (*VerifyResult, error) {
	rawTx, err := v.rpcCall(ctx, "eth_getTransactionByHash", []interface{}{txid})
	if err != nil {
		return nil, fmt.Errorf("get transaction: %w", err)
	}
	var tx verifyTx
	if len(rawTx) == 0 || string(rawTx) == "null" {
		return &VerifyResult{
			OK: false, Note: "transaction not found on Ethereum mainnet",
			TxHash: txid, Confirmations: confirmations,
		}, nil
	}
	if err := json.Unmarshal(rawTx, &tx); err != nil {
		return nil, fmt.Errorf("parse transaction: %w", err)
	}
	wei, ok := new(big.Int).SetString(strings.TrimPrefix(strings.ToLower(tx.Value), "0x"), 16)
	if !ok {
		return nil, fmt.Errorf("parse tx value %q", tx.Value)
	}
	ethValue := weiToEther(wei)

	if ethValue.Cmp(amount) < 0 {
		return &VerifyResult{
			OK: false,
			Note: fmt.Sprintf("on-chain ETH transfer value %s is below the claimed amount (claimed >= %s required)",
				ethValue.Text('f', -1), amount.Text('f', -1)),
			TxHash: txid, Confirmations: confirmations,
		}, nil
	}
	if !v.destinationOK(tx.To) {
		return &VerifyResult{
			OK: false,
			Note: fmt.Sprintf("transfer destination %s does not match the platform deposit address", tx.To),
			TxHash: txid, Confirmations: confirmations,
		}, nil
	}
	return v.approvedResult("ETH", ethValue, txid, confirmations, tx.To), nil
}

// verifyERC20 scans the receipt logs for a Transfer event of the asset's
// canonical contract whose value covers the claim (and whose recipient
// matches the deposit address when one is configured).
func (v *DepositVerifier) verifyERC20(asset string, logs []verifyLog, amount *big.Float, confirmations int, txid string) (*VerifyResult, error) {
	contract, decimals := tokenContract(asset)
	if contract == "" {
		return &VerifyResult{OK: false, Note: "asset " + asset + " is not auto-verifiable on Ethereum mainnet"}, nil
	}

	var closestV *big.Float // largest same-token value seen, for the failure note
	var closestTo string
	for _, lg := range logs {
		if !strings.EqualFold(lg.Address, contract) || len(lg.Topics) != 3 || lg.Topics[0] != erc20TransferTopic {
			continue
		}
		rawVal := strings.TrimPrefix(strings.ToLower(lg.Data), "0x")
		valInt, ok := new(big.Int).SetString(rawVal, 16)
		if !ok {
			continue
		}
		val := new(big.Float).Quo(new(big.Float).SetInt(valInt),
			new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)))
		to := topicToAddress(lg.Topics[2])
		if val.Cmp(amount) >= 0 && v.destinationOK(to) {
			return v.approvedResult(asset, val, txid, confirmations, to), nil
		}
		if closestV == nil || val.Cmp(closestV) > 0 {
			closestV, closestTo = val, to
		}
	}

	note := fmt.Sprintf("no on-chain %s transfer >= claimed amount found in tx %s", asset, txid)
	if closestV != nil {
		note = fmt.Sprintf("on-chain %s transfer value %s is below the claimed amount (claimed >= %s required)",
			asset, closestV.Text('f', -1), amount.Text('f', -1))
		if closestTo != "" && v.DepositAddress != "" && !strings.EqualFold(closestTo, v.DepositAddress) {
			note += fmt.Sprintf("; transfer destination %s does not match the platform deposit address", closestTo)
		}
	} else if v.DepositAddress != "" {
		note += fmt.Sprintf(" (expected destination %s)", v.DepositAddress)
	}
	return &VerifyResult{OK: false, Note: note, TxHash: txid, Confirmations: confirmations}, nil
}

// approvedResult builds the success outcome, flagging when the recipient
// address could not be checked.
func (v *DepositVerifier) approvedResult(asset string, matched *big.Float, txid string, confirmations int, to string) *VerifyResult {
	note := fmt.Sprintf("auto-verified via Alchemy: %s transfer of %s confirmed in tx %s (%d confirmations)",
		asset, matched.Text('f', -1), txid, confirmations)
	if v.DepositAddress == "" {
		note += "; recipient address NOT checked (no platform deposit address configured)"
	} else {
		note += fmt.Sprintf("; recipient %s matches platform deposit address", to)
	}
	return &VerifyResult{
		OK: true, Note: note, MatchedAmount: matched,
		Confirmations: confirmations, TxHash: txid,
	}
}

// tokenContract maps an auto-verifiable token to its canonical mainnet
// contract + decimals ("" when the asset is not a supported token).
func tokenContract(asset string) (string, int) {
	switch asset {
	case "USDT":
		return erc20ContractUSDT, erc20DecimalsUSDT
	case "USDC":
		return erc20ContractUSDC, erc20DecimalsUSDC
	}
	return "", 0
}

// weiToEther converts wei into ETH as a big.Float.
func weiToEther(wei *big.Int) *big.Float {
	return new(big.Float).Quo(new(big.Float).SetInt(wei), new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)))
}

// topicToAddress extracts the 20-byte address from a 32-byte log topic.
func topicToAddress(topic string) string {
	t := strings.TrimPrefix(strings.ToLower(topic), "0x")
	if len(t) < 40 {
		return ""
	}
	return "0x" + t[len(t)-40:]
}

// destinationOK checks the transfer recipient against the configured deposit
// address (case-insensitively). Without a configured address every
// destination passes and the outcome note records the skipped check.
func (v *DepositVerifier) destinationOK(to string) bool {
	if v.DepositAddress == "" {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(to), strings.TrimSpace(v.DepositAddress))
}
