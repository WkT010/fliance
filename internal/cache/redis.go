package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/WkT010/nexa-exchange/internal/matching"
	"github.com/redis/go-redis/v9"
)

type RedisCache struct {
	client *redis.Client
	prefix string
	ttl    time.Duration
}

type Config struct {
	Addr   string
	Pass   string
	DB     int
	Prefix string
	TTL    time.Duration
}

func DefaultConfig() *Config {
	return &Config{Addr: "localhost:6379", Pass: "", DB: 0, Prefix: "nexa:", TTL: 5 * time.Second}
}

func New(cfg *Config) *RedisCache {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	if cfg.TTL == 0 {
		cfg.TTL = 5 * time.Second
	}
	rdb := redis.NewClient(&redis.Options{
		Addr: cfg.Addr, Password: cfg.Pass, DB: cfg.DB,
		DialTimeout: 3 * time.Second, ReadTimeout: 2 * time.Second, WriteTimeout: 2 * time.Second,
	})
	return &RedisCache{client: rdb, prefix: cfg.Prefix, ttl: cfg.TTL}
}

func (c *RedisCache) Close() error                   { return c.client.Close() }
func (c *RedisCache) Ping(ctx context.Context) error { return c.client.Ping(ctx).Err() }

func (c *RedisCache) keyOrderbook(pair string) string { return c.prefix + "orderbook:" + pair }
func (c *RedisCache) SetOrderbook(ctx context.Context, pair string, depth *matching.OrderBookDepth) error {
	data, _ := json.Marshal(depth)
	return c.client.Set(ctx, c.keyOrderbook(pair), data, c.ttl).Err()
}
func (c *RedisCache) GetOrderbook(ctx context.Context, pair string) (*matching.OrderBookDepth, error) {
	data, err := c.client.Get(ctx, c.keyOrderbook(pair)).Bytes()
	if err != nil {
		return nil, fmt.Errorf("get: %w", err)
	}
	var depth matching.OrderBookDepth
	if err := json.Unmarshal(data, &depth); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return &depth, nil
}

func (c *RedisCache) keyTicker(pair string) string { return c.prefix + "ticker:" + pair }
func (c *RedisCache) SetTicker(ctx context.Context, pair string, ticker map[string]interface{}) error {
	data, _ := json.Marshal(ticker)
	return c.client.Set(ctx, c.keyTicker(pair), data, c.ttl).Err()
}
func (c *RedisCache) GetTicker(ctx context.Context, pair string) (map[string]interface{}, error) {
	data, err := c.client.Get(ctx, c.keyTicker(pair)).Bytes()
	if err != nil {
		return nil, fmt.Errorf("get: %w", err)
	}
	var ticker map[string]interface{}
	if err := json.Unmarshal(data, &ticker); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return ticker, nil
}

func (c *RedisCache) keyOrder(id string) string { return c.prefix + "order:" + id }
func (c *RedisCache) SetOrder(ctx context.Context, o *matching.Order) error {
	data, _ := json.Marshal(o)
	return c.client.Set(ctx, c.keyOrder(o.ID), data, 10*time.Minute).Err()
}
func (c *RedisCache) GetOrder(ctx context.Context, id string) (*matching.Order, error) {
	data, err := c.client.Get(ctx, c.keyOrder(id)).Bytes()
	if err != nil {
		return nil, fmt.Errorf("get: %w", err)
	}
	var o matching.Order
	if err := json.Unmarshal(data, &o); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return &o, nil
}

func (c *RedisCache) RateLimit(ctx context.Context, key string, max int, window time.Duration) (bool, error) {
	rk := c.prefix + "ratelimit:" + key
	now := time.Now().UnixNano()
	windowNano := window.Nanoseconds()
	c.client.ZRemRangeByScore(ctx, rk, "0", fmt.Sprintf("%d", now-windowNano))
	count, err := c.client.ZCard(ctx, rk).Result()
	if err != nil {
		return false, err
	}
	if int(count) >= max {
		return false, nil
	}
	member := fmt.Sprintf("%d", now)
	c.client.ZAdd(ctx, rk, redis.Z{Score: float64(now), Member: member})
	c.client.Expire(ctx, rk, window)
	return true, nil
}
