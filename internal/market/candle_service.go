package market

import (
	"database/sql"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/WkT010/nexa-exchange/internal/matching"
)

// CandleStore persists OHLCV aggregates. The default implementation is
// PostgreSQL; a memory-backed implementation is provided for tests.
type CandleStore interface {
	SaveCandle(c *matching.Candle) error
	GetCandles(pair, interval string, start, end int64, limit int) ([]*matching.Candle, error)
}

// PGCandleStore is a PostgreSQL-backed CandleStore.
type PGCandleStore struct{ db *sql.DB }

// NewPGCandleStore creates a PostgreSQL candle store.
func NewPGCandleStore(db *sql.DB) *PGCandleStore { return &PGCandleStore{db: db} }

// SaveCandle upserts a candle row.
func (s *PGCandleStore) SaveCandle(c *matching.Candle) error {
	if c == nil {
		return nil
	}
	_, err := s.db.Exec(`
		INSERT INTO candles (pair, interval, timestamp, open, high, low, close, volume, close_time)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (pair, interval, timestamp) DO UPDATE SET
			open = EXCLUDED.open,
			high = EXCLUDED.high,
			low = EXCLUDED.low,
			close = EXCLUDED.close,
			volume = EXCLUDED.volume,
			close_time = EXCLUDED.close_time
	`, c.Pair, c.Interval, c.Timestamp, text(c.Open), text(c.High), text(c.Low), text(c.Close), text(c.Volume), c.CloseTime)
	return err
}

// SaveCandles upserts a batch of candles in a single statement (chunked),
// avoiding one remote round-trip per row during backfills.
func (s *PGCandleStore) SaveCandles(cs []*matching.Candle) error {
	rows := make([]*matching.Candle, 0, len(cs))
	for _, c := range cs {
		if c != nil {
			rows = append(rows, c)
		}
	}
	const chunk = 100
	for i := 0; i < len(rows); i += chunk {
		end := i + chunk
		if end > len(rows) {
			end = len(rows)
		}
		batch := rows[i:end]
		var sb strings.Builder
		sb.WriteString(`INSERT INTO candles (pair, interval, timestamp, open, high, low, close, volume, close_time) VALUES `)
		args := make([]interface{}, 0, len(batch)*9)
		for j, c := range batch {
			if j > 0 {
				sb.WriteString(", ")
			}
			base := j * 9
			sb.WriteString(fmt.Sprintf("($%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d)",
				base+1, base+2, base+3, base+4, base+5, base+6, base+7, base+8, base+9))
			args = append(args, c.Pair, c.Interval, c.Timestamp, text(c.Open), text(c.High), text(c.Low), text(c.Close), text(c.Volume), c.CloseTime)
		}
		sb.WriteString(` ON CONFLICT (pair, interval, timestamp) DO UPDATE SET
			open = EXCLUDED.open,
			high = EXCLUDED.high,
			low = EXCLUDED.low,
			close = EXCLUDED.close,
			volume = EXCLUDED.volume,
			close_time = EXCLUDED.close_time`)
		if _, err := s.db.Exec(sb.String(), args...); err != nil {
			return err
		}
	}
	return nil
}

// GetCandles returns candles ordered by timestamp ascending.
func (s *PGCandleStore) GetCandles(pair, interval string, start, end int64, limit int) ([]*matching.Candle, error) {
	q := `SELECT pair, interval, timestamp, open, high, low, close, volume, close_time FROM candles WHERE pair=$1 AND interval=$2`
	args := []interface{}{pair, interval}
	n := 3
	if start > 0 {
		q += fmt.Sprintf(" AND timestamp >= $%d", n)
		args = append(args, start)
		n++
	}
	if end > 0 {
		q += fmt.Sprintf(" AND timestamp <= $%d", n)
		args = append(args, end)
		n++
	}
	q += " ORDER BY timestamp ASC"
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT $%d", n)
		args = append(args, limit)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*matching.Candle
	for rows.Next() {
		c := &matching.Candle{Open: new(big.Float), High: new(big.Float), Low: new(big.Float), Close: new(big.Float), Volume: new(big.Float)}
		var o, h, l, cl, v string
		if err := rows.Scan(&c.Pair, &c.Interval, &c.Timestamp, &o, &h, &l, &cl, &v, &c.CloseTime); err != nil {
			return nil, err
		}
		c.Open.Parse(o, 10)
		c.High.Parse(h, 10)
		c.Low.Parse(l, 10)
		c.Close.Parse(cl, 10)
		c.Volume.Parse(v, 10)
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// MemoryCandleStore is an in-memory CandleStore for tests.
type MemoryCandleStore struct {
	mu      sync.RWMutex
	candles []*matching.Candle
}

// NewMemoryCandleStore creates an in-memory candle store.
func NewMemoryCandleStore() *MemoryCandleStore { return &MemoryCandleStore{} }

// SaveCandles stores a batch of candles.
func (m *MemoryCandleStore) SaveCandles(cs []*matching.Candle) error {
	for _, c := range cs {
		if err := m.SaveCandle(c); err != nil {
			return err
		}
	}
	return nil
}

// SaveCandle stores a candle.
func (m *MemoryCandleStore) SaveCandle(c *matching.Candle) error {
	if c == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, existing := range m.candles {
		if existing.Pair == c.Pair && existing.Interval == c.Interval && existing.Timestamp == c.Timestamp {
			m.candles[i] = c
			return nil
		}
	}
	m.candles = append(m.candles, c)
	return nil
}

// GetCandles returns matching candles.
func (m *MemoryCandleStore) GetCandles(pair, interval string, start, end int64, limit int) ([]*matching.Candle, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*matching.Candle
	for _, c := range m.candles {
		if c.Pair != pair || c.Interval != interval {
			continue
		}
		if start > 0 && c.Timestamp < start {
			continue
		}
		if end > 0 && c.Timestamp > end {
			continue
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Timestamp < out[j].Timestamp })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// CandleService consumes trades from the matching engine and persists candles.
type CandleService struct {
	store CandleStore
	mu    sync.Mutex
	// lastCandle caches the current open candle per (pair, interval) to avoid
	// DB round-trips on every trade.
	lastCandle map[string]*matching.Candle
}

// NewCandleService creates a candle service.
func NewCandleService(store CandleStore) *CandleService {
	return &CandleService{
		store:      store,
		lastCandle: make(map[string]*matching.Candle),
	}
}

// RecordTrade updates candles for a trade.
func (s *CandleService) RecordTrade(t *matching.Trade) error {
	if t == nil || t.Price == nil || t.Quantity == nil {
		return nil
	}
	for _, interval := range matching.IntervalNames() {
		secs := matching.IntervalSeconds(interval)
		if secs == 0 {
			continue
		}
		if err := s.updateCandle(t, interval, secs); err != nil {
			return err
		}
	}
	return nil
}

func (s *CandleService) updateCandle(t *matching.Trade, interval string, secs int64) error {
	nsPerBucket := secs * int64(time.Second)
	ts := (t.CreatedAt / nsPerBucket) * nsPerBucket
	closeTime := ts + nsPerBucket - 1
	key := t.Pair + ":" + interval

	s.mu.Lock()
	c, ok := s.lastCandle[key]
	if !ok || c == nil || c.Timestamp != ts {
		// Start a new candle. We could load from DB here; for simplicity we
		// seed from the trade price.
		c = &matching.Candle{
			Pair:      t.Pair,
			Interval:  interval,
			Open:      newBigFloatCopy(t.Price),
			High:      newBigFloatCopy(t.Price),
			Low:       newBigFloatCopy(t.Price),
			Close:     newBigFloatCopy(t.Price),
			Volume:    newBigFloatCopy(t.Quantity),
			Timestamp: ts,
			CloseTime: closeTime,
		}
		s.lastCandle[key] = c
	} else {
		if t.Price.Cmp(c.High) > 0 {
			c.High = newBigFloatCopy(t.Price)
		}
		if t.Price.Cmp(c.Low) < 0 {
			c.Low = newBigFloatCopy(t.Price)
		}
		c.Close = newBigFloatCopy(t.Price)
		c.Volume.Add(c.Volume, t.Quantity)
		c.CloseTime = closeTime
	}
	s.mu.Unlock()

	if s.store != nil {
		return s.store.SaveCandle(c)
	}
	return nil
}

// Candles returns stored candles for a pair/interval window.
func (s *CandleService) Candles(pair, interval string, start, end int64, limit int) ([]*matching.Candle, error) {
	if s.store == nil {
		return []*matching.Candle{}, nil
	}
	return s.store.GetCandles(pair, interval, start, end, limit)
}

// SaveCandle persists a pre-built candle (e.g. backfilled from Binance
// klines). It bypasses the lastCandle trade-aggregation cache on purpose.
func (s *CandleService) SaveCandle(c *matching.Candle) error {
	if s.store == nil || c == nil {
		return nil
	}
	return s.store.SaveCandle(c)
}

// SaveCandles upserts a batch, delegating to the store's batch path when
// available so backfills avoid one round-trip per row.
func (s *CandleService) SaveCandles(cs []*matching.Candle) error {
	if s.store == nil || len(cs) == 0 {
		return nil
	}
	if batcher, ok := s.store.(interface{ SaveCandles([]*matching.Candle) error }); ok {
		return batcher.SaveCandles(cs)
	}
	for _, c := range cs {
		if err := s.store.SaveCandle(c); err != nil {
			return err
		}
	}
	return nil
}

func newBigFloatCopy(f *big.Float) *big.Float {
	if f == nil {
		return nil
	}
	x := new(big.Float)
	x.SetPrec(128)
	x.Set(f)
	return x
}

func text(f *big.Float) string {
	if f == nil {
		return "0"
	}
	return f.Text('f', 18)
}
