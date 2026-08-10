package market

import (
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"
)

type AlchemyPriceResponse struct {
	Data []AlchemyTokenPrice `json:"data"`
}
type AlchemyTokenPrice struct {
	Symbol string         `json:"symbol"`
	Prices []AlchemyPrice `json:"prices"`
}
type AlchemyPrice struct {
	Currency, Value, LastUpdatedAt string
}

type AlchemyPriceFeed struct {
	client  *http.Client
	apiKey  string
	baseURL string
}

func NewAlchemyPriceFeed(apiKey string) *AlchemyPriceFeed {
	return &AlchemyPriceFeed{
		client: &http.Client{Timeout: 10 * time.Second},
		apiKey: apiKey, baseURL: "https://api.g.alchemy.com/prices/v1",
	}
}

var alchemyPairs = map[string]string{
	"BTC/USDT": "BTC", "ETH/USDT": "ETH", "SOL/USDT": "SOL",
	"BNB/USDT": "BNB", "ADA/USDT": "ADA", "DOGE/USDT": "DOGE",
	"XRP/USDT": "XRP", "UNI/USDT": "UNI", "LINK/USDT": "LINK",
	"MATIC/USDT": "MATIC", "ARB/USDT": "ARB", "OP/USDT": "OP",
	"AAVE/USDT": "AAVE", "CRV/USDT": "CRV",
}

func (a *AlchemyPriceFeed) FetchAllTickers() (map[string]*Ticker, error) {
	syms := make([]string, 0, len(alchemyPairs))
	seen := map[string]bool{}
	for _, s := range alchemyPairs {
		if !seen[s] {
			syms = append(syms, s)
			seen[s] = true
		}
	}
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/tokens/by-symbol?symbols=%s", a.baseURL, strings.Join(syms, ",")), nil)
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var r AlchemyPriceResponse
	json.NewDecoder(resp.Body).Decode(&r)
	tickers := map[string]*Ticker{}
	for _, t := range r.Data {
		pair := ""
		for p, s := range alchemyPairs {
			if s == t.Symbol {
				pair = p
				break
			}
		}
		if pair == "" || len(t.Prices) == 0 {
			continue
		}
		p := t.Prices[0]
		last, _ := new(big.Float).SetString(p.Value)
		ts, _ := time.Parse(time.RFC3339Nano, p.LastUpdatedAt)
		tickers[pair] = &Ticker{Pair: pair, Last: last, Volume24h: new(big.Float), Timestamp: ts.UnixMilli()}
	}
	return tickers, nil
}

func (a *AlchemyPriceFeed) FetchTicker(pair string) (*Ticker, error) {
	s, ok := alchemyPairs[pair]
	if !ok {
		return nil, fmt.Errorf("unsupported: %s", pair)
	}
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/tokens/by-symbol?symbols=%s", a.baseURL, s), nil)
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var r AlchemyPriceResponse
	json.NewDecoder(resp.Body).Decode(&r)
	if len(r.Data) == 0 || len(r.Data[0].Prices) == 0 {
		return nil, fmt.Errorf("no data")
	}
	p := r.Data[0].Prices[0]
	last, _ := new(big.Float).SetString(p.Value)
	ts, _ := time.Parse(time.RFC3339Nano, p.LastUpdatedAt)
	return &Ticker{Pair: pair, Last: last, Volume24h: new(big.Float), Timestamp: ts.UnixMilli()}, nil
}
