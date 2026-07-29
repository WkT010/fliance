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
	uniswap *market.UniswapSubgraphProvider
}

func NewPriceHandler(apiKey string) *PriceHandler {
	return &PriceHandler{
		feed:    market.NewWSPriceFeed(apiKey),
		uniswap: market.NewUniswapSubgraphProvider(),
	}
}

func (h *PriceHandler) bestTicker(pair string) (*market.Ticker, string) {
	if t, err := h.uniswap.FetchTicker(pair); err == nil && t != nil {
		return t, "uniswap-v3-subgraph"
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
	c.JSON(http.StatusOK, gin.H{
		"pair":     pair,
		"internal": gin.H{"available": internal != nil},
		"uniswap":  gin.H{"available": uni != nil, "error": uniErr},
	})
}