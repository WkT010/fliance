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

func (h *PriceHandler) GetTicker(c *gin.Context) {
	pair := c.Param("pair")
	if pair == "" { c.JSON(http.StatusBadRequest, gin.H{"error": "pair required"}); return }
	t := h.feed.Get(pair)
	if t == nil { c.JSON(http.StatusNotFound, gin.H{"error": "pair not found"}); return }
	c.JSON(http.StatusOK, gin.H{"pair":t.Pair,"last":t.Last.String(),"source":"alchemy-ws","timestamp":t.Timestamp})
}

func (h *PriceHandler) GetAllTickers(c *gin.Context) {
	tickers := h.feed.GetAll()
	result := make([]gin.H, 0, len(tickers))
	for _, t := range tickers {
		result = append(result, gin.H{"pair":t.Pair,"last":t.Last.String(),"source":"alchemy-ws"})
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
