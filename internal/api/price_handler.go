package api

import (
	"fmt"
	"math/big"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/WkT010/nexa-exchange/internal/market"
)

type PriceHandler struct {
	feed    *market.WSPriceFeed
	uniswap *market.UniswapRPCProvider
	binance *market.BinancePriceFeed
}

func NewPriceHandler(apiKey string) *PriceHandler {
	return &PriceHandler{
		feed:    market.NewWSPriceFeed(apiKey),
		uniswap: market.NewUniswapRPCProvider(apiKey),
		binance: market.NewBinancePriceFeed(),
	}
}

func (h *PriceHandler) bestTicker(pair string) (*market.Ticker, string) {
	if t, err := h.uniswap.FetchTicker(pair); err == nil && t != nil {
		return t, "uniswap-v3-rpc"
	}
	if t := h.feed.Get(pair); t != nil {
		return t, "alchemy-ws"
	}
	if t, err := h.binance.FetchTicker(pair); err == nil && t != nil {
		return t, "binance"
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
	// Prefer Binance for 24h stats when available, then overlay Uniswap EVM prices.
	if all, err := h.binance.FetchAllTickers(); err == nil {
		for pair, t := range all {
			merged[pair] = t
		}
	}
	for pair, meta := range market.UniV3Pools {
		if t, err := h.uniswap.FetchTicker(pair); err == nil && t != nil {
			// Preserve Binance 24h stats if present, otherwise use Uniswap entirely.
			if existing, ok := merged[pair]; ok && existing != nil {
				t.Volume24h = existing.Volume24h
				t.QuoteVolume24h = existing.QuoteVolume24h
				t.High24h = existing.High24h
				t.Low24h = existing.Low24h
				t.Open24h = existing.Open24h
				t.Change24h = existing.Change24h
				t.ChangePct24h = existing.ChangePct24h
			}
			merged[pair] = t
		}
		_ = meta
	}
	for pair, t := range h.feed.GetAll() {
		if _, ok := merged[pair]; !ok {
			merged[pair] = t
		}
	}
	out := make([]gin.H, 0, len(merged))
	for _, t := range merged {
		out = append(out, gin.H{
			"pair":           t.Pair,
			"last":           safeFloatStr(t.Last),
			"bid":            safeFloatStr(t.Bid),
			"ask":            safeFloatStr(t.Ask),
			"source":         "composite",
			"volume_24h":     safeFloatStr(t.Volume24h),
			"quote_volume_24h": safeFloatStr(t.QuoteVolume24h),
			"high_24h":       safeFloatStr(t.High24h),
			"low_24h":        safeFloatStr(t.Low24h),
			"open_24h":       safeFloatStr(t.Open24h),
			"change_24h":     safeFloatStr(t.Change24h),
			"change_pct_24h": safeFloatStr(t.ChangePct24h),
			"timestamp":      t.Timestamp,
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

func (h *PriceHandler) GetPriceComparison(c *gin.Context) {
	pair := strings.TrimPrefix(c.Param("pair"), "/")
	if pair == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pair required"})
		return
	}
	internal := h.feed.Get(pair)
	uni, uniErr := h.uniswap.FetchTicker(pair)
	out := gin.H{"pair": pair}
	if internal != nil {
		out["internal"] = gin.H{"available": true, "last": internal.Last.String()}
	} else {
		out["internal"] = gin.H{"available": false}
	}
	if uni != nil {
		out["uniswap"] = gin.H{"available": true, "last": uni.Last.String()}
	} else {
		out["uniswap"] = gin.H{"available": false, "error": uniErr}
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
		"pair":       r.Pair,
		"token_in":   r.TokenIn,
		"token_out":  r.TokenOut,
		"amount_in":  amt.String(),
		"to":         q.To,
		"data":       q.Data,
		"value":      q.Value,
		"gas_limit":  q.GasLimit,
		"router":     q.Router,
		"pool":       meta.Address,
		"fee_tier":   meta.Fee,
		"source":     "uniswap-v3-rpc",
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