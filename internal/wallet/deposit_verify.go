package wallet

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// On-chain deposit verification (Alchemy, multi-chain).
//
// Verifies a user-submitted deposit claim (network + txid + asset + amount)
// against the chain using STANDARD JSON-RPC methods only
// (eth_getTransactionReceipt / eth_getTransactionByHash / eth_blockNumber).
// The Enhanced API alchemy_getAssetTransfers is deliberately NOT used: its
// txHash filter was observed to be ignored entirely (responses returned
// unrelated transfers from the whole chain), which would have allowed
// unrelated transfers to approve claims.
//
//   - ETH            → tx.value of the transaction itself
//   - USDT / USDC    → ERC-20 Transfer event logs of the canonical contract
//     inside the transaction's receipt
//
// Supported networks (whitelist): eth-mainnet, polygon-mainnet,
// arbitrum-mainnet, optimism-mainnet, base-mainnet. Every other network, and
// every other asset (BTC/ADA/SOL/BNB, …), is NOT verifiable through this
// component and stays on the manual-review path.
//
// Security invariants (funds safety — enforced unconditionally):
//
//  1. Recipient check: the on-chain recipient (tx.to for native ETH, the
//     Transfer log topics[2] for ERC-20) MUST equal the claimant's own
//     platform deposit address (compared case-insensitively). The address is
//     supplied by the handler from the database — never from the client. An
//     empty deposit address ALWAYS fails verification.
//  2. Confirmation floor: the transaction needs at least MinConfirmations
//     confirmations (default 12, override via DEPOSIT_MIN_CONFIRMATIONS).
//  3. Amount: the on-chain value must cover the claimed amount.
//  4. Asset/network: only whitelisted asset+network combinations with a
//     known canonical contract can pass.
//
// The verifier never panics and never credits anything itself: it only
// reports an outcome. Infrastructure failures (network, RPC errors, parse
// problems) surface as a Go error, which callers must treat as "cannot
// verify → keep the claim pending" — never as an approval.
// ─────────────────────────────────────────────────────────────────────────────

// Supported network slugs (Alchemy JSON-RPC network naming). These values are
// part of the public API contract of POST /wallet/deposit/claim.
const (
	NetworkEthMainnet      = "eth-mainnet"
	NetworkPolygonMainnet  = "polygon-mainnet"
	NetworkArbitrumMainnet = "arbitrum-mainnet"
	NetworkOptimismMainnet = "optimism-mainnet"
	NetworkBaseMainnet     = "base-mainnet"

	// DefaultNetwork is assumed when a claim omits the network field.
	DefaultNetwork = NetworkEthMainnet
)

// supportedNetworks is the whitelist of networks the verifier will route to.
var supportedNetworks = map[string]bool{
	NetworkEthMainnet:      true,
	NetworkPolygonMainnet:  true,
	NetworkArbitrumMainnet: true,
	NetworkOptimismMainnet: true,
	NetworkBaseMainnet:     true,
}

// IsValidNetwork reports whether network is on the auto-verification
// whitelist.
func IsValidNetwork(network string) bool { return supportedNetworks[network] }

// SupportedNetworks returns the whitelist in a stable order (for logs/UI).
func SupportedNetworks() []string {
	return []string{
		NetworkEthMainnet, NetworkPolygonMainnet, NetworkArbitrumMainnet,
		NetworkOptimismMainnet, NetworkBaseMainnet,
	}
}

// NormalizeNetwork trims + lowercases a client-supplied network value and
// maps the empty string to DefaultNetwork. It does NOT validate — unknown
// values must still be rejected by IsValidNetwork (kept separately so the
// handler can persist what the client actually sent).
func NormalizeNetwork(network string) string {
	n := strings.ToLower(strings.TrimSpace(network))
	if n == "" {
		return DefaultNetwork
	}
	return n
}

// MinConfirmations is the confirmation floor a deposit must reach before it
// may be auto-approved. Default 12; override with the
// DEPOSIT_MIN_CONFIRMATIONS environment variable (values < 1 are ignored:
// disabling the floor would re-open the reorg/spend race it exists to close).
const (
	DefaultMinConfirmations    = 12
	EnvDepositMinConfirmations = "DEPOSIT_MIN_CONFIRMATIONS"
)

// MinConfirmations resolves the effective confirmation floor from the
// environment, falling back to DefaultMinConfirmations.
func MinConfirmations() int {
	if v := os.Getenv(EnvDepositMinConfirmations); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n >= 1 {
			return n
		}
	}
	return DefaultMinConfirmations
}

// ─────────────────────────────────────────────────────────────────────────────
// Canonical token contracts per network.
//
// Sources: Ethereum addresses match the Alchemy alchemy-api skill reference
// ("Common token addresses") and the public Etherscan listings; the L2
// addresses are the publicly documented canonical deployments from Circle
// (native USDC), Tether (USDT) and the respective chain explorers
// (Polygonscan / Arbiscan / Optimistic Etherscan / Basescan). Where a chain
// carries BOTH a native and a legacy bridged ("*.e") USDC, both are accepted:
// they are the same asset and rejecting one variant would push genuine
// deposits to manual review without adding any safety (the recipient-address
// check is what binds a transfer to the claimant).
//
// NOTE: Base mainnet has NO canonical USDT deployment — USDT claims on
// base-mainnet therefore always fall back to manual review.
// ─────────────────────────────────────────────────────────────────────────────

// tokenSpec describes one asset's canonical contract(s) on one network.
type tokenSpec struct {
	contracts []string // accepted contract addresses (any variant)
	decimals  int
}

// networkTokens maps network → asset → tokenSpec.
var networkTokens = map[string]map[string]tokenSpec{
	NetworkEthMainnet: {
		"USDT": {contracts: []string{"0xdAC17F958D2ee523a2206206994597C13D831ec7"}, decimals: 6},
		"USDC": {contracts: []string{"0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"}, decimals: 6},
	},
	NetworkPolygonMainnet: {
		// Tether USD on Polygon (Polygonscan) + native Circle USDC and the
		// legacy bridged USDC.e.
		"USDT": {contracts: []string{"0xc2132D05D31c914a87C6611C10748AEb04B58e8F"}, decimals: 6},
		"USDC": {contracts: []string{
			"0x3c499c542cEF5E3811e1192ce70d8cC03d5c3359", // native (Circle)
			"0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174", // bridged USDC.e
		}, decimals: 6},
	},
	NetworkArbitrumMainnet: {
		// Tether USD on Arbitrum One (Arbiscan) + native Circle USDC and the
		// legacy bridged USDC.e.
		"USDT": {contracts: []string{"0xFd086bC7CD5C481DCC9C85ebE478A1C0b69FCbb9"}, decimals: 6},
		"USDC": {contracts: []string{
			"0xaf88d065e77c8cC2239327C5EDb3A432268e5831", // native (Circle)
			"0xFF970A61A04b1cA14834A43f5dE4533eBDDB5CC8", // bridged USDC.e
		}, decimals: 6},
	},
	NetworkOptimismMainnet: {
		// Tether USD on OP Mainnet (Optimistic Etherscan) + native Circle
		// USDC and the legacy bridged USDC.e.
		"USDT": {contracts: []string{"0x94b008aA00579c1307B0EF2c499aD98a8ce58e58"}, decimals: 6},
		"USDC": {contracts: []string{
			"0x0b2C639c533813f4Aa9D7837CAf62653d097Ff85", // native (Circle)
			"0x7F5c764cBc14f9669B88837ca1490cCa17c31607", // bridged USDC.e
		}, decimals: 6},
	},
	NetworkBaseMainnet: {
		// Native Circle USDC on Base (Alchemy alchemy-api skill reference).
		// No USDT: Base has no canonical Tether deployment.
		"USDC": {contracts: []string{"0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"}, decimals: 6},
	},
}

// tokenContractFor resolves the accepted contract addresses + decimals for
// asset on network (nil when the combination has no canonical contract).
func tokenContractFor(network, asset string) *tokenSpec {
	byAsset, ok := networkTokens[network]
	if !ok {
		return nil
	}
	spec, ok := byAsset[asset]
	if !ok {
		return nil
	}
	return &spec
}

// erc20TransferTopic is keccak256("Transfer(address,address,uint256)").
const erc20TransferTopic = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"

// depositVerifyRPCID is the fixed JSON-RPC request id; the verifier issues
// one request per call so a constant id is fine.
const depositVerifyRPCID = 1

// IsAutoVerifiableAsset reports whether a claim asset can in principle be
// checked against one of the supported EVM networks (ETH native + the two
// stablecoin ERC-20s). The per-network contract availability is enforced
// separately during verification.
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
	Network       string
}

// DepositVerifier checks deposit claims against one or more EVM networks
// through Alchemy JSON-RPC endpoints (standard eth_* methods only).
type DepositVerifier struct {
	rpcURLs map[string]string // network → JSON-RPC endpoint
	client  *http.Client
	// MinConfirmations is the confirmation floor (see MinConfirmations()).
	// Zero means "use the environment-resolved default".
	MinConfirmations int
}

// NewDepositVerifier builds a single-network verifier for eth-mainnet (kept
// for callers/tests that only need Ethereum). timeout bounds every HTTP
// round-trip (10s is a sensible default; ctx may tighten it further).
func NewDepositVerifier(rpcURL string, timeout time.Duration) *DepositVerifier {
	return NewMultiChainDepositVerifier(map[string]string{NetworkEthMainnet: rpcURL}, timeout)
}

// NewMultiChainDepositVerifier builds a verifier covering every network in
// rpcURLs (network slug → Alchemy JSON-RPC endpoint, e.g.
// https://eth-mainnet.g.alchemy.com/v2/<key>).
func NewMultiChainDepositVerifier(rpcURLs map[string]string, timeout time.Duration) *DepositVerifier {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	urls := make(map[string]string, len(rpcURLs))
	for k, v := range rpcURLs {
		urls[k] = v
	}
	return &DepositVerifier{
		rpcURLs:          urls,
		client:           &http.Client{Timeout: timeout},
		MinConfirmations: MinConfirmations(),
	}
}

// minConf resolves the effective confirmation floor.
func (v *DepositVerifier) minConf() int {
	if v.MinConfirmations >= 1 {
		return v.MinConfirmations
	}
	return DefaultMinConfirmations
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

// rpcCall performs one JSON-RPC round-trip against the network's endpoint.
// json.Number is preserved for numeric fields (big.Float parsing later) via a
// UseNumber decoder.
func (v *DepositVerifier) rpcCall(ctx context.Context, rpcURL, method string, params []interface{}) (json.RawMessage, error) {
	body, err := json.Marshal(depositVerifyRPCReq{
		JSONRPC: "2.0", Method: method, Params: params, ID: depositVerifyRPCID,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal rpc request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rpcURL, bytes.NewReader(body))
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
//   - (result, nil) with result.OK=false → definitive negative (unknown
//     network, missing deposit address, insufficient confirmations, tx
//     missing, failed receipt, wrong recipient, no matching transfer) — keep
//     pending for manual review
//   - (nil, err)                         → infrastructure/parse failure, i.e.
//     "cannot verify" — keep pending, never approve
//
// depositAddress is the claimant's platform deposit address resolved
// server-side; it is MANDATORY — an empty address always fails.
func (v *DepositVerifier) VerifyDeposit(ctx context.Context, network, asset, txid string, amount *big.Float, depositAddress string) (*VerifyResult, error) {
	network = NormalizeNetwork(network)
	asset = strings.ToUpper(strings.TrimSpace(asset))

	if !IsValidNetwork(network) {
		return &VerifyResult{
			OK: false, Network: network,
			Note: fmt.Sprintf("network %q is not supported for auto-verification (manual review required)", network),
		}, nil
	}
	if !IsAutoVerifiableAsset(asset) {
		return &VerifyResult{
			OK: false, Network: network,
			Note: "asset " + asset + " is not auto-verifiable on " + network,
		}, nil
	}
	// Mandatory recipient binding: without the claimant's own platform
	// deposit address ANY successful transfer of the right amount could be
	// claimed, so an empty address must never pass.
	depositAddress = strings.TrimSpace(depositAddress)
	if depositAddress == "" {
		return &VerifyResult{
			OK: false, Network: network,
			Note: fmt.Sprintf("user has no platform deposit address for %s; cannot verify recipient (manual review required)", asset),
		}, nil
	}
	if amount == nil || amount.Sign() <= 0 {
		return &VerifyResult{OK: false, Network: network, Note: "invalid claimed amount"}, nil
	}
	txid = strings.TrimSpace(txid)
	if !strings.HasPrefix(strings.ToLower(txid), "0x") || len(txid) != 66 {
		return &VerifyResult{
			OK: false, Network: network,
			Note: "txid is not a 32-byte EVM transaction hash",
		}, nil
	}
	rpcURL := v.rpcURLs[network]
	if rpcURL == "" {
		return &VerifyResult{
			OK: false, Network: network,
			Note: fmt.Sprintf("no RPC endpoint configured for network %s (manual review required)", network),
		}, nil
	}

	// 1. The transaction must be mined and its receipt successful.
	rawReceipt, err := v.rpcCall(ctx, rpcURL, "eth_getTransactionReceipt", []interface{}{txid})
	if err != nil {
		return nil, fmt.Errorf("get receipt: %w", err)
	}
	var receipt verifyReceipt
	if len(rawReceipt) == 0 || string(rawReceipt) == "null" {
		return &VerifyResult{
			OK: false, Note: "transaction not found or not yet mined on " + network,
			TxHash: txid, Confirmations: 0, Network: network,
		}, nil
	}
	if err := json.Unmarshal(rawReceipt, &receipt); err != nil {
		return nil, fmt.Errorf("parse receipt: %w", err)
	}
	if receipt.Status != "0x1" {
		return &VerifyResult{
			OK: false, Note: "transaction reverted on chain (receipt status " + receipt.Status + ")",
			TxHash: txid, Network: network,
		}, nil
	}

	// 2. Confirmation floor — a definitive negative when below the minimum
	// (the claim may legitimately pass later; it stays pending, never
	// rejected). Unknown confirmations (-1) count as below the floor.
	confirmations := -1
	if rawHead, err := v.rpcCall(ctx, rpcURL, "eth_blockNumber", nil); err == nil {
		var head string
		if json.Unmarshal(rawHead, &head) == nil {
			headN, okHead := new(big.Int).SetString(strings.TrimPrefix(head, "0x"), 16)
			txN, okTx := new(big.Int).SetString(strings.TrimPrefix(receipt.BlockNumber, "0x"), 16)
			if okHead && okTx {
				confirmations = int(new(big.Int).Add(new(big.Int).Sub(headN, txN), big.NewInt(1)).Int64())
			}
		}
	}
	if min := v.minConf(); confirmations < min {
		return &VerifyResult{
			OK: false,
			Note: fmt.Sprintf("insufficient confirmations on %s: %d (minimum %d required)",
				network, confirmations, min),
			TxHash: txid, Confirmations: confirmations, Network: network,
		}, nil
	}

	// 3. Match the claimed asset inside the transaction itself, binding the
	// recipient to the claimant's deposit address.
	switch asset {
	case "ETH":
		return v.verifyNativeETH(ctx, network, rpcURL, txid, amount, depositAddress, confirmations)
	default: // USDT / USDC
		return v.verifyERC20(network, asset, receipt.Logs, amount, depositAddress, confirmations, txid)
	}
}

// verifyNativeETH checks the transaction's own native value (tx.value) and
// its recipient — exactly what the sender transferred and to whom.
func (v *DepositVerifier) verifyNativeETH(ctx context.Context, network, rpcURL, txid string, amount *big.Float, depositAddress string, confirmations int) (*VerifyResult, error) {
	rawTx, err := v.rpcCall(ctx, rpcURL, "eth_getTransactionByHash", []interface{}{txid})
	if err != nil {
		return nil, fmt.Errorf("get transaction: %w", err)
	}
	var tx verifyTx
	if len(rawTx) == 0 || string(rawTx) == "null" {
		return &VerifyResult{
			OK: false, Note: "transaction not found on " + network,
			TxHash: txid, Confirmations: confirmations, Network: network,
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

	if !addressEqual(tx.To, depositAddress) {
		return &VerifyResult{
			OK:     false,
			Note:   fmt.Sprintf("transfer destination %s does not match the user's platform deposit address", tx.To),
			TxHash: txid, Confirmations: confirmations, Network: network,
		}, nil
	}
	if ethValue.Cmp(amount) < 0 {
		return &VerifyResult{
			OK: false,
			Note: fmt.Sprintf("on-chain ETH transfer value %s is below the claimed amount (claimed >= %s required)",
				ethValue.Text('f', -1), amount.Text('f', -1)),
			TxHash: txid, Confirmations: confirmations, Network: network,
		}, nil
	}
	return v.approvedResult(network, "ETH", ethValue, txid, confirmations, tx.To), nil
}

// verifyERC20 scans the receipt logs for a Transfer event of the asset's
// canonical contract(s) on the claimed network whose recipient is the
// claimant's deposit address and whose value covers the claim.
func (v *DepositVerifier) verifyERC20(network, asset string, logs []verifyLog, amount *big.Float, depositAddress string, confirmations int, txid string) (*VerifyResult, error) {
	spec := tokenContractFor(network, asset)
	if spec == nil {
		return &VerifyResult{
			OK:     false,
			Note:   fmt.Sprintf("asset %s has no canonical contract on %s; not auto-verifiable (manual review required)", asset, network),
			TxHash: txid, Confirmations: confirmations, Network: network,
		}, nil
	}

	var closestV *big.Float // largest same-token value seen, for the failure note
	var closestTo string
	for _, lg := range logs {
		if len(lg.Topics) != 3 || lg.Topics[0] != erc20TransferTopic || !contractMatches(lg.Address, spec.contracts) {
			continue
		}
		rawVal := strings.TrimPrefix(strings.ToLower(lg.Data), "0x")
		valInt, ok := new(big.Int).SetString(rawVal, 16)
		if !ok {
			continue
		}
		val := new(big.Float).Quo(new(big.Float).SetInt(valInt),
			new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(spec.decimals)), nil)))
		to := topicToAddress(lg.Topics[2])
		if addressEqual(to, depositAddress) && val.Cmp(amount) >= 0 {
			return v.approvedResult(network, asset, val, txid, confirmations, to), nil
		}
		if closestV == nil || val.Cmp(closestV) > 0 {
			closestV, closestTo = val, to
		}
	}

	note := fmt.Sprintf("no on-chain %s transfer to the deposit address >= claimed amount found in tx %s", asset, txid)
	if closestV != nil {
		if !addressEqual(closestTo, depositAddress) {
			note = fmt.Sprintf("on-chain %s transfer destination %s does not match the user's platform deposit address",
				asset, closestTo)
		} else {
			note = fmt.Sprintf("on-chain %s transfer value %s is below the claimed amount (claimed >= %s required)",
				asset, closestV.Text('f', -1), amount.Text('f', -1))
		}
	}
	return &VerifyResult{OK: false, Note: note, TxHash: txid, Confirmations: confirmations, Network: network}, nil
}

// approvedResult builds the success outcome.
func (v *DepositVerifier) approvedResult(network, asset string, matched *big.Float, txid string, confirmations int, to string) *VerifyResult {
	note := fmt.Sprintf("auto-verified via Alchemy: %s transfer of %s to the user's deposit address %s confirmed in tx %s on %s (%d confirmations)",
		asset, matched.Text('f', -1), to, txid, network, confirmations)
	return &VerifyResult{
		OK: true, Note: note, MatchedAmount: matched,
		Confirmations: confirmations, TxHash: txid, Network: network,
	}
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

// addressEqual compares two EVM addresses case-insensitively (EIP-55
// checksum casing must never matter for funds attribution).
func addressEqual(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	return strings.EqualFold(a, b)
}

// contractMatches reports whether a log address equals any accepted contract
// (case-insensitively).
func contractMatches(logAddr string, contracts []string) bool {
	for _, c := range contracts {
		if strings.EqualFold(logAddr, c) {
			return true
		}
	}
	return false
}
