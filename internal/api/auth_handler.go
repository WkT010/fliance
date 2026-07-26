package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
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
}

type AuthHandler struct {
	m     *auth.JWTManager
	store UserStore
}

func NewAuthHandler(m *auth.JWTManager, store UserStore) *AuthHandler {
	return &AuthHandler{m: m, store: store}
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
	u, err := h.store.GetByEmail(r.Email)
	if err != nil {
		c.JSON(401, gin.H{"error": "invalid email or password"})
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(r.Password)); err != nil {
		c.JSON(401, gin.H{"error": "invalid email or password"})
		return
	}
	t, _ := h.m.GenerateToken(u.ID, u.Role, 24*time.Hour)
	rt, _ := h.m.GenerateToken(u.ID, u.Role, 7*24*time.Hour)
	c.JSON(200, gin.H{"token": t, "refresh_token": rt, "user_id": u.ID, "role": u.Role, "expires_in": 86400})
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
	u := &User{ID: uid, Email: r.Email, PasswordHash: string(hash), Role: "user", CreatedAt: time.Now().UnixNano(), UpdatedAt: time.Now().UnixNano()}
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

func (h *AuthHandler) AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		a := c.GetHeader("Authorization")
		if a == "" {
			c.AbortWithStatusJSON(401, gin.H{"error": "authorization header required"})
			return
		}
		p := strings.SplitN(a, " ", 2)
		if len(p) != 2 || !strings.EqualFold(p[0], "bearer") {
			c.AbortWithStatusJSON(401, gin.H{"error": "bearer token required"})
			return
		}
		cl, err := h.m.ValidateToken(p[1])
		if err != nil {
			c.AbortWithStatusJSON(401, gin.H{"error": "invalid or expired token"})
			return
		}
		c.Set("user_id", cl.UserID)
		c.Set("role", cl.Role)
		c.Next()
	}
}
