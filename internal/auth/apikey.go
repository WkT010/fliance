package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

var ErrAPIKeyNotFound = errors.New("api key not found")

type APIKey struct {
	KeyID, Secret, UserID string
	Permissions []string
	CreatedAt, ExpiresAt time.Time
	Active bool
}

type APIKeyStore interface {
	Save(*APIKey) error
	Get(string) (*APIKey, error)
	Revoke(string) error
	ListByUser(string) ([]*APIKey, error)
}

func GenerateAPIKey() (keyID, secret string, err error) {
	kb := make([]byte, 32)
	if _, e := rand.Read(kb); e != nil { return "", "", fmt.Errorf("key: %w", e) }
	sb := make([]byte, 64)
	if _, e := rand.Read(sb); e != nil { return "", "", fmt.Errorf("secret: %w", e) }
	return hex.EncodeToString(kb), hex.EncodeToString(sb), nil
}

func (k *APIKey) Validate(secret string) bool {
	return subtle.ConstantTimeCompare([]byte(k.Secret), []byte(secret)) == 1
}
