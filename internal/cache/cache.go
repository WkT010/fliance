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
	RateLimit(ctx context.Context, key string, max int, window time.Duration) (bool, error)
}
