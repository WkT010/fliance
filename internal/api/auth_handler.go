package api

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/WkT010/nexa-exchange/internal/auth"
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
	failures int
	lockedUntil time.Time
}

type AuthHandler struct {
	m     *auth.JWTManager
	store UserStore

	lockoutMu       sync.Mutex
	lockouts        map[string]*lockoutEntry
	lockoutMax      int
	lockoutDuration time.Duration
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
	}
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
	// Account lockout check.
	if h.isLocked(r.Email) {
		c.JSON(423, gin.H{"error": "account locked due to repeated failures", "retry_after_seconds": int(h.lockoutDuration.Seconds())})
		return
	}
	u, err := h.store.GetByEmail(r.Email)
	if err != nil {
		h.recordFailure(r.Email)
		c.JSON(401, gin.H{"error": "invalid email or password"})
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(r.Password)); err != nil {
		h.recordFailure(r.Email)
		c.JSON(401, gin.H{"error": "invalid email or password"})
		return
	}
	h.resetFailures(r.Email)
	t, _ := h.m.GenerateToken(u.ID, u.Role, 24*time.Hour)
	rt, _ := h.m.GenerateToken(u.ID, u.Role, 7*24*time.Hour)
	c.JSON(200, gin.H{
		"token":         t,
		"refresh_token": rt,
		"user_id":       u.ID,
		"role":          u.Role,
		"expires_in":    86400,
	})
}

// isLocked reports whether the email is currently in a lockout window.
func (h *AuthHandler) isLocked(email string) bool {
	h.lockoutMu.Lock()
	defer h.lockoutMu.Unlock()
	e, ok := h.lockouts[email]
	if !ok {
		return false
	}
	return time.Now().Before(e.lockedUntil)
}

// recordFailure increments the failure counter and locks the account if the
// threshold is reached.
func (h *AuthHandler) recordFailure(email string) {
	h.lockoutMu.Lock()
	defer h.lockoutMu.Unlock()
	e, ok := h.lockouts[email]
	if !ok {
		e = &lockoutEntry{}
		h.lockouts[email] = e
	}
	e.failures++
	if e.failures >= h.lockoutMax {
		e.lockedUntil = time.Now().Add(h.lockoutDuration)
	}
}

// resetFailures clears the failure counter on successful login.
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
	c.JSON(201, gin.H{"user_id": u.ID, "email": u.Email})
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
	t, _ := h.m.GenerateToken(cl.UserID, cl.Role, 24*time.Hour)
	rt, _ := h.m.GenerateToken(cl.UserID, cl.Role, 7*24*time.Hour)
	c.JSON(200, gin.H{"token": t, "refresh_token": rt, "expires_in": 86400})
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
	c.JSON(http.StatusOK, gin.H{"status": "password updated"})
}

// Logout is a stateless no-op: clients simply discard their token. The endpoint
// exists so that future server-side token revocation (e.g. a Redis blacklist)
// can be wired in without changing the API surface.
func (h *AuthHandler) Logout(c *gin.Context) {
	c.JSON(200, gin.H{"status": "logged out"})
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
		c.Set("user_id", cl.UserID)
		c.Set("role", cl.Role)
		c.Next()
	}
}
