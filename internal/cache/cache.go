package cache

import (
	"context"
	"time"

	"github.com/WkT010/nexa-exchange/internal/matching"
)

type Cache interface {
	Close() error
	Ping(ctx context.Context) error
	SetOrderbook(ctx context.Context, pair string, depth *matching.OrderBookDepth) error
	GetOrderbook(ctx context.Context, pair string) (*matching.OrderBookDepth, error)
	SetTicker(ctx context.Context, pair string, ticker map[string]interface{}) error
	GetTicker(ctx context.Context, pair string) (map[string]interface{}, error)
	SetOrder(ctx context.Context, o *matching.Order) error
	GetOrder(ctx context.Context, id string) (*matching.Order, error)
	// RateLimit 滑动窗口限速：在 window 窗口内最多允许 max 次调用。
	// 返回 true 表示放行并已记录本次调用，false 表示超限拒绝。
	// 所有实现（Redis / 内存）必须保持该语义一致。
	RateLimit(ctx context.Context, key string, max int, window time.Duration) (bool, error)
}
