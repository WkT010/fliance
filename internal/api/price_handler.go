package api

import (
	"sync"
	"time"
	"github.com/gin-gonic/gin"
	"github.com/WkT010/nexa-exchange/internal/market"
)

type PriceCache struct {
	mu      sync.RWMutex
	tickers map[string]*market.Ticker
	feed    *market.AlchemyPriceFeed
}

func NewPriceCache(apiKey string) *PriceCache {
	pc := &PriceCache{
		tickers: make(map[string]*market.Ticker),
		feed:    market.NewAlchemyPriceFeed(apiKey),
	}
	go func() {
		pc.refresh()
		for range time.NewTicker(15 * time.Second).C { pc.refresh() }
	}()
	return pc
}

func (pc *PriceCache) refresh() {
	t, err := pc.feed.FetchAllTickers()
	if err != nil { return }
	pc.mu.Lock()
	pc.tickers = t
	pc.mu.Unlock()
}

func (pc *PriceCache) Get(pair string) *market.Ticker {
	pc.mu.RLock(); defer pc.mu.RUnlock()
	return pc.tickers[pair]
}

type PriceHandler struct{ cache *PriceCache }
func NewPriceHandler(cache *PriceCache) *PriceHandler { return &PriceHandler{cache: cache} }

func (h *PriceHandler) GetTicker(c *gin.Context) {
	pair := c.Param("pair")
	if pair == "" { AbortWithError(c, NewError(400, "pair required")); return }
	t := h.cache.Get(pair)
	if t == nil { AbortWithError(c, NewError(404, "pair not found")); return }
	c.JSON(200, gin.H{"pair": t.Pair, "last": t.Last.String(), "source": "alchemy", "timestamp": t.Timestamp})
}

func (h *PriceHandler) GetAllTickers(c *gin.Context) {
	h.cache.mu.RLock()
	result := make([]gin.H, 0, len(h.cache.tickers))
	for _, t := range h.cache.tickers {
		result = append(result, gin.H{"pair": t.Pair, "last": t.Last.String(), "source": "alchemy"})
	}
	h.cache.mu.RUnlock()
	c.JSON(200, gin.H{"tickers": result, "count": len(result)})
}
