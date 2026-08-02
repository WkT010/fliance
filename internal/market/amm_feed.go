package market

import (
	"fmt"
	"math/big"
	"math/rand"
	"sync"
	"time"

	"github.com/WkT010/nexa-exchange/internal/amm"
	"github.com/WkT010/nexa-exchange/internal/matching"
)

// AMMService is the subset of amm.Service the feed needs. Declared here so the
// market package depends on a small interface, not the concrete service, and so
// the feed can be unit-tested with a fake.
type AMMService interface {
	ListPools() ([]*amm.Pool, error)
	GetPoolByPair(pair string) (*amm.Pool, error)
	SavePoolReserves(pool *amm.Pool) error
	SaveSwapRecord(sw *amm.Swap) error
}

// TradeRecorder consumes simulated trades so they can be aggregated into
// candles / 24h stats by an external service (e.g. CandleService). The feed
// calls RecordTrade best-effort on every simulated swap; a nil/error return is
// logged and never blocks the simulator.
type TradeRecorder interface {
	RecordTrade(t *matching.Trade) error
}

// poolState holds the per-pair rolling stats the feed derives from AMM reserves.
type poolState struct {
	pool      *amm.Pool
	seedPrice *big.Float // mid at load time; simulator mean-reverts toward this
	open24h   *big.Float
	high24h   *big.Float
	low24h    *big.Float
	volume24h *big.Float // base-asset volume accumulated since load
	trades    []RecentTrade
}

// AMMPriceFeed is a fully self-contained price source: every ticker / depth /
// trade it exposes is derived from AMM pool reserves. A background simulator
// (see amm_simulator.go) periodically perturbs the pools with small swaps so
// prices move even when no real trader is active. With no external dependency,
// the exchange still shows a live market when cut off from Binance/Uniswap.
type AMMPriceFeed struct {
	svc      AMMService
	rng      *rand.Rand
	mu       sync.RWMutex
	pools    map[string]*poolState
	recorder TradeRecorder // optional; receives every simulated trade for candles
}

// NewAMMPriceFeed constructs an empty feed. Call Reload() (or have the simulator
// do it on its first tick) to load pools from the AMM store.
func NewAMMPriceFeed(svc AMMService) *AMMPriceFeed {
	return &AMMPriceFeed{
		svc:   svc,
		rng:   rand.New(rand.NewSource(time.Now().UnixNano())),
		pools: make(map[string]*poolState),
	}
}

// SetTradeRecorder wires a candle/24h-stats recorder. Once set, every simulated
// swap also feeds a *matching.Trade to the recorder so /api/v2/klines and the
// matching engine's market-data stats reflect simulator activity. Safe to call
// once at startup; not safe to swap concurrently with ApplySimulatedSwap.
func (f *AMMPriceFeed) SetTradeRecorder(r TradeRecorder) { f.recorder = r }

// Reload refreshes the in-memory pool state from the AMM store. Existing 24h
// stats are preserved; pools not yet tracked are initialized with the current
// mid as their seed/open/high/low. Safe to call repeatedly.
func (f *AMMPriceFeed) Reload() error {
	pools, err := f.svc.ListPools()
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range pools {
		mid := poolMidPrice(p)
		if mid == nil || mid.Sign() <= 0 {
			continue
		}
		st, ok := f.pools[p.Pair]
		if !ok || st == nil {
			f.pools[p.Pair] = &poolState{
				pool:      p,
				seedPrice: new(big.Float).Copy(mid),
				open24h:   new(big.Float).Copy(mid),
				high24h:   new(big.Float).Copy(mid),
				low24h:     new(big.Float).Copy(mid),
				volume24h: big.NewFloat(0),
			}
			continue
		}
		// Refresh reserves (a real user swap/liquidity change may have moved them)
		// while keeping the rolling 24h stats.
		st.pool = p
		if mid.Cmp(st.high24h) > 0 {
			st.high24h = new(big.Float).Copy(mid)
		}
		if st.low24h.Sign() == 0 || mid.Cmp(st.low24h) < 0 {
			st.low24h = new(big.Float).Copy(mid)
		}
	}
	return nil
}

// poolMidPrice returns reserve1/reserve0 (quote per base) for a constant-product
// pool, or nil if reserves are not positive.
func poolMidPrice(p *amm.Pool) *big.Float {
	if p == nil || p.Reserve0 == nil || p.Reserve0.Sign() <= 0 || p.Reserve1 == nil || p.Reserve1.Sign() <= 0 {
		return nil
	}
	return new(big.Float).Quo(p.Reserve1, p.Reserve0)
}

// Price returns the current mid for a pair (quote per base), or nil if unknown.
func (f *AMMPriceFeed) Price(pair string) *big.Float {
	f.mu.RLock()
	defer f.mu.RUnlock()
	st := f.pools[pair]
	if st == nil {
		return nil
	}
	return poolMidPrice(st.pool)
}

// Get returns a snapshot ticker for a pair, or nil if the pair has no pool.
func (f *AMMPriceFeed) Get(pair string) *Ticker {
	f.mu.RLock()
	st := f.pools[pair]
	if st == nil {
		f.mu.RUnlock()
		return nil
	}
	t := f.tickerFromState(st)
	f.mu.RUnlock()
	return t
}

// GetAll returns snapshot tickers for every known pool.
func (f *AMMPriceFeed) GetAll() map[string]*Ticker {
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make(map[string]*Ticker, len(f.pools))
	for pair, st := range f.pools {
		out[pair] = f.tickerFromState(st)
	}
	return out
}

// FetchTicker / FetchAllTickers mirror the BinancePriceFeed signatures so the
// PriceHandler can treat the AMM feed as a drop-in source.
func (f *AMMPriceFeed) FetchTicker(pair string) (*Ticker, error) {
	if t := f.Get(pair); t != nil {
		return t, nil
	}
	return nil, fmt.Errorf("amm: no pool for %s", pair)
}

func (f *AMMPriceFeed) FetchAllTickers() (map[string]*Ticker, error) {
	all := f.GetAll()
	if len(all) == 0 {
		return nil, fmt.Errorf("amm: no pools loaded")
	}
	return all, nil
}

// tickerFromState builds a Ticker from a poolState. Caller must hold f.mu (or
// its read lock).
func (f *AMMPriceFeed) tickerFromState(st *poolState) *Ticker {
	mid := poolMidPrice(st.pool)
	if mid == nil {
		return &Ticker{Pair: st.pool.Pair, Volume24h: new(big.Float), Timestamp: time.Now().UnixMilli()}
	}
	fee := big.NewFloat(0.003)
	if st.pool.FeeRate != nil && st.pool.FeeRate.Sign() > 0 {
		fee = st.pool.FeeRate
	}
	bid := new(big.Float).Mul(mid, new(big.Float).Sub(big.NewFloat(1), fee))
	ask := new(big.Float).Mul(mid, new(big.Float).Add(big.NewFloat(1), fee))
	spread := new(big.Float).Sub(ask, bid)
	change := new(big.Float).Sub(mid, st.open24h)
	pct := new(big.Float)
	if st.open24h.Sign() > 0 {
		pct.Quo(change, st.open24h)
	}
	return &Ticker{
		Pair:          st.pool.Pair,
		Last:          mid,
		Bid:           bid,
		Ask:           ask,
		Spread:        spread,
		Volume24h:     new(big.Float).Copy(st.volume24h),
		QuoteVolume24h: new(big.Float).Mul(st.volume24h, mid),
		High24h:       new(big.Float).Copy(st.high24h),
		Low24h:        new(big.Float).Copy(st.low24h),
		Open24h:       new(big.Float).Copy(st.open24h),
		Change24h:     change,
		ChangePct24h:  pct,
		Timestamp:     time.Now().UnixMilli(),
	}
}

// FetchDepth synthesizes a limit-order-book-style depth ladder around the AMM
// mid price. AMMs have no discrete levels, so we generate `limit` rungs each
// side at increasing distance from mid with tapering quantity — enough for the
// order-book UI to render a realistic book whose spread matches the pool fee.
func (f *AMMPriceFeed) FetchDepth(pair string, limit int) (*Depth, error) {
	f.mu.RLock()
	st := f.pools[pair]
	f.mu.RUnlock()
	if st == nil {
		return nil, fmt.Errorf("amm: no pool for %s", pair)
	}
	mid := poolMidPrice(st.pool)
	if mid == nil {
		return nil, fmt.Errorf("amm: empty pool %s", pair)
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	fee := big.NewFloat(0.003)
	if st.pool.FeeRate != nil && st.pool.FeeRate.Sign() > 0 {
		fee = st.pool.FeeRate
	}
	// Each rung steps out by half the fee; quantity tapers linearly so deeper
	// levels show less size (mirrors a real book thinning away from mid).
	bids := make([]DepthLevel, 0, limit)
	asks := make([]DepthLevel, 0, limit)
	baseReserve := st.pool.Reserve0
	if baseReserve == nil || baseReserve.Sign() <= 0 {
		baseReserve = big.NewFloat(1)
	}
	one := big.NewFloat(1)
	for i := 0; i < limit; i++ {
		// distance factor: (i+1) * fee/2
		dist := new(big.Float).Mul(fee, big.NewFloat(float64(i+1)*0.5))
		qtyFrac := new(big.Float).Mul(baseReserve, big.NewFloat(0.0008))
		qtyFrac.Mul(qtyFrac, new(big.Float).Sub(one, big.NewFloat(float64(i)/float64(2*limit))))
		if qtyFrac.Sign() <= 0 {
			qtyFrac = new(big.Float).Mul(baseReserve, big.NewFloat(0.00005))
		}
		bidPrice := new(big.Float).Mul(mid, new(big.Float).Sub(one, dist))
		askPrice := new(big.Float).Mul(mid, new(big.Float).Add(one, dist))
		bids = append(bids, DepthLevel{Price: bidPrice, Quantity: new(big.Float).Copy(qtyFrac)})
		asks = append(asks, DepthLevel{Price: askPrice, Quantity: new(big.Float).Copy(qtyFrac)})
	}
	return &Depth{Pair: pair, Bids: bids, Asks: asks}, nil
}

// FetchRecentTrades returns the most recent simulated trades (newest first).
func (f *AMMPriceFeed) FetchRecentTrades(pair string, limit int) ([]RecentTrade, error) {
	f.mu.RLock()
	st := f.pools[pair]
	f.mu.RUnlock()
	if st == nil {
		return nil, fmt.Errorf("amm: no pool for %s", pair)
	}
	if limit <= 0 || limit > 1000 {
		limit = 50
	}
	n := len(st.trades)
	if n == 0 {
		return []RecentTrade{}, nil
	}
	start := n - limit
	if start < 0 {
		start = 0
	}
	// Newest first.
	out := make([]RecentTrade, 0, n-start)
	for i := n - 1; i >= start; i-- {
		out = append(out, st.trades[i])
	}
	return out, nil
}

// ApplySimulatedSwap runs one small swap against the pool for `pair`, persists
// the new reserves, records the swap, and updates rolling 24h stats + the recent
// trade tape. Called by the simulator on each tick. It reloads the pool from the
// AMM store first so real-user swaps/liquidity changes are picked up.
//
// direction: 1 = buy (taker acquires base, spends quote); -1 = sell (taker
// acquires quote, spends base). If direction is 0, a weighted random direction
// biased toward mean-reversion is chosen.
func (f *AMMPriceFeed) ApplySimulatedSwap(pair string, direction int) error {
	f.mu.Lock()
	// Reload the pool from the store so a real swap that landed since the last
	// tick is reflected before we perturb further.
	p, err := f.svc.GetPoolByPair(pair)
	if err != nil || p == nil {
		f.mu.Unlock()
		return fmt.Errorf("amm: pool %s not found: %v", pair, err)
	}
	st, ok := f.pools[pair]
	if !ok || st == nil {
		f.mu.Unlock()
		return fmt.Errorf("amm: pool %s not tracked", pair)
	}
	st.pool = p

	mid := poolMidPrice(p)
	if mid == nil || mid.Sign() <= 0 {
		f.mu.Unlock()
		return fmt.Errorf("amm: empty pool %s", pair)
	}

	dir := direction
	if dir == 0 {
		// Mean-reverting bias: if price above seed, lean toward selling base
		// (which pushes price down); if below, lean toward buying.
		if mid.Cmp(st.seedPrice) > 0 {
			dir = -1
		} else {
			dir = 1
		}
		// 30% chance to override with pure random walk for liveliness.
		if f.rng.Intn(10) < 3 {
			dir = 1 - 2*f.rng.Intn(2)
		}
	}

	// Trade size: 0.05%–0.5% of base reserve — small enough to move price slowly.
	sizeFrac := 0.0005 + f.rng.Float64()*0.0045
	tradeSize := new(big.Float).Mul(p.Reserve0, big.NewFloat(sizeFrac))

	var tokenIn, tokenOut string
	var amountIn, execPrice *big.Float
	if dir == 1 {
		// Buy base: spend quote, receive base.
		tokenIn, tokenOut = p.Token1, p.Token0
		amountIn = new(big.Float).Mul(tradeSize, mid)
	} else {
		// Sell base: spend base, receive quote.
		tokenIn, tokenOut = p.Token0, p.Token1
		amountIn = new(big.Float).Copy(tradeSize)
	}

	amountOut, _, _, err := amm.QuoteSwap(p, tokenIn, amountIn)
	if err != nil {
		f.mu.Unlock()
		return err
	}
	amm.ApplySwap(p, tokenIn, amountIn, amountOut)

	// Effective trade price (quote per base).
	if dir == 1 {
		// bought `amountOut` base for `amountIn` quote
		execPrice = new(big.Float).Quo(amountIn, amountOut)
	} else {
		// sold `amountIn` base for `amountOut` quote
		execPrice = new(big.Float).Quo(amountOut, amountIn)
	}

	// Persist moved reserves.
	if err := f.svc.SavePoolReserves(p); err != nil {
		f.mu.Unlock()
		return fmt.Errorf("amm: persist reserves %s: %w", pair, err)
	}

	// Record swap (best-effort; failure doesn't break the price feed).
	_ = f.svc.SaveSwapRecord(&amm.Swap{
		PoolID:    p.ID,
		TokenIn:   tokenIn,
		TokenOut:  tokenOut,
		AmountIn:  new(big.Float).Copy(amountIn),
		AmountOut: new(big.Float).Copy(amountOut),
	})

	// Update rolling stats.
	newMid := poolMidPrice(p)
	if newMid != nil {
		if newMid.Cmp(st.high24h) > 0 {
			st.high24h = new(big.Float).Copy(newMid)
		}
		if st.low24h.Sign() == 0 || newMid.Cmp(st.low24h) < 0 {
			st.low24h = new(big.Float).Copy(newMid)
		}
	}
	st.volume24h.Add(st.volume24h, tradeSize)

	// Append to recent trade tape (cap at 100).
	isBuyer := dir == 1
	tradeTimeMs := time.Now().UnixMilli()
	st.trades = append(st.trades, RecentTrade{
		Pair:     pair,
		Price:    execPrice,
		Quantity: tradeSize,
		Time:     tradeTimeMs,
		IsBuyer:  isBuyer,
	})
	if len(st.trades) > 100 {
		st.trades = st.trades[len(st.trades)-100:]
	}
	// Capture the recorder reference under the lock; emit after unlock so a
	// slow DB write in RecordTrade never blocks other readers of the feed.
	recorder := f.recorder
	f.mu.Unlock()

	// Best-effort: feed the simulated trade to the candle/24h recorder so K-lines
	// and the matching engine's market-data stats reflect simulator activity.
	// The trade is built with a synthetic ID and the taker side derived from the
	// swap direction (buy base => taker is buyer).
	if recorder != nil {
		takerSide := matching.Sell
		if isBuyer {
			takerSide = matching.Buy
		}
		_ = recorder.RecordTrade(&matching.Trade{
			ID:        fmt.Sprintf("sim_%s_%d", pair, tradeTimeMs),
			Pair:      pair,
			Price:     new(big.Float).Copy(execPrice),
			Quantity:  new(big.Float).Copy(tradeSize),
			TakerSide: takerSide,
			CreatedAt: tradeTimeMs * int64(time.Millisecond),
		})
	}
	return nil
}

// Pairs returns the list of pairs the feed currently knows about.
func (f *AMMPriceFeed) Pairs() []string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make([]string, 0, len(f.pools))
	for pair := range f.pools {
		out = append(out, pair)
	}
	return out
}
