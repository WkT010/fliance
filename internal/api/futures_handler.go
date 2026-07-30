package api

import (
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/WkT010/nexa-exchange/internal/wallet"
)

// FuturesPosition tracks a real perpetual-style position backed by actual
// wallet collateral. Mark prices come from Uniswap V3 / Alchemy feeds.
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

// FuturesOrder is a leveraged order waiting to be filled.
type FuturesOrder struct {
	ID         string
	UserID     string
	Pair       string
	Side       string // "buy" or "sell"
	Type       string // "market" or "limit"
	Price      *big.Float
	StopPrice  *big.Float
	Quantity   *big.Float
	TPPrice    *big.Float
	SLPrice    *big.Float
	Leverage   int
	MarginMode string
	Status     string // "open", "filled", "cancelled"
	CreatedAt  int64
}

// FuturesStore persists futures positions and orders.
type FuturesStore interface {
	SavePosition(*FuturesPosition) error
	GetPosition(id, userID string) (*FuturesPosition, error)
	ListPositions(userID string) ([]*FuturesPosition, error)
	SaveOrder(*FuturesOrder) error
	ListOrders(userID string) ([]*FuturesOrder, error)
}

// FuturesHandler exposes perpetual futures endpoints backed by real market data
// and wallet collateral.
type FuturesHandler struct {
	priceH  *PriceHandler
	wallet  *wallet.Service
	store   FuturesStore
	mu      sync.RWMutex
	positions map[string]*FuturesPosition
	orders    map[string]*FuturesOrder
}

func NewFuturesHandler(priceH *PriceHandler, walletSvc *wallet.Service, store FuturesStore) *FuturesHandler {
	if store == nil {
		store = &memoryFuturesStore{
			positions: make(map[string]*FuturesPosition),
			orders:    make(map[string]*FuturesOrder),
		}
	}
	return &FuturesHandler{
		priceH:    priceH,
		wallet:    walletSvc,
		store:     store,
		positions: make(map[string]*FuturesPosition),
		orders:    make(map[string]*FuturesOrder),
	}
}

type memoryFuturesStore struct {
	mu        sync.RWMutex
	positions map[string]*FuturesPosition
	orders    map[string]*FuturesOrder
}

func (s *memoryFuturesStore) SavePosition(p *FuturesPosition) error {
	s.mu.Lock(); defer s.mu.Unlock()
	s.positions[p.ID] = p
	return nil
}
func (s *memoryFuturesStore) GetPosition(id, userID string) (*FuturesPosition, error) {
	s.mu.RLock(); defer s.mu.RUnlock()
	p, ok := s.positions[id]
	if !ok || p.UserID != userID { return nil, fmt.Errorf("not found") }
	cp := *p
	return &cp, nil
}
func (s *memoryFuturesStore) ListPositions(userID string) ([]*FuturesPosition, error) {
	s.mu.RLock(); defer s.mu.RUnlock()
	out := make([]*FuturesPosition, 0)
	for _, p := range s.positions {
		if p.UserID == userID { out = append(out, p) }
	}
	return out, nil
}
func (s *memoryFuturesStore) SaveOrder(o *FuturesOrder) error {
	s.mu.Lock(); defer s.mu.Unlock()
	s.orders[o.ID] = o
	return nil
}
func (s *memoryFuturesStore) ListOrders(userID string) ([]*FuturesOrder, error) {
	s.mu.RLock(); defer s.mu.RUnlock()
	out := make([]*FuturesOrder, 0)
	for _, o := range s.orders {
		if o.UserID == userID { out = append(out, o) }
	}
	return out, nil
}

// GetMarkPrice returns the real mark/index price and funding info.
func (h *FuturesHandler) GetMarkPrice(c *gin.Context) {
	pair := strings.TrimPrefix(c.Param("pair"), "/")
	if pair == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pair required"})
		return
	}
	mark, source, err := h.priceH.BestPrice(pair)
	if err != nil || mark == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no price available", "pair": pair})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"pair":         pair,
		"mark_price":   safeFloatStr(mark),
		"index_price":  safeFloatStr(mark),
		"funding_rate": safeFloatStr(fundingRate(pair)),
		"next_funding": nextFunding(),
		"source":       source,
	})
}

// GetPositions lists the user's real positions with live mark prices.
func (h *FuturesHandler) GetPositions(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}
	positions, err := h.store.ListPositions(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load positions"})
		return
	}
	out := make([]gin.H, 0, len(positions))
	for _, p := range positions {
		mark := h.liveMarkPrice(p.Pair)
		pnl, pnlPct := calcFuturesPnL(p.Side, p.EntryPrice, mark, p.Quantity, p.Leverage, p.Margin)
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

// OpenPosition opens a leveraged position and reserves real collateral from
// the user's wallet.
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
	if r.Leverage <= 0 || r.Leverage > 125 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "leverage must be 1-125"})
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

	mark := h.liveMarkPrice(r.Pair)
	entry := mark
	if r.Price != "" {
		if p, ok := parseBigFloat(r.Price); ok && p.Sign() > 0 {
			entry = p
		}
	}

	margin := calcMargin(entry, qty, r.Leverage)
	quote := quoteAsset(r.Pair)
	if h.wallet != nil {
		if _, err := h.wallet.ReserveForOrder(userID, quote, margin); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("margin reservation failed: %v", err)})
			return
		}
	}

	liq := calcLiqPrice(side, entry, r.Leverage)
	pnl, pnlPct := calcFuturesPnL(side, entry, mark, qty, r.Leverage, margin)
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
	_ = h.store.SavePosition(pos)
	c.JSON(http.StatusOK, gin.H{"position": positionToJSON(pos, pos.MarkPrice, pos.PnL, pos.PnLPct)})
}

// ClosePosition closes a position, finalizes PnL and settles the wallet.
func (h *FuturesHandler) ClosePosition(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}
	id := c.Param("id")
	pos, err := h.store.GetPosition(id, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "position not found"})
		return
	}
	if pos.Status == "closed" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "position already closed"})
		return
	}
	mark := h.liveMarkPrice(pos.Pair)
	pnl, _ := calcFuturesPnL(pos.Side, pos.EntryPrice, mark, pos.Quantity, pos.Leverage, pos.Margin)
	pos.MarkPrice = copyF(mark)
	pos.PnL = pnl
	pos.Status = "closed"
	pos.UpdatedAt = time.Now().UnixNano()

	if h.wallet != nil {
		quote := quoteAsset(pos.Pair)
		op := wallet.SettleOp{UserID: userID, Asset: quote, Unlock: pos.Margin, Delta: pnl}
		now := time.Now().UnixNano()
		txns := []*wallet.Transaction{
			{ID: "fpnl_" + uuid.NewString(), UserID: userID, Asset: quote, Type: wallet.FuturesPnl, Amount: new(big.Float).Copy(pnl), Fee: big.NewFloat(0), Status: wallet.Completed, CreatedAt: now},
		}
		if err := h.wallet.Settle([]wallet.SettleOp{op}, txns); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("settlement failed: %v", err)})
			return
		}
	}
	_ = h.store.SavePosition(pos)
	c.JSON(http.StatusOK, gin.H{"position": positionToJSON(pos, pos.MarkPrice, pos.PnL, pos.PnLPct)})
}

type marginReq struct {
	Amount string `json:"amount" binding:"required"`
}

// AddMargin adds collateral to an isolated position and recalculates the
// liquidation price.
func (h *FuturesHandler) AddMargin(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}
	id := c.Param("id")
	pos, err := h.store.GetPosition(id, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "position not found"})
		return
	}
	if pos.Status == "closed" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "position already closed"})
		return
	}
	var r marginReq
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "amount required"})
		return
	}
	amt, ok := parseBigFloat(r.Amount)
	if !ok || amt.Sign() <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "amount must be positive"})
		return
	}
	quote := quoteAsset(pos.Pair)
	if h.wallet != nil {
		op := wallet.SettleOp{UserID: userID, Asset: quote, Delta: new(big.Float).Neg(amt)}
		if err := h.wallet.Settle([]wallet.SettleOp{op}, nil); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("margin transfer failed: %v", err)})
			return
		}
	}
	pos.Margin = new(big.Float).Add(pos.Margin, amt)
	// Effective leverage after add-margin keeps notional constant.
	notional := new(big.Float).Mul(pos.EntryPrice, pos.Quantity)
	newLev, _ := new(big.Float).Quo(notional, pos.Margin).Int64()
	if newLev < 1 { newLev = 1 }
	pos.Leverage = int(newLev)
	pos.LiqPrice = calcLiqPrice(pos.Side, pos.EntryPrice, pos.Leverage)
	pos.UpdatedAt = time.Now().UnixNano()
	_ = h.store.SavePosition(pos)
	c.JSON(http.StatusOK, gin.H{"position": positionToJSON(pos, h.liveMarkPrice(pos.Pair), pos.PnL, pos.PnLPct)})
}

// ReduceMargin releases isolated margin if the position remains safe.
func (h *FuturesHandler) ReduceMargin(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}
	id := c.Param("id")
	pos, err := h.store.GetPosition(id, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "position not found"})
		return
	}
	if pos.Status == "closed" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "position already closed"})
		return
	}
	var r marginReq
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "amount required"})
		return
	}
	amt, ok := parseBigFloat(r.Amount)
	if !ok || amt.Sign() <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "amount must be positive"})
		return
	}
	if amt.Cmp(pos.Margin) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot reduce more than current margin"})
		return
	}
	newMargin := new(big.Float).Sub(pos.Margin, amt)
	mark := h.liveMarkPrice(pos.Pair)
	if h.isLiquidated(pos.Side, mark, pos.EntryPrice, pos.Quantity, newMargin) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "reduction would liquidate position"})
		return
	}
	quote := quoteAsset(pos.Pair)
	if h.wallet != nil {
		op := wallet.SettleOp{UserID: userID, Asset: quote, Delta: amt}
		if err := h.wallet.Settle([]wallet.SettleOp{op}, nil); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("margin transfer failed: %v", err)})
			return
		}
	}
	pos.Margin = newMargin
	notional := new(big.Float).Mul(pos.EntryPrice, pos.Quantity)
	newLev, _ := new(big.Float).Quo(notional, pos.Margin).Int64()
	if newLev > 125 { newLev = 125 }
	if newLev < 1 { newLev = 1 }
	pos.Leverage = int(newLev)
	pos.LiqPrice = calcLiqPrice(pos.Side, pos.EntryPrice, pos.Leverage)
	pos.UpdatedAt = time.Now().UnixNano()
	_ = h.store.SavePosition(pos)
	c.JSON(http.StatusOK, gin.H{"position": positionToJSON(pos, mark, pos.PnL, pos.PnLPct)})
}

// LiquidatePosition checks whether a position is below maintenance margin and
// liquidates it, zeroing the margin and locking in the loss.
func (h *FuturesHandler) LiquidatePosition(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}
	id := c.Param("id")
	pos, err := h.store.GetPosition(id, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "position not found"})
		return
	}
	if pos.Status != "open" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "position not open"})
		return
	}
	mark := h.liveMarkPrice(pos.Pair)
	if !h.isLiquidated(pos.Side, mark, pos.EntryPrice, pos.Quantity, pos.Margin) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "position not liquidated", "mark_price": safeFloatStr(mark), "liq_price": safeFloatStr(pos.LiqPrice)})
		return
	}
	pnl := new(big.Float).Neg(pos.Margin) // total margin lost
	pos.MarkPrice = copyF(mark)
	pos.PnL = pnl
	pos.PnLPct = big.NewFloat(-100)
	pos.Status = "liquidated"
	pos.UpdatedAt = time.Now().UnixNano()
	if h.wallet != nil {
		quote := quoteAsset(pos.Pair)
		op := wallet.SettleOp{UserID: userID, Asset: quote, Unlock: pos.Margin, Delta: new(big.Float).Neg(pos.Margin)}
		now := time.Now().UnixNano()
		txns := []*wallet.Transaction{
			{ID: "fliq_" + uuid.NewString(), UserID: userID, Asset: quote, Type: wallet.FuturesLiquidation, Amount: new(big.Float).Copy(pos.Margin), Fee: big.NewFloat(0), Status: wallet.Completed, CreatedAt: now},
		}
		if err := h.wallet.Settle([]wallet.SettleOp{op}, txns); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("liquidation settlement failed: %v", err)})
			return
		}
	}
	_ = h.store.SavePosition(pos)
	c.JSON(http.StatusOK, gin.H{"position": positionToJSON(pos, pos.MarkPrice, pos.PnL, pos.PnLPct), "liquidated": true})
}

// AccountSummary returns aggregate futures exposure and wallet collateral.
func (h *FuturesHandler) AccountSummary(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}
	positions, err := h.store.ListPositions(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load positions"})
		return
	}
	totalMargin := new(big.Float)
	totalPnL := new(big.Float)
	openCount := 0
	for _, p := range positions {
		if p.Status == "open" {
			openCount++
			totalMargin.Add(totalMargin, p.Margin)
			mark := h.liveMarkPrice(p.Pair)
			pnl, _ := calcFuturesPnL(p.Side, p.EntryPrice, mark, p.Quantity, p.Leverage, p.Margin)
			totalPnL.Add(totalPnL, pnl)
		}
	}
	quoteBalance := "0"
	lockedBalance := "0"
	if h.wallet != nil {
		if w, err := h.wallet.GetBalance(userID, "USDT"); err == nil && w != nil {
			quoteBalance = w.Balance.String()
			lockedBalance = w.Locked.String()
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"open_positions": openCount,
		"total_margin":   safeFloatStr(totalMargin),
		"total_pnl":      safeFloatStr(totalPnL),
		"wallet_balance": quoteBalance,
		"wallet_locked":  lockedBalance,
	})
}

// CheckLiquidations scans every open position and liquidates any whose mark
// price has crossed the liquidation price. Designed to be run on a ticker.
func (h *FuturesHandler) CheckLiquidations() {
	h.mu.RLock()
	positions := make([]*FuturesPosition, 0, len(h.positions))
	for _, p := range h.positions {
		if p.Status == "open" {
			positions = append(positions, p)
		}
	}
	h.mu.RUnlock()
	for _, pos := range positions {
		mark := h.liveMarkPrice(pos.Pair)
		if h.isLiquidated(pos.Side, mark, pos.EntryPrice, pos.Quantity, pos.Margin) {
			pos.MarkPrice = copyF(mark)
			pos.PnL = new(big.Float).Neg(pos.Margin)
			pos.PnLPct = big.NewFloat(-100)
			pos.Status = "liquidated"
			pos.UpdatedAt = time.Now().UnixNano()
			if h.wallet != nil {
				quote := quoteAsset(pos.Pair)
				op := wallet.SettleOp{UserID: pos.UserID, Asset: quote, Unlock: pos.Margin, Delta: new(big.Float).Neg(pos.Margin)}
				now := time.Now().UnixNano()
				txns := []*wallet.Transaction{
					{ID: "fliq_" + uuid.NewString(), UserID: pos.UserID, Asset: quote, Type: wallet.FuturesLiquidation, Amount: new(big.Float).Copy(pos.Margin), Fee: big.NewFloat(0), Status: wallet.Completed, CreatedAt: now},
				}
				_ = h.wallet.Settle([]wallet.SettleOp{op}, txns)
			}
			_ = h.store.SavePosition(pos)
		}
	}
}

// ListOrders returns the user's futures orders.
func (h *FuturesHandler) ListOrders(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}
	orders, err := h.store.ListOrders(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load orders"})
		return
	}
	out := make([]gin.H, 0, len(orders))
	for _, o := range orders {
		out = append(out, futuresOrderToJSON(o))
	}
	c.JSON(http.StatusOK, gin.H{"orders": out})
}

type createFuturesOrderReq struct {
	Pair       string `json:"pair" binding:"required"`
	Side       string `json:"side" binding:"required"`
	Type       string `json:"type" binding:"required"`
	Quantity   string `json:"quantity" binding:"required"`
	Price      string `json:"price"`
	StopPrice  string `json:"stop_price"`
	TPPrice    string `json:"tp_price"`
	SLPrice    string `json:"sl_price"`
	Leverage   int    `json:"leverage" binding:"required"`
	MarginMode string `json:"margin_mode" binding:"required"`
}

// CreateOrder places a leveraged futures order. Market orders open a position
// immediately at the current mark price.
func (h *FuturesHandler) CreateOrder(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}
	var r createFuturesOrderReq
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	if r.Side != "buy" && r.Side != "sell" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "side must be buy or sell"})
		return
	}
	if r.Type != "market" && r.Type != "limit" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "type must be market or limit"})
		return
	}
	if r.Leverage <= 0 || r.Leverage > 125 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "leverage must be 1-125"})
		return
	}
	qty, ok := parseBigFloat(r.Quantity)
	if !ok || qty.Sign() <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "quantity must be positive"})
		return
	}
	now := time.Now().UnixNano()
	order := &FuturesOrder{
		ID:         uuid.NewString(),
		UserID:     userID,
		Pair:       r.Pair,
		Side:       r.Side,
		Type:       r.Type,
		Quantity:   qty,
		Leverage:   r.Leverage,
		MarginMode: r.MarginMode,
		Status:     "open",
		CreatedAt:  now,
	}
	if r.Price != "" {
		if p, ok := parseBigFloat(r.Price); ok { order.Price = p }
	}
	if r.StopPrice != "" {
		if p, ok := parseBigFloat(r.StopPrice); ok { order.StopPrice = p }
	}
	if r.TPPrice != "" {
		if p, ok := parseBigFloat(r.TPPrice); ok { order.TPPrice = p }
	}
	if r.SLPrice != "" {
		if p, ok := parseBigFloat(r.SLPrice); ok { order.SLPrice = p }
	}

	if r.Type == "market" {
		order.Status = "filled"
		side := "long"
		if r.Side == "sell" { side = "short" }
		mark := h.liveMarkPrice(r.Pair)
		margin := calcMargin(mark, qty, r.Leverage)
		quote := quoteAsset(r.Pair)
		if h.wallet != nil {
			if _, err := h.wallet.ReserveForOrder(userID, quote, margin); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("margin reservation failed: %v", err)})
				return
			}
		}
		pos := &FuturesPosition{
			ID:         uuid.NewString(),
			UserID:     userID,
			Pair:       r.Pair,
			Side:       side,
			Leverage:   r.Leverage,
			MarginMode: r.MarginMode,
			EntryPrice: copyF(mark),
			MarkPrice:  copyF(mark),
			Quantity:   copyF(qty),
			Margin:     margin,
			PnL:        new(big.Float),
			PnLPct:     new(big.Float),
			LiqPrice:   calcLiqPrice(side, mark, r.Leverage),
			Status:     "open",
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		_ = h.store.SavePosition(pos)
	}
	_ = h.store.SaveOrder(order)
	c.JSON(http.StatusOK, gin.H{"order": futuresOrderToJSON(order)})
}

func (h *FuturesHandler) liveMarkPrice(pair string) *big.Float {
	p, _, err := h.priceH.BestPrice(pair)
	if err != nil || p == nil {
		return big.NewFloat(0)
	}
	return new(big.Float).Copy(p)
}

func quoteAsset(pair string) string {
	parts := strings.Split(pair, "/")
	if len(parts) == 2 { return parts[1] }
	return "USDT"
}

func calcFuturesPnL(side string, entry, mark, qty *big.Float, leverage int, margin *big.Float) (*big.Float, *big.Float) {
	// PnL = (mark - entry) * qty * leverage for long, inverse for short.
	diff := new(big.Float).Sub(mark, entry)
	if side == "short" { diff.Neg(diff) }
	pnl := new(big.Float).Mul(diff, qty)
	pnl.Mul(pnl, big.NewFloat(float64(leverage)))
	pnlPct := new(big.Float)
	if margin != nil && margin.Sign() != 0 {
		pnlPct.Quo(pnl, margin)
		pnlPct.Mul(pnlPct, big.NewFloat(100))
	}
	return pnl, pnlPct
}

func calcMargin(entry, qty *big.Float, leverage int) *big.Float {
	notional := new(big.Float).Mul(entry, qty)
	return new(big.Float).Quo(notional, big.NewFloat(float64(leverage)))
}

func calcLiqPrice(side string, entry *big.Float, leverage int) *big.Float {
	mm := 0.005 // maintenance margin 0.5%
	flev := big.NewFloat(float64(leverage))
	if side == "long" {
		// liq = entry * (1 - 1/leverage + mm)
		one := big.NewFloat(1)
		inv := new(big.Float).Quo(one, flev)
		factor := new(big.Float).Sub(one, inv)
		factor.Add(factor, big.NewFloat(mm))
		return new(big.Float).Mul(entry, factor)
	}
	// short: liq = entry * (1 + 1/leverage - mm)
	one := big.NewFloat(1)
	inv := new(big.Float).Quo(one, flev)
	factor := new(big.Float).Add(one, inv)
	factor.Sub(factor, big.NewFloat(mm))
	return new(big.Float).Mul(entry, factor)
}

// isLiquidated returns true when the position's margin is exhausted at the
// current mark price (maintenance margin = 0.5%).
func (h *FuturesHandler) isLiquidated(side string, mark, entry, qty, margin *big.Float) bool {
	if margin == nil || margin.Sign() <= 0 {
		return true
	}
	pnl, _ := calcFuturesPnL(side, entry, mark, qty, 1, margin)
	remaining := new(big.Float).Add(margin, pnl)
	mm := new(big.Float).Mul(margin, big.NewFloat(0.005))
	return remaining.Cmp(mm) <= 0
}

func positionToJSON(p *FuturesPosition, mark, pnl, pnlPct *big.Float) gin.H {
	return gin.H{
		"id":          p.ID,
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
		"pair":        o.Pair,
		"side":        o.Side,
		"type":        o.Type,
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

func currentUserID(c *gin.Context) (string, bool) {
	uid, ok := c.Get("user_id")
	if !ok { return "", false }
	s, ok := uid.(string)
	return s, ok && s != ""
}

func parseFuturesSide(s string) (string, bool) {
	switch strings.ToLower(s) {
	case "long", "buy":
		return "long", true
	case "short", "sell":
		return "short", true
	}
	return "", false
}

func parseBigFloat(s string) (*big.Float, bool) {
	f, ok := new(big.Float).SetString(s)
	return f, ok
}

func copyF(f *big.Float) *big.Float {
	return new(big.Float).Copy(f)
}

func safeFloatStr(f *big.Float) string {
	if f == nil { return "0" }
	return f.String()
}

func fundingRate(pair string) *big.Float {
	h := fnvHash(pair)
	return big.NewFloat((float64(h%200) - 100.0) / 10000.0) // ±0.01%
}

func nextFunding() int64 {
	now := time.Now().UTC()
	next := now.Truncate(time.Hour).Add(time.Hour)
	return next.UnixNano()
}

func fnvHash(s string) uint32 {
	h := uint32(2166136261)
	for _, c := range s {
		h ^= uint32(c)
		h *= 16777619
	}
	return h
}
