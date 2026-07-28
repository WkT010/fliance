package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/WkT010/nexa-exchange/internal/market"
)

type PriceHandler struct{ feed *market.WSPriceFeed }

func NewPriceHandler(apiKey string) *PriceHandler {
	return &PriceHandler{feed: market.NewWSPriceFeed(apiKey)}
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
