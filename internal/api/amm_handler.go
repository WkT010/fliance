package api

import (
	"math/big"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/WkT010/nexa-exchange/internal/amm"
)

// AmmService is the subset of amm.Service used by the HTTP layer.
type AmmService interface {
	CreatePool(pair, token0, token1 string, feeRate *big.Float) (*amm.Pool, error)
	AddLiquidity(userID, poolID string, amount0, amount1 *big.Float) (*amm.Pool, *amm.LPPosition, *big.Float, *big.Float, error)
	RemoveLiquidity(userID, poolID string, shares *big.Float) (*amm.Pool, *big.Float, *big.Float, error)
	ExecuteSwap(userID, poolID, tokenIn string, amountIn *big.Float) (*amm.Swap, error)
	Quote(poolID, tokenIn string, amountIn *big.Float) (*big.Float, *big.Float, *big.Float, error)
	GetPool(id string) (*amm.Pool, error)
	ListPools() ([]*amm.Pool, error)
	GetPositionByPool(userID, poolID string) (*amm.LPPosition, error)
	ListPositionsByUser(userID string) ([]*amm.LPPosition, error)
	ListSwaps(poolID string, limit, offset int) ([]*amm.Swap, error)
}

// AmmHandler exposes internal AMM endpoints: pools, liquidity, and swaps.
type AmmHandler struct {
	svc AmmService
}

func NewAmmHandler(svc AmmService) *AmmHandler { return &AmmHandler{svc: svc} }

// ListPools returns all internal AMM pools.
// GET /api/v2/amm/pools
func (h *AmmHandler) ListPools(c *gin.Context) {
	pools, err := h.svc.ListPools()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load pools"})
		return
	}
	if pools == nil { pools = []*amm.Pool{} }
	out := make([]gin.H, len(pools))
	for i, p := range pools {
		out[i] = poolToJSON(p)
	}
	c.JSON(http.StatusOK, gin.H{"pools": out})
}

// GetPool returns a single pool by ID.
// GET /api/v2/amm/pools/:id
func (h *AmmHandler) GetPool(c *gin.Context) {
	id := c.Param("id")
	p, err := h.svc.GetPool(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "pool not found"})
		return
	}
	c.JSON(http.StatusOK, poolToJSON(p))
}

// CreatePool creates a new internal AMM pool (admin in production).
// POST /api/v2/amm/pools
func (h *AmmHandler) CreatePool(c *gin.Context) {
	var r struct {
		Pair    string  `json:"pair" binding:"required"`
		Token0  string  `json:"token0" binding:"required"`
		Token1  string  `json:"token1" binding:"required"`
		FeeRate float64 `json:"fee_rate"`
	}
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pair, token0 and token1 required"})
		return
	}
	fee := big.NewFloat(r.FeeRate)
	if fee.Sign() <= 0 {
		fee = big.NewFloat(0.003)
	}
	p, err := h.svc.CreatePool(r.Pair, r.Token0, r.Token1, fee)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, poolToJSON(p))
}

// AddLiquidity adds liquidity to a pool.
// POST /api/v2/amm/pools/:id/add-liquidity
func (h *AmmHandler) AddLiquidity(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}
	poolID := c.Param("id")
	var r struct {
		Amount0 string `json:"amount0" binding:"required"`
		Amount1 string `json:"amount1" binding:"required"`
	}
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "amount0 and amount1 required"})
		return
	}
	a0, ok0 := new(big.Float).SetString(r.Amount0)
	a1, ok1 := new(big.Float).SetString(r.Amount1)
	if !ok0 || !ok1 || a0.Sign() <= 0 || a1.Sign() <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid amounts"})
		return
	}
	pool, pos, used1, shares, err := h.svc.AddLiquidity(userID, poolID, a0, a1)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"pool":          poolToJSON(pool),
		"position":      lpPositionToJSON(pos),
		"amount0":       safeFloatStr(a0),
		"amount1":       safeFloatStr(used1),
		"shares_minted": safeFloatStr(shares),
	})
}

// RemoveLiquidity removes liquidity from a pool.
// POST /api/v2/amm/pools/:id/remove-liquidity
func (h *AmmHandler) RemoveLiquidity(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}
	poolID := c.Param("id")
	var r struct {
		Shares string `json:"shares" binding:"required"`
	}
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "shares required"})
		return
	}
	shares, ok := new(big.Float).SetString(r.Shares)
	if !ok || shares.Sign() <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid shares"})
		return
	}
	pool, amount0, amount1, err := h.svc.RemoveLiquidity(userID, poolID, shares)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"pool":     poolToJSON(pool),
		"amount0":  safeFloatStr(amount0),
		"amount1":  safeFloatStr(amount1),
		"shares":   safeFloatStr(shares),
	})
}

// QuoteSwap returns an AMM swap quote without executing.
// POST /api/v2/amm/swap/quote
func (h *AmmHandler) QuoteSwap(c *gin.Context) {
	var r struct {
		PoolID   string `json:"pool_id" binding:"required"`
		TokenIn  string `json:"token_in" binding:"required"`
		AmountIn string `json:"amount_in" binding:"required"`
	}
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pool_id, token_in and amount_in required"})
		return
	}
	amt, ok := new(big.Float).SetString(r.AmountIn)
	if !ok || amt.Sign() <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid amount_in"})
		return
	}
	amountOut, fee, _, err := h.svc.Quote(r.PoolID, r.TokenIn, amt)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"pool_id":    r.PoolID,
		"token_in":   r.TokenIn,
		"amount_in":  safeFloatStr(amt),
		"amount_out": safeFloatStr(amountOut),
		"fee":        safeFloatStr(fee),
	})
}

// ExecuteSwap executes an internal AMM swap.
// POST /api/v2/amm/swap
func (h *AmmHandler) ExecuteSwap(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}
	var r struct {
		PoolID   string `json:"pool_id" binding:"required"`
		TokenIn  string `json:"token_in" binding:"required"`
		AmountIn string `json:"amount_in" binding:"required"`
	}
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pool_id, token_in and amount_in required"})
		return
	}
	amt, ok2 := new(big.Float).SetString(r.AmountIn)
	if !ok2 || amt.Sign() <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid amount_in"})
		return
	}
	sw, err := h.svc.ExecuteSwap(userID, r.PoolID, r.TokenIn, amt)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, swapToJSON(sw))
}

// GetPosition returns the user's position in a pool.
// GET /api/v2/amm/pools/:id/position
func (h *AmmHandler) GetPosition(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}
	poolID := c.Param("id")
	pos, err := h.svc.GetPositionByPool(userID, poolID)
	if err != nil || pos == nil {
		c.JSON(http.StatusOK, gin.H{"position": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"position": lpPositionToJSON(pos)})
}

// ListPositions returns the user's LP positions across all pools.
// GET /api/v2/amm/positions
func (h *AmmHandler) ListPositions(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}
	positions, err := h.svc.ListPositionsByUser(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load positions"})
		return
	}
	if positions == nil { positions = []*amm.LPPosition{} }
	out := make([]gin.H, len(positions))
	for i, pos := range positions {
		out[i] = lpPositionToJSON(pos)
	}
	c.JSON(http.StatusOK, gin.H{"positions": out})
}

// ListSwaps returns recent swaps for a pool.
// GET /api/v2/amm/pools/:id/swaps
func (h *AmmHandler) ListSwaps(c *gin.Context) {
	poolID := c.Param("id")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if limit <= 0 || limit > 500 { limit = 50 }
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if offset < 0 { offset = 0 }
	swaps, err := h.svc.ListSwaps(poolID, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load swaps"})
		return
	}
	if swaps == nil { swaps = []*amm.Swap{} }
	out := make([]gin.H, len(swaps))
	for i, sw := range swaps {
		out[i] = swapToJSON(sw)
	}
	c.JSON(http.StatusOK, gin.H{"swaps": out, "limit": limit, "offset": offset})
}

func poolToJSON(p *amm.Pool) gin.H {
	return gin.H{
		"id":         p.ID,
		"pair":       p.Pair,
		"token0":     p.Token0,
		"token1":     p.Token1,
		"reserve0":   safeFloatStr(p.Reserve0),
		"reserve1":   safeFloatStr(p.Reserve1),
		"lp_shares":  safeFloatStr(p.LPShares),
		"fee_rate":   safeFloatStr(p.FeeRate),
		"status":     p.Status,
		"created_at": p.CreatedAt,
		"updated_at": p.UpdatedAt,
	}
}

func lpPositionToJSON(pos *amm.LPPosition) gin.H {
	return gin.H{
		"id":         pos.ID,
		"user_id":    pos.UserID,
		"pool_id":    pos.PoolID,
		"shares":     safeFloatStr(pos.Shares),
		"created_at": pos.CreatedAt,
		"updated_at": pos.UpdatedAt,
	}
}

func swapToJSON(sw *amm.Swap) gin.H {
	return gin.H{
		"id":         sw.ID,
		"pool_id":    sw.PoolID,
		"user_id":    sw.UserID,
		"token_in":   sw.TokenIn,
		"token_out":  sw.TokenOut,
		"amount_in":  safeFloatStr(sw.AmountIn),
		"amount_out": safeFloatStr(sw.AmountOut),
		"fee":        safeFloatStr(sw.Fee),
		"created_at": sw.CreatedAt,
	}
}
