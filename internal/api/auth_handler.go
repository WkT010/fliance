package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/WkT010/nexa-exchange/internal/auth"
	"github.com/WkT010/nexa-exchange/internal/cache"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

type User struct {
	ID           string `json:"id"`
	Email        string `json:"email"`
	PasswordHash string `json:"-"`
	Role         string `json:"role"`
	CreatedAt    int64  `json:"created_at"`
	UpdatedAt    int64  `json:"updated_at"`
}

type UserStore interface {
	GetByEmail(string) (*User, error)
	GetByID(string) (*User, error)
	Create(*User) error
	Update(*User) error
}

// lockoutEntry tracks consecutive failed login attempts per user (by email).
// After the threshold is reached the account is locked for the configured
// duration. Successful login resets the counter.
type lockoutEntry struct {
	failures    int
	lockedUntil time.Time
}

type AuthHandler struct {
	m     *auth.JWTManager
	store UserStore

	lockoutMu       sync.Mutex
	lockouts        map[string]*lockoutEntry
	lockoutMax      int
	lockoutDuration time.Duration

	// cache backs the distributed lockout counter when available (nil =>
	// in-memory fallback). blacklist revokes tokens on logout / password
	// change / refresh rotation.
	cache     cache.Cache
	blacklist *TokenBlacklist
}

// NewAuthHandler constructs an auth handler with default lockout policy
// (5 failures => 15-minute lockout). Use SetLockoutPolicy to override.
func NewAuthHandler(m *auth.JWTManager, store UserStore) *AuthHandler {
	return &AuthHandler{
		m:               m,
		store:           store,
		lockouts:        make(map[string]*lockoutEntry),
		lockoutMax:      5,
		lockoutDuration: 15 * time.Minute,
		blacklist:       NewTokenBlacklist(nil),
	}
}

// SetCache wires the shared cache abstraction. It enables distributed login
// lockout (key "lockout:<email>", sliding window = lockout duration) and
// cache write-through for the token blacklist. With a nil cache both degrade
// to in-memory operation.
func (h *AuthHandler) SetCache(cc cache.Cache) {
	h.cache = cc
	h.blacklist = NewTokenBlacklist(cc)
}

// TokenBlacklist exposes the revocation store so the WebSocket layer can
// apply the same checks to token-based WS authentication.
func (h *AuthHandler) TokenBlacklist() *TokenBlacklist {
	return h.blacklist
}

// SetLockoutPolicy configures the account-lockout threshold and duration.
func (h *AuthHandler) SetLockoutPolicy(maxFailures int, lockDuration time.Duration) {
	if maxFailures > 0 {
		h.lockoutMax = maxFailures
	}
	if lockDuration > 0 {
		h.lockoutDuration = lockDuration
	}
}

type loginReq struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=8"`
}

func (h *AuthHandler) Login(c *gin.Context) {
	var r loginReq
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(400, gin.H{"error": "invalid credentials format"})
		return
	}
	if h.store == nil {
		c.JSON(500, gin.H{"error": "authentication unavailable"})
		return
	}
	// Account lockout. In memory mode the lock state is readable without side
	// effects, so locked accounts are rejected before any password work. In
	// cache mode the only primitive is RateLimit, which consumes quota on
	// every call and therefore cannot be used as a read-only pre-check: the
	// quota is consumed on the failure path instead (recordFailure), so
	// successful logins never count against the lockout budget.
	if h.cache == nil && h.isLocked(r.Email) {
		c.JSON(423, gin.H{"error": "account locked due to repeated failures", "retry_after_seconds": int(h.lockoutDuration.Seconds())})
		return
	}
	u, err := h.store.GetByEmail(r.Email)
	if err != nil {
		if h.recordFailure(r.Email) {
			c.JSON(423, gin.H{"error": "account locked due to repeated failures", "retry_after_seconds": int(h.lockoutDuration.Seconds())})
			return
		}
		c.JSON(401, gin.H{"error": "invalid email or password"})
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(r.Password)); err != nil {
		if h.recordFailure(r.Email) {
			c.JSON(423, gin.H{"error": "account locked due to repeated failures", "retry_after_seconds": int(h.lockoutDuration.Seconds())})
			return
		}
		c.JSON(401, gin.H{"error": "invalid email or password"})
		return
	}
	h.resetFailures(r.Email)
	t, _ := h.m.GenerateToken(u.ID, u.Role, 24*time.Hour)
	rt, _ := h.m.GenerateToken(u.ID, u.Role, 7*24*time.Hour)
	c.JSON(200, gin.H{
		"token":         t,
		"access_token":  t,
		"refresh_token": rt,
		"role":          u.Role,
		"expires_in":    86400,
	})
}

// isLocked reports whether the email is in a lockout window (in-memory
// counters only). It never mutates state; the cache-backed path cannot offer
// a side-effect-free read, so it is skipped when a cache is configured and
// lock detection happens in recordFailure instead.
func (h *AuthHandler) isLocked(email string) bool {
	h.lockoutMu.Lock()
	defer h.lockoutMu.Unlock()
	e, ok := h.lockouts[email]
	if !ok {
		return false
	}
	return time.Now().Before(e.lockedUntil)
}

// recordFailure counts a failed login and returns true when the account is
// already locked (i.e. this attempt happened after the threshold was
// reached). Only failed attempts consume quota — successful logins never
// touch the counter, so normal users cannot lock themselves out by logging
// in often. Cache mode uses the RateLimit sliding window keyed by email
// (persists across restarts/instances); on cache errors it falls back to the
// in-memory counter. Known limitation of the cache mode: the lock only
// becomes visible on the attempt *after* the quota is exhausted, so one
// correct-password login may still slip through immediately after the last
// counted failure; the window then rejects every further failure.
func (h *AuthHandler) recordFailure(email string) bool {
	if h.cache != nil {
		allowed, err := h.cache.RateLimit(context.Background(), "lockout:"+email, h.lockoutMax, h.lockoutDuration)
		if err == nil {
			// The quota-exhausting attempt itself is still allowed by
			// RateLimit; the next attempt is refused, which we surface as
			// "locked". Same observable behaviour as the memory path.
			return !allowed
		}
		slog.Warn("lockout cache unavailable, falling back to in-memory lockout", "err", err)
	}
	h.lockoutMu.Lock()
	defer h.lockoutMu.Unlock()
	e, ok := h.lockouts[email]
	locked := ok && time.Now().Before(e.lockedUntil)
	if !ok {
		e = &lockoutEntry{}
		h.lockouts[email] = e
	}
	e.failures++
	if e.failures >= h.lockoutMax {
		e.lockedUntil = time.Now().Add(h.lockoutDuration)
	}
	return locked
}

// resetFailures clears the failure counter on successful login (in-memory
// fallback only; the cache window expires on its own).
func (h *AuthHandler) resetFailures(email string) {
	h.lockoutMu.Lock()
	defer h.lockoutMu.Unlock()
	delete(h.lockouts, email)
}

type regReq struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=8"`
}

func (h *AuthHandler) Register(c *gin.Context) {
	var r regReq
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(400, gin.H{"error": "email and password required (min 8 chars)"})
		return
	}
	if h.store == nil {
		c.JSON(500, gin.H{"error": "registration unavailable"})
		return
	}
	if existing, _ := h.store.GetByEmail(r.Email); existing != nil {
		c.JSON(409, gin.H{"error": "email already registered"})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(r.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(500, gin.H{"error": "internal error"})
		return
	}
	idBytes := make([]byte, 8)
	rand.Read(idBytes)
	uid := fmt.Sprintf("usr_%s", hex.EncodeToString(idBytes))
	now := time.Now().UnixNano()
	u := &User{ID: uid, Email: r.Email, PasswordHash: string(hash), Role: "user", CreatedAt: now, UpdatedAt: now}
	if err := h.store.Create(u); err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
			c.JSON(409, gin.H{"error": "email already registered"})
			return
		}
		c.JSON(500, gin.H{"error": "create failed"})
		return
	}
	accessToken, _ := h.m.GenerateToken(u.ID, u.Role, 24*time.Hour)
	c.JSON(201, gin.H{
		"user_id":      u.ID,
		"email":        u.Email,
		"token":        accessToken,
		"access_token": accessToken,
		"expires_in":   86400,
	})
}

type refreshReq struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

func (h *AuthHandler) RefreshToken(c *gin.Context) {
	var r refreshReq
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(400, gin.H{"error": "refresh_token required"})
		return
	}
	cl, err := h.m.ValidateToken(r.RefreshToken)
	if err != nil {
		c.JSON(401, gin.H{"error": "invalid refresh token"})
		return
	}
	// Token-type distinction: a token explicitly marked as an access token
	// must never be usable on the refresh endpoint. Legacy tokens carry no
	// type claim and remain accepted for backward compatibility.
	if tt, _ := parseExtraClaims(r.RefreshToken); tt == "access" {
		c.JSON(401, gin.H{"error": "invalid refresh token"})
		return
	}
	if h.blacklist != nil {
		if cl.ExpiresAt != nil {
			// Refresh-token rotation with an atomic check-and-revoke: the
			// presented refresh token is single-use, and RevokeIfAbsent
			// closes the race where two concurrent refresh requests both
			// passed an IsRevoked check before either revoked the token.
			// Only the winner may mint new tokens.
			if !h.blacklist.RevokeIfAbsent(r.RefreshToken, cl.ExpiresAt.Time) {
				c.JSON(401, gin.H{"error": "invalid refresh token"})
				return
			}
		} else if h.blacklist.IsRevoked(r.RefreshToken) {
			// No expiry claim to anchor a revocation window; fall back to
			// the plain membership check.
			c.JSON(401, gin.H{"error": "invalid refresh token"})
			return
		}
	}
	t, _ := h.m.GenerateToken(cl.UserID, cl.Role, 24*time.Hour)
	rt, _ := h.m.GenerateToken(cl.UserID, cl.Role, 7*24*time.Hour)
	c.JSON(200, gin.H{"token": t, "access_token": t, "refresh_token": rt, "expires_in": 86400})
}

type changePasswordReq struct {
	CurrentPassword string `json:"current_password" binding:"required,min=8"`
	NewPassword     string `json:"new_password" binding:"required,min=8"`
}

// ChangePassword allows an authenticated user to update their password.
// POST /api/v2/auth/change-password
func (h *AuthHandler) ChangePassword(c *gin.Context) {
	uid, _ := c.Get("user_id")
	userID, _ := uid.(string)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var r changePasswordReq
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "current_password and new_password required (min 8 chars)"})
		return
	}
	u, err := h.store.GetByID(userID)
	if err != nil || u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not found"})
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(r.CurrentPassword)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "current password is incorrect"})
		return
	}
	newHash, err := bcrypt.GenerateFromPassword([]byte(r.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash password"})
		return
	}
	// Update expects a user with the new password hash and refreshed timestamp.
	u.PasswordHash = string(newHash)
	u.UpdatedAt = time.Now().UnixNano()
	if err := h.store.Update(u); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update password"})
		return
	}
	// Revoke the token used for this request: after a password change the
	// old credential must stop working immediately.
	if tok := currentBearerToken(c); tok != "" {
		h.revokeCurrent(c, tok)
	}
	c.JSON(http.StatusOK, gin.H{"status": "password updated"})
}

// Logout revokes the presented token (TTL = remaining token lifetime) so it
// can no longer pass the Auth middleware. The JSON surface is unchanged for
// backward compatibility.
func (h *AuthHandler) Logout(c *gin.Context) {
	if tok := currentBearerToken(c); tok != "" {
		h.revokeCurrent(c, tok)
	}
	c.JSON(200, gin.H{"status": "logged out"})
}

// revokeCurrent blacklists a validated token until its natural expiry.
func (h *AuthHandler) revokeCurrent(c *gin.Context, token string) {
	if h.blacklist == nil {
		return
	}
	if cl, err := h.m.ValidateToken(token); err == nil && cl.ExpiresAt != nil {
		h.blacklist.Revoke(token, cl.ExpiresAt.Time)
	}
}

// currentBearerToken extracts the raw bearer token from the Authorization
// header ("" when absent or malformed).
func currentBearerToken(c *gin.Context) string {
	a := c.GetHeader("Authorization")
	if a == "" {
		return ""
	}
	p := strings.SplitN(a, " ", 2)
	if len(p) != 2 || !strings.EqualFold(p[0], "bearer") {
		return ""
	}
	return p[1]
}

// parseExtraClaims decodes optional claims ("token_type"/"typ" and "jti")
// from a JWT payload without re-verifying the signature — callers must only
// invoke it on tokens that already passed JWTManager.ValidateToken. It exists
// because internal/auth.Claims cannot be extended here; once the token issuer
// starts embedding these claims, access/refresh tokens become distinguishable
// and legacy type-less tokens keep working.
func parseExtraClaims(tokenStr string) (tokenType, jti string) {
	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		return "", ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", ""
	}
	var extra struct {
		TokenType string `json:"token_type"`
		Typ       string `json:"typ"`
		JTI       string `json:"jti"`
	}
	if err := json.Unmarshal(payload, &extra); err != nil {
		return "", ""
	}
	tt := extra.TokenType
	if tt == "" {
		tt = extra.Typ
	}
	return tt, extra.JTI
}

// AuthMiddleware validates a Bearer JWT and sets user_id/role on the context.
// If an X-API-Key header is present and was already validated by
// APIKeyMiddleware (user_id already set), the JWT check is skipped.
func (h *AuthHandler) AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// If API-key middleware already authenticated the request, honour it.
		if uid, ok := c.Get("user_id"); ok && uid != nil && uid.(string) != "" {
			c.Next()
			return
		}
		a := c.GetHeader("Authorization")
		if a == "" {
			c.AbortWithStatusJSON(401, gin.H{"error": "authorization header required", "request_id": c.GetString("request_id")})
			return
		}
		p := strings.SplitN(a, " ", 2)
		if len(p) != 2 || !strings.EqualFold(p[0], "bearer") {
			c.AbortWithStatusJSON(401, gin.H{"error": "bearer token required", "request_id": c.GetString("request_id")})
			return
		}
		cl, err := h.m.ValidateToken(p[1])
		if err != nil {
			c.AbortWithStatusJSON(401, gin.H{"error": "invalid or expired token", "request_id": c.GetString("request_id")})
			return
		}
		// Revoked tokens (logout / password change / rotated refresh tokens)
		// are rejected even before their natural expiry.
		if h.blacklist != nil && h.blacklist.IsRevoked(p[1]) {
			c.AbortWithStatusJSON(401, gin.H{"error": "invalid or expired token", "request_id": c.GetString("request_id")})
			return
		}
		// Token-type distinction: refresh tokens must never authenticate API
		// requests. Legacy tokens without a type claim stay accepted.
		if tt, _ := parseExtraClaims(p[1]); tt == "refresh" {
			c.AbortWithStatusJSON(401, gin.H{"error": "invalid or expired token", "request_id": c.GetString("request_id")})
			return
		}
		c.Set("user_id", cl.UserID)
		c.Set("role", cl.Role)
		c.Next()
	}
}
