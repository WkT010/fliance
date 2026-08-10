package api

import (
	"fmt"
	"math/big"
	"net/http"
	"strings"

	"github.com/WkT010/nexa-exchange/internal/market"
	"github.com/gin-gonic/gin"
)

type PriceHandler struct {
	// ammFeed is the primary, self-contained price source. Every ticker, depth
	// and recent-trade the handler exposes is derived from AMM pool reserves,
	// so the exchange shows a live market with no external dependency.
	ammFeed *market.AMMPriceFeed
	// External sources are kept but only consulted when externalFallback is
	// enabled (opt-in, default off) AND the AMM feed has no data for a pair.
	// This preserves the "不依赖于源" contract by default while leaving a
	// escape hatch for operators who want to blend in real-world prices.
	feed             *market.WSPriceFeed
	uniswap          *market.UniswapRPCProvider
	binance          *market.BinancePriceFeed
	externalFallback bool
}

func NewPriceHandler(apiKey string) *PriceHandler {
	return &PriceHandler{
		feed:    market.NewWSPriceFeed(apiKey),
		uniswap: market.NewUniswapRPCProvider(apiKey),
		binance: market.NewBinancePriceFeed(),
	}
}

// SetAMMFeed wires the self-contained AMM price feed as the primary source.
// Once set, all ticker/depth/recent-trade reads prefer the AMM feed.
func (h *PriceHandler) SetAMMFeed(f *market.AMMPriceFeed) {
	h.ammFeed = f
}

// SetExternalFallback enables/disables consulting Binance/Uniswap/Alchemy when
// the AMM feed has no data for a pair. Default is off (fully self-contained).
func (h *PriceHandler) SetExternalFallback(on bool) {
	h.externalFallback = on
}

func (h *PriceHandler) bestTicker(pair string) (*market.Ticker, string) {
	// Primary: AMM feed (self-contained, always available once seeded).
	if h.ammFeed != nil {
		if t, err := h.ammFeed.FetchTicker(pair); err == nil && t != nil && t.Last != nil && t.Last.Sign() > 0 {
			return t, "amm"
		}
	}
	if !h.externalFallback {
		return nil, ""
	}
	// Opt-in external fallback only when the AMM feed has nothing for the pair.
	if t, err := h.binance.FetchTicker(pair); err == nil && t != nil {
		return t, "binance"
	}
	if t, err := h.uniswap.FetchTicker(pair); err == nil && t != nil {
		return t, "uniswap-v3-rpc"
	}
	if t := h.feed.Get(pair); t != nil {
		return t, "alchemy-ws"
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

// MarketDepth returns an L2 order book synthesized from the AMM pool's mid
// price + fee. Used by the order handler when the matching engine's own book
// is empty (the common case on a fresh deployment), so the trading UI shows a
// realistic, self-contained book instead of a blank panel or external data.
func (h *PriceHandler) MarketDepth(pair string, limit int) (*market.Depth, error) {
	if h.ammFeed != nil {
		return h.ammFeed.FetchDepth(pair, limit)
	}
	if h.externalFallback {
		return h.binance.FetchDepth(pair, limit)
	}
	return nil, fmt.Errorf("amm: no feed available")
}

// RecentTrades returns the simulator's recent trade tape for `pair`. Used by
// the order handler as a fallback when the matching engine has no recorded
// fills, so the recent-trades panel shows live, self-contained activity.
func (h *PriceHandler) RecentTrades(pair string, limit int) ([]market.RecentTrade, error) {
	if h.ammFeed != nil {
		return h.ammFeed.FetchRecentTrades(pair, limit)
	}
	if h.externalFallback {
		return h.binance.FetchRecentTrades(pair, limit)
	}
	return nil, fmt.Errorf("amm: no feed available")
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

func (h *PriceHandler) allTickers() []gin.H {
	merged := make(map[string]*market.Ticker)
	// Primary: AMM feed covers every seeded market and is always available.
	if h.ammFeed != nil {
		if all, err := h.ammFeed.FetchAllTickers(); err == nil {
			for pair, t := range all {
				merged[pair] = t
			}
		}
	}
	// Opt-in external fallback only fills pairs the AMM feed does not cover.
	if h.externalFallback {
		if all, err := h.binance.FetchAllTickers(); err == nil {
			for pair, t := range all {
				if existing, ok := merged[pair]; ok && existing != nil && existing.Last != nil && existing.Last.Sign() > 0 {
					continue
				}
				merged[pair] = t
			}
		}
		for pair := range market.UniV3Pools {
			if existing, ok := merged[pair]; ok && existing != nil && existing.Last != nil && existing.Last.Sign() > 0 {
				continue
			}
			if t, err := h.uniswap.FetchTicker(pair); err == nil && t != nil {
				merged[pair] = t
			}
		}
		for pair, t := range h.feed.GetAll() {
			if _, ok := merged[pair]; !ok {
				merged[pair] = t
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
			"source":           "amm",
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

// GetPriceComparison returns the internal (AMM) price for `pair` alongside
// the Uniswap V3 reference price. The internal column uses the AMM feed (the
// platform's primary, self-contained price source) — not the legacy Alchemy
// WebSocket feed — so the comparison always reflects the same data the
// trading UI is using. The Uniswap column returns N/A when the RPC provider
// is unavailable (the common case on a self-contained deployment).
func (h *PriceHandler) GetPriceComparison(c *gin.Context) {
	pair := strings.TrimPrefix(c.Param("pair"), "/")
	if pair == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pair required"})
		return
	}
	out := gin.H{"pair": pair}
	if h.ammFeed != nil {
		if t, err := h.ammFeed.FetchTicker(pair); err == nil && t != nil && t.Last != nil && t.Last.Sign() > 0 {
			out["internal"] = gin.H{"available": true, "last": t.Last.String(), "source": "amm"}
		} else {
			out["internal"] = gin.H{"available": false, "error": "no AMM pool for this pair"}
		}
	} else {
		out["internal"] = gin.H{"available": false, "error": "AMM feed not configured"}
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
