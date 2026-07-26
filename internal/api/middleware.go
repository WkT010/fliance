package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type rateEntry struct {
	count   int
	resetAt time.Time
}

// RateLimiter implements a simple in-memory token-bucket-style limiter keyed by
// client IP. For multi-instance deployments the Redis-backed limiter in
// internal/cache/redis.go should be used instead (gated by
// cfg.EnableRedisRateLimit).
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
			c.Header("Retry-After", strconvFormatInt(int64(window.Seconds())))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":       "rate limit exceeded",
				"retry_after": window.Seconds(),
				"request_id":  c.GetString("request_id"),
			})
			return
		}
		mu.Unlock()
		c.Next()
	}
}

// CORSMiddleware is the legacy wildcard CORS middleware. New code should use
// CORSMiddlewareConfig (router.go) which is driven by configuration.
func CORSMiddleware() gin.HandlerFunc {
	return CORSMiddlewareConfig([]string{"*"}, false)
}

// LoggerMiddleware logs each request in Apache-style format with the request
// ID for traceability.
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

// randomID returns a hex-encoded random string of the requested byte length.
// Used for request IDs and other ephemeral identifiers.
func randomID(nBytes int) string {
	b := make([]byte, nBytes)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// strconvFormatInt is a tiny indirection to avoid importing strconv here just
// for one Int64 -> string conversion.
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

// AdminOnly is a guard middleware that aborts with 403 if the authenticated
// user is not an admin. It must run after the auth middleware.
func AdminOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		role, _ := c.Get("role")
		if role != "admin" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error":      "admin only",
				"request_id": c.GetString("request_id"),
			})
			return
		}
		c.Next()
	}
}

// RequirePermission is a guard that ensures the authenticated principal (JWT or
// API key) has the given permission. Permission "admin" always passes.
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
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
					"error":      "permission denied: " + perm,
					"request_id": c.GetString("request_id"),
				})
				return
			}
		}
		// JWT path has no permissions list; allow if role is admin.
		if role, _ := c.Get("role"); role == "admin" {
			c.Next()
			return
		}
		// Default: allow read-only for any authenticated user, require
		// explicit permission for trade/withdraw actions.
		if perm == "read" {
			c.Next()
			return
		}
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"error":      "permission denied: " + perm,
			"request_id": c.GetString("request_id"),
		})
	}
}

// stripBearer removes a leading "Bearer " prefix from a token string.
func stripBearer(s string) string {
	if len(s) > 7 && strings.EqualFold(s[:7], "bearer ") {
		return s[7:]
	}
	return s
}
