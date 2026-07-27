package market

import (
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"time"
)

type BinanceTicker struct {
	Symbol, LastPrice, BidPrice, AskPrice, Volume, QuoteVolume, HighPrice, LowPrice, OpenPrice, PriceChange string
}

type BinancePriceFeed struct {
	client  *http.Client
	baseURL string
}

func NewBinancePriceFeed() *BinancePriceFeed {
	return &BinancePriceFeed{client: &http.Client{Timeout: 10 * time.Second}, baseURL: "https://api.binance.com"}
}

var supportedPairs = map[string]string{
	"BTC/USDT": "BTCUSDT", "ETH/USDT": "ETHUSDT", "SOL/USDT": "SOLUSDT",
	"BNB/USDT": "BNBUSDT", "ADA/USDT": "ADAUSDT",
}

func symbolToPair(symbol string) string {
	for p, s := range supportedPairs { if s == symbol { return p } }
	return ""
}

func (b *BinancePriceFeed) FetchTicker(pair string) (*Ticker, error) {
	s, ok := supportedPairs[pair]
	if !ok { return nil, fmt.Errorf("unsupported: %s", pair) }
	resp, err := b.client.Get(fmt.Sprintf("%s/api/v3/ticker/24hr?symbol=%s", b.baseURL, s))
	if err != nil { return nil, fmt.Errorf("binance: %w", err) }
	defer resp.Body.Close()
	if resp.StatusCode != 200 { return nil, fmt.Errorf("status %d", resp.StatusCode) }
	var bt BinanceTicker
	json.NewDecoder(resp.Body).Decode(&bt)
	return toTicker(&bt, pair), nil
}

func (b *BinancePriceFeed) FetchAllTickers() (map[string]*Ticker, error) {
	resp, err := b.client.Get(fmt.Sprintf("%s/api/v3/ticker/24hr", b.baseURL))
	if err != nil { return nil, fmt.Errorf("binance: %w", err) }
	defer resp.Body.Close()
	var all []BinanceTicker
	json.NewDecoder(resp.Body).Decode(&all)
	result := make(map[string]*Ticker)
	for _, bt := range all {
		if pair := symbolToPair(bt.Symbol); pair != "" {
			result[pair] = toTicker(&bt, pair)
		}
	}
	return result, nil
}

func toTicker(bt *BinanceTicker, pair string) *Ticker {
	last, _ := new(big.Float).SetString(bt.LastPrice)
	bid, _ := new(big.Float).SetString(bt.BidPrice)
	ask, _ := new(big.Float).SetString(bt.AskPrice)
	vol, _ := new(big.Float).SetString(bt.Volume)
	qvol, _ := new(big.Float).SetString(bt.QuoteVolume)
	high, _ := new(big.Float).SetString(bt.HighPrice)
	low, _ := new(big.Float).SetString(bt.LowPrice)
	open, _ := new(big.Float).SetString(bt.OpenPrice)
	chg, _ := new(big.Float).SetString(bt.PriceChange)
	spread := new(big.Float).Sub(ask, bid)
	pct := new(big.Float)
	if open.Sign() > 0 { pct.Quo(chg, open) }
	return &Ticker{
		Pair: pair, Last: last, Bid: bid, Ask: ask, Spread: spread,
		Volume24h: vol, QuoteVolume24h: qvol, High24h: high, Low24h: low,
		Open24h: open, Change24h: chg, ChangePct24h: pct,
		Timestamp: time.Now().UnixMilli(),
	}
}
