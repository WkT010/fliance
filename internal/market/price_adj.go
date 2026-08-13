package market

import (
	"database/sql"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"
)

// PriceAdjustment is an operator-controlled price correction for one pair:
// adjusted = raw*Multiplier + Offset. It only affects prices served to
// clients (tickers); underlying pool reserves and external feeds are never
// modified.
type PriceAdjustment struct {
	Pair       string
	Multiplier *big.Float
	Offset     *big.Float
	UpdatedBy  string
	UpdatedAt  int64
}

// isIdentity reports whether the adjustment changes nothing.
func (a *PriceAdjustment) isIdentity() bool {
	if a == nil {
		return true
	}
	multOne := a.Multiplier == nil || a.Multiplier.Cmp(big.NewFloat(1)) == 0
	offZero := a.Offset == nil || a.Offset.Sign() == 0
	return multOne && offZero
}

// PriceAdjuster keeps the per-pair adjustments in memory (fast read path on
// every ticker request) with optional Postgres persistence (migration 010's
// price_adjustments table). A nil db degrades to memory-only.
type PriceAdjuster struct {
	mu   sync.RWMutex
	db   *sql.DB
	adjs map[string]*PriceAdjustment
}

// NewPriceAdjuster creates an adjuster; db may be nil (memory-only).
func NewPriceAdjuster(db *sql.DB) *PriceAdjuster {
	return &PriceAdjuster{db: db, adjs: make(map[string]*PriceAdjustment)}
}

// normalizePair canonicalises a pair key: the admin API accepts URL-safe
// dash form (BTC-USDT) while internal tickers use the slash form
// (BTC/USDT), so both spellings must hit the same adjustment entry.
func normalizePair(pair string) string {
	return strings.ToUpper(strings.ReplaceAll(pair, "-", "/"))
}

// LoadAll (re)loads every adjustment from the database into memory. Called
// at boot and safe to call after out-of-band changes. Missing table or nil
// db degrade to an empty set.
func (p *PriceAdjuster) LoadAll() error {
	if p.db == nil {
		return nil
	}
	rows, err := p.db.Query(
		`SELECT pair, multiplier, "offset", COALESCE(updated_by,''), COALESCE(updated_at,0) FROM price_adjustments`)
	if err != nil {
		return fmt.Errorf("price adjustments load: %w", err)
	}
	defer rows.Close()
	loaded := make(map[string]*PriceAdjustment)
	for rows.Next() {
		var pair, multStr, offStr string
		var updatedBy string
		var updatedAt int64
		if err := rows.Scan(&pair, &multStr, &offStr, &updatedBy, &updatedAt); err != nil {
			return fmt.Errorf("price adjustments scan: %w", err)
		}
		adj := &PriceAdjustment{Pair: pair, UpdatedBy: updatedBy, UpdatedAt: updatedAt}
		if m, ok := new(big.Float).SetString(multStr); ok {
			adj.Multiplier = m
		} else {
			adj.Multiplier = big.NewFloat(1)
		}
		if o, ok := new(big.Float).SetString(offStr); ok {
			adj.Offset = o
		} else {
			adj.Offset = big.NewFloat(0)
		}
		loaded[normalizePair(pair)] = adj
	}
	if err := rows.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	p.adjs = loaded
	p.mu.Unlock()
	return nil
}

// Upsert stores an adjustment (memory + database when available).
func (p *PriceAdjuster) Upsert(adj *PriceAdjustment) error {
	if adj == nil || adj.Pair == "" {
		return fmt.Errorf("pair required")
	}
	adj.Pair = normalizePair(adj.Pair)
	if adj.Multiplier == nil {
		adj.Multiplier = big.NewFloat(1)
	}
	if adj.Offset == nil {
		adj.Offset = big.NewFloat(0)
	}
	if adj.UpdatedAt == 0 {
		adj.UpdatedAt = time.Now().UnixNano()
	}
	if p.db != nil {
		if _, err := p.db.Exec(
			`INSERT INTO price_adjustments (pair, multiplier, "offset", updated_by, updated_at)
			 VALUES ($1,$2,$3,$4,$5)
			 ON CONFLICT (pair) DO UPDATE
			   SET multiplier=EXCLUDED.multiplier, "offset"=EXCLUDED."offset",
			       updated_by=EXCLUDED.updated_by, updated_at=EXCLUDED.updated_at`,
			adj.Pair, adj.Multiplier.Text('f', 8), adj.Offset.Text('f', 18), adj.UpdatedBy, adj.UpdatedAt); err != nil {
			return fmt.Errorf("price adjustment upsert: %w", err)
		}
	}
	p.mu.Lock()
	cp := *adj
	cp.Multiplier = new(big.Float).Copy(adj.Multiplier)
	cp.Offset = new(big.Float).Copy(adj.Offset)
	p.adjs[adj.Pair] = &cp
	p.mu.Unlock()
	return nil
}

// Get returns a copy of the adjustment for a pair (nil when none).
func (p *PriceAdjuster) Get(pair string) *PriceAdjustment {
	p.mu.RLock()
	adj := p.adjs[normalizePair(pair)]
	p.mu.RUnlock()
	if adj == nil {
		return nil
	}
	cp := *adj
	cp.Multiplier = new(big.Float).Copy(adj.Multiplier)
	cp.Offset = new(big.Float).Copy(adj.Offset)
	return &cp
}

// All returns copies of every stored adjustment.
func (p *PriceAdjuster) All() []*PriceAdjustment {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]*PriceAdjustment, 0, len(p.adjs))
	for _, adj := range p.adjs {
		cp := *adj
		cp.Multiplier = new(big.Float).Copy(adj.Multiplier)
		cp.Offset = new(big.Float).Copy(adj.Offset)
		out = append(out, &cp)
	}
	return out
}

// ApplyTicker returns the adjusted view of a ticker WITHOUT mutating it:
// cached tickers are shared across requests, so a fresh copy carries the
// adjustment. Identity adjustments (or none) return the original pointer,
// keeping the common path allocation-free.
func (p *PriceAdjuster) ApplyTicker(t *Ticker) *Ticker {
	if p == nil || t == nil {
		return t
	}
	p.mu.RLock()
	adj := p.adjs[normalizePair(t.Pair)]
	p.mu.RUnlock()
	if adj.isIdentity() {
		return t
	}
	cp := *t
	cp.Last = adjVal(t.Last, adj)
	cp.Bid = adjVal(t.Bid, adj)
	cp.Ask = adjVal(t.Ask, adj)
	cp.High24h = adjVal(t.High24h, adj)
	cp.Low24h = adjVal(t.Low24h, adj)
	cp.Open24h = adjVal(t.Open24h, adj)
	// Re-derive the spread/change fields from the adjusted values so the
	// numbers stay internally consistent.
	if cp.Bid != nil && cp.Ask != nil {
		cp.Spread = new(big.Float).Sub(cp.Ask, cp.Bid)
	}
	if cp.Last != nil && cp.Open24h != nil {
		cp.Change24h = new(big.Float).Sub(cp.Last, cp.Open24h)
		if cp.Open24h.Sign() != 0 {
			cp.ChangePct24h = new(big.Float).Quo(cp.Change24h, cp.Open24h)
		}
	}
	return &cp
}

// adjVal applies v*mult + offset (nil-safe).
func adjVal(v *big.Float, adj *PriceAdjustment) *big.Float {
	if v == nil {
		return nil
	}
	out := new(big.Float)
	if adj.Multiplier != nil {
		out.Mul(v, adj.Multiplier)
	} else {
		out.Set(v)
	}
	if adj.Offset != nil && adj.Offset.Sign() != 0 {
		out.Add(out, adj.Offset)
	}
	return out
}
