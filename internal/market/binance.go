package market

import (
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"
)

type BinanceTicker struct {
	Symbol, LastPrice, BidPrice, AskPrice, Volume, QuoteVolume, HighPrice, LowPrice, OpenPrice, PriceChange string
}

type BinancePriceFeed struct {
	client  *http.Client
	mirrors []string
	mu      sync.RWMutex
	active  int // index of last-known-good mirror
}

func NewBinancePriceFeed() *BinancePriceFeed {
	return &BinancePriceFeed{
		client: &http.Client{Timeout: 6 * time.Second},
		// api.binance.com is geo-blocked in some regions; data-api.binance.vision
		// is Binance's public market-data mirror and serves the identical
		// /api/v3/ticker/24hr payload without authentication. Try the mirror
		// first, then fall back to the canonical host.
		mirrors: []string{"https://data-api.binance.vision", "https://api.binance.com"},
		active:  0,
	}
}

var supportedPairs = map[string]string{
	"BTC/USDT": "BTCUSDT", "ETH/USDT": "ETHUSDT", "SOL/USDT": "SOLUSDT",
	"BNB/USDT": "BNBUSDT", "ADA/USDT": "ADAUSDT",
}

func symbolToPair(symbol string) string {
	for p, s := range supportedPairs { if s == symbol { return p } }
	return ""
}

// get fetches a path from the last-known-good mirror first, then falls back
// through the remaining mirrors. The working mirror is cached so subsequent
// requests skip dead endpoints instead of waiting for the full timeout each
// time.
func (b *BinancePriceFeed) get(path string) (*http.Response, error) {
	b.mu.RLock()
	start := b.active
	mirrors := b.mirrors
	b.mu.RUnlock()
	var lastErr error
	for i := 0; i < len(mirrors); i++ {
		idx := (start + i) % len(mirrors)
		resp, err := b.client.Get(mirrors[idx] + path)
		if err == nil {
			if idx != start {
				b.mu.Lock()
				b.active = idx
				b.mu.Unlock()
			}
			return resp, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func (b *BinancePriceFeed) FetchTicker(pair string) (*Ticker, error) {
	s, ok := supportedPairs[pair]
	if !ok { return nil, fmt.Errorf("unsupported: %s", pair) }
	resp, err := b.get(fmt.Sprintf("/api/v3/ticker/24hr?symbol=%s", s))
	if err != nil { return nil, fmt.Errorf("binance: %w", err) }
	defer resp.Body.Close()
	if resp.StatusCode != 200 { return nil, fmt.Errorf("status %d", resp.StatusCode) }
	var bt BinanceTicker
	json.NewDecoder(resp.Body).Decode(&bt)
	return toTicker(&bt, pair), nil
}

// FetchAllTickers fetches only the supported symbols in parallel rather than
// downloading Binance's full ~2MB all-symbols payload, which makes every poll
// take tens of seconds and quickly trips rate limits.
func (b *BinancePriceFeed) FetchAllTickers() (map[string]*Ticker, error) {
	result := make(map[string]*Ticker)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for pair, sym := range supportedPairs {
		wg.Add(1)
		go func(pair, sym string) {
			defer wg.Done()
			resp, err := b.get(fmt.Sprintf("/api/v3/ticker/24hr?symbol=%s", sym))
			if err != nil {
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				return
			}
			var bt BinanceTicker
			if json.NewDecoder(resp.Body).Decode(&bt) != nil {
				return
			}
			t := toTicker(&bt, pair)
			mu.Lock()
			result[pair] = t
			mu.Unlock()
		}(pair, sym)
	}
	wg.Wait()
	if len(result) == 0 {
		return nil, fmt.Errorf("binance: no tickers available")
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
