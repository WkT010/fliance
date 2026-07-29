package api

import (
	"errors"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/WkT010/nexa-exchange/internal/matching"
	"github.com/WkT010/nexa-exchange/internal/observability"
	"github.com/WkT010/nexa-exchange/internal/risk"
)

type OrderHandler struct {
	engines     map[string]*matching.MatchingEngine
	exchange    *matching.Exchange
	store       *store.PGOrderStore
	candleStore CandlestickStore
	// ... full file content
}

// GetCandles with *pair support
func (h *OrderHandler) GetCandles(c *gin.Context) {
	pair := strings.TrimPrefix(c.Param("pair"), "/")
	if h.getEngine(pair) == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported pair"})
		return
	}
	interval := c.DefaultQuery("interval", "1m")
	if matching.IntervalSeconds(interval) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported interval"})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "500"))
	if limit <= 0 || limit > 1500 {
		limit = 500
	}
	var candles []*matching.Candle
	if h.candleStore != nil {
		stored, err := h.candleStore.Candles(pair, interval, 0, 0, limit)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load candles"})
			return
		}
		candles = stored
	} else if engine := h.getEngine(pair); engine != nil && engine.MD != nil {
		candles = engine.MD.Candles(interval, limit, 0, 0)
	}
	if candles == nil {
		candles = []*matching.Candle{}
	}
	result := make([]gin.H, len(candles))
	for i, cd := range candles {
		result[i] = candleToJSON(cd)
	}
	c.JSON(http.StatusOK, gin.H{"candles": result, "interval": interval, "pair": pair})
}

// GetOrderbook with *pair support
func (h *OrderHandler) GetOrderbook(c *gin.Context) {
	pair := strings.TrimPrefix(c.Param("pair"), "/")
	engine := h.getEngine(pair)
	if engine == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported pair: " + pair})
		return
	}
	ob := engine.OrderBook
	if ob == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "orderbook not available"})
		return
	}
	bids, asks := ob.Snapshot()
	c.JSON(http.StatusOK, matching.OrderbookSnapshot{Pair: pair, Bids: bids, Asks: asks, SeqNo: ob.Sequence()})
}

// GetTrades with *pair support
func (h *OrderHandler) GetTrades(c *gin.Context) {
	pair := strings.TrimPrefix(c.Param("pair"), "/")
	engine := h.getEngine(pair)
	if engine == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported pair"})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	var trades []*matching.Trade
	if h.store != nil {
		trades, _ = h.store.GetTrades(pair, limit)
	}
	if trades == nil {
		trades = make([]*matching.Trade, 0)
	}
	out := make([]gin.H, len(trades))
	for i, t := range trades {
		out[i] = tradeToJSON(t)
	}
	c.JSON(http.StatusOK, gin.H{"trades": out})
}

// GetTicker with *pair support
func (h *OrderHandler) GetTicker(c *gin.Context) {
	pair := strings.TrimPrefix(c.Param("pair"), "/")
	engine := h.getEngine(pair)
	if engine == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported pair: " + pair})
		return
	}
	if engine.MD != nil {
		var bid, ask *big.Float
		if b := engine.OrderBook.BestBid(); b != nil {
			bid = b.Price
		}
		if a := engine.OrderBook.BestAsk(); a != nil {
			ask = a.Price
		}
		t := engine.MD.Ticker(bid, ask)
		c.JSON(http.StatusOK, tickerToJSON(t))
		return
	}
	// fallback: top-of-book
	bestBid := engine.OrderBook.BestBid()
	bestAsk := engine.OrderBook.BestAsk()
	if bestBid == nil || bestAsk == nil {
		c.JSON(http.StatusOK, gin.H{"pair": pair})
		return
	}
	c.JSON(http.StatusOK, gin.H{"pair": pair, "bid": safeFloatStr(bestBid.Price), "ask": safeFloatStr(bestAsk.Price)})
}