package market

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"
)

// Uniswap V3 contract addresses on Ethereum mainnet.
const (
	UniV3QuoterV1 = "0xb27308f9F90D607463bb33eA1BeBb41C27CE5AB6"
	UniV3SwapRouter = "0xE592427A0AEce92De3Edee1F18E0157C05861564"
)

// UniV3PoolMeta maps a display pair to on-chain metadata.
// Token0/Token1 follow the pool's natural ordering (token0 < token1 by address).
type UniV3PoolMeta struct {
	Address string
	Token0  string // base symbol, e.g. ETH
	Token1  string // quote symbol, e.g. USDC
	Fee     int    // fee tier in hundredths of a bip (500, 3000, 10000)
	Decimals0 int
	Decimals1 int
}

var UniV3Pools = map[string]*UniV3PoolMeta{
	// Token0/Token1 MUST match the pool's natural ordering (lower address first).
	"ETH/USDC": {Address: "0x88e6A0c2dDD26FEEb64F039a2c41296FcB3f5640", Token0: "USDC", Token1: "ETH", Fee: 500, Decimals0: 6, Decimals1: 18},
	"ETH/USDT": {Address: "0x11b815efB8f581194ae79006d24E0d814B7697F6", Token0: "ETH", Token1: "USDT", Fee: 3000, Decimals0: 18, Decimals1: 6},
	"WBTC/USDC": {Address: "0x99ac8cA7087fA4A2A1FB6357269965A2014ABc35", Token0: "USDC", Token1: "WBTC", Fee: 3000, Decimals0: 6, Decimals1: 8},
	"LINK/ETH": {Address: "0xa6Cc3C2531FdaA6a1D6d0ae5C1bF8BedBED5ff66", Token0: "LINK", Token1: "ETH", Fee: 3000, Decimals0: 18, Decimals1: 18},
	"UNI/ETH":  {Address: "0x1d42064Fc4Beb5F8aAF85F4617AE8b3b5B8Bd801", Token0: "UNI", Token1: "ETH", Fee: 3000, Decimals0: 18, Decimals1: 18},
	"AAVE/ETH": {Address: "0x5aB53EE1d50eeF2C1DD1dC5725060B7d10d9B532", Token0: "AAVE", Token1: "ETH", Fee: 3000, Decimals0: 18, Decimals1: 18},
}

// tokenAddresses is reused from alchemy_dex.go but duplicated here to keep the
// package self-contained in case alchemy_dex.go is removed/renamed.
var uniTokenAddresses = map[string]string{
	"ETH":  "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2",
	"USDT": "0xdAC17F958D2ee523a2206206994597C13D831ec7",
	"USDC": "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
	"WBTC": "0x2260FAC5E5542a773Aa44fBCfeDf7C193bc2C599",
	"BTC":  "0x2260FAC5E5542a773Aa44fBCfeDf7C193bc2C599",
	"LINK": "0x514910771AF9Ca656af840dff83E8264EcF986CA",
	"UNI":  "0x1f9840a85d5aF5bf1D1762F925BDADdC4201F984",
	"AAVE": "0x7Fc66500c84A76Ad7e9c93437bFc5Ac33E2DDaE9",
	"CRV":  "0xD533a949740bb3306d119CC777fa900bA034cd52",
}

// UniswapRPCProvider fetches on-chain prices and swap quotes directly from
// Uniswap V3 pool contracts via JSON-RPC (Alchemy).
type UniswapRPCProvider struct {
	chain  *AlchemyMultiChain
	client *http.Client
}

func NewUniswapRPCProvider(apiKey string) *UniswapRPCProvider {
	return &UniswapRPCProvider{
		chain:  NewAlchemyMultiChain(apiKey),
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (u *UniswapRPCProvider) SetChain(chain *AlchemyMultiChain) { u.chain = chain }

func (u *UniswapRPCProvider) chainOrDefault() *AlchemyMultiChain {
	if u.chain != nil {
		return u.chain
	}
	return NewAlchemyMultiChain("")
}

// slot0Selector is the function selector for slot0().
const slot0Selector = "0x3850c7bd"

// FetchTicker returns the current token0/token1 price from the Uniswap V3 pool
// using the slot0 sqrtPriceX96 value.
func (u *UniswapRPCProvider) FetchTicker(pair string) (*Ticker, error) {
	meta, ok := UniV3Pools[pair]
	if !ok {
		return nil, fmt.Errorf("unsupported uniswap pair: %s", pair)
	}
	data := slot0Selector + strings.Repeat("0", 64)
	res, err := u.chainOrDefault().Call("ETH", "eth_call", []interface{}{
		map[string]interface{}{"to": meta.Address, "data": data},
		"latest",
	})
	if err != nil {
		return nil, fmt.Errorf("slot0 call failed: %w", err)
	}
	var hexStr string
	if err := json.Unmarshal(res, &hexStr); err != nil {
		return nil, fmt.Errorf("decode slot0 result: %w", err)
	}
	raw, err := sqrtPriceX96ToPrice(hexStr, meta.Decimals0, meta.Decimals1)
	if err != nil {
		return nil, err
	}
	// sqrtPriceX96ToPrice returns token1/token0 (after decimal adjustment).
	// The display pair is "BASE/QUOTE" and callers expect quote-per-base. When
	// the pool's token0 is the base token, raw is already quote/base; otherwise
	// token0 is the quote token and raw is base/quote (inverted), so flip it.
	parts := strings.Split(pair, "/")
	price := raw
	if len(parts) == 2 && meta.Token0 != parts[0] {
		price = new(big.Float).Quo(big.NewFloat(1), raw)
	}
	// An AMM pool exposes a single mid price; there is no native bid/ask
	// book, so report the mid for both sides rather than fabricating a spread.
	return &Ticker{
		Pair:      pair,
		Last:      price,
		Bid:       price,
		Ask:       price,
		Volume24h: new(big.Float),
		Timestamp: time.Now().UnixMilli(),
	}, nil
}

// sqrtPriceX96ToPrice parses the raw eth_call output and converts sqrtPriceX96
// to a human-readable token0/token1 price.
func sqrtPriceX96ToPrice(raw string, decimals0, decimals1 int) (*big.Float, error) {
	raw = strings.TrimPrefix(raw, "0x")
	if len(raw) < 64 {
		return nil, fmt.Errorf("slot0 response too short")
	}
	sqrtHex := raw[:64]
	sqrtInt := new(big.Int)
	if _, ok := sqrtInt.SetString(sqrtHex, 16); !ok {
		return nil, fmt.Errorf("invalid sqrtPriceX96")
	}
	// price = (sqrtPriceX96 / 2^96)^2
	sqrtF := new(big.Float).SetInt(sqrtInt)
	den := new(big.Float).SetFloat64(1)
	den.SetInt(new(big.Int).Lsh(big.NewInt(1), 96))
	price := new(big.Float).Quo(sqrtF, den)
	price.Mul(price, price)

	// Adjust for token decimals: token0 price in token1 terms.
	// price_raw = (token1_wei / token0_wei). To get human price
	// (token1 / token0) we multiply by 10^(decimals0 - decimals1).
	if decimals0 > decimals1 {
		mul := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals0-decimals1)), nil))
		price.Mul(price, mul)
	} else if decimals1 > decimals0 {
		div := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals1-decimals0)), nil))
		price.Quo(price, div)
	}
	return price, nil
}

// QuoteSwapRequest is the input for a Uniswap V3 exact-input single quote.
type QuoteSwapRequest struct {
	Pair       string
	AmountIn   *big.Float
	TokenIn    string
	TokenOut   string
	Fee        int
	DecimalsIn int
	DecimalsOut int
}

// QuoteSwapResult contains the expected output and execution price.
type QuoteSwapResult struct {
	AmountIn    *big.Float
	AmountOut   *big.Float
	ExecutionPrice *big.Float // tokenOut per tokenIn
	Pool        string
	FeeTier     int
}

// SwapTxRequest is the input for building an unsigned Uniswap V3 swap tx.
type SwapTxRequest struct {
	Pair            string
	TokenIn         string
	TokenOut        string
	AmountIn        *big.Float
	AmountOutMin    *big.Float // optional slippage protection; 0 = no protection
	Recipient       string     // receiver of tokenOut
	Deadline        int64      // unix seconds; 0 = 20 min from now
}

// SwapTxResult contains the unsigned transaction payload ready for signing.
type SwapTxResult struct {
	To       string
	Data     string
	Value    string
	GasLimit uint64
	Router   string
}

// QuoteExactInputSingle fetches a real Uniswap V3 quote for an exact-input swap.
func (u *UniswapRPCProvider) QuoteExactInputSingle(req QuoteSwapRequest) (*QuoteSwapResult, error) {
	meta, ok := UniV3Pools[req.Pair]
	if !ok {
		return nil, fmt.Errorf("unsupported pair: %s", req.Pair)
	}
	tokenIn, ok1 := uniTokenAddresses[req.TokenIn]
	tokenOut, ok2 := uniTokenAddresses[req.TokenOut]
	if !ok1 || !ok2 {
		return nil, fmt.Errorf("unknown token: %s/%s", req.TokenIn, req.TokenOut)
	}

	amountInWei := floatToWei(req.AmountIn, req.DecimalsIn)
	data := "f7729d0a" + // quoteExactInputSingle(address,address,uint24,uint256,uint160)
		padAddress(tokenIn) +
		padAddress(tokenOut) +
		fmt.Sprintf("%064x", req.Fee) +
		fmt.Sprintf("%064s", amountInWei.Text(16)) +
		strings.Repeat("0", 64)

	res, err := u.chainOrDefault().Call("ETH", "eth_call", []interface{}{
		map[string]interface{}{"to": UniV3QuoterV1, "data": "0x" + data},
		"latest",
	})
	if err != nil {
		return nil, fmt.Errorf("quote call failed: %w", err)
	}
	var hexStr string
	if err := json.Unmarshal(res, &hexStr); err != nil {
		return nil, fmt.Errorf("decode quote result: %w", err)
	}
	raw := strings.TrimPrefix(hexStr, "0x")
	if len(raw) < 128 {
		return nil, fmt.Errorf("quote response too short")
	}
	amountOutWei := new(big.Int)
	amountOutWei.SetString(raw[64:128], 16)
	amountOut := weiToFloat(amountOutWei, req.DecimalsOut)

	executionPrice := new(big.Float).Quo(amountOut, req.AmountIn)
	return &QuoteSwapResult{
		AmountIn:       req.AmountIn,
		AmountOut:      amountOut,
		ExecutionPrice: executionPrice,
		Pool:           meta.Address,
		FeeTier:        req.Fee,
	}, nil
}

// BuildSwapTx builds an unsigned exactInputSingle swap transaction calldata.
// The returned data can be passed to a wallet (e.g. MetaMask) for signing and
// broadcast. No funds move until the user signs.
func (u *UniswapRPCProvider) BuildSwapTx(req SwapTxRequest) (*SwapTxResult, error) {
	meta, ok := UniV3Pools[req.Pair]
	if !ok {
		return nil, fmt.Errorf("unsupported pair: %s", req.Pair)
	}
	tokenIn, ok1 := uniTokenAddresses[req.TokenIn]
	tokenOut, ok2 := uniTokenAddresses[req.TokenOut]
	if !ok1 || !ok2 {
		return nil, fmt.Errorf("unknown token: %s/%s", req.TokenIn, req.TokenOut)
	}
	decimalsIn := meta.Decimals0
	if meta.Token0 != req.TokenIn {
		decimalsIn = meta.Decimals1
	}
	amountInWei := floatToWei(req.AmountIn, decimalsIn)

	amountOutMin := big.NewInt(0)
	if req.AmountOutMin != nil && req.AmountOutMin.Sign() > 0 {
		amountOutMin, _ = req.AmountOutMin.Int(nil)
	}

	deadline := req.Deadline
	if deadline == 0 {
		deadline = time.Now().Add(20 * time.Minute).Unix()
	}

	recipient := req.Recipient
	if recipient == "" {
		recipient = "0x0000000000000000000000000000000000000000"
	}

	// exactInputSingle((address,address,uint24,address,uint256,uint256,uint256,uint160))
	selector := "0x04e45aaf"
	data := selector +
		padAddress(tokenIn) +
		padAddress(tokenOut) +
		fmt.Sprintf("%064x", meta.Fee) +
		padAddress(recipient) +
		encodeUint256(big.NewInt(deadline)) +
		encodeUint256(amountInWei) +
		encodeUint256(amountOutMin) +
		encodeUint256(big.NewInt(0)) // sqrtPriceLimitX96

	return &SwapTxResult{
		To:       UniV3SwapRouter,
		Data:     "0x" + data,
		Value:    "0x0",
		GasLimit: 250000,
		Router:   UniV3SwapRouter,
	}, nil
}

func floatToWei(f *big.Float, decimals int) *big.Int {
	multiplier := new(big.Float).SetFloat64(1)
	for i := 0; i < decimals; i++ {
		multiplier.Mul(multiplier, big.NewFloat(10))
	}
	wei := new(big.Float).Mul(f, multiplier)
	i, _ := wei.Int(nil)
	return i
}

func weiToFloat(i *big.Int, decimals int) *big.Float {
	f := new(big.Float).SetInt(i)
	divisor := new(big.Float).SetFloat64(1)
	for j := 0; j < decimals; j++ {
		divisor.Mul(divisor, big.NewFloat(10))
	}
	return new(big.Float).Quo(f, divisor)
}

// hex helper for swap calldata construction.
func encodeUint256(n *big.Int) string {
	return fmt.Sprintf("%064s", n.Text(16))
}

func encodeAddress(addr string) string {
	return padAddress(addr)
}

func encodeUint24(n int) string {
	return fmt.Sprintf("%064x", n)
}

func encodeBytes(data []byte) string {
	length := len(data)
	return fmt.Sprintf("%064x", 32) + fmt.Sprintf("%064x", length) + hex.EncodeToString(data) + strings.Repeat("0", (32-length%32)*2%64)
}
