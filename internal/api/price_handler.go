package api

import (
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/WkT010/nexa-exchange/internal/market"
	"github.com/gin-gonic/gin"
)

type PriceHandler struct {
	// binanceWS is the primary price source: real Binance market data pushed
	// over the combined WebSocket stream and cached in memory.
	binanceWS *market.BinanceWSClient
	// binance is the REST feed, used by the background poller as the second
	// source when the WS cache is missing/stale, and for on-demand depth /
	// trade / kline fetches.
	binance *market.BinancePriceFeed
	// ammFeed is the last-resort fallback: pool-reserve-derived prices keep
	// the exchange showing a market when every external source is down. AMM
	// pool state is never overwritten by external data.
	ammFeed *market.AMMPriceFeed
	// Legacy external providers, kept only for the dedicated Uniswap
	// comparison endpoints.
	feed    *market.WSPriceFeed
	uniswap *market.UniswapRPCProvider

	// staleness is the freshness window for cached market data; anything
	// older degrades to the next source in the chain.
	staleness time.Duration

	// REST poller cache (second source behind the WS cache).
	pollMu      sync.RWMutex
	restTickers map[string]*market.Ticker
	pollDone    chan struct{}

	// Tiny TTL cache so REST depth/trades fallbacks don't hammer the mirror
	// on every frontend poll.
	restCacheMu sync.Mutex
	restDepth   map[string]depthCacheEntry
	restTrades  map[string]tradesCacheEntry
}

type depthCacheEntry struct {
	at    time.Time
	depth *market.Depth
}

type tradesCacheEntry struct {
	at     time.Time
	trades []market.RecentTrade
}

func NewPriceHandler(apiKey string, binanceMirrors []string) *PriceHandler {
	return &PriceHandler{
		feed:       market.NewWSPriceFeed(apiKey),
		uniswap:    market.NewUniswapRPCProvider(apiKey),
		binance:    market.NewBinancePriceFeed(binanceMirrors),
		staleness:  10 * time.Second,
		restDepth:  make(map[string]depthCacheEntry),
		restTrades: make(map[string]tradesCacheEntry),
	}
}

// BinanceFeed exposes the REST feed so other handlers (e.g. kline backfill)
// can reuse the same mirror/backoff state.
func (h *PriceHandler) BinanceFeed() *market.BinancePriceFeed { return h.binance }

// SetAMMFeed wires the AMM price feed used as the last-resort fallback.
func (h *PriceHandler) SetAMMFeed(f *market.AMMPriceFeed) {
	h.ammFeed = f
}

// SetBinanceWS wires the Binance WebSocket cache as the primary source.
func (h *PriceHandler) SetBinanceWS(ws *market.BinanceWSClient) {
	h.binanceWS = ws
}

// SetStaleness overrides the freshness window (MARKET_DATA_STALENESS).
func (h *PriceHandler) SetStaleness(d time.Duration) {
	if d > 0 {
		h.staleness = d
	}
}

func (h *PriceHandler) isFresh(unixMs int64) bool {
	if unixMs <= 0 {
		return false
	}
	return time.Since(time.UnixMilli(unixMs)) <= h.staleness
}

// StartPolling launches the REST ticker poller (BINANCE_POLL_INTERVAL). It is
// the second source in the chain: used whenever the WS cache has no fresh
// data for a pair.
func (h *PriceHandler) StartPolling(interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	h.pollMu.Lock()
	if h.pollDone != nil {
		h.pollMu.Unlock()
		return
	}
	h.pollDone = make(chan struct{})
	done := h.pollDone
	h.pollMu.Unlock()
	go func() {
		h.pollOnce()
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				h.pollOnce()
			}
		}
	}()
}

// StopPolling stops the REST poller.
func (h *PriceHandler) StopPolling() {
	h.pollMu.Lock()
	defer h.pollMu.Unlock()
	if h.pollDone != nil {
		close(h.pollDone)
		h.pollDone = nil
	}
}

func (h *PriceHandler) pollOnce() {
	all, err := h.binance.FetchAllTickers()
	if err != nil || len(all) == 0 {
		slog.Debug("binance rest poll failed", "err", err)
		return
	}
	now := time.Now().UnixMilli()
	for _, t := range all {
		if t != nil {
			t.Timestamp = now // poll time = cache freshness reference
		}
	}
	h.pollMu.Lock()
	h.restTickers = all
	h.pollMu.Unlock()
}

func (h *PriceHandler) restTicker(pair string) *market.Ticker {
	h.pollMu.RLock()
	defer h.pollMu.RUnlock()
	if h.restTickers == nil {
		return nil
	}
	return h.restTickers[pair]
}

// bestTicker resolves a pair through the source chain:
// Binance WS cache -> Binance REST poll cache -> AMM pool feed.
// Each cached source is freshness-gated (MARKET_DATA_STALENESS); stale data
// degrades to the next source instead of being served.
func (h *PriceHandler) bestTicker(pair string) (*market.Ticker, string) {
	if h.binanceWS != nil {
		if t, _ := h.binanceWS.Ticker(pair); t != nil && t.Last != nil && t.Last.Sign() > 0 && h.isFresh(t.Timestamp) {
			return t, "binance-ws"
		}
	}
	if t := h.restTicker(pair); t != nil && t.Last != nil && t.Last.Sign() > 0 && h.isFresh(t.Timestamp) {
		return t, "binance-rest"
	}
	// Primary sources stale or absent: degrade to the self-contained AMM feed.
	if h.ammFeed != nil {
		if t, err := h.ammFeed.FetchTicker(pair); err == nil && t != nil && t.Last != nil && t.Last.Sign() > 0 {
			return t, "amm"
		}
	}
	return nil, ""
}

func (h *PriceHandler) BestPrice(pair string) (*big.Float, string, error) {
	t, source := h.bestTicker(pair)
	if t == nil {
		return nil, "", fmt.Errorf("no price available for %s", pair)
	}
	return new(big.Float).Copy(t.Last), source, nil
}

// MarketDepth returns an L2 order book through the source chain: Binance WS
// depth cache -> Binance REST snapshot -> AMM-synthesized ladder. Used by the
// order handler when the matching engine's own book is empty.
func (h *PriceHandler) MarketDepth(pair string, limit int) (*market.Depth, error) {
	if h.binanceWS != nil {
		if d, at := h.binanceWS.Depth(pair); d != nil && (len(d.Bids) > 0 || len(d.Asks) > 0) && h.isFresh(at) {
			return d, nil
		}
	}
	if d := h.cachedRESTDepth(pair, limit); d != nil {
		return d, nil
	}
	if h.ammFeed != nil {
		return h.ammFeed.FetchDepth(pair, limit)
	}
	return nil, fmt.Errorf("no depth source available")
}

// cachedRESTDepth fetches a REST depth snapshot at most once per 2s per pair
// so frontend polling cannot hammer the mirror.
func (h *PriceHandler) cachedRESTDepth(pair string, limit int) *market.Depth {
	h.restCacheMu.Lock()
	defer h.restCacheMu.Unlock()
	if e, ok := h.restDepth[pair]; ok && time.Since(e.at) < 2*time.Second {
		return e.depth
	}
	d, err := h.binance.FetchDepth(pair, limit)
	if err != nil || d == nil {
		return nil
	}
	h.restDepth[pair] = depthCacheEntry{at: time.Now(), depth: d}
	return d
}

// RecentTrades returns the recent trade tape through the source chain:
// Binance WS tape -> Binance REST aggTrades -> AMM simulator tape.
func (h *PriceHandler) RecentTrades(pair string, limit int) ([]market.RecentTrade, error) {
	if h.binanceWS != nil {
		if rt, at := h.binanceWS.RecentTrades(pair, limit); len(rt) > 0 && h.isFresh(at) {
			return rt, nil
		}
	}
	if rt := h.cachedRESTTrades(pair, limit); rt != nil {
		return rt, nil
	}
	if h.ammFeed != nil {
		return h.ammFeed.FetchRecentTrades(pair, limit)
	}
	return nil, fmt.Errorf("no trades source available")
}

func (h *PriceHandler) cachedRESTTrades(pair string, limit int) []market.RecentTrade {
	h.restCacheMu.Lock()
	defer h.restCacheMu.Unlock()
	if e, ok := h.restTrades[pair]; ok && time.Since(e.at) < 2*time.Second {
		return e.trades
	}
	rt, err := h.binance.FetchRecentTrades(pair, limit)
	if err != nil || len(rt) == 0 {
		return nil
	}
	h.restTrades[pair] = tradesCacheEntry{at: time.Now(), trades: rt}
	return rt
}

func (h *PriceHandler) GetTicker(c *gin.Context) {
	pair := strings.TrimPrefix(c.Param("pair"), "/")
	if pair == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pair required"})
		return
	}
	t, source := h.bestTicker(pair)
	if t == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "pair not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"pair":      t.Pair,
		"last":      t.Last.String(),
		"source":    source,
		"timestamp": t.Timestamp,
	})
}

func (h *PriceHandler) GetAllTickers(c *gin.Context) {
	tickers := h.allTickers()
	c.JSON(http.StatusOK, gin.H{"tickers": tickers, "count": len(tickers)})
}

// allTickers merges every source in priority order and reports the actual
// source per pair (binance-ws / binance-rest / amm). Binance covers the five
// listed spot pairs; the AMM feed fills any remaining pool-backed pairs.
func (h *PriceHandler) allTickers() []gin.H {
	merged := make(map[string]*market.Ticker)
	sources := make(map[string]string)
	// 1. Binance WS cache (fresh entries only).
	if h.binanceWS != nil {
		for pair, t := range h.binanceWS.AllTickers() {
			if t != nil && t.Last != nil && t.Last.Sign() > 0 && h.isFresh(t.Timestamp) {
				merged[pair] = t
				sources[pair] = "binance-ws"
			}
		}
	}
	// 2. Binance REST poll cache fills pairs the WS cache doesn't cover.
	h.pollMu.RLock()
	rest := h.restTickers
	h.pollMu.RUnlock()
	for pair, t := range rest {
		if _, ok := merged[pair]; ok {
			continue
		}
		if t != nil && t.Last != nil && t.Last.Sign() > 0 && h.isFresh(t.Timestamp) {
			merged[pair] = t
			sources[pair] = "binance-rest"
		}
	}
	// 3. AMM feed as the last-resort fallback for anything still missing.
	if h.ammFeed != nil {
		if all, err := h.ammFeed.FetchAllTickers(); err == nil {
			for pair, t := range all {
				if _, ok := merged[pair]; ok {
					continue
				}
				if t != nil && t.Last != nil && t.Last.Sign() > 0 {
					merged[pair] = t
					sources[pair] = "amm"
				}
			}
		}
	}
	out := make([]gin.H, 0, len(merged))
	for _, t := range merged {
		out = append(out, gin.H{
			"pair":             t.Pair,
			"last":             safeFloatStr(t.Last),
			"bid":              safeFloatStr(t.Bid),
			"ask":              safeFloatStr(t.Ask),
			"source":           sources[t.Pair],
			"volume_24h":       safeFloatStr(t.Volume24h),
			"quote_volume_24h": safeFloatStr(t.QuoteVolume24h),
			"high_24h":         safeFloatStr(t.High24h),
			"low_24h":          safeFloatStr(t.Low24h),
			"open_24h":         safeFloatStr(t.Open24h),
			"change_24h":       safeFloatStr(t.Change24h),
			"change_pct_24h":   safeFloatStr(t.ChangePct24h),
			"timestamp":        t.Timestamp,
		})
	}
	return out
}

func (h *PriceHandler) GetUniswapTicker(c *gin.Context) {
	pair := strings.TrimPrefix(c.Param("pair"), "/")
	if pair == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pair required"})
		return
	}
	t, err := h.uniswap.FetchTicker(pair)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, t)
}

// GetPriceComparison returns the exchange's live market price for `pair`
// (resolved through the same source chain the trading UI uses: Binance WS ->
// Binance REST -> AMM) alongside the Uniswap V3 reference price. The Uniswap
// column returns N/A when the RPC provider is unavailable.
func (h *PriceHandler) GetPriceComparison(c *gin.Context) {
	pair := strings.TrimPrefix(c.Param("pair"), "/")
	if pair == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pair required"})
		return
	}
	out := gin.H{"pair": pair}
	if t, source := h.bestTicker(pair); t != nil {
		out["internal"] = gin.H{"available": true, "last": t.Last.String(), "source": source}
	} else {
		out["internal"] = gin.H{"available": false, "error": "no price source available for this pair"}
	}
	uni, uniErr := h.uniswap.FetchTicker(pair)
	if uni != nil && uni.Last != nil && uni.Last.Sign() > 0 {
		out["uniswap"] = gin.H{"available": true, "last": uni.Last.String(), "source": "uniswap-v3-rpc"}
	} else {
		msg := "external price source not available"
		if uniErr != nil {
			msg = uniErr.Error()
		}
		out["uniswap"] = gin.H{"available": false, "error": msg}
	}
	c.JSON(http.StatusOK, out)
}

// BuildSwap returns an unsigned Uniswap V3 exact-input swap transaction.
// POST /api/v2/swap/build
func (h *PriceHandler) BuildSwap(c *gin.Context) {
	var r struct {
		Pair         string `json:"pair" binding:"required"`
		AmountIn     string `json:"amount_in" binding:"required"`
		TokenIn      string `json:"token_in" binding:"required"`
		TokenOut     string `json:"token_out" binding:"required"`
		Recipient    string `json:"recipient"`
		AmountOutMin string `json:"amount_out_min"`
		Deadline     int64  `json:"deadline"`
	}
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pair, amount_in, token_in, token_out required"})
		return
	}
	meta, ok := market.UniV3Pools[r.Pair]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported pair"})
		return
	}
	amt, ok := new(big.Float).SetString(r.AmountIn)
	if !ok || amt.Sign() <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid amount_in"})
		return
	}
	var amountOutMin *big.Float
	if r.AmountOutMin != "" {
		if m, ok := new(big.Float).SetString(r.AmountOutMin); ok {
			amountOutMin = m
		}
	}
	q, err := h.uniswap.BuildSwapTx(market.SwapTxRequest{
		Pair:         r.Pair,
		TokenIn:      r.TokenIn,
		TokenOut:     r.TokenOut,
		AmountIn:     amt,
		AmountOutMin: amountOutMin,
		Recipient:    r.Recipient,
		Deadline:     r.Deadline,
	})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"pair":      r.Pair,
		"token_in":  r.TokenIn,
		"token_out": r.TokenOut,
		"amount_in": amt.String(),
		"to":        q.To,
		"data":      q.Data,
		"value":     q.Value,
		"gas_limit": q.GasLimit,
		"router":    q.Router,
		"pool":      meta.Address,
		"fee_tier":  meta.Fee,
		"source":    "uniswap-v3-rpc",
	})
}

// QuoteSwap returns a real Uniswap V3 exact-input swap quote.
// POST /api/v2/swap/quote
func (h *PriceHandler) QuoteSwap(c *gin.Context) {
	var r struct {
		Pair     string `json:"pair" binding:"required"`
		AmountIn string `json:"amount_in" binding:"required"`
		TokenIn  string `json:"token_in" binding:"required"`
		TokenOut string `json:"token_out" binding:"required"`
	}
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pair, amount_in, token_in, token_out required"})
		return
	}
	meta, ok := market.UniV3Pools[r.Pair]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported pair"})
		return
	}
	amt, ok := new(big.Float).SetString(r.AmountIn)
	if !ok || amt.Sign() <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid amount_in"})
		return
	}
	decimalsIn := meta.Decimals0
	decimalsOut := meta.Decimals1
	if meta.Token0 != r.TokenIn {
		decimalsIn, decimalsOut = meta.Decimals1, meta.Decimals0
	}
	q, err := h.uniswap.QuoteExactInputSingle(market.QuoteSwapRequest{
		Pair:        r.Pair,
		AmountIn:    amt,
		TokenIn:     r.TokenIn,
		TokenOut:    r.TokenOut,
		Fee:         meta.Fee,
		DecimalsIn:  decimalsIn,
		DecimalsOut: decimalsOut,
	})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"pair":            r.Pair,
		"token_in":        r.TokenIn,
		"token_out":       r.TokenOut,
		"amount_in":       q.AmountIn.String(),
		"amount_out":      q.AmountOut.String(),
		"execution_price": q.ExecutionPrice.String(),
		"pool":            q.Pool,
		"fee_tier":        q.FeeTier,
		"source":          "uniswap-v3-rpc",
	})
}
