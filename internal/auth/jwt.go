package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

type JWTManager struct {
	secret []byte
	issuer string
}

func NewJWTManager(secret string, issuer string) *JWTManager {
	return &JWTManager{secret: []byte(secret), issuer: issuer}
}

func (m *JWTManager) GenerateToken(userID, role string, dur time.Duration) (string, error) {
	now := time.Now()
	c := &Claims{
		UserID: userID,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: m.issuer, IssuedAt: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(dur)), Subject: userID,
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
