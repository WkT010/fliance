package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/WkT010/nexa-exchange/internal/market"
)

type PriceHandler struct {
	feed    *market.WSPriceFeed
	uniswap *market.UniswapSubgraphProvider
}

func NewPriceHandler(apiKey string) *PriceHandler {
	return &PriceHandler{
		feed:    market.NewWSPriceFeed(apiKey),
		uniswap: market.NewUniswapSubgraphProvider(),
	}
}

// bestTicker prefers Uniswap V3 subgraph (no API quota) and falls back to the
// Alchemy price feed when the pair is not supported by Uniswap or the subgraph
// is unreachable.
func (h *PriceHandler) bestTicker(pair string) (*market.Ticker, string) {
	if t, err := h.uniswap.FetchTicker(pair); err == nil && t != nil {
		return t, "uniswap-v3-subgraph"
	}
	if t := h.feed.Get(pair); t != nil {
		return t, "alchemy-ws"
	}
	return nil, ""
}

func (h *PriceHandler) GetTicker(c *gin.Context) {
	pair := c.Param("pair")
	if pair == "" { c.JSON(http.StatusBadRequest, gin.H{"error": "pair required"}); return }
	t, source := h.bestTicker(pair)
	if t == nil { c.JSON(http.StatusNotFound, gin.H{"error": "pair not found"}); return }
	c.JSON(http.StatusOK, gin.H{"pair":t.Pair,"last":t.Last.String(),"source":source,"timestamp":t.Timestamp})
}

func (h *PriceHandler) GetAllTickers(c *gin.Context) {
	// Start with Alchemy feed so every configured pair has a quote, then overlay
	// Uniswap prices where available to honor the low-cost priority.
	merged := make(map[string]*market.Ticker)
	sources := make(map[string]string)
	for _, t := range h.feed.GetAll() {
		cp := *t
		merged[t.Pair] = &cp
		sources[t.Pair] = "alchemy-ws"
	}
	for _, pair := range h.uniswap.SupportedPairs() {
		if t, err := h.uniswap.FetchTicker(pair); err == nil && t != nil {
			cp := *t
			merged[pair] = &cp
			sources[pair] = "uniswap-v3-subgraph"
		}
	}
	result := make([]gin.H, 0, len(merged))
	for _, t := range merged {
		result = append(result, gin.H{"pair":t.Pair,"last":t.Last.String(),"source":sources[t.Pair]})
	}
	c.JSON(200, gin.H{"tickers":result,"count":len(result)})
}

// GetUniswapTicker returns the on-chain Uniswap V3 price for a supported pair.
// GET /api/v2/price/uniswap/:pair
func (h *PriceHandler) GetUniswapTicker(c *gin.Context) {
	pair := c.Param("pair")
	if pair == "" { c.JSON(http.StatusBadRequest, gin.H{"error": "pair required"}); return }
	t, err := h.uniswap.FetchTicker(pair)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"pair":      t.Pair,
		"last":      t.Last.String(),
		"source":    "uniswap-v3-subgraph",
		"timestamp": t.Timestamp,
	})
}

// GetPriceComparison returns both the internal/Alchemy price and the Uniswap price.
// GET /api/v2/price/compare/:pair
func (h *PriceHandler) GetPriceComparison(c *gin.Context) {
	pair := c.Param("pair")
	if pair == "" { c.JSON(http.StatusBadRequest, gin.H{"error": "pair required"}); return }
	internal := h.feed.Get(pair)
	uni, uniErr := h.uniswap.FetchTicker(pair)
	c.JSON(http.StatusOK, gin.H{
		"pair": pair,
		"internal": func() gin.H {
			if internal == nil { return gin.H{"available": false} }
			return gin.H{"available": true, "last": internal.Last.String(), "timestamp": internal.Timestamp, "source": "alchemy-ws"}
		}(),
		"uniswap": func() gin.H {
			if uniErr != nil { return gin.H{"available": false, "error": uniErr.Error()} }
			return gin.H{"available": true, "last": uni.Last.String(), "timestamp": uni.Timestamp, "source": "uniswap-v3-subgraph"}
		}(),
	})
}
