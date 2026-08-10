package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

var ErrAPIKeyNotFound = errors.New("api key not found")

type APIKey struct {
	KeyID       string
	Secret      string
	UserID      string
	Permissions []string
	CreatedAt   time.Time
	ExpiresAt   time.Time
	Active      bool

	// upgrade is an optional hook bound by the backing store. When a legacy
	// plaintext secret is successfully verified, Validate uses it to lazily
	// persist the Argon2id hash, upgrading the record without downtime.
	upgrade func(hashedSecret string) error
}

// BindSecretUpgrader attaches a store-provided callback that persists a newly
// computed secret hash during lazy migration from plaintext secrets.
func (k *APIKey) BindSecretUpgrader(fn func(hashedSecret string) error) { k.upgrade = fn }

type APIKeyStore interface {
	Save(*APIKey) error
	Get(string) (*APIKey, error)
	Revoke(string) error
	ListByUser(string) ([]*APIKey, error)
}

func GenerateAPIKey() (keyID, secret string, err error) {
	kb := make([]byte, 32)
	if _, e := rand.Read(kb); e != nil {
		return "", "", fmt.Errorf("key: %w", e)
	}
	sb := make([]byte, 64)
	if _, e := rand.Read(sb); e != nil {
		return "", "", fmt.Errorf("secret: %w", e)
	}
	return hex.EncodeToString(kb), hex.EncodeToString(sb), nil
}

// Argon2id parameters for API secret hashing.
const (
	argonTime    = 1
	argonMemory  = 64 * 1024 // 64 MB
	argonThreads = 4
	argonKeyLen  = 32
	argonSaltLen = 16
)

const argonPrefix = "$argon2id$"

// IsHashedSecret reports whether the stored secret is an Argon2id hash
// ($argon2id$v=19$...) rather than a legacy plaintext value.
func IsHashedSecret(stored string) bool {
	return strings.HasPrefix(stored, argonPrefix)
}

// HashSecret hashes an API secret with Argon2id and a random 16-byte salt,
// encoded as $argon2id$v=19$m=65536,t=1,p=4$<salt-b64>$<hash-b64>.
func HashSecret(secret string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("salt: %w", err)
	}
	h := argon2.IDKey([]byte(secret), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(h)), nil
}

// parseArgon2 extracts the salt, expected hash and parameters from an encoded
// Argon2id string.
func parseArgon2(encoded string) (salt, hash []byte, mem, iter uint32, par uint8, err error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return nil, nil, 0, 0, 0, errors.New("malformed argon2 hash")
	}
	if parts[2] != "v=19" {
		return nil, nil, 0, 0, 0, errors.New("unsupported argon2 version")
	}
	if _, err = fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &iter, &par); err != nil {
		return nil, nil, 0, 0, 0, fmt.Errorf("argon2 params: %w", err)
	}
	if salt, err = base64.RawStdEncoding.DecodeString(parts[4]); err != nil {
		return nil, nil, 0, 0, 0, fmt.Errorf("argon2 salt: %w", err)
	}
	if hash, err = base64.RawStdEncoding.DecodeString(parts[5]); err != nil {
		return nil, nil, 0, 0, 0, fmt.Errorf("argon2 hash: %w", err)
	}
	return salt, hash, mem, iter, par, nil
}

// VerifySecret checks a candidate secret against the stored value. Hashed
// secrets ($argon2id$...) are re-derived with the stored salt and compared in
// constant time; legacy plaintext values are compared directly in constant
// time (preserving the previous timing-attack protection).
func VerifySecret(secret, stored string) bool {
	if IsHashedSecret(stored) {
		salt, want, mem, iter, par, err := parseArgon2(stored)
		if err != nil {
			return false
		}
		calc := argon2.IDKey([]byte(secret), salt, iter, mem, par, uint32(len(want)))
		return subtle.ConstantTimeCompare(calc, want) == 1
	}
	return subtle.ConstantTimeCompare([]byte(stored), []byte(secret)) == 1
}

// Validate verifies the supplied secret against the key's stored secret
// (hashed or legacy plaintext). On a successful legacy plaintext match it
// lazily upgrades the stored secret to an Argon2id hash when the store bound
// an upgrader; upgrade failures are best-effort and never reject the request.
func (k *APIKey) Validate(secret string) bool {
	if !VerifySecret(secret, k.Secret) {
		return false
	}
	if !IsHashedSecret(k.Secret) && k.upgrade != nil {
		if hashed, err := HashSecret(secret); err == nil {
			if err := k.upgrade(hashed); err == nil {
				k.Secret = hashed
			}
		}
	}
	return true
}
