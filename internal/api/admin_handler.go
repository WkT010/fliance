package api

import (
	"math/big"
	"net/http"
	"strconv"

	"github.com/WkT010/nexa-exchange/internal/audit"
	"github.com/WkT010/nexa-exchange/internal/market"
	"github.com/WkT010/nexa-exchange/internal/matching"
	"github.com/WkT010/nexa-exchange/internal/risk"
	"github.com/WkT010/nexa-exchange/internal/wallet"
	"github.com/gin-gonic/gin"
)

// WithdrawalManager is the subset of wallet.WithdrawalService used by admin endpoints.
type WithdrawalManager interface {
	ListPendingWithdrawals(limit int) ([]*wallet.Transaction, error)
	ListByStatus(status wallet.TxStatus, limit, offset int) ([]*wallet.Transaction, error)
	ListUserWithdrawals(userID string, limit, offset int) ([]*wallet.Transaction, error)
	GetWithdrawal(txID string) (*wallet.Transaction, error)
	ApproveWithdrawal(txID string) error
	RejectWithdrawal(txID string) error
	AddAddress(entry wallet.AddressBookEntry)
	ListAddresses(userID, asset string) []wallet.AddressBookEntry
	SetLimit(limit *wallet.WithdrawalLimit)
}

// RiskManager is the subset of risk.Engine used by admin endpoints.
type RiskManager interface {
	GetPairConfig(pair string) *risk.PairConfig
	SetPairConfig(cfg *risk.PairConfig)
	AllPairs() []*risk.PairConfig
	GetUserLimit(userID string) *risk.UserLimit
	SetUserLimit(ul *risk.UserLimit)
}

// ExchangeManager is the subset of matching.ExchangeEngine used by admin endpoints.
type ExchangeManager interface {
	Snapshot(pair string) error
	SnapshotAll() error
	Engines() map[string]*matching.MatchingEngine
}

// AdminHandler exposes production operations endpoints: withdrawal review,
// address whitelisting, risk-rule management and on-demand snapshots.
type AdminHandler struct {
	withdrawals WithdrawalManager
	risk        RiskManager
	exchange    ExchangeManager

	// AMM admin hooks. Wired from cmd/api-gateway when the AMM price engine is
	// enabled; nil otherwise (endpoints return 503 if not configured).
	ammSim       *market.Simulator
	ammFeed      *market.AMMPriceFeed
	ammBootstrap func() error // create+seed default pools then reload feed

	// Audit trail for admin actions. Optional: nil disables auditing (the
	// logger's methods are nil-safe).
	audit *audit.Logger
}

// NewAdminHandler constructs an admin handler.
func NewAdminHandler(w WithdrawalManager, r RiskManager, ex ExchangeManager) *AdminHandler {
	return &AdminHandler{withdrawals: w, risk: r, exchange: ex}
}

// SetAuditLogger wires the asynchronous admin audit logger. Optional: without
// it admin endpoints simply do not record audit entries.
func (h *AdminHandler) SetAuditLogger(l *audit.Logger) { h.audit = l }

// ListWithdrawals returns withdrawals pending review or broadcast.
// GET /api/v2/admin/withdrawals?status=&limit=100&offset=0
func (h *AdminHandler) ListWithdrawals(c *gin.Context) {
	status := c.Query("status")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if offset < 0 {
		offset = 0
	}

	var txs []*wallet.Transaction
	var err error
	switch status {
	case "pending":
		txs, err = h.withdrawals.ListPendingWithdrawals(limit)
	case "reviewing":
		// ListPendingWithdrawals already includes reviewing; filter for explicit requests.
		all, err := h.withdrawals.ListPendingWithdrawals(limit)
		if err == nil {
			for _, t := range all {
				if t.Status == wallet.WithdrawalReviewing {
					txs = append(txs, t)
				}
			}
		}
	case "approved":
		txs, err = listByStatus(h.withdrawals, wallet.WithdrawalApproved, limit, offset)
	case "broadcast":
		txs, err = listByStatus(h.withdrawals, wallet.WithdrawalBroadcast, limit, offset)
	case "completed":
		txs, err = listByStatus(h.withdrawals, wallet.WithdrawalCompleted, limit, offset)
	case "rejected":
		txs, err = listByStatus(h.withdrawals, wallet.WithdrawalRejected, limit, offset)
	default:
		txs, err = h.withdrawals.ListPendingWithdrawals(limit)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list withdrawals", "detail": err.Error()})
		return
	}
	if txs == nil {
		txs = []*wallet.Transaction{}
	}
	out := make([]gin.H, len(txs))
	for i, t := range txs {
		out[i] = txToJSON(t)
	}
	c.JSON(http.StatusOK, gin.H{"withdrawals": out, "limit": limit, "offset": offset})
}

func listByStatus(m WithdrawalManager, status wallet.TxStatus, limit, offset int) ([]*wallet.Transaction, error) {
	return m.ListByStatus(status, limit, offset)
}

// ApproveWithdrawal moves a reviewing withdrawal to approved.
// POST /api/v2/admin/withdrawals/:id/approve
func (h *AdminHandler) ApproveWithdrawal(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "withdrawal id required"})
		return
	}
	err := h.withdrawals.ApproveWithdrawal(id)
	h.audit.Log(c, "admin.withdrawal.approve", "withdrawal", id, nil, err)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "approved", "id": id})
}

// RejectWithdrawal cancels a pending/reviewing withdrawal and releases funds.
// POST /api/v2/admin/withdrawals/:id/reject
func (h *AdminHandler) RejectWithdrawal(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "withdrawal id required"})
		return
	}
	err := h.withdrawals.RejectWithdrawal(id)
	h.audit.Log(c, "admin.withdrawal.reject", "withdrawal", id, nil, err)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "rejected", "id": id})
}

// ListUserWithdrawals returns a user's withdrawal history.
// GET /api/v2/admin/users/:id/withdrawals?limit=50&offset=0
func (h *AdminHandler) ListUserWithdrawals(c *gin.Context) {
	userID := c.Param("id")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if offset < 0 {
		offset = 0
	}
	txs, err := h.withdrawals.ListUserWithdrawals(userID, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list withdrawals"})
		return
	}
	if txs == nil {
		txs = []*wallet.Transaction{}
	}
	out := make([]gin.H, len(txs))
	for i, t := range txs {
		out[i] = txToJSON(t)
	}
	c.JSON(http.StatusOK, gin.H{"withdrawals": out, "limit": limit, "offset": offset})
}

// ListAddresses returns whitelisted addresses for a user/asset.
// GET /api/v2/admin/users/:id/addresses?asset=BTC
func (h *AdminHandler) ListAddresses(c *gin.Context) {
	userID := c.Param("id")
	asset := c.Query("asset")
	entries := h.withdrawals.ListAddresses(userID, asset)
	out := make([]gin.H, len(entries))
	for i, e := range entries {
		out[i] = gin.H{"id": e.ID, "asset": e.Asset, "address": e.Address, "label": e.Label, "created_at": e.CreatedAt}
	}
	c.JSON(http.StatusOK, gin.H{"addresses": out})
}

// AddAddress whitelists a withdrawal address for a user.
// POST /api/v2/admin/users/:id/addresses { "asset":"BTC","address":"...","label":"cold" }
func (h *AdminHandler) AddAddress(c *gin.Context) {
	userID := c.Param("id")
	var r struct {
		Asset   string `json:"asset" binding:"required"`
		Address string `json:"address" binding:"required"`
		Label   string `json:"label"`
	}
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "asset and address required"})
		return
	}
	h.withdrawals.AddAddress(wallet.AddressBookEntry{
		UserID:  userID,
		Asset:   r.Asset,
		Address: r.Address,
		Label:   r.Label,
	})
	h.audit.Log(c, "admin.user.address.add", "user_address", userID, gin.H{
		"asset": r.Asset, "address": r.Address, "label": r.Label,
	}, nil)
	c.JSON(http.StatusCreated, gin.H{"status": "added", "user_id": userID, "asset": r.Asset, "address": r.Address})
}

// SetDailyLimit sets a per-user per-asset daily withdrawal limit.
// POST /api/v2/admin/users/:id/limits { "asset":"BTC","daily_limit":"10" }
func (h *AdminHandler) SetDailyLimit(c *gin.Context) {
	userID := c.Param("id")
	var r struct {
		Asset      string `json:"asset" binding:"required"`
		DailyLimit string `json:"daily_limit" binding:"required"`
		WindowHrs  int    `json:"window_hours"`
	}
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "asset and daily_limit required"})
		return
	}
	limitF := new(big.Float)
	if _, _, err := limitF.Parse(r.DailyLimit, 10); err != nil || limitF.Sign() < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid daily_limit"})
		return
	}
	if r.WindowHrs <= 0 {
		r.WindowHrs = 24
	}
	h.withdrawals.SetLimit(&wallet.WithdrawalLimit{
		UserID:      userID,
		Asset:       r.Asset,
		DailyLimit:  limitF,
		WindowHours: r.WindowHrs,
	})
	h.audit.Log(c, "admin.user.limit.set", "withdrawal_limit", userID, gin.H{
		"asset": r.Asset, "daily_limit": r.DailyLimit, "window_hours": r.WindowHrs,
	}, nil)
	c.JSON(http.StatusOK, gin.H{"status": "ok", "user_id": userID, "asset": r.Asset, "daily_limit": r.DailyLimit})
}

// GetPairRisk returns the current risk configuration for a pair.
// GET /api/v2/admin/risk/pairs/:pair
func (h *AdminHandler) GetPairRisk(c *gin.Context) {
	pair := c.Param("pair")
	cfg := h.risk.GetPairConfig(pair)
	if cfg == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "pair risk config not found"})
		return
	}
	c.JSON(http.StatusOK, pairRiskToJSON(cfg))
}

// ListPairRisk returns all pair risk configurations.
// GET /api/v2/admin/risk/pairs
func (h *AdminHandler) ListPairRisk(c *gin.Context) {
	cfgs := h.risk.AllPairs()
	out := make([]gin.H, len(cfgs))
	for i, cfg := range cfgs {
		out[i] = pairRiskToJSON(cfg)
	}
	c.JSON(http.StatusOK, gin.H{"pairs": out})
}

// UpdatePairRisk updates the risk configuration for a pair.
// PUT /api/v2/admin/risk/pairs/:pair
func (h *AdminHandler) UpdatePairRisk(c *gin.Context) {
	pair := c.Param("pair")
	cfg := h.risk.GetPairConfig(pair)
	if cfg == nil {
		cfg = &risk.PairConfig{Pair: pair}
	}
	var r struct {
		MinNotional         string `json:"min_notional"`
		MaxNotional         string `json:"max_notional"`
		MinQty              string `json:"min_qty"`
		MaxQty              string `json:"max_qty"`
		TickSize            string `json:"tick_size"`
		LotSize             string `json:"lot_size"`
		PriceBandPct        string `json:"price_band_pct"`
		CircuitBreakerPct   string `json:"circuit_breaker_pct"`
		ReferencePrice      string `json:"reference_price"`
		MarketOrdersEnabled *bool  `json:"market_orders_enabled"`
		TradingEnabled      *bool  `json:"trading_enabled"`
	}
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	applyBigOpt(&cfg.MinNotional, r.MinNotional)
	applyBigOpt(&cfg.MaxNotional, r.MaxNotional)
	applyBigOpt(&cfg.MinQty, r.MinQty)
	applyBigOpt(&cfg.MaxQty, r.MaxQty)
	applyBigOpt(&cfg.TickSize, r.TickSize)
	applyBigOpt(&cfg.LotSize, r.LotSize)
	applyBigOpt(&cfg.PriceBandPct, r.PriceBandPct)
	applyBigOpt(&cfg.CircuitBreakerPct, r.CircuitBreakerPct)
	applyBigOpt(&cfg.ReferencePrice, r.ReferencePrice)
	if r.MarketOrdersEnabled != nil {
		cfg.MarketOrdersEnabled = *r.MarketOrdersEnabled
	}
	if r.TradingEnabled != nil {
		cfg.TradingEnabled = *r.TradingEnabled
	}
	h.risk.SetPairConfig(cfg)
	h.audit.Log(c, "admin.risk.pair.update", "pair", pair, gin.H{
		"min_notional": r.MinNotional, "max_notional": r.MaxNotional,
		"min_qty": r.MinQty, "max_qty": r.MaxQty,
		"tick_size": r.TickSize, "lot_size": r.LotSize,
		"price_band_pct": r.PriceBandPct, "circuit_breaker_pct": r.CircuitBreakerPct,
		"reference_price":       r.ReferencePrice,
		"market_orders_enabled": r.MarketOrdersEnabled, "trading_enabled": r.TradingEnabled,
	}, nil)
	c.JSON(http.StatusOK, pairRiskToJSON(cfg))
}

// PausePair suspends trading for a pair.
// POST /api/v2/admin/pairs/:pair/pause
func (h *AdminHandler) PausePair(c *gin.Context) {
	pair := c.Param("pair")
	h.setTradingEnabled(pair, false)
	h.audit.Log(c, "admin.pair.pause", "pair", pair, nil, nil)
	c.JSON(http.StatusOK, gin.H{"pair": pair, "trading_enabled": false})
}

// ResumePair re-enables trading for a pair.
// POST /api/v2/admin/pairs/:pair/resume
func (h *AdminHandler) ResumePair(c *gin.Context) {
	pair := c.Param("pair")
	h.setTradingEnabled(pair, true)
	h.audit.Log(c, "admin.pair.resume", "pair", pair, nil, nil)
	c.JSON(http.StatusOK, gin.H{"pair": pair, "trading_enabled": true})
}

func (h *AdminHandler) setTradingEnabled(pair string, enabled bool) {
	cfg := h.risk.GetPairConfig(pair)
	if cfg == nil {
		cfg = &risk.PairConfig{Pair: pair}
	}
	cfg.TradingEnabled = enabled
	h.risk.SetPairConfig(cfg)
}

// GetUserRisk returns the risk limits for a user.
// GET /api/v2/admin/risk/users/:id
func (h *AdminHandler) GetUserRisk(c *gin.Context) {
	userID := c.Param("id")
	ul := h.risk.GetUserLimit(userID)
	if ul == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user risk limits not found"})
		return
	}
	c.JSON(http.StatusOK, userLimitToJSON(ul))
}

// UpdateUserRisk sets the risk limits for a user.
// PUT /api/v2/admin/risk/users/:id
func (h *AdminHandler) UpdateUserRisk(c *gin.Context) {
	userID := c.Param("id")
	ul := h.risk.GetUserLimit(userID)
	if ul == nil {
		ul = &risk.UserLimit{UserID: userID}
	}
	var r struct {
		MaxOpenOrders      int               `json:"max_open_orders"`
		OrdersPerMinute    int               `json:"orders_per_minute"`
		OrdersPerHour      int               `json:"orders_per_hour"`
		OrdersPerDay       int               `json:"orders_per_day"`
		DailyOrderNotional map[string]string `json:"daily_order_notional"`
		MaxPosition        map[string]string `json:"max_position"`
	}
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	ul.MaxOpenOrders = r.MaxOpenOrders
	ul.OrdersPerMinute = r.OrdersPerMinute
	ul.OrdersPerHour = r.OrdersPerHour
	ul.OrdersPerDay = r.OrdersPerDay
	if r.DailyOrderNotional != nil {
		ul.DailyOrderNotional = make(map[string]*big.Float)
		for pair, v := range r.DailyOrderNotional {
			f := new(big.Float)
			if _, _, err := f.Parse(v, 10); err == nil {
				ul.DailyOrderNotional[pair] = f
			}
		}
	}
	if r.MaxPosition != nil {
		ul.MaxPosition = make(map[string]*big.Float)
		for pair, v := range r.MaxPosition {
			f := new(big.Float)
			if _, _, err := f.Parse(v, 10); err == nil {
				ul.MaxPosition[pair] = f
			}
		}
	}
	h.risk.SetUserLimit(ul)
	h.audit.Log(c, "admin.risk.user.update", "user", userID, gin.H{
		"max_open_orders": r.MaxOpenOrders, "orders_per_minute": r.OrdersPerMinute,
		"orders_per_hour": r.OrdersPerHour, "orders_per_day": r.OrdersPerDay,
		"daily_order_notional": r.DailyOrderNotional, "max_position": r.MaxPosition,
	}, nil)
	c.JSON(http.StatusOK, userLimitToJSON(ul))
}

// TriggerSnapshot forces a snapshot of a single pair or all pairs.
// POST /api/v2/admin/snapshots { "pair":"BTC/USDT" }  (empty = all)
func (h *AdminHandler) TriggerSnapshot(c *gin.Context) {
	var r struct {
		Pair string `json:"pair"`
	}
	_ = c.ShouldBindJSON(&r)
	if h.exchange == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "exchange not available"})
		return
	}
	var err error
	if r.Pair == "" {
		err = h.exchange.SnapshotAll()
	} else {
		err = h.exchange.Snapshot(r.Pair)
	}
	target := r.Pair
	if target == "" {
		target = "all"
	}
	h.audit.Log(c, "admin.snapshot.trigger", "snapshot", target, nil, err)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "snapshot failed", "detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "pair": r.Pair})
}

func pairRiskToJSON(cfg *risk.PairConfig) gin.H {
	return gin.H{
		"pair":                  cfg.Pair,
		"min_notional":          safeFloatStr(cfg.MinNotional),
		"max_notional":          safeFloatStr(cfg.MaxNotional),
		"min_qty":               safeFloatStr(cfg.MinQty),
		"max_qty":               safeFloatStr(cfg.MaxQty),
		"tick_size":             safeFloatStr(cfg.TickSize),
		"lot_size":              safeFloatStr(cfg.LotSize),
		"price_band_pct":        safeFloatStr(cfg.PriceBandPct),
		"circuit_breaker_pct":   safeFloatStr(cfg.CircuitBreakerPct),
		"reference_price":       safeFloatStr(cfg.ReferencePrice),
		"market_orders_enabled": cfg.MarketOrdersEnabled,
		"trading_enabled":       cfg.TradingEnabled,
	}
}

func userLimitToJSON(ul *risk.UserLimit) gin.H {
	dn := make(map[string]string)
	for pair, v := range ul.DailyOrderNotional {
		dn[pair] = safeFloatStr(v)
	}
	mp := make(map[string]string)
	for pair, v := range ul.MaxPosition {
		mp[pair] = safeFloatStr(v)
	}
	return gin.H{
		"user_id":              ul.UserID,
		"max_open_orders":      ul.MaxOpenOrders,
		"orders_per_minute":    ul.OrdersPerMinute,
		"orders_per_hour":      ul.OrdersPerHour,
		"orders_per_day":       ul.OrdersPerDay,
		"daily_order_notional": dn,
		"max_position":         mp,
	}
}

func applyBigOpt(dst **big.Float, s string) {
	if s == "" {
		return
	}
	f := new(big.Float)
	if _, _, err := f.Parse(s, 10); err == nil {
		*dst = f
	}
}
