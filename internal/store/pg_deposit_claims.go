package store

import (
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/WkT010/nexa-exchange/internal/api"
	"github.com/WkT010/nexa-exchange/internal/wallet"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

// PGDepositClaimStore persists manual deposit claims (migration 012) and
// implements api.DepositClaimStore. Approval of a claim is fully atomic: the
// status transition, the spot-wallet credit and the type=1 deposit ledger
// entry all commit in one database transaction (mirrors the consistency
// guarantees of PGWalletStore.Settle), so a claim can never be credited
// twice or half-credited.
type PGDepositClaimStore struct{ db *sql.DB }

// NewPGDepositClaimStore creates a PostgreSQL deposit-claim store.
func NewPGDepositClaimStore(db *sql.DB) *PGDepositClaimStore { return &PGDepositClaimStore{db: db} }

const depositClaimSelectCols = `id, user_id, asset, amount, txid,
	COALESCE(screenshot_path,''), status,
	COALESCE(reject_reason,''), COALESCE(reviewer_id,''),
	created_at, COALESCE(reviewed_at,0)`

func scanDepositClaim(row interface{ Scan(...interface{}) error }) (*api.DepositClaim, error) {
	cl := &api.DepositClaim{Amount: new(big.Float)}
	var amountStr string
	err := row.Scan(&cl.ID, &cl.UserID, &cl.Asset, &amountStr, &cl.TxID,
		&cl.ScreenshotPath, &cl.Status, &cl.RejectReason, &cl.ReviewerID,
		&cl.CreatedAt, &cl.ReviewedAt)
	if err != nil {
		return nil, err
	}
	if _, _, err := cl.Amount.Parse(amountStr, 10); err != nil {
		return nil, fmt.Errorf("deposit claim amount %q: %w", amountStr, err)
	}
	return cl, nil
}

// SubmitClaim records a new pending claim. The global UNIQUE(txid) constraint
// guards against double crediting; a violation surfaces as
// api.ErrDepositClaimTxidDuplicate so the handler can answer 409.
func (s *PGDepositClaimStore) SubmitClaim(cl *api.DepositClaim) error {
	if cl == nil {
		return errors.New("nil deposit claim")
	}
	if cl.Amount == nil || cl.Amount.Sign() <= 0 {
		return wallet.ErrNegativeAmount
	}
	_, err := s.db.Exec(
		`INSERT INTO deposit_claims (id, user_id, asset, amount, txid, screenshot_path, status, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		cl.ID, cl.UserID, cl.Asset, cl.Amount.Text('f', 18), cl.TxID,
		nullString(cl.ScreenshotPath), cl.Status, cl.CreatedAt)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return api.ErrDepositClaimTxidDuplicate
		}
		return fmt.Errorf("deposit claim submit: %w", err)
	}
	return nil
}

// GetClaimByID loads one claim by ID (nil when absent).
func (s *PGDepositClaimStore) GetClaimByID(id string) (*api.DepositClaim, error) {
	row := s.db.QueryRow(`SELECT `+depositClaimSelectCols+` FROM deposit_claims WHERE id=$1`, id)
	cl, err := scanDepositClaim(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("deposit claim get: %w", err)
	}
	return cl, nil
}

// ListClaimsByUser returns the user's own claims, newest first.
func (s *PGDepositClaimStore) ListClaimsByUser(userID string) ([]*api.DepositClaim, error) {
	rows, err := s.db.Query(
		`SELECT `+depositClaimSelectCols+` FROM deposit_claims WHERE user_id=$1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("deposit claim list: %w", err)
	}
	defer rows.Close()
	return collectDepositClaims(rows)
}

// ListClaimsForAdmin returns claims filtered by status (newest first, empty
// status = all) enriched with the claimant's uid/email from the users table.
func (s *PGDepositClaimStore) ListClaimsForAdmin(status string, limit, offset int) ([]*api.DepositClaim, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	query := `SELECT c.id, c.user_id, c.asset, c.amount, c.txid,
		COALESCE(c.screenshot_path,''), c.status,
		COALESCE(c.reject_reason,''), COALESCE(c.reviewer_id,''),
		c.created_at, COALESCE(c.reviewed_at,0), COALESCE(u.email,'')
		FROM deposit_claims c LEFT JOIN users u ON u.id = c.user_id`
	var (
		rows *sql.Rows
		err  error
	)
	if status != "" {
		rows, err = s.db.Query(query+` WHERE c.status=$1 ORDER BY c.created_at DESC LIMIT $2 OFFSET $3`,
			status, limit, offset)
	} else {
		rows, err = s.db.Query(query+` ORDER BY c.created_at DESC LIMIT $1 OFFSET $2`, limit, offset)
	}
	if err != nil {
		return nil, fmt.Errorf("deposit claim admin list: %w", err)
	}
	defer rows.Close()
	var out []*api.DepositClaim
	for rows.Next() {
		cl := &api.DepositClaim{Amount: new(big.Float)}
		var amountStr string
		if err := rows.Scan(&cl.ID, &cl.UserID, &cl.Asset, &amountStr, &cl.TxID,
			&cl.ScreenshotPath, &cl.Status, &cl.RejectReason, &cl.ReviewerID,
			&cl.CreatedAt, &cl.ReviewedAt, &cl.Email); err != nil {
			return nil, err
		}
		if _, _, err := cl.Amount.Parse(amountStr, 10); err != nil {
			return nil, fmt.Errorf("deposit claim amount %q: %w", amountStr, err)
		}
		// users.id IS the (scrambled) numeric UID; legacy usr_* ids keep
		// their string form.
		cl.UID = cl.UserID
		out = append(out, cl)
	}
	return out, rows.Err()
}

func collectDepositClaims(rows *sql.Rows) ([]*api.DepositClaim, error) {
	var out []*api.DepositClaim
	for rows.Next() {
		cl, err := scanDepositClaim(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, cl)
	}
	return out, rows.Err()
}

// ReviewClaim atomically transitions a pending claim. action must be
// "approved" or "rejected". Approval credits the user's spot wallet with the
// claimed amount and writes one type=1 (deposit) ledger entry inside the SAME
// database transaction as the status update — the claim can never be
// credited without being marked approved, and never twice (the status guard
// `WHERE status='pending'` decides the race via RowsAffected). Rejection only
// records the reason.
func (s *PGDepositClaimStore) ReviewClaim(id, reviewerID, action, reason string) (*api.DepositClaim, error) {
	if action != "approved" && action != "rejected" {
		return nil, fmt.Errorf("invalid review action %q", action)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	cl, err := scanDepositClaim(tx.QueryRow(
		`SELECT `+depositClaimSelectCols+` FROM deposit_claims WHERE id=$1`, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("deposit claim %s not found", id)
		}
		return nil, fmt.Errorf("deposit claim get: %w", err)
	}

	now := time.Now().UnixNano()
	res, err := tx.Exec(
		`UPDATE deposit_claims SET status=$2, reject_reason=$3, reviewer_id=$4, reviewed_at=$5
		 WHERE id=$1 AND status='pending'`,
		id, action, reason, reviewerID, now)
	if err != nil {
		return nil, fmt.Errorf("deposit claim review: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, errors.New("claim is not pending review (already reviewed)")
	}

	if action == "approved" {
		if err := creditSpotWalletTx(tx, cl); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	cl.Status = action
	cl.RejectReason = reason
	cl.ReviewerID = reviewerID
	cl.ReviewedAt = now
	return cl, nil
}

// creditSpotWalletTx credits the claim amount to the user's spot wallet and
// records the type=1 deposit ledger entry inside the caller's transaction.
// It replicates the exact ledger semantics of PGWalletStore.Settle /
// wallet.Service.Deposit (wallet upsert, balance delta, ledger row with the
// resolved wallet_id) so the accounting stays consistent with every other
// credit path.
func creditSpotWalletTx(tx *sql.Tx, cl *api.DepositClaim) error {
	now := time.Now().UnixNano()
	amtStr := cl.Amount.Text('f', 18)
	wid := "wal_" + nowText() + randSuffix()
	if _, err := tx.Exec(
		`INSERT INTO wallets (id,user_id,asset,balance,locked,created_at,updated_at,account_type)
		 VALUES($1,$2,$3,0,0,$4,$4,$5)
		 ON CONFLICT (user_id, asset, account_type) DO NOTHING`,
		wid, cl.UserID, cl.Asset, now, wallet.AccountSpot); err != nil {
		return fmt.Errorf("upsert spot wallet: %w", err)
	}
	if _, err := tx.Exec(
		`UPDATE wallets SET balance = balance + $1, updated_at = $2
		 WHERE user_id=$3 AND asset=$4 AND account_type=$5`,
		amtStr, now, cl.UserID, cl.Asset, wallet.AccountSpot); err != nil {
		return fmt.Errorf("credit spot wallet: %w", err)
	}
	var walletID string
	if err := tx.QueryRow(
		`SELECT id FROM wallets WHERE user_id=$1 AND asset=$2 AND account_type=$3`,
		cl.UserID, cl.Asset, wallet.AccountSpot).Scan(&walletID); err != nil {
		return fmt.Errorf("resolve spot wallet: %w", err)
	}
	if _, err := tx.Exec(
		`INSERT INTO transactions (id,user_id,wallet_id,type,asset,amount,fee,status,tx_hash,confirmations,created_at)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		"dep_"+uuid.NewString(), cl.UserID, walletID,
		wallet.Deposit, cl.Asset, amtStr, "0",
		wallet.Completed, cl.TxID, 0, now); err != nil {
		return fmt.Errorf("save deposit ledger: %w", err)
	}
	return nil
}

// nullString maps an empty Go string to SQL NULL for nullable TEXT columns.
func nullString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
