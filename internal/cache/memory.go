package cache

import (
	"context"
	"sync"
	"time"

	"github.com/WkT010/nexa-exchange/internal/matching"
)

// rlMaxKeys 为限速窗口表容量上限，超限时按最旧访问清理，防止内存泄漏。
const rlMaxKeys = 10000

type MemoryCache struct {
	mu         sync.RWMutex
	orderbooks map[string]*cacheEntry[*matching.OrderBookDepth]
	tickers    map[string]*cacheEntry[map[string]interface{}]
	orders     map[string]*cacheEntry[*matching.Order]
	defaultTTL time.Duration

	rlMu   sync.Mutex
	rlKeys map[string]*rlWindow
}

// rlWindow 为单个 key 的滑动窗口计数器，与 RedisCache.RateLimit 语义一致。
type rlWindow struct {
	timestamps []int64 // UnixNano 时间戳，单调递增
	lastSeen   int64
}

type cacheEntry[T any] struct {
	data      T
	expiresAt time.Time
}

func NewMemoryCache(ttl time.Duration) *MemoryCache {
	if ttl == 0 {
		ttl = 5 * time.Second
	}
	return &MemoryCache{
		orderbooks: make(map[string]*cacheEntry[*matching.OrderBookDepth]),
		tickers:    make(map[string]*cacheEntry[map[string]interface{}]),
		orders:     make(map[string]*cacheEntry[*matching.Order]),
		defaultTTL: ttl,
		rlKeys:     make(map[string]*rlWindow),
	}
}

func (c *MemoryCache) Close() error                 { return nil }
func (c *MemoryCache) Ping(_ context.Context) error { return nil }

func isExpired[T any](e *cacheEntry[T]) bool { return time.Now().After(e.expiresAt) }

func (c *MemoryCache) SetOrderbook(_ context.Context, pair string, depth *matching.OrderBookDepth) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.orderbooks[pair] = &cacheEntry[*matching.OrderBookDepth]{data: depth, expiresAt: time.Now().Add(c.defaultTTL)}
	return nil
}
func (c *MemoryCache) GetOrderbook(_ context.Context, pair string) (*matching.OrderBookDepth, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.orderbooks[pair]
	if !ok || isExpired(e) {
		return nil, nil
	}
	return e.data, nil
}
func (c *MemoryCache) SetTicker(_ context.Context, pair string, ticker map[string]interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tickers[pair] = &cacheEntry[map[string]interface{}]{data: ticker, expiresAt: time.Now().Add(c.defaultTTL)}
	return nil
}
func (c *MemoryCache) GetTicker(_ context.Context, pair string) (map[string]interface{}, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.tickers[pair]
	if !ok || isExpired(e) {
		return nil, nil
	}
	return e.data, nil
}
func (c *MemoryCache) SetOrder(_ context.Context, o *matching.Order) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.orders[o.ID] = &cacheEntry[*matching.Order]{data: o, expiresAt: time.Now().Add(10 * time.Minute)}
	return nil
}
func (c *MemoryCache) GetOrder(_ context.Context, id string) (*matching.Order, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.orders[id]
	if !ok || isExpired(e) {
		return nil, nil
	}
	return e.data, nil
}

// RateLimit 实现与 Redis 版一致的滑动窗口限速：
// 窗口（window）内最多允许 max 次调用；未超限则记录本次并返回 true，
// 超限返回 false；窗口过期后计数自动重置。
// 惰性清理过期 key，并在超过 rlMaxKeys 时按最旧访问淘汰。
func (c *MemoryCache) RateLimit(_ context.Context, key string, max int, window time.Duration) (bool, error) {
	if max <= 0 || window <= 0 {
		return true, nil
	}
	now := time.Now().UnixNano()
	cutoff := now - window.Nanoseconds()

	c.rlMu.Lock()
	defer c.rlMu.Unlock()

	w, ok := c.rlKeys[key]
	if !ok {
		c.evictRL(cutoff, now)
		w = &rlWindow{}
		c.rlKeys[key] = w
	}
	w.lastSeen = now

	// 惰性清理：丢弃窗口外的过期时间戳
	i := 0
	for i < len(w.timestamps) && w.timestamps[i] <= cutoff {
		i++
	}
	if i > 0 {
		w.timestamps = append([]int64(nil), w.timestamps[i:]...)
	}

	if len(w.timestamps) >= max {
		return false, nil
	}
	w.timestamps = append(w.timestamps, now)
	return true, nil
}

// evictRL 容量保护：先批量清除已过期窗口，仍超限时按最旧访问淘汰。
func (c *MemoryCache) evictRL(cutoff, now int64) {
	if len(c.rlKeys) < rlMaxKeys {
		return
	}
	for k, w := range c.rlKeys {
		if len(w.timestamps) == 0 || w.timestamps[len(w.timestamps)-1] <= cutoff {
			delete(c.rlKeys, k)
		}
	}
	for len(c.rlKeys) >= rlMaxKeys {
		var oldestKey string
		var oldestTs int64 = now
		first := true
		for k, w := range c.rlKeys {
			if first || w.lastSeen < oldestTs {
				oldestKey, oldestTs, first = k, w.lastSeen, false
			}
		}
		if first {
			break
		}
		delete(c.rlKeys, oldestKey)
	}
}
