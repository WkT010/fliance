package api

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"
	"github.com/gin-gonic/gin"
	"github.com/WkT010/nexa-exchange/internal/auth"
)

type User struct {
	ID, Email, PasswordHash, Role string
	CreatedAt, UpdatedAt int64
}

type UserStore interface {
	GetByEmail(string) (*User, error)
	GetByID(string) (*User, error)
	Create(*User) error
}

type AuthHandler struct{ m *auth.JWTManager; store UserStore }

func NewAuthHandler(m *auth.JWTManager, store UserStore) *AuthHandler { return &AuthHandler{m: m, store: store} }

type loginReq struct{ Email, Password string }

func (h *AuthHandler) Login(c *gin.Context) {
	var r loginReq
	if err := c.ShouldBindJSON(&r); err != nil { c.JSON(400, gin.H{"error": err.Error()}); return }
	if h.store == nil { c.JSON(500, gin.H{"error": "no store"}); return }
	u, err := h.store.GetByEmail(r.Email)
	if err != nil { c.JSON(401, gin.H{"error": "invalid"}); return }
	hash := hex.EncodeToString(sha256.New().Sum([]byte(r.Password)))
	if u.PasswordHash != hash { c.JSON(401, gin.H{"error": "invalid"}); return }
	t, _ := h.m.GenerateToken(u.ID, u.Role, 24*time.Hour)
	rt, _ := h.m.GenerateToken(u.ID, u.Role, 7*24*time.Hour)
	c.JSON(200, gin.H{"token": t, "refresh_token": rt, "user_id": u.ID, "role": u.Role})
}

type regReq struct{ Email, Password string }

func (h *AuthHandler) Register(c *gin.Context) {
	var r regReq
	if err := c.ShouldBindJSON(&r); err != nil { c.JSON(400, gin.H{"error": err.Error()}); return }
	if h.store == nil { c.JSON(500, gin.H{"error": "no store"}); return }
	hash := hex.EncodeToString(sha256.New().Sum([]byte(r.Password)))
	u := &User{ID: fmt.Sprintf("usr_%x", hash[:8]), Email: r.Email, PasswordHash: hash, Role: "user", CreatedAt: time.Now().UnixNano()}
	if err := h.store.Create(u); err != nil { c.JSON(500, gin.H{"error": "create failed"}); return }
	c.JSON(201, gin.H{"user_id": u.ID, "email": u.Email})
}

func (h *AuthHandler) AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		a := c.GetHeader("Authorization")
		if a == "" { c.AbortWithStatusJSON(401, gin.H{"error": "no auth"}); return }
		p := strings.SplitN(a, " ", 2)
		if len(p) != 2 || !strings.EqualFold(p[0], "bearer") { c.AbortWithStatusJSON(401, gin.H{"error": "bad format"}); return }
		cl, err := h.m.ValidateToken(p[1])
		if err != nil { c.AbortWithStatusJSON(401, gin.H{"error": "invalid token"}); return }
		c.Set("user_id", cl.UserID); c.Set("role", cl.Role); c.Next()
	}
}