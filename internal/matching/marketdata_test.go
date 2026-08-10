package matching

import (
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
)

// makeTrade creates a Trade with the given price, quantity, and CreatedAt
// timestamp. Price and Quantity are created at DefaultPrecision via newBigFloat.
func makeTrade(price, qty float64, createdAt int64) *Trade {
	p := newBigFloat()
	p.SetFloat64(price)
	q := newBigFloat()
	q.SetFloat64(qty)
	return &Trade{
		ID:        uuid.NewString(),
		Price:     p,
		Quantity:  q,
		CreatedAt: createdAt,
	}
}

func TestMarketDataRecorderInit(t *testing.T) {
	r := NewMarketDataRecorder("BTC/USDT")
	if r.pair != "BTC/USDT" {
		t.Errorf("expected pair BTC/USDT, got %s", r.pair)
	}
	if len(r.trades) != 0 {
		t.Errorf("expected empty trades slice, got %d items", len(r.trades))
	}
	if len(r.candles) != 0 {
		t.Errorf("expected empty candles map, got %d intervals", len(r.candles))
	}
	// RecentTrades on a fresh recorder returns an empty (non-nil) slice.
	rt := r.RecentTrades(10)
	if len(rt) != 0 {
		t.Errorf("expected 0 recent trades, got %d", len(rt))
	}
	// Candles on a fresh recorder returns an empty (non-nil) slice.
	c := r.Candles("1m", 0, 0, 0)
	if len(c) != 0 {
		t.Errorf("expected 0 candles, got %d", len(c))
	}
}

func TestRecordTradeAdds(t *testing.T) {
	r := NewMarketDataRecorder("BTC/USDT")
	now := time.Now().UnixNano()
	tr := makeTrade(50000, 1.5, now)
	r.RecordTrade(tr)
	if len(r.trades) != 1 {
		t.Fatalf("expected 1 trade after recording, got %d", len(r.trades))
	}
	if r.trades[0].ID != tr.ID {
		t.Errorf("recorded trade ID mismatch: expected %s, got %s", tr.ID, r.trades[0].ID)
	}
}

func TestRecordTradeIdempotent(t *testing.T) {
	r := NewMarketDataRecorder("BTC/USDT")
	now := time.Now().UnixNano()
	tr := makeTrade(50000, 1.5, now)
	r.RecordTrade(tr)
	// Re-recording the same trade ID should be a no-op.
	r.RecordTrade(tr)
	if len(r.trades) != 1 {
		t.Fatalf("expected 1 trade after duplicate record, got %d", len(r.trades))
	}
}

func TestRecordTradeEvictsOldTrades(t *testing.T) {
	r := NewMarketDataRecorder("BTC/USDT")
	now := time.Now().UnixNano()
	old := makeTrade(40000, 1.0, now-25*int64(time.Hour))
	recent := makeTrade(50000, 2.0, now)
	r.RecordTrade(old)
	r.RecordTrade(recent)
	// The old trade (>24h) should have been evicted from the rolling window.
	if len(r.trades) != 1 {
		t.Fatalf("expected 1 trade after eviction, got %d", len(r.trades))
	}
	if r.trades[0].ID != recent.ID {
		t.Errorf("expected recent trade to remain, got trade with ID %s", r.trades[0].ID)
	}
}

func TestTickerNoTrades(t *testing.T) {
	r := NewMarketDataRecorder("BTC/USDT")
	before := time.Now().UnixNano()
	tk := r.Ticker(nil, nil)
	after := time.Now().UnixNano()
	if tk.Pair != "BTC/USDT" {
		t.Errorf("expected pair BTC/USDT, got %s", tk.Pair)
	}
	if tk.Timestamp < before || tk.Timestamp > after {
		t.Errorf("timestamp %d not in expected range [%d, %d]", tk.Timestamp, before, after)
	}
	for _, f := range []*big.Float{
		tk.LastPrice, tk.Open24H, tk.High24H, tk.Low24H,
		tk.Volume24H, tk.QuoteVolume24H, tk.Spread, tk.Bid, tk.Ask,
	} {
		if f.Sign() != 0 {
			t.Errorf("expected zero ticker field, got %s", f.String())
		}
	}
}

func TestTickerWithTrades(t *testing.T) {
	r := NewMarketDataRecorder("BTC/USDT")
	now := time.Now().UnixNano()
	// Trades: prices 100, 200, 150 with quantities 1, 2, 3.
	r.RecordTrade(makeTrade(100, 1, now))
	r.RecordTrade(makeTrade(200, 2, now+int64(time.Second)))
	r.RecordTrade(makeTrade(150, 3, now+2*int64(time.Second)))
	tk := r.Ticker(nil, nil)
	// LastPrice = 150 (last trade).
	if tk.LastPrice.Cmp(big.NewFloat(150)) != 0 {
		t.Errorf("expected LastPrice 150, got %s", tk.LastPrice.String())
	}
	// Open24H = 100 (first trade in window).
	if tk.Open24H.Cmp(big.NewFloat(100)) != 0 {
		t.Errorf("expected Open24H 100, got %s", tk.Open24H.String())
	}
	// High24H = 200 (max price).
	if tk.High24H.Cmp(big.NewFloat(200)) != 0 {
		t.Errorf("expected High24H 200, got %s", tk.High24H.String())
	}
	// Low24H = 100 (min price).
	if tk.Low24H.Cmp(big.NewFloat(100)) != 0 {
		t.Errorf("expected Low24H 100, got %s", tk.Low24H.String())
	}
	// Volume24H = 1 + 2 + 3 = 6 (sum of quantities).
	if tk.Volume24H.Cmp(big.NewFloat(6)) != 0 {
		t.Errorf("expected Volume24H 6, got %s", tk.Volume24H.String())
	}
	// QuoteVolume24H = 100*1 + 200*2 + 150*3 = 950 (sum of price*qty).
	if tk.QuoteVolume24H.Cmp(big.NewFloat(950)) != 0 {
		t.Errorf("expected QuoteVolume24H 950, got %s", tk.QuoteVolume24H.String())
	}
}

func TestTickerSpread(t *testing.T) {
	r := NewMarketDataRecorder("BTC/USDT")
	bid := newBigFloatCopy(big.NewFloat(100))
	ask := newBigFloatCopy(big.NewFloat(105))
	tk := r.Ticker(bid, ask)
	if tk.Bid.Cmp(big.NewFloat(100)) != 0 {
		t.Errorf("expected Bid 100, got %s", tk.Bid.String())
	}
	if tk.Ask.Cmp(big.NewFloat(105)) != 0 {
		t.Errorf("expected Ask 105, got %s", tk.Ask.String())
	}
	// Spread = ask - bid = 5.
	if tk.Spread.Cmp(big.NewFloat(5)) != 0 {
		t.Errorf("expected Spread 5, got %s", tk.Spread.String())
	}
}

func TestCandlesSameBucket(t *testing.T) {
	r := NewMarketDataRecorder("BTC/USDT")
	now := time.Now().UnixNano()
	bucketStart := (now / int64(time.Minute)) * int64(time.Minute)
	// Three trades within the same 1m bucket.
	r.RecordTrade(makeTrade(100, 1, bucketStart))
	r.RecordTrade(makeTrade(200, 2, bucketStart+int64(time.Second)))
	r.RecordTrade(makeTrade(150, 3, bucketStart+2*int64(time.Second)))
	candles := r.Candles("1m", 0, 0, 0)
	if len(candles) != 1 {
		t.Fatalf("expected 1 candle for same bucket, got %d", len(candles))
	}
	c := candles[0]
	if c.Open.Cmp(big.NewFloat(100)) != 0 {
		t.Errorf("expected Open 100, got %s", c.Open.String())
	}
	if c.High.Cmp(big.NewFloat(200)) != 0 {
		t.Errorf("expected High 200, got %s", c.High.String())
	}
	if c.Low.Cmp(big.NewFloat(100)) != 0 {
		t.Errorf("expected Low 100, got %s", c.Low.String())
	}
	if c.Close.Cmp(big.NewFloat(150)) != 0 {
		t.Errorf("expected Close 150, got %s", c.Close.String())
	}
	if c.Volume.Cmp(big.NewFloat(6)) != 0 {
		t.Errorf("expected Volume 6, got %s", c.Volume.String())
	}
	if c.Timestamp != bucketStart {
		t.Errorf("expected Timestamp %d, got %d", bucketStart, c.Timestamp)
	}
}

func TestCandlesDifferentBuckets(t *testing.T) {
	r := NewMarketDataRecorder("BTC/USDT")
	now := time.Now().UnixNano()
	bucketStart := (now / int64(time.Minute)) * int64(time.Minute)
	// Trades in two different 1m buckets.
	r.RecordTrade(makeTrade(100, 1, bucketStart))
	r.RecordTrade(makeTrade(200, 2, bucketStart+int64(time.Minute)))
	candles := r.Candles("1m", 0, 0, 0)
	if len(candles) != 2 {
		t.Fatalf("expected 2 candles for different buckets, got %d", len(candles))
	}
	if candles[0].Timestamp == candles[1].Timestamp {
		t.Errorf("expected different bucket timestamps, both are %d", candles[0].Timestamp)
	}
}

func TestCandlesStartEndFilter(t *testing.T) {
	r := NewMarketDataRecorder("BTC/USDT")
	now := time.Now().UnixNano()
	bucketStart := (now / int64(time.Minute)) * int64(time.Minute)
	t1 := bucketStart
	t2 := bucketStart + int64(time.Minute)
	t3 := bucketStart + 2*int64(time.Minute)
	r.RecordTrade(makeTrade(100, 1, t1))
	r.RecordTrade(makeTrade(200, 2, t2))
	r.RecordTrade(makeTrade(300, 3, t3))
	// Filter to only the middle bucket [t2, t2].
	candles := r.Candles("1m", 0, t2, t2)
	if len(candles) != 1 {
		t.Fatalf("expected 1 candle in [t2, t2], got %d", len(candles))
	}
	if candles[0].Timestamp != t2 {
		t.Errorf("expected timestamp %d, got %d", t2, candles[0].Timestamp)
	}
}

func TestCandlesLimit(t *testing.T) {
	r := NewMarketDataRecorder("BTC/USDT")
	now := time.Now().UnixNano()
	bucketStart := (now / int64(time.Minute)) * int64(time.Minute)
	// Five candles, one per minute bucket.
	for i := 0; i < 5; i++ {
		r.RecordTrade(makeTrade(100, 1, bucketStart+int64(i)*int64(time.Minute)))
	}
	// Limit to the 2 most recent.
	candles := r.Candles("1m", 2, 0, 0)
	if len(candles) != 2 {
		t.Fatalf("expected 2 candles, got %d", len(candles))
	}
	// Returned in newest-last order, so the two most recent are at the end.
	expectedFirst := bucketStart + 3*int64(time.Minute)
	expectedLast := bucketStart + 4*int64(time.Minute)
	if candles[0].Timestamp != expectedFirst {
		t.Errorf("expected first candle timestamp %d, got %d", expectedFirst, candles[0].Timestamp)
	}
	if candles[1].Timestamp != expectedLast {
		t.Errorf("expected last candle timestamp %d, got %d", expectedLast, candles[1].Timestamp)
	}
}

func TestCandlesUnknownInterval(t *testing.T) {
	r := NewMarketDataRecorder("BTC/USDT")
	now := time.Now().UnixNano()
	r.RecordTrade(makeTrade(100, 1, now))
	candles := r.Candles("2d", 0, 0, 0)
	if candles == nil {
		t.Fatal("expected non-nil slice for unknown interval")
	}
	if len(candles) != 0 {
		t.Errorf("expected 0 candles for unknown interval, got %d", len(candles))
	}
}

func TestRecentTradesLimit(t *testing.T) {
	r := NewMarketDataRecorder("BTC/USDT")
	now := time.Now().UnixNano()
	tr1 := makeTrade(100, 1, now)
	tr2 := makeTrade(200, 2, now+int64(time.Second))
	tr3 := makeTrade(300, 3, now+2*int64(time.Second))
	r.RecordTrade(tr1)
	r.RecordTrade(tr2)
	r.RecordTrade(tr3)
	// limit = 2: two most recent, newest first.
	rt := r.RecentTrades(2)
	if len(rt) != 2 {
		t.Fatalf("expected 2 trades, got %d", len(rt))
	}
	if rt[0].ID != tr3.ID {
		t.Errorf("expected first (newest) trade to be tr3, got %s", rt[0].ID)
	}
	if rt[1].ID != tr2.ID {
		t.Errorf("expected second trade to be tr2, got %s", rt[1].ID)
	}
	// limit = 0: returns all trades.
	rt = r.RecentTrades(0)
	if len(rt) != 3 {
		t.Fatalf("expected 3 trades for limit=0, got %d", len(rt))
	}
	if rt[0].ID != tr3.ID {
		t.Errorf("expected first (newest) trade to be tr3, got %s", rt[0].ID)
	}
	// limit > len: returns all trades.
	rt = r.RecentTrades(100)
	if len(rt) != 3 {
		t.Fatalf("expected 3 trades for limit>len, got %d", len(rt))
	}
	if rt[0].ID != tr3.ID {
		t.Errorf("expected first (newest) trade to be tr3, got %s", rt[0].ID)
	}
}

func TestIntervalSeconds(t *testing.T) {
	cases := []struct {
		interval string
		expected int64
	}{
		{"1m", 60},
		{"1h", 3600},
		{"1d", 86400},
		{"2d", 0},  // unknown
		{"", 0},    // unknown
		{"abc", 0}, // unknown
	}
	for _, c := range cases {
		got := IntervalSeconds(c.interval)
		if got != c.expected {
			t.Errorf("IntervalSeconds(%q) = %d, expected %d", c.interval, got, c.expected)
		}
	}
}
