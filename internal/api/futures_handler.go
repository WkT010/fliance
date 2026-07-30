package api

import (
	"fmt"
	"hash/fnv"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/WkT010/nexa-exchange/internal/matching"
)

// FuturesPosition represents a simulated perpetual futures position.
type FuturesPosition struct {
	ID         string
	UserID     string
	Pair       string
	Side       string // "long" or "short"
	Leverage   int
	MarginMode string // "isolated" or "cross"
	EntryPrice *big.Float
	MarkPrice  *big.Float
	Quantity   *big.Float
	Margin     *big.Float
	PnL        *big.Float
	PnLPct     *big.Float
	LiqPrice   *big.Float
	Status     string // "open" or "closed"
	CreatedAt  int64
	UpdatedAt  int64
}

// FuturesOrder represents a simulated futures order.
type FuturesOrder struct {
	ID         string
	UserID     string
	Pair       string
	Side       matching.Side
	Type       matching.OrderType
	Price      *big.Float
	StopPrice  *big.Float
	Quantity   *big.Float
	TPPrice    *big.Float
	SLPrice    *big.Float
	Leverage   int
	MarginMode string
	Status     string
	CreatedAt  int64
}

// FuturesHandler is an in-memory simulator for perpetual futures.
type FuturesHandler struct {
	priceH    *PriceHandler
	mu        sync.RWMutex
	positions map[string]*FuturesPosition
	orders    map[string]*FuturesOrder
}

// NewFuturesHandler creates a new in-memory futures simulator.
func NewFuturesHandler(priceH *PriceHandler) *FuturesHandler {
	return &FuturesHandler{
		priceH:    priceH,
		positions: make(map[string]*FuturesPosition),
		orders:    make(map[string]*FuturesOrder),
	}
}

// GetMarkPrice returns simulated mark price, index price and funding info.
// GET /api/v2/futures/mark-price/*pair
func (h *FuturesHandler) GetMarkPrice(c *gin.Context) {
	pair := strings.TrimPrefix(c.Param("pair"), "/")
	if pair == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pair required"})
		return
	}
	mark := h.markPrice(pair)
	index := new(big.Float).SetPrec(128).Copy(mark)
	c.JSON(http.StatusOK, gin.H{
		"pair":         pair,
		"mark_price":   safeFloatStr(mark),
		"index_price":  safeFloatStr(index),
		"funding_rate": safeFloatStr(fundingRate(pair)),
		"next_funding": nextFunding(),
	})
}

// GetPositions lists all open positions for the authenticated user.
// GET /api/v2/futures/positions
func (h *FuturesHandler) GetPositions(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()

	out := make([]gin.H, 0)
	for _, p := range h.positions {
		if p.UserID != userID {
			continue
		}
		mark := h.markPrice(p.Pair)
		pnl := calcPnL(p.Side, p.EntryPrice, mark, p.Quantity, p.Leverage)
		pnlPct := calcPnLPct(pnl, p.Margin)
		out = append(out, positionToJSON(p, mark, pnl, pnlPct))
	}
	c.JSON(http.StatusOK, gin.H{"positions": out})
}

type openPositionReq struct {
	Pair       string `json:"pair" binding:"required"`
	Side       string `json:"side" binding:"required"`
	Leverage   int    `json:"leverage" binding:"required"`
	MarginMode string `json:"margin_mode" binding:"required"`
	Quantity   string `json:"quantity" binding:"required"`
	Price      string `json:"price"`
}

// OpenPosition opens a new simulated futures position.
// POST /api/v2/futures/positions
func (h *FuturesHandler) OpenPosition(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}
	var r openPositionReq
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	side, ok := parseFuturesSide(r.Side)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "side must be long or short"})
		return
	}
	if r.Leverage <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "leverage must be positive"})
		return
	}
	if r.MarginMode != "isolated" && r.MarginMode != "cross" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "margin_mode must be isolated or cross"})
		return
	}
	qty, ok := parseBigFloat(r.Quantity)
	if !ok || qty.Sign() <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "quantity must be positive"})
		return
	}

	mark := h.markPrice(r.Pair)
	entry := mark
	if r.Price != "" {
		if p, ok := parseBigFloat(r.Price); ok && p.Sign() > 0 {
			entry = p
		}
	}

	margin := calcMargin(entry, qty, r.Leverage)
	liq := calcLiqPrice(side, entry, r.Leverage)
	pnl := calcPnL(side, entry, mark, qty, r.Leverage)
	pnlPct := calcPnLPct(pnl, margin)
	now := time.Now().UnixNano()

	pos := &FuturesPosition{
		ID:         uuid.NewString(),
		UserID:     userID,
		Pair:       r.Pair,
		Side:       side,
		Leverage:   r.Leverage,
		MarginMode: r.MarginMode,
		EntryPrice: copyF(entry),
		MarkPrice:  copyF(mark),
		Quantity:   copyF(qty),
		Margin:     margin,
		PnL:        pnl,
		PnLPct:     pnlPct,
		LiqPrice:   liq,
		Status:     "open",
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	h.mu.Lock()
	h.positions[pos.ID] = pos
	h.mu.Unlock()

	c.JSON(http.StatusOK, gin.H{"position": positionToJSON(pos, pos.MarkPrice, pos.PnL, pos.PnLPct)})
}

// ClosePosition closes an existing position and finalizes PnL.
// POST /api/v2/futures/positions/:id/close
func (h *FuturesHandler) ClosePosition(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}
	id := c.Param("id")

	h.mu.Lock()
	defer h.mu.Unlock()
	pos, ok := h.positions[id]
	if !ok || pos.UserID != userID {
		c.JSON(http.StatusNotFound, gin.H{"error": "position not found"})
		return
	}
	if pos.Status == "closed" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "position already closed"})
		return
	}

	mark := h.markPrice(pos.Pair)
	pnl := calcPnL(pos.Side, pos.EntryPrice, mark, pos.Quantity, pos.Leverage)
	pnlPct := calcPnLPct(pnl, pos.Margin)
	pos.MarkPrice = copyF(mark)
	pos.PnL = pnl
	pos.PnLPct = pnlPct
	pos.Status = "closed"
	pos.UpdatedAt = time.Now().UnixNano()

	c.JSON(http.StatusOK, gin.H{"position": positionToJSON(pos, pos.MarkPrice, pos.PnL, pos.PnLPct)})
}

type createOrderReq struct {
	Pair       string `json:"pair" binding:"required"`
	Side       string `json:"side" binding:"required"`
	Type       string `json:"type" binding:"required"`
	Quantity   string `json:"quantity" binding:"required"`
	Price      string `json:"price"`
	StopPrice  string `json:"stop_price"`
	Leverage   int    `json:"leverage" binding:"required"`
	MarginMode string `json:"margin_mode" binding:"required"`
	TPPrice    string `json:"tp_price"`
	SLPrice    string `json:"sl_price"`
}

// CreateOrder creates a simulated futures order. Market orders fill immediately
// and open a position; limit orders stay open.
// POST /api/v2/futures/orders
func (h *FuturesHandler) CreateOrder(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}
	var r createOrderReq
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	side, ok := parseMatchingSide(r.Side)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "side must be buy or sell"})
		return
	}
	oType, ok := parseOrderType(r.Type)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "type must be market or limit"})
		return
	}
	if oType != matching.Market && oType != matching.Limit {
		c.JSON(http.StatusBadRequest, gin.H{"error": "only market and limit orders are supported"})
		return
	}
	if r.Leverage <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "leverage must be positive"})
		return
	}
	if r.MarginMode != "isolated" && r.MarginMode != "cross" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "margin_mode must be isolated or cross"})
		return
	}
	qty, ok := parseBigFloat(r.Quantity)
	if !ok || qty.Sign() <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "quantity must be positive"})
		return
	}

	price := optionalBigFloat(r.Price)
	stopPrice := optionalBigFloat(r.StopPrice)
	tpPrice := optionalBigFloat(r.TPPrice)
	slPrice := optionalBigFloat(r.SLPrice)

	if oType == matching.Limit && (price == nil || price.Sign() <= 0) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "limit orders require a price"})
		return
	}

	order := &FuturesOrder{
		ID:         uuid.NewString(),
		UserID:     userID,
		Pair:       r.Pair,
		Side:       side,
		Type:       oType,
		Price:      price,
		StopPrice:  stopPrice,
		Quantity:   qty,
		TPPrice:    tpPrice,
		SLPrice:    slPrice,
		Leverage:   r.Leverage,
		MarginMode: r.MarginMode,
		Status:     "open",
		CreatedAt:  time.Now().UnixNano(),
	}

	h.mu.Lock()
	h.orders[order.ID] = order

	if oType == matching.Market {
		order.Status = "filled"
		mark := h.markPrice(r.Pair)
		posSide := "long"
		if side == matching.Sell {
			posSide = "short"
		}
		margin := calcMargin(mark, qty, r.Leverage)
		liq := calcLiqPrice(posSide, mark, r.Leverage)
		now := time.Now().UnixNano()
		pos := &FuturesPosition{
			ID:         uuid.NewString(),
			UserID:     userID,
			Pair:       r.Pair,
			Side:       posSide,
			Leverage:   r.Leverage,
			MarginMode: r.MarginMode,
			EntryPrice: copyF(mark),
			MarkPrice:  copyF(mark),
			Quantity:   copyF(qty),
			Margin:     margin,
			PnL:        newF(0),
			PnLPct:     newF(0),
			LiqPrice:   liq,
			Status:     "open",
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		h.positions[pos.ID] = pos
	}
	h.mu.Unlock()

	c.JSON(http.StatusOK, gin.H{"order": futuresOrderToJSON(order)})
}

// ListOrders returns all simulated futures orders for the authenticated user.
// GET /api/v2/futures/orders
func (h *FuturesHandler) ListOrders(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()

	out := make([]gin.H, 0)
	for _, o := range h.orders {
		if o.UserID != userID {
			continue
		}
		out = append(out, futuresOrderToJSON(o))
	}
	c.JSON(http.StatusOK, gin.H{"orders": out})
}

// markPrice returns the current mark price for a pair. It prefers the best
// available spot price; otherwise it falls back to a deterministic value.
func (h *FuturesHandler) markPrice(pair string) *big.Float {
	if price, _, err := h.priceH.BestPrice(pair); err == nil && price != nil {
		return new(big.Float).SetPrec(128).Copy(price)
	}
	return fallbackPrice(pair)
}

// fallbackPrice returns a stable synthetic price derived from the pair string.
func fallbackPrice(pair string) *big.Float {
	h := fnv.New64a()
	_, _ = h.Write([]byte(pair))
	n := h.Sum64()
	whole := new(big.Float).SetPrec(128).SetUint64(n % 100000)
	price := new(big.Float).SetPrec(128).Quo(whole, newF(100))
	price.Add(price, newF(0.01))
	return price
}

// fundingRate returns a deterministic funding rate for the current hour.
func fundingRate(pair string) *big.Float {
	hour := time.Now().UTC().Hour()
	h := fnv.New64a()
	_, _ = h.Write([]byte(fmt.Sprintf("%s:%d", pair, hour)))
	n := h.Sum64()
	step := int64(n%21) - 10 // -10..10
	return new(big.Float).SetPrec(128).SetFloat64(float64(step) * 0.00001)
}

// nextFunding returns the Unix timestamp of the next hour boundary.
func nextFunding() int64 {
	now := time.Now().UTC()
	next := now.Truncate(time.Hour).Add(time.Hour)
	return next.Unix()
}

func calcMargin(entry, qty *big.Float, leverage int) *big.Float {
	notional := new(big.Float).SetPrec(128).Mul(entry, qty)
	lev := newF(float64(leverage))
	return new(big.Float).SetPrec(128).Quo(notional, lev)
}

func calcPnL(side string, entry, mark, qty *big.Float, leverage int) *big.Float {
	diff := new(big.Float).SetPrec(128).Sub(mark, entry)
	if side == "short" {
		diff.Sub(entry, mark)
	}
	qtyAcc := new(big.Float).SetPrec(128).Mul(diff, qty)
	lev := newF(float64(leverage))
	return new(big.Float).SetPrec(128).Mul(qtyAcc, lev)
}

func calcPnLPct(pnl, margin *big.Float) *big.Float {
	if margin == nil || margin.Sign() == 0 {
		return newF(0)
	}
	pct := new(big.Float).SetPrec(128).Quo(pnl, margin)
	return pct.Mul(pct, newF(100))
}

func calcLiqPrice(side string, entry *big.Float, leverage int) *big.Float {
	invLev := new(big.Float).SetPrec(128).Quo(newF(1), newF(float64(leverage)))
	factor := newF(1)
	if side == "long" {
		factor.Sub(factor, invLev)
		factor.Add(factor, newF(0.005))
	} else {
		factor.Add(factor, invLev)
		factor.Sub(factor, newF(0.005))
	}
	return new(big.Float).SetPrec(128).Mul(entry, factor)
}

func positionToJSON(p *FuturesPosition, mark, pnl, pnlPct *big.Float) gin.H {
	return gin.H{
		"id":          p.ID,
		"user_id":     p.UserID,
		"pair":        p.Pair,
		"side":        p.Side,
		"leverage":    p.Leverage,
		"margin_mode": p.MarginMode,
		"entry_price": safeFloatStr(p.EntryPrice),
		"mark_price":  safeFloatStr(mark),
		"quantity":    safeFloatStr(p.Quantity),
		"margin":      safeFloatStr(p.Margin),
		"pnl":         safeFloatStr(pnl),
		"pnl_pct":     safeFloatStr(pnlPct),
		"liq_price":   safeFloatStr(p.LiqPrice),
		"status":      p.Status,
		"created_at":  p.CreatedAt,
		"updated_at":  p.UpdatedAt,
	}
}

func futuresOrderToJSON(o *FuturesOrder) gin.H {
	return gin.H{
		"id":          o.ID,
		"user_id":     o.UserID,
		"pair":        o.Pair,
		"side":        o.Side.String(),
		"type":        o.Type.String(),
		"price":       safeFloatStr(o.Price),
		"stop_price":  safeFloatStr(o.StopPrice),
		"quantity":    safeFloatStr(o.Quantity),
		"tp_price":    safeFloatStr(o.TPPrice),
		"sl_price":    safeFloatStr(o.SLPrice),
		"leverage":    o.Leverage,
		"margin_mode": o.MarginMode,
		"status":      o.Status,
		"created_at":  o.CreatedAt,
	}
}

func parseFuturesSide(s string) (string, bool) {
	switch strings.ToLower(s) {
	case "long":
		return "long", true
	case "short":
		return "short", true
	default:
		return "", false
	}
}

func parseMatchingSide(s string) (matching.Side, bool) {
	switch strings.ToLower(s) {
	case "buy":
		return matching.Buy, true
	case "sell":
		return matching.Sell, true
	default:
		return 0, false
	}
}

func parseOrderType(s string) (matching.OrderType, bool) {
	switch strings.ToLower(s) {
	case "limit":
		return matching.Limit, true
	case "market":
		return matching.Market, true
	default:
		return 0, false
	}
}

func parseBigFloat(s string) (*big.Float, bool) {
	if s == "" {
		return nil, false
	}
	f, _, err := big.ParseFloat(s, 10, 128, big.ToNearestEven)
	if err != nil {
		return nil, false
	}
	return f, true
}

func optionalBigFloat(s string) *big.Float {
	if s == "" {
		return nil
	}
	f, _, err := big.ParseFloat(s, 10, 128, big.ToNearestEven)
	if err != nil {
		return nil
	}
	return f
}

func copyF(x *big.Float) *big.Float {
	if x == nil {
		return nil
	}
	return new(big.Float).SetPrec(128).Copy(x)
}

// newF returns a *big.Float with 128-bit precision set to x.
func newF(x float64) *big.Float {
	return new(big.Float).SetPrec(128).SetFloat64(x)
}

func currentUserID(c *gin.Context) (string, bool) {
	uid, _ := c.Get("user_id")
	userID, ok := uid.(string)
	return userID, ok && userID != ""
}

// safeFloatStr returns a decimal string for a big.Float, or "0" for nil.
// It is shared by several handlers in this package.
func safeFloatStr(f *big.Float) string {
	if f == nil {
		return "0"
	}
	return f.Text('f', -1)
}
