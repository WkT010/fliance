package amm

import (
	"math/big"
	"strconv"
	"time"
)

// Pool represents a constant-product AMM liquidity pool.
type Pool struct {
	ID        string
	Pair      string // e.g. "ETH/USDT"
	Token0    string
	Token1    string
	Reserve0  *big.Float
	Reserve1  *big.Float
	LPShares  *big.Float // total LP shares issued
	FeeRate   *big.Float // swap fee, e.g. 0.003 for 30 bps
	Status    string     // "active" or "paused"
	CreatedAt int64
	UpdatedAt int64
}

// LPPosition tracks a user's liquidity in a pool.
type LPPosition struct {
	ID        string
	UserID    string
	PoolID    string
	Shares    *big.Float
	CreatedAt int64
	UpdatedAt int64
}

// Swap records a single AMM swap executed against a pool.
type Swap struct {
	ID        string
	PoolID    string
	UserID    string
	TokenIn   string
	TokenOut  string
	AmountIn  *big.Float
	AmountOut *big.Float
	Fee       *big.Float
	CreatedAt int64
}

// NewPool creates an empty pool with normalized big.Float fields.
func NewPool(id, pair, token0, token1 string, feeRate *big.Float) *Pool {
	if feeRate == nil || feeRate.Sign() < 0 {
		feeRate = big.NewFloat(0.003)
	}
	now := time.Now().UnixNano()
	return &Pool{
		ID:        id,
		Pair:      pair,
		Token0:    token0,
		Token1:    token1,
		Reserve0:  big.NewFloat(0),
		Reserve1:  big.NewFloat(0),
		LPShares:  big.NewFloat(0),
		FeeRate:   feeRate,
		Status:    "active",
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// PoolShare returns a new LP position for the supplied share amount.
func (p *Pool) PoolShare(userID string, shares *big.Float) *LPPosition {
	now := time.Now().UnixNano()
	return &LPPosition{
		ID:        "lp_" + nowText() + randSuffix(),
		UserID:    userID,
		PoolID:    p.ID,
		Shares:    new(big.Float).Copy(shares),
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func nowText() string { return strconv.FormatInt(time.Now().UnixNano(), 36) }
