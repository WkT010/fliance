package risk

import (
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/WkT010/nexa-exchange/internal/matching"
)

// Common risk-check errors returned to API callers. These map to HTTP 400/403.
var (
	ErrPriceBandBreached   = errors.New("order price outside allowed price band")
	ErrMinNotional         = errors.New("order notional below minimum")
	ErrMaxNotional         = errors.New("order notional above maximum")
	ErrMaxPosition         = errors.New("position limit exceeded")
	ErrOrderRateLimit      = errors.New("order rate limit exceeded")
	ErrDailyVolumeLimit    = errors.New("daily order volume limit exceeded")
	ErrSelfTrade           = errors.New("self-trade prevented")
	ErrMarketOrderDisabled = errors.New("market orders disabled for this pair")
	ErrTradingSuspended    = errors.New("trading suspended for this pair")
)


// PairConfig is the per-pair trading rule set. These values are typically
// loaded from an admin configuration table at startup and refreshed periodically.
type PairConfig struct {
	Pair string

	// Minimum and maximum notional (in quote currency) for a single order.
	MinNotional *big.Float
	MaxNotional *big.Float

	// Minimum and maximum quantity (in base currency) for a single order.
	MinQty *big.Float
	MaxQty *big.Float

	// TickSize and LotSize precision enforcement.
	TickSize *big.Float
	LotSize  *big.Float

	// PriceBandPct prevents orders too far from the reference price, e.g. 0.05
	// means ±5%. A value of 0 disables the band.
	PriceBandPct *big.Float

	// Circuit breaker: if reference price moves more than this pct within the
	// cooldown window, reject new orders until manually resumed.
	CircuitBreakerPct *big.Float

	// ReferencePrice is the last known fair price (index, last trade, etc.).
	ReferencePrice *big.Float

	// Flags
	MarketOrdersEnabled bool
	TradingEnabled      bool
}

// UserLimit is the per-user risk limit.
type UserLimit struct {
	UserID string

	// Max open orders across all pairs.
	MaxOpenOrders int

	// Max order requests per rolling window.
	OrdersPerMinute int
	OrdersPerHour   int
	OrdersPerDay    int

	// Max total notional of orders (filled + open) per day, per pair.
	DailyOrderNotional map[string]*big.Float

	// Max absolute position (sum of buys - sells) per pair. A nil value means
	// no limit for that pair.
	MaxPosition map[string]*big.Float
}

// Engine performs pre-trade risk checks. It is safe for concurrent use.
type Engine struct {
	mu sync.RWMutex

	pairs  map[string]*PairConfig
	limits map[string]*UserLimit

	// orderCounts tracks per-user order timestamps for rate limiting.
	orderCounts map[string][]time.Time
	countMu     sync.Mutex

	// openOrders tracks per-user open-order count (best-effort; authoritative
	// count lives in the order book).
	openOrders map[string]int
	openMu     sync.Mutex
}

// NewEngine creates an empty risk engine.
func NewEngine() *Engine {
	return &Engine{
		pairs:       make(map[string]*PairConfig),
		limits:      make(map[string]*UserLimit),
		orderCounts: make(map[string][]time.Time),
		openOrders:  make(map[string]int),
	}
}

// SetPairConfig registers or replaces the configuration for a trading pair.
func (e *Engine) SetPairConfig(cfg *PairConfig) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pairs[cfg.Pair] = cfg
}

// GetPairConfig returns the configuration for a trading pair, or nil.
func (e *Engine) GetPairConfig(pair string) *PairConfig {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.pairs[pair]
}

// AllPairs returns a snapshot of all registered pair configurations.
func (e *Engine) AllPairs() []*PairConfig {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]*PairConfig, 0, len(e.pairs))
	for _, cfg := range e.pairs {
		out = append(out, cfg)
	}
	return out
}

// SetUserLimit registers or replaces the risk limits for a user.
func (e *Engine) SetUserLimit(ul *UserLimit) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.limits[ul.UserID] = ul
}

// GetUserLimit returns the risk limits for a user, or nil.
func (e *Engine) GetUserLimit(userID string) *UserLimit {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.limits[userID]
}

// Check runs all enabled risk checks against an order request. Returns nil if
// the order passes, otherwise one of the Err* constants above.
func (e *Engine) Check(req matching.OrderRequest) error {
	e.mu.RLock()
	pairCfg := e.pairs[req.Pair]
	e.mu.RUnlock()

	if pairCfg == nil {
		// Unknown pairs are allowed through unless an explicit config exists.
		// Most exchanges reject unknown pairs; we choose to let the order handler
		// decide so the risk engine stays optional.
		return nil
	}

	if !pairCfg.TradingEnabled {
		return ErrTradingSuspended
	}

	if req.Type == matching.Market && !pairCfg.MarketOrdersEnabled {
		return ErrMarketOrderDisabled
	}

	if err := e.checkQuantityPrecision(req, pairCfg); err != nil {
		return err
	}

	if err := e.checkPricePrecision(req, pairCfg); err != nil {
		return err
	}

	if err := e.checkNotional(req, pairCfg); err != nil {
		return err
	}

	if err := e.checkPriceBand(req, pairCfg); err != nil {
		return err
	}

	if err := e.checkUserRateLimits(req); err != nil {
		return err
	}

	if err := e.checkPositionLimit(req); err != nil {
		return err
	}

	return nil
}

func (e *Engine) checkQuantityPrecision(req matching.OrderRequest, cfg *PairConfig) error {
	if cfg.LotSize == nil || cfg.LotSize.Sign() <= 0 || req.Quantity == nil {
		return nil
	}
	q := req.Quantity
	// quantity must be a positive multiple of lot size.
	if q.Sign() <= 0 {
		return errors.New("quantity must be positive")
	}
	mod := new(big.Float).Quo(q, cfg.LotSize)
	if !isIntegral(mod) {
		return fmt.Errorf("quantity must be a multiple of lot size %s", cfg.LotSize.Text('f', -1))
	}
	if cfg.MaxQty != nil && cfg.MaxQty.Sign() > 0 && q.Cmp(cfg.MaxQty) > 0 {
		return errors.New("quantity above maximum")
	}
	if cfg.MinQty != nil && cfg.MinQty.Sign() > 0 && q.Cmp(cfg.MinQty) < 0 {
		return errors.New("quantity below minimum")
	}
	return nil
}

func (e *Engine) checkPricePrecision(req matching.OrderRequest, cfg *PairConfig) error {
	if cfg.TickSize == nil || cfg.TickSize.Sign() <= 0 || req.Price == nil || req.Price.Sign() <= 0 {
		return nil
	}
	mod := new(big.Float).Quo(req.Price, cfg.TickSize)
	if !isIntegral(mod) {
		return fmt.Errorf("price must be a multiple of tick size %s", cfg.TickSize.Text('f', -1))
	}
	return nil
}

func (e *Engine) checkNotional(req matching.OrderRequest, cfg *PairConfig) error {
	notional := req.Notional()
	if notional == nil || notional.Sign() <= 0 {
		return errors.New("invalid notional")
	}
	if cfg.MinNotional != nil && cfg.MinNotional.Sign() > 0 && notional.Cmp(cfg.MinNotional) < 0 {
		return ErrMinNotional
	}
	if cfg.MaxNotional != nil && cfg.MaxNotional.Sign() > 0 && notional.Cmp(cfg.MaxNotional) > 0 {
		return ErrMaxNotional
	}
	return nil
}

func (e *Engine) checkPriceBand(req matching.OrderRequest, cfg *PairConfig) error {
	if cfg.PriceBandPct == nil || cfg.PriceBandPct.Sign() <= 0 || cfg.ReferencePrice == nil || cfg.ReferencePrice.Sign() <= 0 {
		return nil
	}
	if req.Price == nil || req.Price.Sign() <= 0 {
		return nil // market orders skip price band
	}
	band := new(big.Float).Mul(cfg.ReferencePrice, cfg.PriceBandPct)
	lower := new(big.Float).Sub(cfg.ReferencePrice, band)
	upper := new(big.Float).Add(cfg.ReferencePrice, band)
	if req.Price.Cmp(lower) < 0 || req.Price.Cmp(upper) > 0 {
		return ErrPriceBandBreached
	}
	return nil
}

func (e *Engine) checkUserRateLimits(req matching.OrderRequest) error {
	e.mu.RLock()
	limit := e.limits[req.UserID]
	e.mu.RUnlock()
	if limit == nil {
		return nil
	}

	e.countMu.Lock()
	defer e.countMu.Unlock()
	now := time.Now()
	ts := e.orderCounts[req.UserID]
	// prune old entries
	cutoff := now.Add(-24 * time.Hour)
	j := 0
	for _, t := range ts {
		if t.After(cutoff) {
			ts[j] = t
			j++
		}
	}
	ts = ts[:j]

	if limit.OrdersPerDay > 0 && len(ts) >= limit.OrdersPerDay {
		return ErrDailyVolumeLimit
	}

	hourCount := 0
	minuteCount := 0
	hourCutoff := now.Add(-time.Hour)
	minuteCutoff := now.Add(-time.Minute)
	for _, t := range ts {
		if t.After(hourCutoff) {
			hourCount++
		}
		if t.After(minuteCutoff) {
			minuteCount++
		}
	}
	if limit.OrdersPerHour > 0 && hourCount >= limit.OrdersPerHour {
		return ErrOrderRateLimit
	}
	if limit.OrdersPerMinute > 0 && minuteCount >= limit.OrdersPerMinute {
		return ErrOrderRateLimit
	}

	ts = append(ts, now)
	e.orderCounts[req.UserID] = ts
	return nil
}

func (e *Engine) checkPositionLimit(req matching.OrderRequest) error {
	e.mu.RLock()
	limit := e.limits[req.UserID]
	e.mu.RUnlock()
	if limit == nil || limit.MaxPosition == nil {
		return nil
	}
	maxPos, ok := limit.MaxPosition[req.Pair]
	if !ok || maxPos == nil || maxPos.Sign() <= 0 {
		return nil
	}
	// Best-effort position check: we don't know the user's real position here,
	// so this is a coarse guard. The wallet service owns the authoritative
	// balance/position; this check catches obvious abuse.
	qty := req.Quantity
	if qty == nil {
		return nil
	}
	if qty.Cmp(maxPos) > 0 {
		return ErrMaxPosition
	}
	return nil
}

// RecordOpen increments the open-order counter for a user.
func (e *Engine) RecordOpen(userID string) {
	e.openMu.Lock()
	defer e.openMu.Unlock()
	e.openOrders[userID]++
}

// RecordClose decrements the open-order counter for a user.
func (e *Engine) RecordClose(userID string) {
	e.openMu.Lock()
	defer e.openMu.Unlock()
	if e.openOrders[userID] > 0 {
		e.openOrders[userID]--
	}
}

// OpenOrders returns the best-effort open-order count for a user.
func (e *Engine) OpenOrders(userID string) int {
	e.openMu.Lock()
	defer e.openMu.Unlock()
	return e.openOrders[userID]
}

// UpdateReferencePrice sets the fair reference price for a pair. Called by the
// market-data service or matching engine after each trade.
func (e *Engine) UpdateReferencePrice(pair string, price *big.Float) {
	e.mu.Lock()
	defer e.mu.Unlock()
	cfg := e.pairs[pair]
	if cfg == nil {
		cfg = &PairConfig{Pair: pair}
		e.pairs[pair] = cfg
	}
	cfg.ReferencePrice = newBigFloatCopy(price)
}

func isIntegral(f *big.Float) bool {
	if f == nil {
		return false
	}
	i, acc := f.Int(nil)
	return acc == big.Exact && new(big.Float).SetInt(i).Cmp(f) == 0
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
