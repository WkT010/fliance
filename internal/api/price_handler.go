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
}

func NewPriceHandler(apiKey string) *PriceHandler {
	return &PriceHandler{
		feed:    market.NewWSPriceFeed(apiKey),
		uniswap: market.NewUniswapRPCProvider(apiKey),
	}
}

func (h *PriceHandler) bestTicker(pair string) (*market.Ticker, string) {
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
	c.JSON(http.StatusOK, gin.H{"tickers": []interface{}{}, "count": 0})
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