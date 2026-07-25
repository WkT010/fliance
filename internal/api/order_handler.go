package api

import (
	"math/big"
	"net/http"
	"strconv"
	"github.com/gin-gonic/gin"
	"github.com/WkT010/nexa-exchange/internal/matching"
)

type OrderStore interface {
	Save(*matching.Order) error
	Get(string) (*matching.Order, error)
	ListByUser(userID, pair string, status matching.OrderStatus, limit, offset int) ([]*matching.Order, error)
}

type OrderHandler struct{ engine *matching.MatchingEngine; store OrderStore }

func NewOrderHandler(engine *matching.MatchingEngine, store OrderStore) *OrderHandler {
	return &OrderHandler{engine: engine, store: store}
}

type req struct {
	Pair, Side, Type, Price, Quantity, TimeInForce string
}

func (h *OrderHandler) PlaceOrder(c *gin.Context) {
	var r req
	if err := c.ShouldBindJSON(&r); err != nil { c.JSON(400, gin.H{"error": err.Error()}); return }
	uid, _ := c.Get("user_id")
	side := matching.Buy; if r.Side == "sell" { side = matching.Sell }
	var ot matching.OrderType
	switch r.Type { case "limit": ot = matching.Limit; case "market": ot = matching.Market; case "fok": ot = matching.FillOrKill; case "ioc": ot = matching.ImmediateOrCancel; default: c.JSON(400, gin.H{"error": "bad type"}); return }
	var price *big.Float
	if r.Price != "" { price = new(big.Float); if _, _, err := price.Parse(r.Price, 10); err != nil { c.JSON(400, gin.H{"error": "bad price"}); return } }
	qty := new(big.Float)
	if _, _, err := qty.Parse(r.Quantity, 10); err != nil { c.JSON(400, gin.H{"error": "bad qty"}); return }
	o := matching.NewOrder(uid.(string), r.Pair, side, ot, price, qty)
	if r.TimeInForce == "ioc" { o.TimeInForce = matching.IOC }
	if !h.engine.SubmitOrder(o) { c.JSON(503, gin.H{"error": "busy"}); return }
	c.JSON(200, gin.H{"order_id": o.ID, "status": statusStr(o.Status)})
}

func (h *OrderHandler) CancelOrder(c *gin.Context) {
	uid, _ := c.Get("user_id")
	o := h.engine.OrderBook.Get(c.Param("id"))
	if o == nil { c.JSON(404, gin.H{"error": "not found"}); return }
	if o.UserID != uid.(string) { c.JSON(403, gin.H{"error": "not yours"}); return }
	h.engine.OrderBook.Remove(o.ID); o.Status = matching.Cancelled
	c.JSON(200, gin.H{"order_id": o.ID, "status": "cancelled"})
}

func (h *OrderHandler) GetOrder(c *gin.Context) {
	o := h.engine.OrderBook.Get(c.Param("id"))
	if o == nil { c.JSON(404, gin.H{"error": "not found"}); return }
	c.JSON(200, o)
}

func (h *OrderHandler) ListOrders(c *gin.Context) {
	uid, _ := c.Get("user_id")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if h.store == nil { c.JSON(200, gin.H{"orders": []interface{}{}}); return }
	orders, _ := h.store.ListByUser(uid.(string), c.Query("pair"), -1, limit, offset)
	if orders == nil { orders = []*matching.Order{} }
	c.JSON(200, gin.H{"orders": orders})
}

func (h *OrderHandler) GetOrderbook(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	c.JSON(200, h.engine.OrderBook.Depth(limit))
}

func (h *OrderHandler) GetTrades(c *gin.Context) { c.JSON(200, gin.H{"trades": []interface{}{}}) }
func (h *OrderHandler) GetTicker(c *gin.Context) {
	r := gin.H{"pair": c.Param("pair")}
	if b := h.engine.OrderBook.BestBid(); b != nil { r["bid"] = b.Price.String() }
	if a := h.engine.OrderBook.BestAsk(); a != nil { r["ask"] = a.Price.String() }
	c.JSON(200, r)
}
func (h *OrderHandler) GetCandles(c *gin.Context) { c.JSON(200, gin.H{"candles": []interface{}{}}) }

func statusStr(s matching.OrderStatus) string {
	switch s { case matching.New: return "new"; case matching.Filled: return "filled"; case matching.Cancelled: return "cancelled"; case matching.Rejected: return "rejected"; default: return "unknown" }
}