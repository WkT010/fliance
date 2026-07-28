package api

import (
	"errors"
	"math/big"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/WkT010/nexa-exchange/internal/auth"
	"github.com/WkT010/nexa-exchange/internal/pnl"
	"github.com/WkT010/nexa-exchange/internal/wallet"
)

// AccountHandler exposes user-account endpoints: profile, API key management
// and consolidated balances. It is the Binance-style /api/v3/account surface.
type AccountHandler struct {
	users    UserStore
	wallet   WalletService
	apiKeys  auth.APIKeyStore
	pnlSvc   *pnl.Service
}

func NewAccountHandler(users UserStore, walletSvc WalletService, apiKeys auth.APIKeyStore) *AccountHandler {
	return &AccountHandler{users: users, wallet: walletSvc, apiKeys: apiKeys}
}

// SetPnLService wires the profit/loss tracker.
func (h *AccountHandler) SetPnLService(s *pnl.Service) { h.pnlSvc = s }

// GetAccount returns the authenticated user's profile plus consolidated wallet
// balances. Mirrors Binance's GET /api/v3/account.
// GET /api/v2/account
func (h *AccountHandler) GetAccount(c *gin.Context) {
	uid, _ := c.Get("user_id")
	userID, _ := uid.(string)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}
	u, err := h.users.GetByID(userID)
	if err != nil || u == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	balances := []*wallet.Wallet{}
	if h.wallet != nil {
		if ws, err := h.wallet.GetBalances(userID); err == nil {
			balances = ws
		}
	}
	balJSON := make([]gin.H, 0, len(balances))
	for _, w := range balances {
		balJSON = append(balJSON, walletToJSON(w))
	}
	c.JSON(http.StatusOK, gin.H{
		"user_id":    u.ID,
		"email":      u.Email,
		"role":       u.Role,
		"created_at": u.CreatedAt,
		"balances":   balJSON,
	})
}

// GetProfile returns just the user profile (no balances).
// GET /api/v2/account/profile
func (h *AccountHandler) GetProfile(c *gin.Context) {
	uid, _ := c.Get("user_id")
	userID, _ := uid.(string)
	u, err := h.users.GetByID(userID)
	if err != nil || u == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"user_id":    u.ID,
		"email":      u.Email,
		"role":       u.Role,
		"created_at": u.CreatedAt,
		"updated_at": u.UpdatedAt,
	})
}

// --- API Keys --------------------------------------------------------------

// CreateAPIKey issues a new API key for the authenticated user. The secret is
// only returned once - callers must store it.
// POST /api/v2/account/api-keys  { "permissions":["read","trade"] }
func (h *AccountHandler) CreateAPIKey(c *gin.Context) {
	uid, _ := c.Get("user_id")
	userID, _ := uid.(string)
	if h.apiKeys == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "api key store unavailable"})
		return
	}
	var r struct {
		Permissions []string `json:"permissions"`
	}
	_ = c.ShouldBindJSON(&r)
	if len(r.Permissions) == 0 {
		r.Permissions = []string{"read"}
	}
	keyID, secret, err := auth.GenerateAPIKey()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "key generation failed"})
		return
	}
	k := &auth.APIKey{
		KeyID:       keyID,
		Secret:      secret,
		UserID:      userID,
		Permissions: r.Permissions,
		Active:      true,
	}
	if err := h.apiKeys.Save(k); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "save failed"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"key_id":      keyID,
		"secret":      secret,
		"permissions": r.Permissions,
		"note":        "store the secret securely; it will not be shown again",
	})
}

// ListAPIKeys returns the user's API keys (without secrets).
// GET /api/v2/account/api-keys
func (h *AccountHandler) ListAPIKeys(c *gin.Context) {
	uid, _ := c.Get("user_id")
	userID, _ := uid.(string)
	if h.apiKeys == nil {
		c.JSON(http.StatusOK, gin.H{"api_keys": []interface{}{}})
		return
	}
	keys, err := h.apiKeys.ListByUser(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list failed"})
		return
	}
	if keys == nil {
		keys = []*auth.APIKey{}
	}
	out := make([]gin.H, len(keys))
	for i, k := range keys {
		out[i] = gin.H{
			"key_id":      k.KeyID,
			"permissions": k.Permissions,
			"active":      k.Active,
			"created_at":  k.CreatedAt,
			"expires_at":  k.ExpiresAt,
		}
	}
	c.JSON(http.StatusOK, gin.H{"api_keys": out})
}

// RevokeAPIKey deactivates an API key.
// DELETE /api/v2/account/api-keys/:id
func (h *AccountHandler) RevokeAPIKey(c *gin.Context) {
	uid, _ := c.Get("user_id")
	userID, _ := uid.(string)
	keyID := c.Param("id")
	if keyID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "key id required"})
		return
	}
	// Ownership check: list the user's keys first.
	keys, err := h.apiKeys.ListByUser(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list failed"})
		return
	}
	owned := false
	for _, k := range keys {
		if k.KeyID == keyID {
			owned = true
			break
		}
	}
	if !owned {
		c.JSON(http.StatusForbidden, gin.H{"error": "key not owned by user"})
		return
	}
	if err := h.apiKeys.Revoke(keyID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "revoke failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "revoked", "key_id": keyID})
}

// --- Admin endpoints -------------------------------------------------------

// AdminListUsers (admin-only) lists users with pagination.
// GET /api/v2/admin/users?limit=50&offset=0
func (h *AccountHandler) AdminListUsers(c *gin.Context) {
	role, _ := c.Get("role")
	if role != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin only"})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	// We don't have a ListUsers on UserStore; surface that as a TODO by returning
	// a stub here. A real implementation would call users.List(limit, offset).
	c.JSON(http.StatusOK, gin.H{
		"users":  []interface{}{},
		"limit":  limit,
		"offset": offset,
		"note":   "user listing not implemented in store",
	})
	_ = errors.New("placeholder")
}

// GetPnL returns today's realized PnL, total realized PnL, unrealized PnL and
// open positions for the authenticated user.
// GET /api/v2/account/pnl
func (h *AccountHandler) GetPnL(c *gin.Context) {
	uid, _ := c.Get("user_id")
	userID, _ := uid.(string)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}
	if h.pnlSvc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "pnl service unavailable"})
		return
	}

	// Collect reference prices from the best available source (internal tickers).
	refs := make(map[string]*big.Float)
	if h.wallet != nil {
		if ws, err := h.wallet.GetBalances(userID); err == nil {
			for _, w := range ws {
				if w.Asset == "USDT" {
					continue
				}
				pair := w.Asset + "/USDT"
				// The wallet service does not expose market prices; leave refs
				// empty so the UI can fetch mark prices separately. Unrealized
				// PnL will be computed when refs are supplied.
				_ = pair
			}
		}
	}

	summary := h.pnlSvc.Summary(userID, refs)
	c.JSON(http.StatusOK, gin.H{
		"user_id":          summary.UserID,
		"date":             summary.Date,
		"today_realized":   summary.TodayRealized.String(),
		"total_realized":   summary.TotalRealized.String(),
		"unrealized":       summary.Unrealized.String(),
		"positions":        positionsToJSON(summary.Positions),
		"reference_prices": floatMapToJSON(summary.ReferencePrices),
	})
}

func positionsToJSON(positions map[string]*pnl.Position) []gin.H {
	out := make([]gin.H, 0, len(positions))
	for _, p := range positions {
		out = append(out, gin.H{
			"asset":          p.Asset,
			"qty":            p.Qty.String(),
			"avg_cost":       p.AvgCost.String(),
			"realized_pnl":   p.RealizedPnL.String(),
			"last_fill_time": p.LastFillTime,
		})
	}
	return out
}

func floatMapToJSON(m map[string]*big.Float) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v.String()
	}
	return out
}
