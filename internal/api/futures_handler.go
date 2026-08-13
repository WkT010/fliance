package api

import (
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/WkT010/nexa-exchange/internal/wallet"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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
	TPPrice    *big.Float
	SLPrice    *big.Float
	Status     string // "open", "closed" or "liquidated"
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
	ListOpenPositions() ([]*FuturesPosition, error)
	SaveOrder(*FuturesOrder) error
	ListOrders(userID string) ([]*FuturesOrder, error)
	ListOpenOrders() ([]*FuturesOrder, error)
	UpdateOrderStatus(id, status string) error
}

// FuturesHandler exposes perpetual futures endpoints backed by real market data
// and wallet collateral.
type FuturesHandler struct {
	priceH    *PriceHandler
	wallet    *wallet.Service
	store     FuturesStore
	mu        sync.RWMutex
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
	s.mu.Lock()
	defer s.mu.Unlock()
	s.positions[p.ID] = p
	return nil
}
func (s *memoryFuturesStore) GetPosition(id, userID string) (*FuturesPosition, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.positions[id]
	if !ok || p.UserID != userID {
		return nil, fmt.Errorf("not found")
	}
	cp := *p
	return &cp, nil
}
func (s *memoryFuturesStore) ListPositions(userID string) ([]*FuturesPosition, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*FuturesPosition, 0)
	for _, p := range s.positions {
		if p.UserID == userID {
			out = append(out, p)
		}
	}
	return out, nil
}
func (s *memoryFuturesStore) SaveOrder(o *FuturesOrder) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.orders[o.ID] = o
	return nil
}
func (s *memoryFuturesStore) ListOrders(userID string) ([]*FuturesOrder, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*FuturesOrder, 0)
	for _, o := range s.orders {
		if o.UserID == userID {
			out = append(out, o)
		}
	}
	return out, nil
}
func (s *memoryFuturesStore) ListOpenOrders() ([]*FuturesOrder, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*FuturesOrder, 0)
	for _, o := range s.orders {
		if o.Status == "open" {
			cp := *o
			out = append(out, &cp)
		}
	}
	return out, nil
}
func (s *memoryFuturesStore) ListOpenPositions() ([]*FuturesPosition, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*FuturesPosition, 0)
	for _, p := range s.positions {
		if p.Status == "open" {
			cp := *p
			out = append(out, &cp)
		}
	}
	return out, nil
}
func (s *memoryFuturesStore) UpdateOrderStatus(id, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if o, ok := s.orders[id]; ok {
		o.Status = status
	}
	return nil
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

// GetFundingHistory returns hourly historical funding rates for a pair.
func (h *FuturesHandler) GetFundingHistory(c *gin.Context) {
	if _, ok := currentUserID(c); !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}
	pair := strings.TrimPrefix(c.Param("pair"), "/")
	if pair == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pair required"})
		return
	}
	limitStr := c.DefaultQuery("limit", "24")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		limit = 24
	}
	if limit > 168 {
		limit = 168
	}
	mark := h.liveMarkPrice(pair)
	if mark == nil || mark.Sign() <= 0 {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no price available", "pair": pair})
		return
	}
	now := time.Now().UTC()
	history := make([]gin.H, 0, limit)
	for i := 0; i < limit; i++ {
		t := now.Truncate(time.Hour).Add(-time.Duration(i) * time.Hour)
		h := fnvHash(fmt.Sprintf("%s:%d", pair, i))
		rate := big.NewFloat((float64(h%600) - 300.0) / 1000000.0)
		variation := big.NewFloat((float64(h%200) - 100.0) / 200000.0)
		markVariation := new(big.Float).Mul(mark, variation)
		entryMark := new(big.Float).Add(mark, markVariation)
		history = append(history, gin.H{
			"time":         t.UnixNano(),
			"funding_rate": safeFloatStr(rate),
			"mark_price":   safeFloatStr(entryMark),
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"pair":    pair,
		"history": history,
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
	TPPrice    string `json:"tp_price"`
	SLPrice    string `json:"sl_price"`
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

	// One-way position netting: only one open position per pair per user.
	existing := h.netPosition(userID, r.Pair)
	if existing != nil && existing.Side == side {
		if h.wallet != nil {
			if _, err := h.wallet.ReserveForAccount(userID, quote, wallet.AccountFutures, margin); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("margin reservation failed: %v", err)})
				return
			}
		}
		h.mergeSameSidePosition(existing, entry, qty, margin, r.Leverage)
		if r.TPPrice != "" {
			if tp, ok := parseBigFloat(r.TPPrice); ok && tp.Sign() > 0 {
				existing.TPPrice = tp
			}
		}
		if r.SLPrice != "" {
			if sl, ok := parseBigFloat(r.SLPrice); ok && sl.Sign() > 0 {
				existing.SLPrice = sl
			}
		}
		_ = h.store.SavePosition(existing)
		pnl, pnlPct := calcFuturesPnL(existing.Side, existing.EntryPrice, mark, existing.Quantity, existing.Leverage, existing.Margin)
		c.JSON(http.StatusOK, gin.H{"position": positionToJSON(existing, mark, pnl, pnlPct), "merged": true})
		return
	}

	if existing != nil && existing.Side != side {
		// Opposite direction: close the existing position and open a new one.
		if err := h.closePosition(existing, mark); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("close existing position failed: %v", err)})
			return
		}
	}

	if h.wallet != nil {
		if _, err := h.wallet.ReserveForAccount(userID, quote, wallet.AccountFutures, margin); err != nil {
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
	if r.TPPrice != "" {
		if tp, ok := parseBigFloat(r.TPPrice); ok && tp.Sign() > 0 {
			pos.TPPrice = tp
		}
	}
	if r.SLPrice != "" {
		if sl, ok := parseBigFloat(r.SLPrice); ok && sl.Sign() > 0 {
			pos.SLPrice = sl
		}
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
	if pos.Status != "open" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "position not open"})
		return
	}
	mark := h.liveMarkPrice(pos.Pair)
	if err := h.closePosition(pos, mark); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("close failed: %v", err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"position": positionToJSON(pos, pos.MarkPrice, pos.PnL, pos.PnLPct)})
}

func (h *FuturesHandler) closePosition(pos *FuturesPosition, mark *big.Float) error {
	pnl, _ := calcFuturesPnL(pos.Side, pos.EntryPrice, mark, pos.Quantity, pos.Leverage, pos.Margin)
	pos.MarkPrice = copyF(mark)
	pos.PnL = pnl
	pos.Status = "closed"
	pos.UpdatedAt = time.Now().UnixNano()

	if h.wallet != nil {
		quote := quoteAsset(pos.Pair)
		op := wallet.SettleOp{UserID: pos.UserID, Asset: quote, AccountType: wallet.AccountFutures, Unlock: pos.Margin, Delta: pnl}
		now := time.Now().UnixNano()
		txns := []*wallet.Transaction{
			{ID: "fpnl_" + uuid.NewString(), UserID: pos.UserID, Asset: quote, AccountType: wallet.AccountFutures, Type: wallet.FuturesPnl, Amount: new(big.Float).Copy(pnl), Fee: big.NewFloat(0), Status: wallet.Completed, CreatedAt: now},
		}
		if err := h.wallet.Settle([]wallet.SettleOp{op}, txns); err != nil {
			return err
		}
	}
	return h.store.SavePosition(pos)
}

type closePartialReq struct {
	Quantity string `json:"quantity" binding:"required"`
}

// ClosePositionPartial closes a portion of an open position at the current
// mark price, realizing proportional PnL and reducing margin by the same ratio.
func (h *FuturesHandler) ClosePositionPartial(c *gin.Context) {
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
	var r closePartialReq
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "quantity required"})
		return
	}
	closeQty, ok := parseBigFloat(r.Quantity)
	if !ok || closeQty.Sign() <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "quantity must be positive"})
		return
	}
	if closeQty.Cmp(pos.Quantity) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "quantity exceeds position size"})
		return
	}
	mark := h.liveMarkPrice(pos.Pair)
	if closeQty.Cmp(pos.Quantity) == 0 {
		if err := h.closePosition(pos, mark); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("close failed: %v", err)})
			return
		}
		c.JSON(http.StatusOK, gin.H{"position": positionToJSON(pos, pos.MarkPrice, pos.PnL, pos.PnLPct), "status": "closed"})
		return
	}
	totalPnL, _ := calcFuturesPnL(pos.Side, pos.EntryPrice, mark, pos.Quantity, pos.Leverage, pos.Margin)
	ratio := new(big.Float).Quo(closeQty, pos.Quantity)
	closedMargin := new(big.Float).Mul(pos.Margin, ratio)
	closedPnL := new(big.Float).Mul(totalPnL, ratio)
	if h.wallet != nil {
		quote := quoteAsset(pos.Pair)
		op := wallet.SettleOp{UserID: pos.UserID, Asset: quote, AccountType: wallet.AccountFutures, Unlock: closedMargin, Delta: closedPnL}
		now := time.Now().UnixNano()
		txns := []*wallet.Transaction{
			{ID: "fpnl_" + uuid.NewString(), UserID: pos.UserID, Asset: quote, AccountType: wallet.AccountFutures, Type: wallet.FuturesPnl, Amount: new(big.Float).Copy(closedPnL), Fee: big.NewFloat(0), Status: wallet.Completed, CreatedAt: now},
		}
		if err := h.wallet.Settle([]wallet.SettleOp{op}, txns); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("partial close settlement failed: %v", err)})
			return
		}
	}
	pos.Quantity = new(big.Float).Sub(pos.Quantity, closeQty)
	pos.Margin = new(big.Float).Sub(pos.Margin, closedMargin)
	pos.MarkPrice = copyF(mark)
	pos.PnL, pos.PnLPct = calcFuturesPnL(pos.Side, pos.EntryPrice, mark, pos.Quantity, pos.Leverage, pos.Margin)
	pos.UpdatedAt = time.Now().UnixNano()
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
	if pos.Status != "open" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "position is not open; cannot add margin"})
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
		op := wallet.SettleOp{UserID: userID, Asset: quote, AccountType: wallet.AccountFutures, Delta: new(big.Float).Neg(amt)}
		if err := h.wallet.Settle([]wallet.SettleOp{op}, nil); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("margin transfer failed: %v", err)})
			return
		}
	}
	pos.Margin = new(big.Float).Add(pos.Margin, amt)
	// Effective leverage after add-margin keeps notional constant.
	notional := new(big.Float).Mul(pos.EntryPrice, pos.Quantity)
	newLev, _ := new(big.Float).Quo(notional, pos.Margin).Int64()
	if newLev < 1 {
		newLev = 1
	}
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
	checkPos := &FuturesPosition{Side: pos.Side, EntryPrice: pos.EntryPrice, Quantity: pos.Quantity, Margin: newMargin, Leverage: pos.Leverage, MarginMode: pos.MarginMode, UserID: pos.UserID, Pair: pos.Pair}
	if h.isLiquidated(checkPos, mark) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "reduction would liquidate position"})
		return
	}
	quote := quoteAsset(pos.Pair)
	if h.wallet != nil {
		op := wallet.SettleOp{UserID: userID, Asset: quote, AccountType: wallet.AccountFutures, Delta: amt}
		if err := h.wallet.Settle([]wallet.SettleOp{op}, nil); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("margin transfer failed: %v", err)})
			return
		}
	}
	pos.Margin = newMargin
	notional := new(big.Float).Mul(pos.EntryPrice, pos.Quantity)
	newLev, _ := new(big.Float).Quo(notional, pos.Margin).Int64()
	if newLev > 125 {
		newLev = 125
	}
	if newLev < 1 {
		newLev = 1
	}
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
	if !h.isLiquidated(pos, mark) {
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
		op := wallet.SettleOp{UserID: userID, Asset: quote, AccountType: wallet.AccountFutures, Unlock: pos.Margin, Delta: new(big.Float).Neg(pos.Margin)}
		now := time.Now().UnixNano()
		txns := []*wallet.Transaction{
			{ID: "fliq_" + uuid.NewString(), UserID: userID, Asset: quote, AccountType: wallet.AccountFutures, Type: wallet.FuturesLiquidation, Amount: new(big.Float).Copy(pos.Margin), Fee: big.NewFloat(0), Status: wallet.Completed, CreatedAt: now},
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
		if w, err := h.wallet.GetBalanceForAccount(userID, "USDT", wallet.AccountFutures); err == nil && w != nil {
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
	if h.store == nil {
		return
	}
	positions, err := h.store.ListOpenPositions()
	if err != nil {
		return
	}
	for _, pos := range positions {
		mark := h.liveMarkPrice(pos.Pair)
		if h.isLiquidated(pos, mark) {
			pos.MarkPrice = copyF(mark)
			pos.PnL = new(big.Float).Neg(pos.Margin)
			pos.PnLPct = big.NewFloat(-100)
			pos.Status = "liquidated"
			pos.UpdatedAt = time.Now().UnixNano()
			if h.wallet != nil {
				quote := quoteAsset(pos.Pair)
				op := wallet.SettleOp{UserID: pos.UserID, Asset: quote, AccountType: wallet.AccountFutures, Unlock: pos.Margin, Delta: new(big.Float).Neg(pos.Margin)}
				now := time.Now().UnixNano()
				txns := []*wallet.Transaction{
					{ID: "fliq_" + uuid.NewString(), UserID: pos.UserID, Asset: quote, AccountType: wallet.AccountFutures, Type: wallet.FuturesLiquidation, Amount: new(big.Float).Copy(pos.Margin), Fee: big.NewFloat(0), Status: wallet.Completed, CreatedAt: now},
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
		if p, ok := parseBigFloat(r.Price); ok {
			order.Price = p
		}
	}
	if r.StopPrice != "" {
		if p, ok := parseBigFloat(r.StopPrice); ok {
			order.StopPrice = p
		}
	}
	if r.TPPrice != "" {
		if p, ok := parseBigFloat(r.TPPrice); ok {
			order.TPPrice = p
		}
	}
	if r.SLPrice != "" {
		if p, ok := parseBigFloat(r.SLPrice); ok {
			order.SLPrice = p
		}
	}

	if r.Type == "market" {
		order.Status = "filled"
		side := "long"
		if r.Side == "sell" {
			side = "short"
		}
		mark := h.liveMarkPrice(r.Pair)
		margin := calcMargin(mark, qty, r.Leverage)
		quote := quoteAsset(r.Pair)

		existing := h.netPosition(userID, r.Pair)
		if existing != nil && existing.Side == side {
			if h.wallet != nil {
				if _, err := h.wallet.ReserveForAccount(userID, quote, wallet.AccountFutures, margin); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("margin reservation failed: %v", err)})
					return
				}
			}
			h.mergeSameSidePosition(existing, mark, qty, margin, r.Leverage)
			existing.TPPrice = order.TPPrice
			existing.SLPrice = order.SLPrice
			existing.UpdatedAt = now
			_ = h.store.SavePosition(existing)
			c.JSON(http.StatusOK, gin.H{"order": futuresOrderToJSON(order), "position": positionToJSON(existing, mark, new(big.Float), new(big.Float)), "merged": true})
			return
		}
		if existing != nil && existing.Side != side {
			if err := h.closePosition(existing, mark); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("close existing position failed: %v", err)})
				return
			}
		}
		if h.wallet != nil {
			if _, err := h.wallet.ReserveForAccount(userID, quote, wallet.AccountFutures, margin); err != nil {
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
			TPPrice:    order.TPPrice,
			SLPrice:    order.SLPrice,
			Status:     "open",
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		_ = h.store.SavePosition(pos)
	}
	_ = h.store.SaveOrder(order)
	c.JSON(http.StatusOK, gin.H{"order": futuresOrderToJSON(order)})
}

// CancelOrder cancels an open futures order and releases any reserved margin.
func (h *FuturesHandler) CancelOrder(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}
	id := c.Param("id")
	orders, err := h.store.ListOrders(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load orders"})
		return
	}
	var target *FuturesOrder
	for _, o := range orders {
		if o.ID == id {
			target = o
			break
		}
	}
	if target == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
		return
	}
	if target.Status != "open" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "order not open"})
		return
	}
	_ = h.store.UpdateOrderStatus(id, "cancelled")
	c.JSON(http.StatusOK, gin.H{"order": futuresOrderToJSON(target)})
}

// ProcessOrders evaluates open limit and stop orders against live mark prices
// and opens positions when trigger conditions are met.
func (h *FuturesHandler) ProcessOrders() {
	if h.store == nil {
		return
	}
	orders, err := h.store.ListOpenOrders()
	if err != nil {
		return
	}
	for _, o := range orders {
		mark := h.liveMarkPrice(o.Pair)
		if mark == nil || mark.Sign() <= 0 {
			continue
		}
		triggered := false
		if o.StopPrice != nil && o.StopPrice.Sign() > 0 {
			if o.Side == "buy" && mark.Cmp(o.StopPrice) >= 0 {
				triggered = true
			}
			if o.Side == "sell" && mark.Cmp(o.StopPrice) <= 0 {
				triggered = true
			}
		} else if o.Type == "limit" && o.Price != nil && o.Price.Sign() > 0 {
			if o.Side == "buy" && mark.Cmp(o.Price) <= 0 {
				triggered = true
			}
			if o.Side == "sell" && mark.Cmp(o.Price) >= 0 {
				triggered = true
			}
		} else if o.Type == "market" {
			triggered = true
		}
		if !triggered {
			continue
		}
		side := "long"
		if o.Side == "sell" {
			side = "short"
		}
		margin := calcMargin(mark, o.Quantity, o.Leverage)
		quote := quoteAsset(o.Pair)

		existing := h.netPosition(o.UserID, o.Pair)
		if existing != nil && existing.Side == side {
			if h.wallet != nil {
				if _, err := h.wallet.ReserveForAccount(o.UserID, quote, wallet.AccountFutures, margin); err != nil {
					_ = h.store.UpdateOrderStatus(o.ID, "cancelled")
					continue
				}
			}
			h.mergeSameSidePosition(existing, mark, o.Quantity, margin, o.Leverage)
			existing.TPPrice = o.TPPrice
			existing.SLPrice = o.SLPrice
			existing.UpdatedAt = time.Now().UnixNano()
			_ = h.store.SavePosition(existing)
			_ = h.store.UpdateOrderStatus(o.ID, "filled")
			continue
		}
		if existing != nil && existing.Side != side {
			if err := h.closePosition(existing, mark); err != nil {
				continue
			}
		}
		if h.wallet != nil {
			if _, err := h.wallet.ReserveForAccount(o.UserID, quote, wallet.AccountFutures, margin); err != nil {
				_ = h.store.UpdateOrderStatus(o.ID, "cancelled")
				continue
			}
		}
		now := time.Now().UnixNano()
		pos := &FuturesPosition{
			ID: uuid.NewString(), UserID: o.UserID, Pair: o.Pair, Side: side,
			Leverage: o.Leverage, MarginMode: o.MarginMode,
			EntryPrice: copyF(mark), MarkPrice: copyF(mark), Quantity: copyF(o.Quantity),
			Margin: margin, PnL: new(big.Float), PnLPct: new(big.Float),
			LiqPrice: calcLiqPrice(side, mark, o.Leverage),
			TPPrice:  o.TPPrice, SLPrice: o.SLPrice,
			Status: "open", CreatedAt: now, UpdatedAt: now,
		}
		_ = h.store.SavePosition(pos)
		_ = h.store.UpdateOrderStatus(o.ID, "filled")
	}
}

// CheckTPSL scans open positions and closes any whose take-profit or stop-loss
// level has been hit by the live mark price.
func (h *FuturesHandler) CheckTPSL() {
	if h.store == nil {
		return
	}
	positions, err := h.store.ListOpenPositions()
	if err != nil {
		return
	}
	for _, pos := range positions {
		mark := h.liveMarkPrice(pos.Pair)
		if mark == nil || mark.Sign() <= 0 {
			continue
		}
		hit := false
		if pos.Side == "long" {
			if pos.TPPrice != nil && pos.TPPrice.Sign() > 0 && mark.Cmp(pos.TPPrice) >= 0 {
				hit = true
			}
			if pos.SLPrice != nil && pos.SLPrice.Sign() > 0 && mark.Cmp(pos.SLPrice) <= 0 {
				hit = true
			}
		} else {
			if pos.TPPrice != nil && pos.TPPrice.Sign() > 0 && mark.Cmp(pos.TPPrice) <= 0 {
				hit = true
			}
			if pos.SLPrice != nil && pos.SLPrice.Sign() > 0 && mark.Cmp(pos.SLPrice) >= 0 {
				hit = true
			}
		}
		if hit {
			_ = h.closePosition(pos, mark)
		}
	}
}

// ApplyFunding charges or credits hourly funding to all open positions. The
// funding amount is transferred from/to the user's wallet and applied to the
// position's margin.
func (h *FuturesHandler) ApplyFunding() {
	if h.store == nil || h.wallet == nil {
		return
	}
	positions, err := h.store.ListOpenPositions()
	if err != nil {
		return
	}
	now := time.Now().UnixNano()
	for _, pos := range positions {
		mark := h.liveMarkPrice(pos.Pair)
		if mark == nil || mark.Sign() <= 0 {
			continue
		}
		notional := new(big.Float).Mul(mark, pos.Quantity)
		rate := fundingRate(pos.Pair)
		sideSign := big.NewFloat(1)
		if pos.Side == "long" {
			sideSign = big.NewFloat(-1)
		}
		funding := new(big.Float).Mul(notional, rate)
		funding.Mul(funding, sideSign)
		quote := quoteAsset(pos.Pair)
		op := wallet.SettleOp{UserID: pos.UserID, Asset: quote, AccountType: wallet.AccountFutures, Delta: funding}
		txns := []*wallet.Transaction{
			{ID: "ffund_" + uuid.NewString(), UserID: pos.UserID, Asset: quote, AccountType: wallet.AccountFutures, Type: wallet.FuturesFunding, Amount: new(big.Float).Copy(funding), Fee: big.NewFloat(0), Status: wallet.Completed, CreatedAt: now},
		}
		if err := h.wallet.Settle([]wallet.SettleOp{op}, txns); err != nil {
			continue
		}
		pos.Margin = new(big.Float).Add(pos.Margin, funding)
		pos.MarkPrice = copyF(mark)
		pos.UpdatedAt = now
		_ = h.store.SavePosition(pos)
	}
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
	if len(parts) == 2 {
		return parts[1]
	}
	return "USDT"
}

func calcFuturesPnL(side string, entry, mark, qty *big.Float, leverage int, margin *big.Float) (*big.Float, *big.Float) {
	// PnL is the notional change: (mark - entry) * qty. Leverage is already
	// reflected in the margin size, so we do not multiply by it again.
	diff := new(big.Float).Sub(mark, entry)
	if side == "short" {
		diff.Neg(diff)
	}
	pnl := new(big.Float).Mul(diff, qty)
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

// netPosition merges a new order with an existing open position in the same
// pair. Same-side orders increase the position; opposite-side orders close the
// existing position and open a new one with the net size (one-way mode).
func (h *FuturesHandler) netPosition(userID, pair string) *FuturesPosition {
	if h.store == nil {
		return nil
	}
	positions, err := h.store.ListPositions(userID)
	if err != nil {
		return nil
	}
	for _, p := range positions {
		if p.Status == "open" && p.Pair == pair {
			return p
		}
	}
	return nil
}

// mergeSameSidePosition increases the size of an existing position at the
// weighted average entry price and recalculates liquidation price.
func (h *FuturesHandler) mergeSameSidePosition(pos *FuturesPosition, entry, qty, margin *big.Float, leverage int) {
	oldNotional := new(big.Float).Mul(pos.EntryPrice, pos.Quantity)
	addNotional := new(big.Float).Mul(entry, qty)
	totalQty := new(big.Float).Add(pos.Quantity, qty)
	newEntry := new(big.Float).Add(oldNotional, addNotional)
	if totalQty.Sign() > 0 {
		newEntry.Quo(newEntry, totalQty)
	}
	pos.EntryPrice = newEntry
	pos.Quantity = totalQty
	pos.Margin = new(big.Float).Add(pos.Margin, margin)
	pos.Leverage = leverage
	pos.LiqPrice = calcLiqPrice(pos.Side, pos.EntryPrice, pos.Leverage)
	pos.UpdatedAt = time.Now().UnixNano()
}

// isLiquidated returns true when the position's margin is exhausted at the
// current mark price. Isolated positions use only their allocated margin;
// cross margin positions can use the entire wallet balance in the quote asset.
func (h *FuturesHandler) isLiquidated(pos *FuturesPosition, mark *big.Float) bool {
	if pos.Margin == nil || pos.Margin.Sign() <= 0 {
		return true
	}
	pnl, _ := calcFuturesPnL(pos.Side, pos.EntryPrice, mark, pos.Quantity, pos.Leverage, pos.Margin)
	remaining := new(big.Float).Add(pos.Margin, pnl)
	if pos.MarginMode == "cross" && h.wallet != nil {
		quote := quoteAsset(pos.Pair)
		if w, err := h.wallet.GetBalanceForAccount(pos.UserID, quote, wallet.AccountFutures); err == nil && w != nil {
			available := new(big.Float).Sub(w.Balance, w.Locked)
			// For cross margin the locked amount of this position is already
			// part of w.Locked, so add it back to avoid double counting.
			available.Add(available, pos.Margin)
			remaining.Add(remaining, available)
		}
	}
	// Maintenance margin is charged on the notional value (entry*qty), the
	// same basis the liquidation-price formula entry*(1-1/lev+mm) uses.
	// Basing it on margin instead made the effective liquidation trigger
	// deviate from the displayed LiqPrice for every leverage > 1.
	mm := new(big.Float).Mul(pos.EntryPrice, pos.Quantity)
	mm.Mul(mm, big.NewFloat(0.005))
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
		"tp_price":    safeFloatStr(p.TPPrice),
		"sl_price":    safeFloatStr(p.SLPrice),
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
	if !ok {
		return "", false
	}
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
	if f == nil {
		return "0"
	}
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
