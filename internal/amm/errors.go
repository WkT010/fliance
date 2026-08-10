package amm

import "errors"

var (
	ErrInvalidAmount         = errors.New("amount must be positive")
	ErrInvalidToken          = errors.New("token not in pool")
	ErrEmptyPool             = errors.New("pool has no liquidity")
	ErrInsufficientLiquidity = errors.New("insufficient liquidity")
	ErrPoolPaused            = errors.New("pool is paused")
	ErrPoolNotFound          = errors.New("pool not found")
	ErrPositionNotFound      = errors.New("liquidity position not found")
	ErrInsufficientBalance   = errors.New("insufficient wallet balance")
	ErrSlippageExceeded      = errors.New("slippage exceeded: output below min_amount_out")
)
