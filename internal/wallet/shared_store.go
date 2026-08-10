package wallet

import (
	"context"
	"os"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// ---------------------------------------------------------------------------
// L2 shared store for horizontal scaling (plan 6.2)
// ---------------------------------------------------------------------------
//
// The wallet Service keeps processed-fill dedup records and per-order
// reservations in local memory (L1). For multi-instance (HA) deployments an
// optional L2 shared store can be injected via Service.SetSharedStore:
//
//   - processedFills: write-through; SettleFill idempotency uses SetNX as an
//     atomic cross-instance claim, so two instances consuming the same fill
//     event settle it exactly once.
//   - reservations:   write-through; an instance that never saw an order can
//     still release its reserved funds by loading the record from L2.
//
// When the L2 store is nil or unavailable the service degrades gracefully to
// the existing local-only behaviour (a single warning is logged).
//
// Wiring (cmd/ is intentionally NOT modified by this package; connect it in
// cmd/wallet-service/main.go or cmd/api-gateway/main.go when convenient):
//
//	rs, err := wallet.SharedStoreFromEnv() // reads WALLET_SHARED_STORE_ADDR
//	if err != nil { log.Printf("shared store disabled: %v", err) }
//	if rs != nil {
//	    walletSvc.SetSharedStore(rs)
//	}

// RedisLike is the minimal KV surface the wallet L2 cache needs. It is
// deliberately narrower than go-redis's full API so tests can fake it easily
// and alternate backends (memcached etc.) can be adapted.
type RedisLike interface {
	// SetNX atomically sets key=value if absent. Returns true if the key was
	// set (claim acquired), false if it already existed.
	SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error)
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string, ttl time.Duration) error
	Del(ctx context.Context, key string) error
	Expire(ctx context.Context, key string, ttl time.Duration) error
}

// ErrSharedKeyMissing is returned by Get when the key does not exist,
// mirroring redis.Nil so adapters can translate backend-specific misses.
var ErrSharedKeyMissing = redis.Nil

// redisSharedStore adapts a *redis.Client to RedisLike.
type redisSharedStore struct{ c *redis.Client }

// NewRedisSharedStore wraps a go-redis client as the wallet L2 store.
func NewRedisSharedStore(c *redis.Client) RedisLike { return &redisSharedStore{c: c} }

func (r *redisSharedStore) SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	return r.c.SetNX(ctx, key, value, ttl).Result()
}
func (r *redisSharedStore) Get(ctx context.Context, key string) (string, error) {
	return r.c.Get(ctx, key).Result() // redis.Nil propagates as ErrSharedKeyMissing
}
func (r *redisSharedStore) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	return r.c.Set(ctx, key, value, ttl).Err()
}
func (r *redisSharedStore) Del(ctx context.Context, key string) error {
	return r.c.Del(ctx, key).Err()
}
func (r *redisSharedStore) Expire(ctx context.Context, key string, ttl time.Duration) error {
	return r.c.Expire(ctx, key, ttl).Err()
}

// Shared-store environment configuration.
const (
	// EnvSharedStoreAddr enables the L2 store when set, e.g. "localhost:6379".
	EnvSharedStoreAddr = "WALLET_SHARED_STORE_ADDR"
	// EnvSharedStoreDB optionally selects the Redis DB index (default 0).
	EnvSharedStoreDB = "WALLET_SHARED_STORE_DB"
	// EnvSharedStorePassword optionally sets the Redis AUTH password.
	EnvSharedStorePassword = "WALLET_SHARED_STORE_PASSWORD"
)

// SharedStoreFromEnv builds the L2 store from environment variables. It
// returns (nil, nil) when WALLET_SHARED_STORE_ADDR is unset, letting callers
// keep the pure-local behaviour without special-casing. go-redis connects
// lazily, so this does not require a reachable server at startup; runtime
// unavailability is handled by graceful degradation inside the Service.
func SharedStoreFromEnv() (RedisLike, error) {
	addr := os.Getenv(EnvSharedStoreAddr)
	if addr == "" {
		return nil, nil
	}
	opts := &redis.Options{Addr: addr}
	if db := os.Getenv(EnvSharedStoreDB); db != "" {
		n, err := strconv.Atoi(db)
		if err != nil {
			return nil, err
		}
		opts.DB = n
	}
	if pw := os.Getenv(EnvSharedStorePassword); pw != "" {
		opts.Password = pw
	}
	return NewRedisSharedStore(redis.NewClient(opts)), nil
}

// L2 key layout. All keys carry the "wallet:" namespace so multiple services
// can share one Redis instance.
const (
	sharedFillPrefix = "wallet:fill:" // + fillID  -> claim owner (instance ID)
	sharedResPrefix  = "wallet:res:"  // + orderID -> reservation JSON
)
