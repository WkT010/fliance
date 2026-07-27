package api

import (
	"github.com/gin-gonic/gin"
	"github.com/WkT010/nexa-exchange/internal/market"
)

type PriceHandler struct{ feed *market.WSPriceFeed }

func NewPriceHandler(apiKey string) *PriceHandler {
	return &PriceHandler{feed: market.NewWSPriceFeed(apiKey)}
}

func (h *PriceHandler) GetTicker(c *gin.Context) {
	pair := c.Param("pair")
	if pair == "" { AbortWithError(c, NewError(400, "pair required")); return }
	t := h.feed.Get(pair)
	if t == nil { AbortWithError(c, NewError(404, "pair not found")); return }
	c.JSON(200, gin.H{"pair":t.Pair,"last":t.Last.String(),"source":"alchemy-ws","timestamp":t.Timestamp})
}

func (h *PriceHandler) GetAllTickers(c *gin.Context) {
	tickers := h.feed.GetAll()
	result := make([]gin.H, 0, len(tickers))
	for _, t := range tickers {
		result = append(result, gin.H{"pair":t.Pair,"last":t.Last.String(),"source":"alchemy-ws"})
	}
	c.JSON(200, gin.H{"tickers":result,"count":len(result)})
}
