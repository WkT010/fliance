package market

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

type ChainConfig struct {
	Name, Symbol, RPCURL, WSURL string
	Enabled bool
	Decimals int
}

type AlchemyMultiChain struct {
	apiKey string
	Chains map[string]*ChainConfig
	client *http.Client
}

func NewAlchemyMultiChain(apiKey string) *AlchemyMultiChain {
	amc := &AlchemyMultiChain{apiKey: apiKey, client: &http.Client{Timeout: 15 * time.Second}, Chains: make(map[string]*ChainConfig)}
	for _, c := range []*ChainConfig{
		{"Ethereum", "ETH", fmt.Sprintf("https://eth-mainnet.g.alchemy.com/v2/%s", apiKey), fmt.Sprintf("wss://eth-mainnet.g.alchemy.com/v2/%s", apiKey), true, 18},
		{"Polygon", "POLYGON", fmt.Sprintf("https://polygon-mainnet.g.alchemy.com/v2/%s", apiKey), fmt.Sprintf("wss://polygon-mainnet.g.alchemy.com/v2/%s", apiKey), true, 18},
		{"Arbitrum", "ARB", fmt.Sprintf("https://arb-mainnet.g.alchemy.com/v2/%s", apiKey), fmt.Sprintf("wss://arb-mainnet.g.alchemy.com/v2/%s", apiKey), true, 18},
		{"Optimism", "OP", fmt.Sprintf("https://opt-mainnet.g.alchemy.com/v2/%s", apiKey), fmt.Sprintf("wss://opt-mainnet.g.alchemy.com/v2/%s", apiKey), true, 18},
	} { amc.Chains[c.Symbol] = c }
	return amc
}

type jr struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  []interface{}   `json:"params"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct{ Code int; Message string } `json:"error,omitempty"`
}

func (amc *AlchemyMultiChain) Call(chain, method string, params []interface{}) (json.RawMessage, error) {
	c, ok := amc.Chains[chain]
	if !ok { return nil, fmt.Errorf("unknown: %s", chain) }
	b, _ := json.Marshal(jr{JSONRPC: "2.0", Method: method, Params: params, ID: 1})
	resp, err := amc.client.Post(c.RPCURL, "application/json", bytes.NewReader(b))
	if err != nil { return nil, err }
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var r jr
	json.Unmarshal(body, &r)
	if r.Error != nil { return nil, fmt.Errorf("alchemy %d: %s", r.Error.Code, r.Error.Message) }
	return r.Result, nil
}

func (amc *AlchemyMultiChain) GetBlockNumber(chain string) (uint64, error) {
	r, _ := amc.Call(chain, "eth_blockNumber", nil)
	var h string; json.Unmarshal(r, &h)
	var n uint64; fmt.Sscanf(h, "0x%x", &n)
	return n, nil
}

func (amc *AlchemyMultiChain) GetBalance(chain, addr string) (string, error) {
	r, _ := amc.Call(chain, "eth_getBalance", []interface{}{addr, "latest"})
	var h string; json.Unmarshal(r, &h)
	return h, nil
}

func (amc *AlchemyMultiChain) GetTokenBalances(chain, addr string) (json.RawMessage, error) {
	return amc.Call(chain, "alchemy_getTokenBalances", []interface{}{addr, "erc20"})
}

func (amc *AlchemyMultiChain) StartBlockMonitor(cb func(string, uint64)) {
	for sym, c := range amc.Chains {
		if !c.Enabled { continue }
		go func(s string) {
			for range time.NewTicker(12 * time.Second).C {
				n, err := amc.GetBlockNumber(s)
				if err != nil { continue }
				log.Printf("[alchemy] %s block %d", s, n)
				if cb != nil { cb(s, n) }
			}
		}(sym)
	}
}
