package amm

import (
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/WkT010/nexa-exchange/internal/wallet"
)

// WalletService is the subset of wallet.Service required by the AMM engine.
type WalletService interface {
	GetBalance(userID, asset string) (*wallet.Wallet, error)
	Settle(ops []wallet.SettleOp, txns []*wallet.Transaction) error
}

// Service orchestrates AMM pool operations and wallet settlements.
type Service struct {
	store  Store
	wallet WalletService
	mu     sync.RWMutex
}

func NewService(store Store, wallet WalletService) *Service {
	return &Service{store: store, wallet: wallet}
}

// CreatePool initializes a new AMM pool. Admin only in production.
func (s *Service) CreatePool(pair, token0, token1 string, feeRate *big.Float) (*Pool, error) {
	if feeRate == nil || feeRate.Sign() < 0 || feeRate.Cmp(big.NewFloat(1)) >= 0 {
		return nil, fmt.Errorf("fee_rate must be in [0,1)")
	}
	id := "pool_" + uuid.NewString()
	pool := NewPool(id, pair, token0, token1, feeRate)
	if err := s.store.SavePool(pool); err != nil {
		return nil, fmt.Errorf("save pool: %w", err)
	}
	return pool, nil
}

// AddLiquidity deposits token0 and token1 into the pool, mints LP shares,
// and debits the user's wallet.
func (s *Service) AddLiquidity(userID, poolID string, amount0, amount1 *big.Float) (*Pool, *LPPosition, *big.Float, *big.Float, error) {
	if amount0 == nil || amount0.Sign() <= 0 || amount1 == nil || amount1.Sign() <= 0 {
		return nil, nil, nil, nil, ErrInvalidAmount
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	pool, err := s.store.GetPool(poolID)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	finalAmount1, shares, err := AddLiquidity(pool, amount0, amount1)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	// Debit user wallets.
	if s.wallet != nil {
		op0 := wallet.SettleOp{UserID: userID, Asset: pool.Token0, Delta: new(big.Float).Neg(amount0)}
		op1 := wallet.SettleOp{UserID: userID, Asset: pool.Token1, Delta: new(big.Float).Neg(finalAmount1)}
		if err := s.wallet.Settle([]wallet.SettleOp{op0, op1}, nil); err != nil {
			return nil, nil, nil, nil, fmt.Errorf("wallet debit failed: %w", err)
		}
	}

	// Update pool reserves and total shares.
	pool.Reserve0.Add(pool.Reserve0, amount0)
	pool.Reserve1.Add(pool.Reserve1, finalAmount1)
	pool.LPShares.Add(pool.LPShares, shares)
	pool.UpdatedAt = time.Now().UnixNano()
	if err := s.store.UpdatePoolReserves(pool.ID, textF(pool.Reserve0), textF(pool.Reserve1), textF(pool.LPShares)); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("update pool: %w", err)
	}

	// Upsert LP position: merge new shares with any existing position.
	existing, _ := s.store.GetPositionByPool(userID, pool.ID)
	pos := &LPPosition{
		ID:        "lp_" + uuid.NewString(),
		UserID:    userID,
		PoolID:    pool.ID,
		Shares:    new(big.Float).Copy(shares),
		CreatedAt: time.Now().UnixNano(),
		UpdatedAt: time.Now().UnixNano(),
	}
	if existing != nil && existing.Shares != nil {
		pos.ID = existing.ID
		pos.CreatedAt = existing.CreatedAt
		pos.Shares.Add(pos.Shares, existing.Shares)
	}
	if err := s.store.SavePosition(pos); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("save position: %w", err)
	}

	return pool, pos, finalAmount1, shares, nil
}

// RemoveLiquidity burns LP shares and credits the user with the proportional
// amount of each token.
func (s *Service) RemoveLiquidity(userID, poolID string, shares *big.Float) (*Pool, *big.Float, *big.Float, error) {
	if shares == nil || shares.Sign() <= 0 {
		return nil, nil, nil, ErrInvalidAmount
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	pool, err := s.store.GetPool(poolID)
	if err != nil {
		return nil, nil, nil, err
	}

	amount0, amount1, err := RemoveLiquidity(pool, shares)
	if err != nil {
		return nil, nil, nil, err
	}

	pos, err := s.store.GetPositionByPool(userID, poolID)
	if err != nil {
		return nil, nil, nil, err
	}
	if pos == nil || pos.Shares.Cmp(shares) < 0 {
		return nil, nil, nil, ErrInsufficientLiquidity
	}

	// Credit user wallets.
	if s.wallet != nil {
		op0 := wallet.SettleOp{UserID: userID, Asset: pool.Token0, Delta: amount0}
		op1 := wallet.SettleOp{UserID: userID, Asset: pool.Token1, Delta: amount1}
		if err := s.wallet.Settle([]wallet.SettleOp{op0, op1}, nil); err != nil {
			return nil, nil, nil, fmt.Errorf("wallet credit failed: %w", err)
		}
	}

	// Update pool.
	pool.Reserve0.Sub(pool.Reserve0, amount0)
	pool.Reserve1.Sub(pool.Reserve1, amount1)
	pool.LPShares.Sub(pool.LPShares, shares)
	pool.UpdatedAt = time.Now().UnixNano()
	if err := s.store.UpdatePoolReserves(pool.ID, textF(pool.Reserve0), textF(pool.Reserve1), textF(pool.LPShares)); err != nil {
		return nil, nil, nil, fmt.Errorf("update pool: %w", err)
	}

	// Update position.
	pos.Shares.Sub(pos.Shares, shares)
	pos.UpdatedAt = time.Now().UnixNano()
	if err := s.store.SavePosition(pos); err != nil {
		return nil, nil, nil, fmt.Errorf("save position: %w", err)
	}

	return pool, amount0, amount1, nil
}

// Quote returns the expected output for a swap without executing it.
func (s *Service) Quote(poolID, tokenIn string, amountIn *big.Float) (*big.Float, *big.Float, *big.Float, error) {
	pool, err := s.store.GetPool(poolID)
	if err != nil {
		return nil, nil, nil, err
	}
	return QuoteSwap(pool, tokenIn, amountIn)
}

// ExecuteSwap performs a swap against the pool, debiting the input token and
// crediting the output token.
func (s *Service) ExecuteSwap(userID, poolID, tokenIn string, amountIn *big.Float) (*Swap, error) {
	if amountIn == nil || amountIn.Sign() <= 0 {
		return nil, ErrInvalidAmount
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	pool, err := s.store.GetPool(poolID)
	if err != nil {
		return nil, err
	}

	amountOut, fee, _, err := QuoteSwap(pool, tokenIn, amountIn)
	if err != nil {
		return nil, err
	}

	var tokenOut string
	if tokenIn == pool.Token0 {
		tokenOut = pool.Token1
	} else {
		tokenOut = pool.Token0
	}

	// Debit input and credit output.
	if s.wallet != nil {
		opIn := wallet.SettleOp{UserID: userID, Asset: tokenIn, Delta: new(big.Float).Neg(amountIn)}
		opOut := wallet.SettleOp{UserID: userID, Asset: tokenOut, Delta: amountOut}
		if err := s.wallet.Settle([]wallet.SettleOp{opIn, opOut}, nil); err != nil {
			return nil, fmt.Errorf("wallet settlement failed: %w", err)
		}
	}

	// Update pool reserves.
	ApplySwap(pool, tokenIn, amountIn, amountOut)
	if err := s.store.UpdatePoolReserves(pool.ID, textF(pool.Reserve0), textF(pool.Reserve1), textF(pool.LPShares)); err != nil {
		return nil, fmt.Errorf("update pool: %w", err)
	}

	// Record swap.
	sw := &Swap{
		ID:        "swap_" + uuid.NewString(),
		PoolID:    pool.ID,
		UserID:    userID,
		TokenIn:   tokenIn,
		TokenOut:  tokenOut,
		AmountIn:  new(big.Float).Copy(amountIn),
		AmountOut: new(big.Float).Copy(amountOut),
		Fee:       fee,
		CreatedAt: time.Now().UnixNano(),
	}
	if err := s.store.SaveSwap(sw); err != nil {
		return nil, fmt.Errorf("save swap: %w", err)
	}

	return sw, nil
}

func (s *Service) GetPool(id string) (*Pool, error)        { return s.store.GetPool(id) }
func (s *Service) ListPools() ([]*Pool, error)             { return s.store.ListPools() }
func (s *Service) GetPositionByPool(userID, poolID string) (*LPPosition, error) {
	return s.store.GetPositionByPool(userID, poolID)
}
func (s *Service) ListPositionsByUser(userID string) ([]*LPPosition, error) {
	return s.store.ListPositionsByUser(userID)
}
func (s *Service) ListSwaps(poolID string, limit, offset int) ([]*Swap, error) {
	return s.store.ListSwaps(poolID, limit, offset)
}

// GetPoolByPair returns the pool for a trading pair, if one exists.
func (s *Service) GetPoolByPair(pair string) (*Pool, error) {
	return s.store.GetPoolByPair(pair)
}

// SavePoolReserves persists a pool's current reserves + LP shares without
// touching any wallet. Used by the AMM market simulator and the bootstrap
// seeder so they can move pool state (and therefore the derived price)
// without debiting a real user.
func (s *Service) SavePoolReserves(pool *Pool) error {
	if pool == nil {
		return ErrInvalidAmount
	}
	return s.store.UpdatePoolReserves(pool.ID, textF(pool.Reserve0), textF(pool.Reserve1), textF(pool.LPShares))
}

// SaveSwapRecord persists a swap record (e.g. a simulated market-maker swap)
// without wallet settlement, so admin swap history reflects simulator activity.
func (s *Service) SaveSwapRecord(sw *Swap) error {
	if sw == nil {
		return ErrInvalidAmount
	}
	if sw.ID == "" {
		sw.ID = "swap_" + uuid.NewString()
	}
	if sw.CreatedAt == 0 {
		sw.CreatedAt = time.Now().UnixNano()
	}
	return s.store.SaveSwap(sw)
}

// SeedPoolReserves injects initial liquidity into a pool by writing reserves
// directly (bypassing AddLiquidity's wallet debit). LP shares are minted as
// sqrt(reserve0 * reserve1), matching the first-deposit formula in AddLiquidity.
// Used once at bootstrap to give every market starting depth and a starting price.
func (s *Service) SeedPoolReserves(poolID string, reserve0, reserve1 *big.Float) error {
	if reserve0 == nil || reserve0.Sign() <= 0 || reserve1 == nil || reserve1.Sign() <= 0 {
		return ErrInvalidAmount
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pool, err := s.store.GetPool(poolID)
	if err != nil {
		return err
	}
	pool.Reserve0 = new(big.Float).Copy(reserve0)
	pool.Reserve1 = new(big.Float).Copy(reserve1)
	pool.LPShares = sqrtBigFloat(new(big.Float).Mul(reserve0, reserve1))
	pool.UpdatedAt = time.Now().UnixNano()
	return s.store.UpdatePoolReserves(pool.ID, textF(pool.Reserve0), textF(pool.Reserve1), textF(pool.LPShares))
}
