package store

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/lib/pq"

	"github.com/WkT010/nexa-exchange/internal/auth"
)

// PGAPIKeyStore persists API keys in PostgreSQL. Secrets are stored as
// Argon2id hashes in the secret_hash column (migration 007). The legacy
// plaintext secret column is kept only for zero-downtime compatibility: rows
// created before the hashing rollout are verified against the plaintext value
// and lazily upgraded to a hash on first successful validation.
type PGAPIKeyStore struct {
	db *sql.DB

	hashColOnce sync.Once
	hashColOK   bool // secret_hash column is present
}

func NewPGAPIKeyStore(db *sql.DB) *PGAPIKeyStore { return &PGAPIKeyStore{db: db} }

// pgUndefinedColumn is the PostgreSQL error code for "column does not exist".
// It lets us run against databases where migration 007 has not been applied
// yet (zero-downtime rollout).
const pgUndefinedColumn = pq.ErrorCode("42703")

func isUndefinedColumn(err error) bool {
	if pqErr, ok := err.(*pq.Error); ok {
		return pqErr.Code == pgUndefinedColumn
	}
	return false
}

// hashSecretColumn probes once whether the secret_hash column exists.
func (s *PGAPIKeyStore) hasHashColumn() bool {
	s.hashColOnce.Do(func() {
		var one int
		err := s.db.QueryRow(
			`SELECT 1 FROM information_schema.columns WHERE table_name='api_keys' AND column_name='secret_hash'`).Scan(&one)
		s.hashColOK = err == nil
	})
	return s.hashColOK
}

// Save persists the key with the secret stored as an Argon2id hash. The
// plaintext secret is never written to the database.
func (s *PGAPIKeyStore) Save(k *auth.APIKey) error {
	hashed, err := auth.HashSecret(k.Secret)
	if err != nil {
		return fmt.Errorf("hash api secret: %w", err)
	}
	var expires interface{}
	if !k.ExpiresAt.IsZero() {
		expires = k.ExpiresAt.UnixNano()
	}
	if s.hasHashColumn() {
		_, err := s.db.Exec(
			`INSERT INTO api_keys (key_id,secret,secret_hash,user_id,permissions,active,created_at,expires_at)
			 VALUES($1,'',$2,$3,$4,$5,$6,$7)
			 ON CONFLICT (key_id) DO UPDATE SET secret=EXCLUDED.secret, secret_hash=EXCLUDED.secret_hash,
			   permissions=EXCLUDED.permissions, active=EXCLUDED.active, expires_at=EXCLUDED.expires_at`,
			k.KeyID, hashed, k.UserID, strings.Join(k.Permissions, ","), k.Active,
			k.CreatedAt.UnixNano(), expires)
		if err != nil && !isUndefinedColumn(err) {
			return err
		}
		if err == nil {
			return nil
		}
	}
	// Fallback for databases without the secret_hash column yet: store the
	// hash in the legacy column (still never plaintext).
	_, err = s.db.Exec(
		`INSERT INTO api_keys (key_id,secret,user_id,permissions,active,created_at,expires_at)
		 VALUES($1,$2,$3,$4,$5,$6,$7)
		 ON CONFLICT (key_id) DO UPDATE SET secret=EXCLUDED.secret, permissions=EXCLUDED.permissions, active=EXCLUDED.active, expires_at=EXCLUDED.expires_at`,
		k.KeyID, hashed, k.UserID, strings.Join(k.Permissions, ","), k.Active,
		k.CreatedAt.UnixNano(), expires)
	return err
}

// scanAPIKey builds an APIKey from a query row. storedHash is the secret_hash
// column value (empty when the column is absent); the effective stored secret
// prefers the hash and falls back to the legacy column for pre-migration rows.
func (s *PGAPIKeyStore) scanAPIKey(keyID, legacySecret, storedHash, userID, perms string, active bool, created int64, expires sql.NullInt64) *auth.APIKey {
	k := &auth.APIKey{
		KeyID:  keyID,
		Secret: storedHash,
		UserID: userID,
		Active: active,
	}
	if k.Secret == "" {
		k.Secret = legacySecret // legacy plaintext or legacy-column hash
	}
	if perms != "" {
		k.Permissions = strings.Split(perms, ",")
	}
	k.CreatedAt = time.Unix(0, created)
	if expires.Valid {
		k.ExpiresAt = time.Unix(0, expires.Int64)
	}
	k.BindSecretUpgrader(func(hashed string) error { return s.upgradeSecret(keyID, hashed) })
	return k
}

func (s *PGAPIKeyStore) Get(keyID string) (*auth.APIKey, error) {
	var keyIDOut, legacySecret, storedHash, userID, perms string
	var active bool
	var created int64
	var expires sql.NullInt64
	if s.hasHashColumn() {
		err := s.db.QueryRow(
			`SELECT key_id,secret,COALESCE(secret_hash,''),user_id,permissions,active,created_at,expires_at FROM api_keys WHERE key_id=$1`,
			keyID).Scan(&keyIDOut, &legacySecret, &storedHash, &userID, &perms, &active, &created, &expires)
		if err == nil {
			return s.scanAPIKey(keyIDOut, legacySecret, storedHash, userID, perms, active, created, expires), nil
		}
		if err == sql.ErrNoRows {
			return nil, auth.ErrAPIKeyNotFound
		}
		if !isUndefinedColumn(err) {
			return nil, fmt.Errorf("get api key: %w", err)
		}
	}
	err := s.db.QueryRow(
		`SELECT key_id,secret,user_id,permissions,active,created_at,expires_at FROM api_keys WHERE key_id=$1`,
		keyID).Scan(&keyIDOut, &legacySecret, &userID, &perms, &active, &created, &expires)
	if err != nil {
		return nil, fmt.Errorf("get api key: %w", err)
	}
	return s.scanAPIKey(keyIDOut, legacySecret, "", userID, perms, active, created, expires), nil
}

// upgradeSecret lazily migrates a legacy plaintext secret to its Argon2id
// hash after a successful validation. Called best-effort from APIKey.Validate.
func (s *PGAPIKeyStore) upgradeSecret(keyID, hashed string) error {
	if s.hasHashColumn() {
		_, err := s.db.Exec(
			`UPDATE api_keys SET secret_hash=$2, secret='' WHERE key_id=$1`, keyID, hashed)
		if err == nil {
			return nil
		}
		if !isUndefinedColumn(err) {
			return err
		}
	}
	_, err := s.db.Exec(`UPDATE api_keys SET secret=$2 WHERE key_id=$1`, keyID, hashed)
	return err
}

func (s *PGAPIKeyStore) Revoke(keyID string) error {
	_, err := s.db.Exec(`UPDATE api_keys SET active=false WHERE key_id=$1`, keyID)
	return err
}

func (s *PGAPIKeyStore) ListByUser(userID string) ([]*auth.APIKey, error) {
	query := `SELECT key_id,secret,user_id,permissions,active,created_at,expires_at FROM api_keys WHERE user_id=$1 ORDER BY created_at DESC`
	withHash := s.hasHashColumn()
	if withHash {
		query = `SELECT key_id,secret,COALESCE(secret_hash,''),user_id,permissions,active,created_at,expires_at FROM api_keys WHERE user_id=$1 ORDER BY created_at DESC`
	}
	rows, err := s.db.Query(query, userID)
	if err != nil {
		if withHash && isUndefinedColumn(err) {
			rows, err = s.db.Query(
				`SELECT key_id,secret,user_id,permissions,active,created_at,expires_at FROM api_keys WHERE user_id=$1 ORDER BY created_at DESC`,
				userID)
			if err != nil {
				return nil, err
			}
			withHash = false
		} else {
			return nil, err
		}
	}
	defer rows.Close()
	var keys []*auth.APIKey
	for rows.Next() {
		var keyID, legacySecret, storedHash, uid, perms string
		var active bool
		var created int64
		var expires sql.NullInt64
		if withHash {
			if err := rows.Scan(&keyID, &legacySecret, &storedHash, &uid, &perms, &active, &created, &expires); err != nil {
				return nil, err
			}
		} else {
			if err := rows.Scan(&keyID, &legacySecret, &uid, &perms, &active, &created, &expires); err != nil {
				return nil, err
			}
		}
		keys = append(keys, s.scanAPIKey(keyID, legacySecret, storedHash, uid, perms, active, created, expires))
	}
	return keys, rows.Err()
}
