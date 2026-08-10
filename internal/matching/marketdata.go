package matching

import (
	"math/big"
	"sort"
	"sync"
	"time"
)

// Ticker is the 24h rolling statistics for a trading pair.
type Ticker struct {
	Pair           string
	LastPrice      *big.Float
	Bid            *big.Float
	Ask            *big.Float
	Spread         *big.Float
	Volume24H      *big.Float
	QuoteVolume24H *big.Float
	High24H        *big.Float
	Low24H         *big.Float
	Open24H        *big.Float
	Change24H      *big.Float
	ChangePct24H   *big.Float
	Timestamp      int64
}

// Candle is an OHLCV aggregate for a single time bucket.
type Candle struct {
	Pair      string
	Interval  string
	Open      *big.Float
	High      *big.Float
	Low       *big.Float
	Close     *big.Float
	Volume    *big.Float
	Timestamp int64
	CloseTime int64
}

// intervalSeconds maps a candle interval string to its duration in seconds.
var intervalSeconds = map[string]int64{
	"1s":  1,
	"1m":  60,
	"3m":  180,
	"5m":  300,
	"15m": 900,
	"30m": 1800,
	"1h":  3600,
	"2h":  7200,
	"4h":  14400,
	"6h":  21600,
	"8h":  28800,
	"12h": 43200,
	"1d":  86400,
	"3d":  259200,
	"1w":  604800,
	"1M":  2592000,
}

// IntervalSeconds returns the duration of an interval string in seconds, or 0
// if the interval is unknown.
func IntervalSeconds(interval string) int64 { return intervalSeconds[interval] }

// IntervalNames returns all supported candle interval names sorted from shortest
// to longest.
func IntervalNames() []string {
	names := make([]string, 0, len(intervalSeconds))
	for k := range intervalSeconds {
		names = append(names, k)
	}
	sort.Slice(names, func(i, j int) bool {
		return intervalSeconds[names[i]] < intervalSeconds[names[j]]
	})
	return names
}

// MarketDataRecorder aggregates trades into 24h rolling ticker statistics and
// historical OHLCV candles. It is safe for concurrent use.
type MarketDataRecorder struct {
	pair string
	mu   sync.Mutex

	// 24h rolling window of trades (oldest first).
	trades []*Trade
	// seen holds the IDs of trades currently in the window for O(1) dedup.
	// It is evicted in lockstep with the trades slice (time cutoff and
	// maxTrades truncation), so len(seen) <= len(trades) <= maxTrades.
	seen map[string]struct{}

	// Candles aggregated by interval, newest last. We keep a bounded history per
	// interval.
	candles map[string][]*Candle

	maxTrades  int
	maxCandles int
}

// NewMarketDataRecorder constructs a recorder for a pair.
func NewMarketDataRecorder(pair string) *MarketDataRecorder {
	return &MarketDataRecorder{
		pair:       pair,
		candles:    make(map[string][]*Candle),
		seen:       make(map[string]struct{}),
		maxTrades:  100_000,
		maxCandles: 1500,
	}
}

// RecordTrade ingests a trade and updates rolling stats / candles. It is
// idempotent on trade ID (re-recording the same trade is a no-op). Dedup is
// O(1) via the seen map instead of a linear scan of the window.
func (r *MarketDataRecorder) RecordTrade(t *Trade) {
	if t == nil || t.Price == nil || t.Quantity == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.seen[t.ID]; dup {
		return // already recorded
	}
	r.trades = append(r.trades, t)
	r.seen[t.ID] = struct{}{}
	cutoff := timeNowUnixNano() - 24*int64(time.Hour/time.Nanosecond)
	idx := 0
	for idx < len(r.trades) && r.trades[idx].CreatedAt < cutoff {
		idx++
	}
	if idx > 0 {
		for _, ev := range r.trades[:idx] {
			delete(r.seen, ev.ID)
		}
		r.trades = r.trades[idx:]
	}
	if len(r.trades) > r.maxTrades {
		excess := len(r.trades) - r.maxTrades
		for _, ev := range r.trades[:excess] {
			delete(r.seen, ev.ID)
		}
		r.trades = r.trades[excess:]
	}
	r.updateCandlesLocked(t)
}

func (r *MarketDataRecorder) updateCandlesLocked(t *Trade) {
	for interval := range intervalSeconds {
		bucket := intervalSeconds[interval]
		if bucket == 0 {
			continue
		}
		nsPerBucket := bucket * int64(time.Second)
		ts := (t.CreatedAt / nsPerBucket) * nsPerBucket
		closeTime := ts + nsPerBucket - 1
		series := r.candles[interval]
		if len(series) > 0 && series[len(series)-1].Timestamp == ts {
			c := series[len(series)-1]
			if t.Price.Cmp(c.High) > 0 {
				c.High = newBigFloatCopy(t.Price)
			}
			if t.Price.Cmp(c.Low) < 0 {
				c.Low = newBigFloatCopy(t.Price)
			}
			c.Close = newBigFloatCopy(t.Price)
			c.Volume.Add(c.Volume, t.Quantity)
			c.CloseTime = closeTime
			continue
		}
		c := &Candle{
			Pair:      r.pair,
			Interval:  interval,
			Open:      newBigFloatCopy(t.Price),
			High:      newBigFloatCopy(t.Price),
			Low:       newBigFloatCopy(t.Price),
			Close:     newBigFloatCopy(t.Price),
			Volume:    newBigFloatCopy(t.Quantity),
			Timestamp: ts,
			CloseTime: closeTime,
		}
		series = append(series, c)
		if len(series) > r.maxCandles {
			series = series[len(series)-r.maxCandles:]
		}
		r.candles[interval] = series
	}
}

// Ticker computes the 24h rolling ticker from recorded trades plus current
// top-of-book.
func (r *MarketDataRecorder) Ticker(bid, ask *big.Float) *Ticker {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := timeNowUnixNano()
	cutoff := now - 24*int64(time.Hour/time.Nanosecond)
	idx := sort.Search(len(r.trades), func(i int) bool { return r.trades[i].CreatedAt >= cutoff })
	window := r.trades[idx:]

	t := &Ticker{
		Pair:           r.pair,
		LastPrice:      newBigFloat(),
		Volume24H:      newBigFloat(),
		QuoteVolume24H: newBigFloat(),
		High24H:        newBigFloat(),
		Low24H:         newBigFloat(),
		Open24H:        newBigFloat(),
		Change24H:      newBigFloat(),
		ChangePct24H:   newBigFloat(),
		Spread:         newBigFloat(),
		Bid:            newBigFloat(),
		Ask:            newBigFloat(),
		Timestamp:      now,
	}
	if bid != nil {
		t.Bid = newBigFloatCopy(bid)
	}
	if ask != nil {
		t.Ask = newBigFloatCopy(ask)
	}
	if t.Bid.Sign() > 0 && t.Ask.Sign() > 0 {
		t.Spread = new(big.Float).Sub(t.Ask, t.Bid)
	}
	if len(window) == 0 {
		return t
	}
	t.Open24H = newBigFloatCopy(window[0].Price)
	t.LastPrice = newBigFloatCopy(window[len(window)-1].Price)
	high := newBigFloatCopy(window[0].Price)
	low := newBigFloatCopy(window[0].Price)
	for _, tr := range window {
		if tr.Price.Cmp(high) > 0 {
			high = newBigFloatCopy(tr.Price)
		}
		if tr.Price.Cmp(low) < 0 {
			low = newBigFloatCopy(tr.Price)
		}
		t.Volume24H.Add(t.Volume24H, tr.Quantity)
		quote := newBigFloat()
		quote.Mul(tr.Price, tr.Quantity)
		t.QuoteVolume24H.Add(t.QuoteVolume24H, quote)
	}
	t.High24H = high
	t.Low24H = low
	t.Change24H = new(big.Float).Sub(t.LastPrice, t.Open24H)
	if t.Open24H.Sign() > 0 {
		t.ChangePct24H = new(big.Float).Quo(t.Change24H, t.Open24H)
	}
	return t
}

// Candles returns up to `limit` candles for the given interval, optionally
// filtered by [start, end] (inclusive). Returns newest-last ordering.
func (r *MarketDataRecorder) Candles(interval string, limit int, start, end int64) []*Candle {
	r.mu.Lock()
	defer r.mu.Unlock()
	series, ok := r.candles[interval]
	if !ok || len(series) == 0 {
		return []*Candle{}
	}
	out := make([]*Candle, 0, len(series))
	for _, c := range series {
		if start > 0 && c.Timestamp < start {
			continue
		}
		if end > 0 && c.Timestamp > end {
			continue
		}
		out = append(out, c)
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

// RecentTrades returns up to `limit` most recent trades (newest first).
func (r *MarketDataRecorder) RecentTrades(limit int) []*Trade {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := len(r.trades)
	if limit <= 0 || limit > n {
		limit = n
	}
	out := make([]*Trade, limit)
	for i := 0; i < limit; i++ {
		out[i] = r.trades[n-1-i]
	}
	return out
}
