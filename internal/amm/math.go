package amm

import (
	"math/big"
	"time"
)

// precision used for big.Float string conversions.
const precision = 18

// AddLiquidity computes the amount of token1 that must be paired with amount0
// and the new LP shares issued for a pool. Returns (amount1, shares, error).
// For the first deposit, shares = sqrt(amount0 * amount1).
func AddLiquidity(pool *Pool, amount0, amount1 *big.Float) (*big.Float, *big.Float, error) {
	if amount0 == nil || amount0.Sign() <= 0 || amount1 == nil || amount1.Sign() <= 0 {
		return nil, nil, ErrInvalidAmount
	}

	zero := big.NewFloat(0)
	if pool.LPShares.Cmp(zero) == 0 {
		// First deposit: shares = sqrt(amount0 * amount1)
		product := new(big.Float).Mul(amount0, amount1)
		shares := sqrtBigFloat(product)
		return new(big.Float).Copy(amount1), shares, nil
	}

	// Proportional deposit to keep price constant.
	// amount1 = amount0 * reserve1 / reserve0
	amount1Needed := new(big.Float).Mul(amount0, pool.Reserve1)
	amount1Needed.Quo(amount1Needed, pool.Reserve0)

	// Use the smaller of the provided amount1 and the calculated need.
	finalAmount1 := new(big.Float).Copy(amount1)
	if amount1.Cmp(amount1Needed) > 0 {
		finalAmount1 = amount1Needed
	}

	// shares = amount0 * totalShares / reserve0
	shares := new(big.Float).Mul(amount0, pool.LPShares)
	shares.Quo(shares, pool.Reserve0)

	return finalAmount1, shares, nil
}

// RemoveLiquidity calculates how much of each token a user receives for
// burning shares from the pool.
func RemoveLiquidity(pool *Pool, shares *big.Float) (*big.Float, *big.Float, error) {
	if shares == nil || shares.Sign() <= 0 {
		return nil, nil, ErrInvalidAmount
	}
	if pool.LPShares.Cmp(big.NewFloat(0)) == 0 {
		return nil, nil, ErrEmptyPool
	}
	if shares.Cmp(pool.LPShares) > 0 {
		return nil, nil, ErrInsufficientLiquidity
	}

	amount0 := new(big.Float).Mul(shares, pool.Reserve0)
	amount0.Quo(amount0, pool.LPShares)

	amount1 := new(big.Float).Mul(shares, pool.Reserve1)
	amount1.Quo(amount1, pool.LPShares)

	return amount0, amount1, nil
}

// QuoteSwap computes the output amount for a constant-product swap.
// amountOut = reserveOut * amountIn * (1 - fee) / (reserveIn + amountIn * (1 - fee))
func QuoteSwap(pool *Pool, tokenIn string, amountIn *big.Float) (*big.Float, *big.Float, *big.Float, error) {
	if amountIn == nil || amountIn.Sign() <= 0 {
		return nil, nil, nil, ErrInvalidAmount
	}
	if pool.Status != "active" {
		return nil, nil, nil, ErrPoolPaused
	}

	var reserveIn, reserveOut *big.Float
	if tokenIn == pool.Token0 {
		reserveIn, reserveOut = pool.Reserve0, pool.Reserve1
	} else if tokenIn == pool.Token1 {
		reserveIn, reserveOut = pool.Reserve1, pool.Reserve0
	} else {
		return nil, nil, nil, ErrInvalidToken
	}

	if reserveIn.Sign() <= 0 || reserveOut.Sign() <= 0 {
		return nil, nil, nil, ErrEmptyPool
	}

	// fee = amountIn * feeRate
	fee := new(big.Float).Mul(amountIn, pool.FeeRate)
	// amountInWithFee = amountIn - fee
	amountInWithFee := new(big.Float).Sub(amountIn, fee)
	// numerator = reserveOut * amountInWithFee
	numerator := new(big.Float).Mul(reserveOut, amountInWithFee)
	// denominator = reserveIn + amountInWithFee
	denominator := new(big.Float).Add(reserveIn, amountInWithFee)
	amountOut := new(big.Float).Quo(numerator, denominator)

	return amountOut, fee, amountInWithFee, nil
}

// ApplySwap updates pool reserves after a successful swap.
func ApplySwap(pool *Pool, tokenIn string, amountIn, amountOut *big.Float) {
	if tokenIn == pool.Token0 {
		pool.Reserve0.Add(pool.Reserve0, amountIn)
		pool.Reserve1.Sub(pool.Reserve1, amountOut)
	} else {
		pool.Reserve1.Add(pool.Reserve1, amountIn)
		pool.Reserve0.Sub(pool.Reserve0, amountOut)
	}
	pool.UpdatedAt = time.Now().UnixNano()
}

// sqrtBigFloat returns the square root of x using Newton's method.
func sqrtBigFloat(x *big.Float) *big.Float {
	if x.Sign() <= 0 {
		return big.NewFloat(0)
	}
	z := new(big.Float).Copy(x)
	z.Quo(z, big.NewFloat(2))
	if z.Sign() == 0 {
		z = big.NewFloat(1)
	}
	for i := 0; i < 20; i++ {
		// z = (z + x/z) / 2
		t := new(big.Float).Quo(x, z)
		t.Add(t, z)
		t.Quo(t, big.NewFloat(2))
		if t.Cmp(z) == 0 {
			break
		}
		z = t
	}
	return z
}

func textF(f *big.Float) string {
	if f == nil {
		return "0"
	}
	return f.Text('f', precision)
}
