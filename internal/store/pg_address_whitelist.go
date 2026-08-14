package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/WkT010/nexa-exchange/internal/wallet"
)

// PGAddressWhitelistStore persists the withdrawal address whitelist
// (migration 015). It implements wallet.AddressWhitelistStore. Addresses are
// stored normalised (lower-case) for uniqueness and matching; the original
// spelling is kept in address_raw and returned for display.
type PGAddressWhitelistStore struct{ db *sql.DB }

// NewPGAddressWhitelistStore creates a PostgreSQL address-whitelist store.
func NewPGAddressWhitelistStore(db *sql.DB) *PGAddressWhitelistStore {
	return &PGAddressWhitelistStore{db: db}
}

// ListWhitelistAddresses returns a user's whitelisted addresses, optionally
// filtered by asset (empty asset returns all of the user's entries).
func (s *PGAddressWhitelistStore) ListWhitelistAddresses(userID, asset string) ([]wallet.AddressBookEntry, error) {
	query := `SELECT id, user_id, asset, address_raw, COALESCE(label,''), created_at
		FROM address_whitelist WHERE user_id=$1`
	args := []interface{}{userID}
	if asset != "" {
		query += ` AND asset=$2`
		args = append(args, strings.ToUpper(asset))
	}
	query += ` ORDER BY created_at ASC`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("address whitelist list: %w", err)
	}
	defer rows.Close()
	out := make([]wallet.AddressBookEntry, 0, 8)
	for rows.Next() {
		var e wallet.AddressBookEntry
		if err := rows.Scan(&e.ID, &e.UserID, &e.Asset, &e.Address, &e.Label, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("address whitelist scan: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// AddWhitelistAddress inserts one entry idempotently: a duplicate
// (user_id, asset, normalised address) is reported as (false, nil) instead
// of an error so the API can answer with a friendly message.
func (s *PGAddressWhitelistStore) AddWhitelistAddress(entry wallet.AddressBookEntry, createdBy string) (bool, error) {
	res, err := s.db.Exec(
		`INSERT INTO address_whitelist (id, user_id, asset, address, address_raw, label, created_by, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		 ON CONFLICT (user_id, asset, address) DO NOTHING`,
		entry.ID, entry.UserID, strings.ToUpper(entry.Asset),
		strings.ToLower(entry.Address), entry.Address, entry.Label, createdBy, entry.CreatedAt)
	if err != nil {
		return false, fmt.Errorf("address whitelist add: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// RemoveWhitelistAddress deletes one entry; returns false when the address
// was not whitelisted. Already-submitted withdrawals are untouched.
func (s *PGAddressWhitelistStore) RemoveWhitelistAddress(userID, asset, address string) (bool, error) {
	res, err := s.db.Exec(
		`DELETE FROM address_whitelist WHERE user_id=$1 AND asset=$2 AND address=$3`,
		userID, strings.ToUpper(asset), strings.ToLower(strings.TrimSpace(address)))
	if err != nil {
		return false, fmt.Errorf("address whitelist remove: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ContainsWhitelistedAddress reports whether the (normalised) address is in
// the user's whitelist for the asset.
func (s *PGAddressWhitelistStore) ContainsWhitelistedAddress(userID, asset, address string) (bool, error) {
	var one int
	err := s.db.QueryRow(
		`SELECT 1 FROM address_whitelist WHERE user_id=$1 AND asset=$2 AND address=$3`,
		userID, strings.ToUpper(asset), strings.ToLower(strings.TrimSpace(address))).Scan(&one)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("address whitelist lookup: %w", err)
	}
	return true, nil
}
