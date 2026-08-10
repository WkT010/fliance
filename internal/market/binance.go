package market

import (
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/WkT010/nexa-exchange/internal/matching"
)

type BinanceTicker struct {
	Symbol, LastPrice, BidPrice, AskPrice, Volume, QuoteVolume, HighPrice, LowPrice, OpenPrice, PriceChange string
}

type BinancePriceFeed struct {
	client  *http.Client
	mirrors []string
	mu      sync.RWMutex
	active  int // index of last-known-good mirror
	// backoffUntil marks mirrors rate-limited by Binance (HTTP 429/418); they
	// are skipped until the backoff expires instead of burning requests.
	backoffUntil map[int]time.Time
}

// DefaultBinanceMirrors is tried in order when BINANCE_REST_URLS is unset:
// the public market-data mirror first (api.binance.com is geo-blocked in
// some regions), then the canonical host, then a third-party mirror as the
// last-resort degraded source.
var DefaultBinanceMirrors = []string{
	"https://data-api.binance.vision",
	"https://api.binance.com",
	"https://www.usnbweb.red",
}

// NewBinancePriceFeed creates a REST feed over the given mirror list. Empty
// entries are dropped; an empty list falls back to DefaultBinanceMirrors.
func NewBinancePriceFeed(mirrors []string) *BinancePriceFeed {
	var list []string
	for _, m := range mirrors {
		if m = strings.TrimRight(strings.TrimSpace(m), "/"); m != "" {
			list = append(list, m)
		}
	}
	if len(list) == 0 {
		list = append(list, DefaultBinanceMirrors...)
	}
	return &BinancePriceFeed{
		client:       &http.Client{Timeout: 6 * time.Second},
		mirrors:      list,
		active:       0,
		backoffUntil: make(map[int]time.Time),
	}
}

var supportedPairs = map[string]string{
	"BTC/USDT": "BTCUSDT", "ETH/USDT": "ETHUSDT", "SOL/USDT": "SOLUSDT",
	"BNB/USDT": "BNBUSDT", "ADA/USDT": "ADAUSDT",
}

func symbolToPair(symbol string) string {
	for p, s := range supportedPairs {
		if s == symbol {
			return p
		}
	}
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
		b.mu.RLock()
		paused := time.Now().Before(b.backoffUntil[idx])
		b.mu.RUnlock()
		if paused {
			continue
		}
		resp, err := b.client.Get(mirrors[idx] + path)
		if err == nil {
			// Rate-limit responses (429 slowed down, 418 IP ban) must not be
			// passed to callers: back the mirror off and try the next one.
			if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusTeapot {
				delay := rateLimitDelay(resp)
				resp.Body.Close()
				b.mu.Lock()
				b.backoffUntil[idx] = time.Now().Add(delay)
				b.mu.Unlock()
				lastErr = fmt.Errorf("binance rate limited (%d) by %s; backing off %s", resp.StatusCode, mirrors[idx], delay)
				continue
			}
			if idx != start {
				b.mu.Lock()
				b.active = idx
				b.mu.Unlock()
			}
			return resp, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("binance: all mirrors are in rate-limit backoff")
	}
	return nil, lastErr
}

// rateLimitDelay derives the backoff for a 429/418 response, honouring the
// Retry-After header when present (418 bans default to 2 minutes, 429 to 1).
func rateLimitDelay(resp *http.Response) time.Duration {
	def := 60 * time.Second
	if resp.StatusCode == http.StatusTeapot {
		def = 120 * time.Second
	}
	if v := resp.Header.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return def
}

func (b *BinancePriceFeed) FetchTicker(pair string) (*Ticker, error) {
	s, ok := supportedPairs[pair]
	if !ok {
		return nil, fmt.Errorf("unsupported: %s", pair)
	}
	resp, err := b.get(fmt.Sprintf("/api/v3/ticker/24hr?symbol=%s", s))
	if err != nil {
		return nil, fmt.Errorf("binance: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
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

// FetchKlines backfills historical OHLCV candles from Binance's /api/v3/klines
// endpoint. Interval names match Binance's 1:1 for every interval supported by
// the matching engine except "1s". `limit` is clamped to [1, 1000]. Returned
// candles use nanosecond timestamps (open-time / close-time) to match the
// CandleService convention.
func (b *BinancePriceFeed) FetchKlines(pair, interval string, limit int) ([]*matching.Candle, error) {
	sym, ok := supportedPairs[pair]
	if !ok {
		return nil, fmt.Errorf("unsupported pair: %s", pair)
	}
	if interval == "1s" {
		return nil, fmt.Errorf("binance klines: interval %s not available from the public mirror", interval)
	}
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	resp, err := b.get(fmt.Sprintf("/api/v3/klines?symbol=%s&interval=%s&limit=%d", sym, interval, limit))
	if err != nil {
		return nil, fmt.Errorf("binance klines: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("binance klines: status %d", resp.StatusCode)
	}
	var rows [][]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		return nil, fmt.Errorf("binance klines decode: %w", err)
	}
	out := make([]*matching.Candle, 0, len(rows))
	for _, r := range rows {
		if len(r) < 7 {
			continue
		}
		var openMs, closeMs int64
		var o, h, l, cl, v string
		if json.Unmarshal(r[0], &openMs) != nil || json.Unmarshal(r[6], &closeMs) != nil {
			continue
		}
		if json.Unmarshal(r[1], &o) != nil || json.Unmarshal(r[2], &h) != nil ||
			json.Unmarshal(r[3], &l) != nil || json.Unmarshal(r[4], &cl) != nil ||
			json.Unmarshal(r[5], &v) != nil {
			continue
		}
		open, ok1 := new(big.Float).SetString(o)
		high, ok2 := new(big.Float).SetString(h)
		low, ok3 := new(big.Float).SetString(l)
		close_, ok4 := new(big.Float).SetString(cl)
		vol, ok5 := new(big.Float).SetString(v)
		if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 {
			continue
		}
		out = append(out, &matching.Candle{
			Pair: pair, Interval: interval,
			Open: open, High: high, Low: low, Close: close_, Volume: vol,
			Timestamp: openMs * int64(time.Millisecond),
			CloseTime: closeMs * int64(time.Millisecond),
		})
	}
	return out, nil
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
	if open.Sign() > 0 {
		pct.Quo(chg, open)
	}
	return &Ticker{
		Pair: pair, Last: last, Bid: bid, Ask: ask, Spread: spread,
		Volume24h: vol, QuoteVolume24h: qvol, High24h: high, Low24h: low,
		Open24h: open, Change24h: chg, ChangePct24h: pct,
		Timestamp: time.Now().UnixMilli(),
	}
}
