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
	feed    *market.BinancePriceFeed
}

func NewPriceCache() *PriceCache {
	pc := &PriceCache{tickers: make(map[string]*market.Ticker), feed: market.NewBinancePriceFeed()}
	go func() {
		pc.refresh()
		for range time.NewTicker(10 * time.Second).C { pc.refresh() }
	}()
	return pc
}

func (pc *PriceCache) refresh() {
	tickers, err := pc.feed.FetchAllTickers()
	if err != nil { return }
	pc.mu.Lock(); pc.tickers = tickers; pc.mu.Unlock()
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
	if t == nil { AbortWithError(c, NewError(404, "pair not found: "+pair)); return }
	Success(c, gin.H{
		"pair":t.Pair,"last":t.Last.String(),"bid":t.Bid.String(),"ask":t.Ask.String(),
		"spread":t.Spread.String(),"volume_24h":t.Volume24h.String(),
		"high_24h":t.High24h.String(),"low_24h":t.Low24h.String(),
		"change_24h":t.Change24h.String(),"change_pct":t.ChangePct24h.String(),
		"source":"binance","timestamp":t.Timestamp,
	})
}

func (h *PriceHandler) GetAllTickers(c *gin.Context) {
	pc := h.cache
	pc.mu.RLock()
	result := make([]gin.H, 0, len(pc.tickers))
	for _, t := range pc.tickers {
		result = append(result, gin.H{"pair":t.Pair,"last":t.Last.String(),"bid":t.Bid.String(),"ask":t.Ask.String(),"volume_24h":t.Volume24h.String(),"source":"binance"})
	}
	pc.mu.RUnlock()
	Success(c, gin.H{"tickers": result, "count": len(result)})
}
