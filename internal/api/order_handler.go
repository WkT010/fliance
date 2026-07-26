package api

import (
	"math/big"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/WkT010/nexa-exchange/internal/matching"
)

type OrderStore interface {
	Save(*matching.Order) error
	Get(string) (*matching.Order, error)
	ListByUser(userID, pair string, status matching.OrderStatus, limit, offset int) ([]*matching.Order, error)
	SaveTrade(*matching.Trade) error
	GetTrades(pair string, limit int) ([]*matching.Trade, error)
}

type OrderHandler struct {
	engines map[string]*matching.MatchingEngine
	store   OrderStore
}

func NewOrderHandler(engines map[string]*matching.MatchingEngine, store OrderStore) *OrderHandler {
	return &OrderHandler{engines: engines, store: store}
}

type placeOrderReq struct {
	Pair        string `json:"pair" binding:"required"`
	Side        string `json:"side" binding:"required,oneof=buy sell"`
	Type        string `json:"type" binding:"required,oneof=limit market fok ioc iceberg stop_loss stop_limit"`
	Price       string `json:"price"`
	StopPrice   string `json:"stop_price"`
	Quantity    string `json:"quantity" binding:"required"`
	TimeInForce string `json:"time_in_force"`
	IcebergQty  string `json:"iceberg_qty"`
}

func (h *OrderHandler) PlaceOrder(c *gin.Context) {
	var r placeOrderReq
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(400, gin.H{"error": "invalid order: " + err.Error()})
		return
	}
	engine, ok := h.engines[r.Pair]
	if !ok {
		c.JSON(400, gin.H{"error": "unsupported trading pair: " + r.Pair})
		return
	}
	uid, _ := c.Get("user_id")
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
	default:
		c.JSON(400, gin.H{"error": "invalid order type"})
		return
	}
	var price *big.Float
	if r.Price != "" {
		price = new(big.Float)
		if _, _, err := price.Parse(r.Price, 10); err != nil || price.Sign() <= 0 {
			c.JSON(400, gin.H{"error": "invalid price"})
			return
		}
	}
	var stopPrice *big.Float
	if r.StopPrice != "" {
		stopPrice = new(big.Float)
		if _, _, err := stopPrice.Parse(r.StopPrice, 10); err != nil || stopPrice.Sign() <= 0 {
			c.JSON(400, gin.H{"error": "invalid stop price"})
			return
		}
	}
	qty := new(big.Float)
	if _, _, err := qty.Parse(r.Quantity, 10); err != nil || qty.Sign() <= 0 {
		c.JSON(400, gin.H{"error": "invalid quantity"})
		return
	}
	if (ot == matching.Limit || ot == matching.Iceberg || ot == matching.StopLimit) && (price == nil || price.Sign() <= 0) {
		c.JSON(400, gin.H{"error": "price required for limit/iceberg/stop-limit orders"})
		return
	}
	if (ot == matching.StopLoss || ot == matching.StopLimit) && (stopPrice == nil || stopPrice.Sign() <= 0) {
		c.JSON(400, gin.H{"error": "stop price required for stop orders"})
		return
	}
	o := matching.NewOrder(uid.(string), r.Pair, side, ot, price, qty)
	o.StopPrice = stopPrice
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
				c.JSON(400, gin.H{"error": "invalid iceberg quantity"})
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
	if !engine.SubmitOrder(o) {
		c.JSON(503, gin.H{"error": "matching engine busy"})
		return
	}
	if h.store != nil {
		go h.store.Save(o)
	}
	c.JSON(200, gin.H{"order_id": o.ID, "status": o.Status.String()})
}

func (h *OrderHandler) CancelOrder(c *gin.Context) {
	uid, _ := c.Get("user_id")
	pair := c.Query("pair")
	if pair == "" {
		c.JSON(400, gin.H{"error": "pair query parameter required"})
		return
	}
	engine, ok := h.engines[pair]
	if !ok {
		c.JSON(400, gin.H{"error": "unsupported pair"})
		return
	}
	o := engine.OrderBook.Get(c.Param("id"))
	if o == nil {
		c.JSON(404, gin.H{"error": "order not found"})
		return
	}
	if o.UserID != uid.(string) {
		c.JSON(403, gin.H{"error": "order does not belong to user"})
		return
	}
	engine.OrderBook.Remove(o.ID)
	o.Status = matching.Cancelled
	o.UpdatedAt = time.Now().UnixNano()
	if h.store != nil {
		go h.store.Save(o)
	}
	c.JSON(200, gin.H{"order_id": o.ID, "status": "cancelled"})
}

func (h *OrderHandler) GetOrder(c *gin.Context) {
	pair := c.Query("pair")
	if pair == "" {
		c.JSON(400, gin.H{"error": "pair query parameter required"})
		return
	}
	engine, ok := h.engines[pair]
	if !ok {
		c.JSON(400, gin.H{"error": "unsupported pair"})
		return
	}
	o := engine.OrderBook.Get(c.Param("id"))
	if o == nil && h.store != nil {
		var err error
		o, err = h.store.Get(c.Param("id"))
		if err != nil {
			c.JSON(404, gin.H{"error": "order not found"})
			return
		}
	}
	if o == nil {
		c.JSON(404, gin.H{"error": "order not found"})
		return
	}
	c.JSON(200, orderToJSON(o))
}

func (h *OrderHandler) ListOrders(c *gin.Context) {
	uid, _ := c.Get("user_id")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	pair := c.Query("pair")
	if h.store == nil {
		c.JSON(200, gin.H{"orders": []interface{}{}})
		return
	}
	orders, err := h.store.ListByUser(uid.(string), pair, -1, limit, offset)
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to list orders"})
		return
	}
	if orders == nil {
		orders = []*matching.Order{}
	}
	result := make([]gin.H, len(orders))
	for i, o := range orders {
		result[i] = orderToJSON(o)
	}
	c.JSON(200, gin.H{"orders": result})
}

func (h *OrderHandler) GetOrderbook(c *gin.Context) {
	pair := c.Param("pair")
	engine, ok := h.engines[pair]
	if !ok {
		c.JSON(400, gin.H{"error": "unsupported pair: " + pair})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	c.JSON(200, engine.OrderBook.Depth(limit))
}

func (h *OrderHandler) GetTrades(c *gin.Context) {
	pair := c.Param("pair")
	if _, ok := h.engines[pair]; !ok {
		c.JSON(400, gin.H{"error": "unsupported pair"})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	if h.store != nil {
		trades, err := h.store.GetTrades(pair, limit)
		if err != nil {
			c.JSON(500, gin.H{"error": "failed to get trades"})
			return
		}
		if trades == nil {
			trades = []*matching.Trade{}
		}
		result := make([]gin.H, len(trades))
		for i, t := range trades {
			result[i] = tradeToJSON(t)
		}
		c.JSON(200, gin.H{"trades": result})
		return
	}
	c.JSON(200, gin.H{"trades": []interface{}{}})
}

func (h *OrderHandler) GetTicker(c *gin.Context) {
	pair := c.Param("pair")
	engine, ok := h.engines[pair]
	if !ok {
		c.JSON(400, gin.H{"error": "unsupported pair: " + pair})
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
	c.JSON(200, r)
}

func (h *OrderHandler) GetCandles(c *gin.Context) {
	pair := c.Param("pair")
	if _, ok := h.engines[pair]; !ok {
		c.JSON(400, gin.H{"error": "unsupported pair"})
		return
	}
	c.JSON(200, gin.H{"candles": []interface{}{}})
}

func orderToJSON(o *matching.Order) gin.H {
	return gin.H{
		"id": o.ID, "user_id": o.UserID, "pair": o.Pair,
		"side":       map[bool]string{true: "buy", false: "sell"}[o.Side == matching.Buy],
		"type":       orderTypeStr(o.Type),
		"price":      safeFloatStr(o.Price),
		"stop_price": safeFloatStr(o.StopPrice),
		"quantity":   safeFloatStr(o.Quantity),
		"filled_qty": safeFloatStr(o.FilledQty),
		"remaining":  safeFloatStr(o.RemainingQty),
		"status":     o.Status.String(),
		"created_at": o.CreatedAt,
		"updated_at": o.UpdatedAt,
	}
}

func tradeToJSON(t *matching.Trade) gin.H {
	return gin.H{
		"id": t.ID, "buy_order": t.BuyOrderID, "sell_order": t.SellOrderID,
		"pair": t.Pair, "price": safeFloatStr(t.Price),
		"quantity": safeFloatStr(t.Quantity), "created_at": t.CreatedAt,
	}
}

func orderTypeStr(t matching.OrderType) string {
	switch t {
	case matching.Limit:
		return "limit"
	case matching.Market:
		return "market"
	case matching.StopLoss:
		return "stop_loss"
	case matching.StopLimit:
		return "stop_limit"
	case matching.Iceberg:
		return "iceberg"
	case matching.FillOrKill:
		return "fok"
	case matching.ImmediateOrCancel:
		return "ioc"
	default:
		return "unknown"
	}
}

func safeFloatStr(f *big.Float) string {
	if f == nil {
		return "0"
	}
	return f.Text('f', 8)
}
