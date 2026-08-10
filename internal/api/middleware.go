package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/WkT010/nexa-exchange/internal/auth"
	"github.com/WkT010/nexa-exchange/internal/cache"
	"github.com/WkT010/nexa-exchange/internal/observability"
	"github.com/gin-gonic/gin"
)

type rateEntry struct {
	count   int
	resetAt time.Time
}

// isWebSocketUpgrade reports whether the request is a WebSocket upgrade
// (either by path prefix or by the Upgrade header).
func isWebSocketUpgrade(c *gin.Context) bool {
	return strings.HasPrefix(c.Request.URL.Path, "/ws") ||
		strings.EqualFold(c.Request.Header.Get("Upgrade"), "websocket")
}

// RateLimiter implements an in-memory token-bucket-style limiter keyed by
// client IP. For multi-instance deployments, use RedisRateLimiter instead.
//
// WebSocket upgrade requests are intentionally skipped here: they are governed
// by the dedicated WSConnectLimiter (stricter, connection-attempt based), so
// counting them twice would double-punish long-lived clients.
func RateLimiter(requests int, window time.Duration) gin.HandlerFunc {
	if requests <= 0 {
		requests = 100
	}
	var mu sync.Mutex
	clients := make(map[string]*rateEntry)
	go func() {
		for {
			time.Sleep(window)
			mu.Lock()
			now := time.Now()
			for ip, e := range clients {
				if now.After(e.resetAt) {
					delete(clients, ip)
				}
			}
			mu.Unlock()
		}
	}()
	return func(c *gin.Context) {
		// WebSocket upgrades are rate limited by WSConnectLimiter instead.
		if isWebSocketUpgrade(c) {
			c.Next()
			return
		}
		ip := c.ClientIP()
		mu.Lock()
		e, ok := clients[ip]
		now := time.Now()
		if !ok || now.After(e.resetAt) {
			clients[ip] = &rateEntry{count: 1, resetAt: now.Add(window)}
			mu.Unlock()
			c.Next()
			return
		}
		e.count++
		if e.count > requests {
			mu.Unlock()
			abortRateLimit(c, window)
			return
		}
		mu.Unlock()
		c.Next()
	}
}

// RedisRateLimiter uses Redis sorted sets for distributed rate limiting.
// It should be used when EnableRedisRateLimit is true in config.
// WebSocket upgrades are skipped for the same reason as in RateLimiter.
func RedisRateLimiter(rc *cache.RedisCache, requests int, window time.Duration) gin.HandlerFunc {
	if requests <= 0 {
		requests = 100
	}
	if window <= 0 {
		window = time.Second
	}
	return func(c *gin.Context) {
		// WebSocket upgrades are rate limited by WSConnectLimiter instead.
		if isWebSocketUpgrade(c) {
			c.Next()
			return
		}
		key := "ip:" + c.ClientIP()
		allowed, err := rc.RateLimit(c.Request.Context(), key, requests, window)
		if err != nil || !allowed {
			abortRateLimit(c, window)
			return
		}
		c.Next()
	}
}

// WSConnectLimiter limits WebSocket connection attempts per client IP
// (default: 30 per minute). It uses the shared cache abstraction when
// available (distributed deployments) and falls back to an in-memory sliding
// window when the cache is nil. Apply it directly on the /ws route.
func WSConnectLimiter(cc cache.Cache, maxPerWindow int, window time.Duration) gin.HandlerFunc {
	if maxPerWindow <= 0 {
		maxPerWindow = 30
	}
	if window <= 0 {
		window = time.Minute
	}
	var mu sync.Mutex
	mem := make(map[string]*rateEntry)
	go func() {
		for {
			time.Sleep(window)
			mu.Lock()
			now := time.Now()
			for ip, e := range mem {
				if now.After(e.resetAt) {
					delete(mem, ip)
				}
			}
			mu.Unlock()
		}
	}()
	return func(c *gin.Context) {
		ip := c.ClientIP()
		if cc != nil {
			allowed, err := cc.RateLimit(c.Request.Context(), "wsconn:"+ip, maxPerWindow, window)
			if err == nil {
				if !allowed {
					abortRateLimit(c, window)
					return
				}
				c.Next()
				return
			}
			log := observability.WithRequestID(c.Request.Context())
			log.Warn("ws connect limiter: cache unavailable, falling back to in-memory counters", "err", err)
			// fall through to the in-memory path
		}
		mu.Lock()
		e, ok := mem[ip]
		now := time.Now()
		if !ok || now.After(e.resetAt) {
			mem[ip] = &rateEntry{count: 1, resetAt: now.Add(window)}
			mu.Unlock()
			c.Next()
			return
		}
		e.count++
		over := e.count > maxPerWindow
		mu.Unlock()
		if over {
			abortRateLimit(c, window)
			return
		}
		c.Next()
	}
}

// RequestBodyLimit caps the request body size (default 1 MB) to blunt
// large-payload DoS. WebSocket upgrade requests are exempt — they carry no
// meaningful body and the connection is handed to the WebSocket layer.
func RequestBodyLimit(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if maxBytes > 0 && !isWebSocketUpgrade(c) {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		}
		c.Next()
	}
}

func abortRateLimit(c *gin.Context, window time.Duration) {
	c.Header("Retry-After", strconvFormatInt(int64(window.Seconds())))
	c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
		"error":       "rate limit exceeded",
		"retry_after": window.Seconds(),
		"request_id":  c.GetString("request_id"),
	})
}

// OriginChecker validates browser Origins against a configured allow-list.
// It is shared by the CORS middleware and the WebSocket upgrader's
// CheckOrigin so both enforce exactly the same policy.
type OriginChecker struct {
	allowAll bool
	allowed  map[string]struct{}
}

// NewOriginChecker builds a checker from the configured origin list. Entries
// are trimmed and lower-cased; "*" permits every origin (only acceptable in
// development — config.Load refuses it elsewhere).
func NewOriginChecker(origins []string) *OriginChecker {
	oc := &OriginChecker{allowed: make(map[string]struct{}, len(origins))}
	for _, o := range origins {
		o = strings.ToLower(strings.TrimSpace(o))
		if o == "" {
			continue
		}
		if o == "*" {
			oc.allowAll = true
			continue
		}
		oc.allowed[o] = struct{}{}
	}
	return oc
}

// Allowed reports whether the exact origin is on the allow-list. An empty
// origin (non-browser clients, curl, server-to-server calls) is always
// allowed here; browser requests always send an Origin header.
func (o *OriginChecker) Allowed(origin string) bool {
	if origin == "" {
		return true
	}
	if o.allowAll {
		return true
	}
	_, ok := o.allowed[strings.ToLower(strings.TrimSpace(origin))]
	return ok
}

// CORSMiddleware is the legacy wildcard CORS middleware (development only).
func CORSMiddleware() gin.HandlerFunc {
	return CORSMiddlewareConfig([]string{"*"}, false)
}

// CORSMiddlewareConfig returns a strict CORS middleware driven by the
// configured allow-list. Unknown origins receive no Access-Control-Allow-Origin
// header at all (browsers then block the response); preflights from unknown
// origins are rejected with 403. The wildcard is honoured only when
// explicitly configured (development).
func CORSMiddlewareConfig(origins []string, allowCreds bool) gin.HandlerFunc {
	checker := NewOriginChecker(origins)
	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		if origin == "" {
			// Same-origin or non-browser request: no CORS headers needed.
			c.Next()
			return
		}
		if !checker.Allowed(origin) {
			if c.Request.Method == http.MethodOptions {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
			// Respond without ACAO so the browser refuses the response.
			c.Next()
			return
		}
		c.Header("Access-Control-Allow-Origin", origin)
		c.Header("Vary", "Origin")
		if allowCreds {
			c.Header("Access-Control-Allow-Credentials", "true")
		}
		c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Authorization,Content-Type,X-API-Key,X-Request-ID")
		c.Header("Access-Control-Max-Age", "86400")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// RequestIDMiddleware attaches a unique request-id header to each request.
// It also covers WebSocket upgrades so rejected upgrades stay traceable.
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		rid := c.GetHeader("X-Request-ID")
		if rid == "" {
			rid = randomID(16)
		}
		c.Set("request_id", rid)
		c.Header("X-Request-ID", rid)
		// Propagate the id through the request context so structured logs
		// emitted downstream can be correlated via
		// observability.WithRequestID(ctx).
		c.Request = c.Request.WithContext(observability.ContextWithRequestID(c.Request.Context(), rid))
		c.Next()
	}
}

// APIKeyMiddleware validates the X-API-Key header and sets user_id/role on the
// context. It must run after RequestIDMiddleware so error responses can include
// the request id.
func APIKeyMiddleware(store auth.APIKeyStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		keyID := c.GetHeader("X-API-Key")
		secret := c.GetHeader("X-API-Secret")
		if keyID == "" || secret == "" {
			c.Next()
			return
		}
		k, err := store.Get(keyID)
		if err != nil || k == nil || !k.Active {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid api key", "request_id": c.GetString("request_id")})
			return
		}
		if !k.Validate(secret) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid api key", "request_id": c.GetString("request_id")})
			return
		}
		c.Set("user_id", k.UserID)
		c.Set("role", "trader")
		c.Next()
	}
}

// LoggerMiddleware logs each request in Apache-style format with request ID.
func LoggerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		c.Next()
		rid := c.GetString("request_id")
		gin.DefaultWriter.Write([]byte(
			time.Now().Format("2006/01/02 15:04:05") + " " +
				"[" + rid + "] " +
				http.StatusText(c.Writer.Status()) + " " +
				time.Since(start).String() + " " +
				c.Request.Method + " " + path + "\n",
		))
	}
}

func randomID(nBytes int) string {
	b := make([]byte, nBytes)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func strconvFormatInt(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// AdminOnly guards endpoints for admin users only.
func AdminOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		role, _ := c.Get("role")
		if role != "admin" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin only", "request_id": c.GetString("request_id")})
			return
		}
		c.Next()
	}
}

// RequirePermission guards that the authenticated principal has a given permission.
func RequirePermission(perm string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if perms, ok := c.Get("permissions"); ok {
			if list, ok := perms.([]string); ok {
				for _, p := range list {
					if p == perm || p == "admin" {
						c.Next()
						return
					}
				}
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "permission denied: " + perm, "request_id": c.GetString("request_id")})
				return
			}
		}
		if role, _ := c.Get("role"); role == "admin" {
			c.Next()
			return
		}
		if perm == "read" {
			c.Next()
			return
		}
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "permission denied: " + perm, "request_id": c.GetString("request_id")})
	}
}

func stripBearer(s string) string {
	if len(s) > 7 && strings.EqualFold(s[:7], "bearer ") {
		return s[7:]
	}
	return s
}

// TokenBlacklist revokes JWTs before their natural expiry (logout, password
// change, refresh-token rotation). Tokens are keyed by SHA-256 hash so raw
// tokens are never held in memory longer than necessary.
//
// The authoritative membership store is a local map with TTL eviction. When a
// cache.Cache is available, each revocation is additionally recorded through
// the cache's RateLimit primitive (key "tkblack:<hash>", TTL = remaining
// token lifetime) for observability/audit. NOTE: the cache.Cache interface
// exposes no generic read (only RateLimit, which consumes quota and therefore
// cannot serve as an existence probe), so IsRevoked still checks the local
// map only. Multi-instance limitation: a logout/rotation performed on one
// replica is not enforced by the others until the token naturally expires;
// closing this gap requires a cache interface with a real Get/Set.
// When the cache is nil the blacklist degrades gracefully to in-memory
// operation (with a one-time warning).
type TokenBlacklist struct {
	mu      sync.Mutex
	entries map[string]time.Time // sha256(token) -> expiry
	cache   cache.Cache
	warned  bool
}

func NewTokenBlacklist(cc cache.Cache) *TokenBlacklist {
	if cc == nil {
		slog.Warn("token blacklist running in-memory only (no cache configured); revocations do not survive restarts")
	}
	return &TokenBlacklist{entries: make(map[string]time.Time), cache: cc}
}

func tokenKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Revoke blacklists the token until its natural expiry. Already-expired
// tokens are ignored.
func (b *TokenBlacklist) Revoke(token string, expiry time.Time) {
	if token == "" {
		return
	}
	ttl := time.Until(expiry)
	if ttl <= 0 {
		return
	}
	key := tokenKey(token)
	cc := b.record(key, expiry)
	b.writeThrough(cc, key, ttl)
}

// RevokeIfAbsent atomically revokes the token only if it has not been revoked
// yet (check-and-set under the same lock, closing the TOCTOU window of a
// separate IsRevoked-then-Revoke sequence). Returns true when this call
// recorded the revocation (the caller may proceed, e.g. issue rotated
// tokens); false when the token was already revoked (a concurrent request won
// the race and the caller must reject the request). Tokens with no remaining
// lifetime cannot be abused, so they report success without recording.
func (b *TokenBlacklist) RevokeIfAbsent(token string, expiry time.Time) bool {
	if token == "" {
		return false
	}
	ttl := time.Until(expiry)
	if ttl <= 0 {
		return true
	}
	key := tokenKey(token)
	b.mu.Lock()
	if exp, ok := b.entries[key]; ok && time.Now().Before(exp) {
		b.mu.Unlock()
		return false
	}
	b.entries[key] = expiry
	b.purgeExpiredLocked()
	cc := b.cache
	b.mu.Unlock()
	b.writeThrough(cc, key, ttl)
	return true
}

// record stores a revocation entry and returns the cache (if any) for the
// subsequent write-through. Callers must not hold b.mu.
func (b *TokenBlacklist) record(key string, expiry time.Time) cache.Cache {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.entries[key] = expiry
	b.purgeExpiredLocked()
	return b.cache
}

// purgeExpiredLocked opportunistically drops expired entries to bound memory.
// Callers must hold b.mu.
func (b *TokenBlacklist) purgeExpiredLocked() {
	if len(b.entries) > 256 {
		now := time.Now()
		for k, exp := range b.entries {
			if now.After(exp) {
				delete(b.entries, k)
			}
		}
	}
}

// writeThrough mirrors a revocation into the shared cache. Failures are
// logged once (warned latches to true so a flapping cache cannot spam logs).
func (b *TokenBlacklist) writeThrough(cc cache.Cache, key string, ttl time.Duration) {
	if cc == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := cc.RateLimit(ctx, "tkblack:"+key, 1, ttl); err != nil {
		b.mu.Lock()
		if !b.warned {
			b.warned = true
			slog.Warn("token blacklist: cache write failed", "err", err)
		}
		b.mu.Unlock()
	}
}

// IsRevoked reports whether the token has been revoked and is still within
// its revocation window. It checks the local map only: the cache.Cache
// interface has no generic read (RateLimit consumes quota and cannot act as a
// probe), so revocations recorded on another instance are not visible here
// (single-instance limitation, see the type comment).
func (b *TokenBlacklist) IsRevoked(token string) bool {
	if token == "" {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	exp, ok := b.entries[tokenKey(token)]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		return false
	}
	return true
}
