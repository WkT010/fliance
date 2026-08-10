package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	UserID    string `json:"user_id"`
	Role      string `json:"role"`
	TokenType string `json:"token_type,omitempty"`
	jwt.RegisteredClaims
}

// refreshLifetimeThreshold distinguishes access from refresh tokens when both
// are issued through GenerateToken (the API layer signs 24h access and 7-day
// refresh tokens with the same method). Tokens with a longer lifetime are
// stamped as refresh tokens so the API layer's parseExtraClaims checks
// ("token_type" claim) can reject cross-usage. Tokens issued without a type
// claim (legacy) remain accepted everywhere — validation itself never
// enforces the type.
const refreshLifetimeThreshold = 48 * time.Hour

type JWTManager struct {
	secret []byte
	issuer string
}

func NewJWTManager(secret string, issuer string) *JWTManager {
	return &JWTManager{secret: []byte(secret), issuer: issuer}
}

// newJTI returns a 16-byte hex token identifier (jti claim) sourced from
// crypto/rand. On the (practically impossible) read failure it falls back to
// a timestamp-derived value so signing never fails because of jti generation.
func newJTI() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func (m *JWTManager) GenerateToken(userID, role string, dur time.Duration) (string, error) {
	tokenType := "access"
	if dur > refreshLifetimeThreshold {
		tokenType = "refresh"
	}
	return m.generateTypedToken(userID, role, dur, tokenType)
}

// GenerateAccessToken explicitly signs an access token regardless of lifetime.
func (m *JWTManager) GenerateAccessToken(userID, role string, dur time.Duration) (string, error) {
	return m.generateTypedToken(userID, role, dur, "access")
}

// GenerateRefreshToken explicitly signs a refresh token regardless of lifetime.
func (m *JWTManager) GenerateRefreshToken(userID, role string, dur time.Duration) (string, error) {
	return m.generateTypedToken(userID, role, dur, "refresh")
}

func (m *JWTManager) generateTypedToken(userID, role string, dur time.Duration, tokenType string) (string, error) {
	now := time.Now()
	c := &Claims{
		UserID:    userID,
		Role:      role,
		TokenType: tokenType,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: m.issuer, IssuedAt: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(dur)), Subject: userID,
			ID: newJTI(),
		},
	}
	t, err := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(m.secret)
	if err != nil {
		return "", fmt.Errorf("sign: %w", err)
	}
	return t, nil
}

func (m *JWTManager) ValidateToken(tokenStr string) (*Claims, error) {
	t, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected alg: %v", t.Header["alg"])
		}
		return m.secret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("validate: %w", err)
	}
	c, ok := t.Claims.(*Claims)
	if !ok || !t.Valid {
		return nil, fmt.Errorf("invalid claims")
	}
	return c, nil
}
