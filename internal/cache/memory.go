package cache

import (
	"context"
	"sync"
	"time"

	"github.com/WkT010/nexa-exchange/internal/matching"
)

type MemoryCache struct {
	mu         sync.RWMutex
	orderbooks map[string]*cacheEntry[*matching.OrderBookDepth]
	tickers    map[string]*cacheEntry[map[string]interface{}]
	orders     map[string]*cacheEntry[*matching.Order]
	defaultTTL time.Duration
}

type cacheEntry[T any] struct {
	data      T
	expiresAt time.Time
}

func NewMemoryCache(ttl time.Duration) *MemoryCache {
	if ttl == 0 { ttl = 5 * time.Second }
	return &MemoryCache{
		orderbooks: make(map[string]*cacheEntry[*matching.OrderBookDepth]),
		tickers:    make(map[string]*cacheEntry[map[string]interface{}]),
		orders:     make(map[string]*cacheEntry[*matching.Order]),
		defaultTTL: ttl,
	}
}

func (c *MemoryCache) Close() error { return nil }
func (c *MemoryCache) Ping(_ context.Context) error { return nil }

func isExpired[T any](e *cacheEntry[T]) bool { return time.Now().After(e.expiresAt) }

func (c *MemoryCache) SetOrderbook(_ context.Context, pair string, depth *matching.OrderBookDepth) error {
	c.mu.Lock(); defer c.mu.Unlock()
	c.orderbooks[pair] = &cacheEntry[*matching.OrderBookDepth]{data: depth, expiresAt: time.Now().Add(c.defaultTTL)}
	return nil
}
func (c *MemoryCache) GetOrderbook(_ context.Context, pair string) (*matching.OrderBookDepth, error) {
	c.mu.RLock(); defer c.mu.RUnlock()
	e, ok := c.orderbooks[pair]
	if !ok || isExpired(e) { return nil, nil }
	return e.data, nil
}
func (c *MemoryCache) SetTicker(_ context.Context, pair string, ticker map[string]interface{}) error {
	c.mu.Lock(); defer c.mu.Unlock()
	c.tickers[pair] = &cacheEntry[map[string]interface{}]{data: ticker, expiresAt: time.Now().Add(c.defaultTTL)}
	return nil
}
func (c *MemoryCache) GetTicker(_ context.Context, pair string) (map[string]interface{}, error) {
	c.mu.RLock(); defer c.mu.RUnlock()
	e, ok := c.tickers[pair]
	if !ok || isExpired(e) { return nil, nil }
	return e.data, nil
}
func (c *MemoryCache) SetOrder(_ context.Context, o *matching.Order) error {
	c.mu.Lock(); defer c.mu.Unlock()
	c.orders[o.ID] = &cacheEntry[*matching.Order]{data: o, expiresAt: time.Now().Add(10 * time.Minute)}
	return nil
}
func (c *MemoryCache) GetOrder(_ context.Context, id string) (*matching.Order, error) {
	c.mu.RLock(); defer c.mu.RUnlock()
	e, ok := c.orders[id]
	if !ok || isExpired(e) { return nil, nil }
	return e.data, nil
}
func (c *MemoryCache) RateLimit(_ context.Context, _ string, _ int, _ time.Duration) (bool, error) {
	return true, nil
}
