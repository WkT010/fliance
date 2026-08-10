package market

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"
)

const uniswapV3SubgraphURL = "https://api.thegraph.com/subgraphs/name/uniswap/uniswap-v3"

// UniswapPoolMeta maps a display pair to a Uniswap V3 pool address on Ethereum.
var UniswapPoolMeta = map[string]struct {
	Address string
	Token0  string // base token symbol (e.g. ETH)
	Token1  string // quote token symbol (e.g. USDC)
	Fee     int
}{
	"ETH/USDC":  {Address: "0x88e6A0c2dDD26FEEb64F039a2c41296FcB3f5640", Token0: "ETH", Token1: "USDC", Fee: 500},
	"ETH/USDT":  {Address: "0x11b815efB8f581194ae79006d24E0d814B7697F6", Token0: "ETH", Token1: "USDT", Fee: 3000},
	"WBTC/USDC": {Address: "0x99ac8cA7087fA4A2A1FB6357269965A2014ABc35", Token0: "WBTC", Token1: "USDC", Fee: 3000},
	"LINK/ETH":  {Address: "0xa6Cc3C2531FdaA6a1D6d0ae5C1bF8BedBED5ff66", Token0: "LINK", Token1: "ETH", Fee: 3000},
	"UNI/ETH":   {Address: "0x1d42064Fc4Beb5F8aAF85F4617AE8b3b5B8Bd801", Token0: "UNI", Token1: "ETH", Fee: 3000},
	"AAVE/ETH":  {Address: "0x5aB53EE1d50eeF2C1DD1dC5725060B7d10d9B532", Token0: "AAVE", Token1: "ETH", Fee: 3000},
}

// UniswapSubgraphProvider fetches on-chain prices from the Uniswap V3 subgraph.
type UniswapSubgraphProvider struct {
	client  *http.Client
	baseURL string
}

func NewUniswapSubgraphProvider() *UniswapSubgraphProvider {
	return &UniswapSubgraphProvider{
		client:  &http.Client{Timeout: 10 * time.Second},
		baseURL: uniswapV3SubgraphURL,
	}
}

type poolResponse struct {
	Data struct {
		Pool struct {
			Token0Price string `json:"token0Price"`
			Token1Price string `json:"token1Price"`
			VolumeUSD   string `json:"volumeUSD"`
			FeeTier     string `json:"feeTier"`
		} `json:"pool"`
	} `json:"data"`
	Errors []struct{ Message string } `json:"errors"`
}

// FetchTicker returns the current Uniswap V3 price for the requested pair.
// The returned price is token0 / token1 (e.g. ETH per USDC or ETH/USDC).
func (u *UniswapSubgraphProvider) FetchTicker(pair string) (*Ticker, error) {
	meta, ok := UniswapPoolMeta[pair]
	if !ok {
		return nil, fmt.Errorf("unsupported uniswap pair: %s", pair)
	}
	query := fmt.Sprintf(`{ pool(id: "%s") { token0Price token1Price volumeUSD feeTier } }`, strings.ToLower(meta.Address))
	body, _ := json.Marshal(map[string]string{"query": query})
	req, err := http.NewRequest("POST", u.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := u.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var r poolResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	if len(r.Errors) > 0 {
		return nil, fmt.Errorf("subgraph error: %s", r.Errors[0].Message)
	}
	p := r.Data.Pool
	last, ok := new(big.Float).SetString(p.Token0Price)
	if !ok {
		return nil, fmt.Errorf("invalid price")
	}
	inv, _ := new(big.Float).SetString(p.Token1Price)
	vol, _ := new(big.Float).SetString(p.VolumeUSD)
	return &Ticker{
		Pair:      pair,
		Last:      last,
		Bid:       last,
		Ask:       inv,
		Volume24h: vol,
		Timestamp: time.Now().UnixMilli(),
	}, nil
}

// SupportedPairs returns the pairs this provider can quote.
func (u *UniswapSubgraphProvider) SupportedPairs() []string {
	pairs := make([]string, 0, len(UniswapPoolMeta))
	for p := range UniswapPoolMeta {
		pairs = append(pairs, p)
	}
	return pairs
}
