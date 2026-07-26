package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/WkT010/nexa-exchange/internal/auth"
)

// PGAPIKeyStore persists API keys in PostgreSQL. Secrets are stored as plaintext
// here for simplicity; in a production deployment the secret column should hold
// an Argon2id hash (like the users.password_hash column) and Validate should
// hash-verify. The constant-time compare in APIKey.Validate already protects
// against timing leaks at the API layer.
type PGAPIKeyStore struct{ db *sql.DB }

func NewPGAPIKeyStore(db *sql.DB) *PGAPIKeyStore { return &PGAPIKeyStore{db: db} }

func (s *PGAPIKeyStore) Save(k *auth.APIKey) error {
	var expires interface{}
	if !k.ExpiresAt.IsZero() {
		expires = k.ExpiresAt.UnixNano()
	}
	_, err := s.db.Exec(
		`INSERT INTO api_keys (key_id,secret,user_id,permissions,active,created_at,expires_at)
		 VALUES($1,$2,$3,$4,$5,$6,$7)
		 ON CONFLICT (key_id) DO UPDATE SET secret=EXCLUDED.secret, permissions=EXCLUDED.permissions, active=EXCLUDED.active, expires_at=EXCLUDED.expires_at`,
		k.KeyID, k.Secret, k.UserID, strings.Join(k.Permissions, ","), k.Active,
		k.CreatedAt.UnixNano(), expires)
	return err
}

func (s *PGAPIKeyStore) Get(keyID string) (*auth.APIKey, error) {
	k := &auth.APIKey{}
	var perms string
	var created int64
	var expires sql.NullInt64
	err := s.db.QueryRow(
		`SELECT key_id,secret,user_id,permissions,active,created_at,expires_at FROM api_keys WHERE key_id=$1`,
		keyID).Scan(&k.KeyID, &k.Secret, &k.UserID, &perms, &k.Active, &created, &expires)
	if err != nil {
		return nil, fmt.Errorf("get api key: %w", err)
	}
	if perms != "" {
		k.Permissions = strings.Split(perms, ",")
	}
	k.CreatedAt = time.Unix(0, created)
	if expires.Valid {
		k.ExpiresAt = time.Unix(0, expires.Int64)
	}
	return k, nil
}

func (s *PGAPIKeyStore) Revoke(keyID string) error {
	_, err := s.db.Exec(`UPDATE api_keys SET active=false WHERE key_id=$1`, keyID)
	return err
}

func (s *PGAPIKeyStore) ListByUser(userID string) ([]*auth.APIKey, error) {
	rows, err := s.db.Query(
		`SELECT key_id,secret,user_id,permissions,active,created_at,expires_at FROM api_keys WHERE user_id=$1 ORDER BY created_at DESC`,
		userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []*auth.APIKey
	for rows.Next() {
		k := &auth.APIKey{}
		var perms string
		var created int64
		var expires sql.NullInt64
		if err := rows.Scan(&k.KeyID, &k.Secret, &k.UserID, &perms, &k.Active, &created, &expires); err != nil {
			return nil, err
		}
		if perms != "" {
			k.Permissions = strings.Split(perms, ",")
		}
		k.CreatedAt = time.Unix(0, created)
		if expires.Valid {
			k.ExpiresAt = time.Unix(0, expires.Int64)
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}
