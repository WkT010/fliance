package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/WkT010/nexa-exchange/internal/auth"
	"github.com/WkT010/nexa-exchange/internal/cache"
)

type rateEntry struct {
	count   int
	resetAt time.Time
}

// RateLimiter implements an in-memory token-bucket-style limiter keyed by
// client IP. For multi-instance deployments, use RedisRateLimiter instead.
func RateLimiter(requests int, window time.Duration) gin.HandlerFunc {
	if requests <= 0 { requests = 100 }
	var mu sync.Mutex
	clients := make(map[string]*rateEntry)
	go func() {
		for {
			time.Sleep(window)
			mu.Lock()
			now := time.Now()
			for ip, e := range clients {
				if now.After(e.resetAt) { delete(clients, ip) }
			}
			mu.Unlock()
		}
	}()
	return func(c *gin.Context) {
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
func RedisRateLimiter(rc *cache.RedisCache, requests int, window time.Duration) gin.HandlerFunc {
	if requests <= 0 { requests = 100 }
	if window <= 0 { window = time.Second }
	return func(c *gin.Context) {
		key := "ip:" + c.ClientIP()
		allowed, err := rc.RateLimit(c.Request.Context(), key, requests, window)
		if err != nil || !allowed {
			abortRateLimit(c, window)
			return
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

// CORSMiddleware is the legacy wildcard CORS middleware.
func CORSMiddleware() gin.HandlerFunc {
	return CORSMiddlewareConfig([]string{"*"}, false)
}

// CORSMiddlewareConfig returns a CORS middleware driven by configuration.
func CORSMiddlewareConfig(origins []string, allowCreds bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		if origin == "" { origin = "*" }
		c.Header("Access-Control-Allow-Origin", origin)
		if allowCreds { c.Header("Access-Control-Allow-Credentials", "true") }
		c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Authorization,Content-Type,X-API-Key,X-Request-ID")
		c.Header("Access-Control-Max-Age", "86400")
		if c.Request.Method == "OPTIONS" { c.AbortWithStatus(http.StatusNoContent); return }
		c.Next()
	}
}

// RequestIDMiddleware attaches a unique request-id header to each request.
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		rid := c.GetHeader("X-Request-ID")
		if rid == "" {
			rid = randomID(16)
		}
		c.Set("request_id", rid)
		c.Header("X-Request-ID", rid)
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
	if n == 0 { return "0" }
	neg := n < 0
	if neg { n = -n }
	var buf [20]byte
	i := len(buf)
	for n > 0 { i--; buf[i] = byte('0' + n%10); n /= 10 }
	if neg { i--; buf[i] = '-' }
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
					if p == perm || p == "admin" { c.Next(); return }
				}
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "permission denied: " + perm, "request_id": c.GetString("request_id")})
				return
			}
		}
		if role, _ := c.Get("role"); role == "admin" { c.Next(); return }
		if perm == "read" { c.Next(); return }
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "permission denied: " + perm, "request_id": c.GetString("request_id")})
	}
}

func stripBearer(s string) string {
	if len(s) > 7 && strings.EqualFold(s[:7], "bearer ") { return s[7:] }
	return s
}