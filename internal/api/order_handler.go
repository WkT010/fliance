package api

import (
	"math/big"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/WkT010/nexa-exchange/internal/matching"
	"github.com/WkT010/nexa-exchange/internal/risk"
)

// OrderStore is the persistence interface required by the order handler and
// the WebSocket bridge.
type OrderStore interface {
	Save(o *matching.Order) error
	Get(id string) (*matching.Order, error)
	ListByUser(userID, pair string, status matching.OrderStatus, limit, offset int) ([]*matching.Order, error)
	UpdateOrderStatus(id string, status matching.OrderStatus) error
	SaveTrade(t *matching.Trade) error
	GetTrades(pair string, limit int) ([]*matching.Trade, error)
}

// CandlestickStore loads historical OHLCV candles.
type CandlestickStore interface {
	Candles(pair, interval string, from, to int64, limit int) ([]*matching.Candle, error)
}

// OrderReleaser releases reserved balances for cancelled or filled orders.
type OrderReleaser interface {
	ReleaseOrder(orderID, userID string) error
}

type OrderHandler struct {
	engines     map[string]*matching.MatchingEngine
	exchange    *matching.ExchangeEngine
	store       OrderStore
	candleStore CandlestickStore
	wallet      WalletService
	releaser    OrderReleaser
	risk        *risk.Engine
}

// NewOrderHandlerWithExchange constructs an order handler backed by the
// exchange facade.
func NewOrderHandlerWithExchange(ex *matching.ExchangeEngine, store OrderStore, riskEng *risk.Engine) *OrderHandler {
	return &OrderHandler{
		exchange: ex,
		store:    store,
		engines:  make(map[string]*matching.MatchingEngine),
		risk:     riskEng,
	}
}

func (h *OrderHandler) SetWallet(ws WalletService, releaser OrderReleaser) {
	h.wallet = ws
	h.releaser = releaser
}

func (h *OrderHandler) SetCandleStore(cs CandlestickStore) {
	h.candleStore = cs
}

func (h *OrderHandler) getEngine(pair string) *matching.MatchingEngine {
	if h.exchange != nil {
		return h.exchange.Get(pair)
	}
	return h.engines[pair]
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
	depth := ob.Depth(100)
	c.JSON(http.StatusOK, matching.OrderBookDepth{Pair: pair, Bids: depth.Bids, Asks: depth.Asks, SeqNo: depth.SeqNo})
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

// PlaceOrder is a stub that accepts spot orders. Full matching is handled by
// the exchange gRPC service in this build.
func (h *OrderHandler) PlaceOrder(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "spot order placement not implemented in REST gateway"})
}

// CancelOrder is a stub for spot order cancellation.
func (h *OrderHandler) CancelOrder(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "spot order cancellation not implemented in REST gateway"})
}

// CancelAllOrders is a stub for cancelling all spot orders.
func (h *OrderHandler) CancelAllOrders(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "spot order cancellation not implemented in REST gateway"})
}

// GetOrder returns a single spot order.
func (h *OrderHandler) GetOrder(c *gin.Context) {
	id := c.Param("id")
	if h.store == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "order store unavailable"})
		return
	}
	o, err := h.store.Get(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
		return
	}
	c.JSON(http.StatusOK, orderToJSON(o))
}

// ListOrders returns the authenticated user's spot orders.
func (h *OrderHandler) ListOrders(c *gin.Context) {
	uid, _ := c.Get("user_id")
	userID, _ := uid.(string)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}
	pair := c.Query("pair")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if h.store == nil {
		c.JSON(http.StatusOK, gin.H{"orders": []interface{}{}})
		return
	}
	orders, _ := h.store.ListByUser(userID, pair, matching.New, limit, offset)
	if orders == nil {
		orders = []*matching.Order{}
	}
	out := make([]gin.H, len(orders))
	for i, o := range orders {
		out[i] = orderToJSON(o)
	}
	c.JSON(http.StatusOK, gin.H{"orders": out})
}

// ListTickers returns all 24h tickers.
func (h *OrderHandler) ListTickers(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"tickers": []interface{}{}})
}

func candleToJSON(cd *matching.Candle) gin.H {
	return gin.H{
		"pair":       cd.Pair,
		"interval":   cd.Interval,
		"open":       safeFloatStr(cd.Open),
		"high":       safeFloatStr(cd.High),
		"low":        safeFloatStr(cd.Low),
		"close":      safeFloatStr(cd.Close),
		"volume":     safeFloatStr(cd.Volume),
		"timestamp":  cd.Timestamp,
		"close_time": cd.CloseTime,
	}
}

func tradeToJSON(t *matching.Trade) gin.H {
	return gin.H{
		"id":           t.ID,
		"buy_order_id": t.BuyOrderID,
		"sell_order_id": t.SellOrderID,
		"buyer_id":     t.BuyerID,
		"seller_id":    t.SellerID,
		"pair":         t.Pair,
		"price":        safeFloatStr(t.Price),
		"quantity":     safeFloatStr(t.Quantity),
		"taker_side":   t.TakerSide.String(),
		"fee":          safeFloatStr(t.Fee),
		"created_at":   t.CreatedAt,
	}
}

func tickerToJSON(t *matching.Ticker) gin.H {
	return gin.H{
		"pair":              t.Pair,
		"last":              safeFloatStr(t.LastPrice),
		"bid":               safeFloatStr(t.Bid),
		"ask":               safeFloatStr(t.Ask),
		"spread":            safeFloatStr(t.Spread),
		"volume_24h":        safeFloatStr(t.Volume24H),
		"quote_volume_24h":  safeFloatStr(t.QuoteVolume24H),
		"high_24h":          safeFloatStr(t.High24H),
		"low_24h":           safeFloatStr(t.Low24H),
		"open_24h":          safeFloatStr(t.Open24H),
		"change_24h":        safeFloatStr(t.Change24H),
		"change_pct_24h":    safeFloatStr(t.ChangePct24H),
		"timestamp":         t.Timestamp,
	}
}

func orderToJSON(o *matching.Order) gin.H {
	return gin.H{
		"id":              o.ID,
		"client_order_id": o.ClientOrderID,
		"user_id":         o.UserID,
		"pair":            o.Pair,
		"side":            o.Side.String(),
		"type":            o.Type.String(),
		"price":           safeFloatStr(o.Price),
		"stop_price":      safeFloatStr(o.StopPrice),
		"quantity":        safeFloatStr(o.Quantity),
		"filled_qty":      safeFloatStr(o.FilledQty),
		"remaining_qty":   safeFloatStr(o.RemainingQty),
		"time_in_force":   timeInForceToString(o.TimeInForce),
		"status":          o.Status.String(),
		"created_at":      o.CreatedAt,
		"updated_at":      o.UpdatedAt,
	}
}

func timeInForceToString(tif matching.TimeInForce) string {
	switch tif {
	case matching.GTC:
		return "gtc"
	case matching.IOC:
		return "ioc"
	case matching.FOK:
		return "fok"
	case matching.GTD:
		return "gtd"
	default:
		return "unknown"
	}
}
