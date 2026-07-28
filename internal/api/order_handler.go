package api

import (
	"errors"
	"math/big"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/WkT010/nexa-exchange/internal/matching"
	"github.com/WkT010/nexa-exchange/internal/observability"
	"github.com/WkT010/nexa-exchange/internal/risk"
)

// OrderStore is the persistence interface for orders and trades.
type OrderStore interface {
	Save(*matching.Order) error
	Get(string) (*matching.Order, error)
	ListByUser(userID, pair string, status matching.OrderStatus, limit, offset int) ([]*matching.Order, error)
	SaveTrade(*matching.Trade) error
	GetTrades(pair string, limit int) ([]*matching.Trade, error)
}

// OrderReserver locks funds for an order before it enters the matching engine.
// Implemented by wallet.Service. Returns ErrInsufficientBalance (wrapped) if the
// user does not have enough available funds for the order.
type OrderReserver interface {
	ReserveOrder(orderID, userID, pair string, side int, orderType int, price, qty *big.Float) error
}

// OrderReleaser releases leftover reserved funds for an order (e.g. on cancel
// or after the order has fully filled).
type OrderReleaser interface {
	ReleaseOrder(orderID, userID string) error
}

// CandleStore provides persisted OHLCV candles for the market-data endpoints.
// Implemented by *market.CandleService.
type CandleStore interface {
	Candles(pair, interval string, start, end int64, limit int) ([]*matching.Candle, error)
}

// ErrInsufficientBalance is returned by ReserveOrder when the user lacks funds.
// It mirrors wallet.ErrInsufficientBalance but is re-declared here to avoid
// importing the wallet package (which would create a layering violation from
// the api package). Handlers compare via errors.Is on the wrapped error.
var ErrInsufficientBalance = errors.New("insufficient balance")

type OrderHandler struct {
	engines     map[string]*matching.MatchingEngine
	exchange    *matching.ExchangeEngine
	store       OrderStore
	candleStore CandleStore
	reserver    OrderReserver
	releaser    OrderReleaser
	risk        *risk.Engine
}

// NewOrderHandler constructs an order handler from a per-pair engine map.
// Deprecated: use NewOrderHandlerWithExchange for production deployments.
func NewOrderHandler(engines map[string]*matching.MatchingEngine, store OrderStore) *OrderHandler {
	return &OrderHandler{engines: engines, store: store}
}

// NewOrderHandlerWithExchange constructs an order handler backed by the
// exchange facade, optional risk engine and persistence store.
func NewOrderHandlerWithExchange(ex *matching.ExchangeEngine, store OrderStore, riskEng *risk.Engine) *OrderHandler {
	return &OrderHandler{exchange: ex, store: store, risk: riskEng}
}

// SetWallet wires the wallet service (or any type implementing both OrderReserver
// and OrderReleaser). Pass nil to disable wallet integration.
func (h *OrderHandler) SetWallet(r OrderReserver, rel OrderReleaser) {
	h.reserver = r
	h.releaser = rel
}

// SetCandleStore wires a persisted candle store for /klines queries.
func (h *OrderHandler) SetCandleStore(s CandleStore) { h.candleStore = s }

type placeOrderReq struct {
	Pair        string `json:"pair" binding:"required"`
	Side        string `json:"side" binding:"required,oneof=buy sell"`
	Type        string `json:"type" binding:"required,oneof=limit market fok ioc iceberg stop_loss stop_limit post_only"`
	Price       string `json:"price"`
	StopPrice   string `json:"stop_price"`
	Quantity    string `json:"quantity"`
	TimeInForce string `json:"time_in_force"`
	IcebergQty  string `json:"iceberg_qty"`
	ClientOrderID string `json:"client_order_id"`
}

func (h *OrderHandler) PlaceOrder(c *gin.Context) {
	var r placeOrderReq
	if err := c.ShouldBindJSON(&r); err != nil {
		observability.OrdersReceivedTotal.Inc()
		observability.OrdersRejectedTotal.Inc()
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid order: " + err.Error()})
		return
	}
	observability.OrdersReceivedTotal.Inc()

	uid, _ := c.Get("user_id")
	userID, _ := uid.(string)
	if userID == "" {
		observability.OrdersRejectedTotal.Inc()
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}
	side := matching.Buy
	if r.Side == "sell" {
		side = matching.Sell
	}
	var ot matching.OrderType
	switch r.Type {
	case "limit":
		ot = matching.Limit
	case "market":
		ot = matching.Market
	case "fok":
		ot = matching.FillOrKill
	case "ioc":
		ot = matching.ImmediateOrCancel
	case "iceberg":
		ot = matching.Iceberg
	case "stop_loss":
		ot = matching.StopLoss
	case "stop_limit":
		ot = matching.StopLimit
	case "post_only":
		ot = matching.PostOnly
	default:
		observability.OrdersRejectedTotal.Inc()
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid order type"})
		return
	}
	var price *big.Float
	if r.Price != "" {
		price = new(big.Float)
		if _, _, err := price.Parse(r.Price, 10); err != nil || price.Sign() <= 0 {
			observability.OrdersRejectedTotal.Inc()
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid price"})
			return
		}
	}
	var stopPrice *big.Float
	if r.StopPrice != "" {
		stopPrice = new(big.Float)
		if _, _, err := stopPrice.Parse(r.StopPrice, 10); err != nil || stopPrice.Sign() <= 0 {
			observability.OrdersRejectedTotal.Inc()
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid stop price"})
			return
		}
	}
	if r.Quantity == "" {
		observability.OrdersRejectedTotal.Inc()
		c.JSON(http.StatusBadRequest, gin.H{"error": "quantity required"})
		return
	}
	qty := new(big.Float)
	if _, _, err := qty.Parse(r.Quantity, 10); err != nil || qty.Sign() <= 0 {
		observability.OrdersRejectedTotal.Inc()
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid quantity"})
		return
	}
	if (ot == matching.Limit || ot == matching.Iceberg || ot == matching.StopLimit || ot == matching.PostOnly) && (price == nil || price.Sign() <= 0) {
		observability.OrdersRejectedTotal.Inc()
		c.JSON(http.StatusBadRequest, gin.H{"error": "price required for limit/iceberg/stop-limit/post-only orders"})
		return
	}
	if (ot == matching.StopLoss || ot == matching.StopLimit) && (stopPrice == nil || stopPrice.Sign() <= 0) {
		observability.OrdersRejectedTotal.Inc()
		c.JSON(http.StatusBadRequest, gin.H{"error": "stop price required for stop orders"})
		return
	}
	o := matching.NewOrder(userID, r.Pair, side, ot, price, qty)
	o.StopPrice = stopPrice
	o.ClientOrderID = r.ClientOrderID
	if r.TimeInForce == "ioc" {
		o.TimeInForce = matching.IOC
	}
	if r.TimeInForce == "fok" {
		o.TimeInForce = matching.FOK
	}
	if ot == matching.Iceberg {
		if r.IcebergQty != "" {
			iceQty := new(big.Float)
			if _, _, err := iceQty.Parse(r.IcebergQty, 10); err != nil || iceQty.Sign() <= 0 {
				observability.OrdersRejectedTotal.Inc()
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid iceberg quantity"})
				return
			}
			o.IcebergQty = iceQty
			o.VisibleQty = new(big.Float).Copy(iceQty)
			if o.VisibleQty.Cmp(o.Quantity) > 0 {
				o.VisibleQty = new(big.Float).Copy(o.Quantity)
			}
		} else {
			half := new(big.Float).Quo(o.Quantity, big.NewFloat(2))
			o.IcebergQty = half
			o.VisibleQty = new(big.Float).Copy(half)
		}
	}

	// Explicit risk check for deployments that use the legacy engine map path.
	if h.risk != nil {
		req := matching.OrderRequest{
			UserID: userID, Pair: r.Pair, Side: side, Type: ot,
			Price: price, Quantity: qty,
		}
		if err := h.risk.Check(req); err != nil {
			observability.OrdersRejectedTotal.Inc()
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}

	// Pre-trade reservation: lock the funds required for this order before it
	// enters the matching engine. If reservation fails the order is rejected
	// and no funds are touched. Market buys are not pre-locked (price unknown).
	if h.reserver != nil {
		if err := h.reserver.ReserveOrder(o.ID, userID, r.Pair, int(side), int(ot), price, qty); err != nil {
			// Release any partial reservation if the order fails to submit.
			if h.releaser != nil {
				_ = h.releaser.ReleaseOrder(o.ID, userID)
			}
			observability.OrdersRejectedTotal.Inc()
			c.JSON(http.StatusPaymentRequired, gin.H{
				"error":    "insufficient balance",
				"order_id": o.ID,
				"detail":   err.Error(),
			})
			return
		}
	}

	submitted := false
	if h.exchange != nil {
		if err := h.exchange.SubmitOrder(o); err != nil {
			if h.releaser != nil {
				_ = h.releaser.ReleaseOrder(o.ID, userID)
			}
			observability.OrdersRejectedTotal.Inc()
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		submitted = true
	} else if engine, ok := h.engines[r.Pair]; ok {
		if !engine.SubmitOrder(o) {
			if h.releaser != nil {
				_ = h.releaser.ReleaseOrder(o.ID, userID)
			}
			observability.OrdersRejectedTotal.Inc()
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "matching engine busy"})
			return
		}
		submitted = true
	}
	if !submitted {
		if h.releaser != nil {
			_ = h.releaser.ReleaseOrder(o.ID, userID)
		}
		observability.OrdersRejectedTotal.Inc()
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported trading pair: " + r.Pair})
		return
	}

	if h.store != nil {
		go h.store.Save(o)
	}
	observability.OrdersAcceptedTotal.Inc()
	c.JSON(http.StatusOK, gin.H{"order_id": o.ID, "client_order_id": o.ClientOrderID, "status": o.Status.String()})
}

func (h *OrderHandler) CancelOrder(c *gin.Context) {
	uid, _ := c.Get("user_id")
	userID, _ := uid.(string)
	pair := c.Query("pair")
	if pair == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pair query parameter required"})
		return
	}
	engine := h.getEngine(pair)
	if engine == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported pair"})
		return
	}
	orderID := c.Param("id")
	// Use the engine's serialised cancel path so the cancel races neither
	// matching nor other cancels.
	o, err := engine.Cancel(orderID, userID)
	if err != nil {
		// Not in the live book: maybe it already filled or was cancelled.
		// Fall back to the store so we can confirm ownership and update status.
		if h.store != nil {
			if so, serr := h.store.Get(orderID); serr == nil && so != nil {
				if so.UserID != userID {
					c.JSON(http.StatusForbidden, gin.H{"error": "order does not belong to user"})
					return
				}
				if so.Status == matching.Cancelled || so.Status == matching.Filled {
					c.JSON(http.StatusOK, gin.H{"order_id": so.ID, "status": so.Status.String()})
					return
				}
				c.JSON(http.StatusNotFound, gin.H{"error": "order not active"})
				return
			}
		}
		c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
		return
	}
	// Release any remaining reservation so the user's funds return to available
	// immediately. The bridge handles releases for fully-filled orders, but a
	// cancel leaves a leftover reservation that only the API path can release.
	if h.releaser != nil {
		if rerr := h.releaser.ReleaseOrder(o.ID, userID); rerr != nil {
			// Non-fatal: log via response field but don't fail the cancel.
			c.JSON(http.StatusOK, gin.H{"order_id": o.ID, "status": "cancelled", "warning": "release failed"})
			if h.store != nil {
				go h.store.Save(o)
			}
			return
		}
	}
	if h.store != nil {
		go h.store.Save(o)
	}
	observability.OrdersCancelledTotal.Inc()
	c.JSON(http.StatusOK, gin.H{"order_id": o.ID, "status": "cancelled"})
}

// CancelAllOrders cancels every active order for the authenticated user on the
// given pair (or all pairs if no pair is given). Returns the per-pair counts.
func (h *OrderHandler) CancelAllOrders(c *gin.Context) {
	uid, _ := c.Get("user_id")
	userID, _ := uid.(string)
	pair := c.Query("pair")
	total := 0
	results := gin.H{}
	if pair != "" {
		if engine := h.getEngine(pair); engine != nil {
			n := engine.CancelAll(userID)
			total += n
			results[pair] = n
		}
	} else {
		for p, engine := range h.allEngines() {
			n := engine.CancelAll(userID)
			total += n
			results[p] = n
		}
	}
	if total > 0 {
		observability.OrdersCancelledTotal.Add(uint64(total))
	}
	c.JSON(http.StatusOK, gin.H{"cancelled": total, "by_pair": results})
}

func (h *OrderHandler) GetOrder(c *gin.Context) {
	pair := c.Query("pair")
	if pair == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pair query parameter required"})
		return
	}
	engine := h.getEngine(pair)
	if engine == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported pair"})
		return
	}
	o := engine.OrderBook.Get(c.Param("id"))
	if o == nil && h.store != nil {
		var err error
		o, err = h.store.Get(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
			return
		}
	}
	if o == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
		return
	}
	uid, _ := c.Get("user_id")
	if o.UserID != uid.(string) {
		c.JSON(http.StatusForbidden, gin.H{"error": "order does not belong to user"})
		return
	}
	c.JSON(http.StatusOK, orderToJSON(o))
}

func (h *OrderHandler) ListOrders(c *gin.Context) {
	uid, _ := c.Get("user_id")
	userID, _ := uid.(string)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if offset < 0 {
		offset = 0
	}
	pair := c.Query("pair")
	statusStr := c.Query("status")
	var status matching.OrderStatus = -1
	if statusStr != "" {
		if s, ok := parseOrderStatus(statusStr); ok {
			status = s
		}
	}
	if h.store == nil {
		c.JSON(http.StatusOK, gin.H{"orders": []interface{}{}})
		return
	}
	orders, err := h.store.ListByUser(userID, pair, status, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list orders"})
		return
	}
	if orders == nil {
		orders = []*matching.Order{}
	}
	result := make([]gin.H, len(orders))
	for i, o := range orders {
		result[i] = orderToJSON(o)
	}
	c.JSON(http.StatusOK, gin.H{"orders": result, "limit": limit, "offset": offset})
}

func (h *OrderHandler) GetOrderbook(c *gin.Context) {
	pair := c.Param("pair")
	engine := h.getEngine(pair)
	if engine == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported pair: " + pair})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	c.JSON(http.StatusOK, engine.OrderBook.Depth(limit))
}

func (h *OrderHandler) GetTrades(c *gin.Context) {
	pair := c.Param("pair")
	engine := h.getEngine(pair)
	if engine == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported pair"})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	// Prefer the in-memory market data recorder (real-time) and fall back to
	// the persisted store when available.
	if engine.MD != nil {
		trades := engine.MD.RecentTrades(limit)
		result := make([]gin.H, len(trades))
		for i, t := range trades {
			result[i] = tradeToJSON(t)
		}
		c.JSON(http.StatusOK, gin.H{"trades": result})
		return
	}
	if h.store != nil {
		trades, err := h.store.GetTrades(pair, limit)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get trades"})
			return
		}
		if trades == nil {
			trades = []*matching.Trade{}
		}
		result := make([]gin.H, len(trades))
		for i, t := range trades {
			result[i] = tradeToJSON(t)
		}
		c.JSON(http.StatusOK, gin.H{"trades": result})
		return
	}
	c.JSON(http.StatusOK, gin.H{"trades": []interface{}{}})
}

func (h *OrderHandler) GetTicker(c *gin.Context) {
	pair := c.Param("pair")
	engine := h.getEngine(pair)
	if engine == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported pair: " + pair})
		return
	}
	// Prefer the rich 24h ticker from the market data recorder, fall back to a
	// minimal top-of-book snapshot if the recorder is unavailable.
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
	r := gin.H{"pair": pair, "timestamp": time.Now().UnixNano()}
	if b := engine.OrderBook.BestBid(); b != nil {
		r["bid"] = b.Price.String()
		r["bid_qty"] = b.RemainingQty.String()
	}
	if a := engine.OrderBook.BestAsk(); a != nil {
		r["ask"] = a.Price.String()
		r["ask_qty"] = a.RemainingQty.String()
	}
	spread := "0"
	if r["bid"] != nil && r["ask"] != nil {
		bid, _ := new(big.Float).SetString(r["bid"].(string))
		ask, _ := new(big.Float).SetString(r["ask"].(string))
		if bid != nil && ask != nil {
			s := new(big.Float).Sub(ask, bid)
			spread = s.Text('f', 2)
		}
	}
	r["spread"] = spread
	c.JSON(http.StatusOK, r)
}

// GetTicker24h returns the full 24h rolling ticker. Same payload as GetTicker
// when the market data recorder is wired.
func (h *OrderHandler) GetTicker24h(c *gin.Context) {
	h.GetTicker(c)
}

// ListTickers returns a 24h ticker for every active pair (Binance-style).
func (h *OrderHandler) ListTickers(c *gin.Context) {
	engines := h.allEngines()
	out := make([]gin.H, 0, len(engines))
	for _, engine := range engines {
		if engine.MD == nil {
			continue
		}
		var bid, ask *big.Float
		if b := engine.OrderBook.BestBid(); b != nil {
			bid = b.Price
		}
		if a := engine.OrderBook.BestAsk(); a != nil {
			ask = a.Price
		}
		t := engine.MD.Ticker(bid, ask)
		out = append(out, tickerToJSON(t))
	}
	c.JSON(http.StatusOK, gin.H{"tickers": out})
}

func (h *OrderHandler) GetCandles(c *gin.Context) {
	pair := c.Param("pair")
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
	var start, end int64
	if s := c.Query("start_time"); s != "" {
		if v, err := strconv.ParseInt(s, 10, 64); err == nil {
			start = v
		}
	}
	if e := c.Query("end_time"); e != "" {
		if v, err := strconv.ParseInt(e, 10, 64); err == nil {
			end = v
		}
	}

	var candles []*matching.Candle
	if h.candleStore != nil {
		stored, err := h.candleStore.Candles(pair, interval, start, end, limit)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load candles"})
			return
		}
		candles = stored
	} else if engine := h.getEngine(pair); engine != nil && engine.MD != nil {
		candles = engine.MD.Candles(interval, limit, start, end)
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

// getEngine resolves a matching engine by pair, preferring the exchange facade.
func (h *OrderHandler) getEngine(pair string) *matching.MatchingEngine {
	if h.exchange != nil {
		return h.exchange.Get(pair)
	}
	return h.engines[pair]
}

// allEngines returns the full engine map, using the exchange facade when set.
func (h *OrderHandler) allEngines() map[string]*matching.MatchingEngine {
	if h.exchange != nil {
		return h.exchange.Engines()
	}
	return h.engines
}

// ListCandleIntervals returns the supported candle intervals.
func (h *OrderHandler) ListCandleIntervals(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"intervals": []string{"1s", "1m", "3m", "5m", "15m", "30m", "1h", "2h", "4h", "6h", "8h", "12h", "1d", "3d", "1w", "1M"},
	})
}

func orderToJSON(o *matching.Order) gin.H {
	return gin.H{
		"id": o.ID, "client_order_id": o.ClientOrderID, "user_id": o.UserID, "pair": o.Pair,
		"side":         sideStr(o.Side),
		"type":         o.Type.String(),
		"price":        safeFloatStr(o.Price),
		"stop_price":   safeFloatStr(o.StopPrice),
		"quantity":     safeFloatStr(o.Quantity),
		"filled_qty":   safeFloatStr(o.FilledQty),
		"remaining":    safeFloatStr(o.RemainingQty),
		"iceberg_qty":  safeFloatStr(o.IcebergQty),
		"visible_qty":  safeFloatStr(o.VisibleQty),
		"time_in_force": tifStr(o.TimeInForce),
		"status":       o.Status.String(),
		"created_at":   o.CreatedAt,
		"updated_at":   o.UpdatedAt,
	}
}

func tradeToJSON(t *matching.Trade) gin.H {
	side := t.TakerSide.String()
	return gin.H{
		"id": t.ID, "buy_order": t.BuyOrderID, "sell_order": t.SellOrderID,
		"pair": t.Pair, "price": safeFloatStr(t.Price),
		"quantity": safeFloatStr(t.Quantity), "taker_side": side,
		"side": side, "time": t.CreatedAt,
	}
}

func tickerToJSON(t *matching.Ticker) gin.H {
	return gin.H{
		"pair":             t.Pair,
		"last":             safeFloatStr(t.LastPrice),
		"last_price":       safeFloatStr(t.LastPrice),
		"bid":              safeFloatStr(t.Bid),
		"ask":              safeFloatStr(t.Ask),
		"spread":           safeFloatStr(t.Spread),
		"volume_24h":       safeFloatStr(t.Volume24H),
		"quote_volume_24h": safeFloatStr(t.QuoteVolume24H),
		"high_24h":         safeFloatStr(t.High24H),
		"low_24h":          safeFloatStr(t.Low24H),
		"open_24h":         safeFloatStr(t.Open24H),
		"change_24h":       safeFloatStr(t.Change24H),
		"change_pct_24h":   safeFloatStr(t.ChangePct24H),
		"timestamp":        t.Timestamp,
	}
}

func candleToJSON(c *matching.Candle) gin.H {
	return gin.H{
		"pair": c.Pair, "interval": c.Interval,
		"open": safeFloatStr(c.Open), "high": safeFloatStr(c.High),
		"low": safeFloatStr(c.Low), "close": safeFloatStr(c.Close),
		"volume": safeFloatStr(c.Volume),
		"timestamp": c.Timestamp, "close_time": c.CloseTime,
	}
}

func sideStr(s matching.Side) string {
	if s == matching.Buy {
		return "buy"
	}
	return "sell"
}

func tifStr(t matching.TimeInForce) string {
	switch t {
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

func parseOrderStatus(s string) (matching.OrderStatus, bool) {
	switch s {
	case "new":
		return matching.New, true
	case "partially_filled":
		return matching.PartiallyFilled, true
	case "filled":
		return matching.Filled, true
	case "cancelled", "canceled":
		return matching.Cancelled, true
	case "rejected":
		return matching.Rejected, true
	case "expired":
		return matching.Expired, true
	}
	return -1, false
}

func safeFloatStr(f *big.Float) string {
	if f == nil {
		return "0"
	}
	return f.Text('f', 8)
}
