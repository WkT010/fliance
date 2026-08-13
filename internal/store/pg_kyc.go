package store

import (
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/WkT010/nexa-exchange/internal/api"
)

// PGKycStore persists KYC submissions and platform withdrawal limits
// (migration 010). It implements api.KycStore and wallet.PlatformLimitLoader.
type PGKycStore struct{ db *sql.DB }

// NewPGKycStore creates a PostgreSQL KYC store.
func NewPGKycStore(db *sql.DB) *PGKycStore { return &PGKycStore{db: db} }

const kycSelectCols = `id, user_id, COALESCE(full_name,''), COALESCE(id_number,''),
	COALESCE(doc_front,''), COALESCE(doc_back,''), status,
	COALESCE(reject_reason,''), COALESCE(reviewer_id,''),
	COALESCE(submitted_at,0), COALESCE(reviewed_at,0)`

func scanKycSubmission(row interface{ Scan(...interface{}) error }) (*api.KycSubmission, error) {
	s := &api.KycSubmission{}
	err := row.Scan(&s.ID, &s.UserID, &s.FullName, &s.IDNumber, &s.DocFront, &s.DocBack,
		&s.Status, &s.RejectReason, &s.ReviewerID, &s.SubmittedAt, &s.ReviewedAt)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// Submit records a new pending KYC submission.
func (s *PGKycStore) Submit(sub *api.KycSubmission) error {
	if sub == nil {
		return errors.New("nil submission")
	}
	_, err := s.db.Exec(
		`INSERT INTO kyc_submissions (id, user_id, full_name, id_number, doc_front, doc_back, status, submitted_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		sub.ID, sub.UserID, sub.FullName, sub.IDNumber, sub.DocFront, sub.DocBack, sub.Status, sub.SubmittedAt)
	if err != nil {
		return fmt.Errorf("kyc submit: %w", err)
	}
	return nil
}

// GetLatestByUser returns the user's most recent submission, or nil when the
// user has never submitted.
func (s *PGKycStore) GetLatestByUser(userID string) (*api.KycSubmission, error) {
	row := s.db.QueryRow(
		`SELECT `+kycSelectCols+` FROM kyc_submissions WHERE user_id=$1 ORDER BY submitted_at DESC LIMIT 1`, userID)
	sub, err := scanKycSubmission(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("kyc latest: %w", err)
	}
	return sub, nil
}

// ListByStatus returns submissions filtered by status (newest first). An
// empty status returns all submissions.
func (s *PGKycStore) ListByStatus(status string, limit, offset int) ([]*api.KycSubmission, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	var (
		rows *sql.Rows
		err  error
	)
	if status != "" {
		rows, err = s.db.Query(
			`SELECT `+kycSelectCols+` FROM kyc_submissions WHERE status=$1 ORDER BY submitted_at DESC LIMIT $2 OFFSET $3`,
			status, limit, offset)
	} else {
		rows, err = s.db.Query(
			`SELECT `+kycSelectCols+` FROM kyc_submissions ORDER BY submitted_at DESC LIMIT $1 OFFSET $2`,
			limit, offset)
	}
	if err != nil {
		return nil, fmt.Errorf("kyc list: %w", err)
	}
	defer rows.Close()
	var out []*api.KycSubmission
	for rows.Next() {
		sub, err := scanKycSubmission(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}

// Review atomically transitions a pending submission to approved/rejected.
// The status change is guarded by `WHERE status='pending'` so two reviewers
// racing on the same submission cannot both succeed (RowsAffected decides).
// Approval also raises the user's kyc_level to 1. The reviewed submission is
// returned so callers can invalidate caches for the affected user.
func (s *PGKycStore) Review(id, reviewerID, action, reason string) (*api.KycSubmission, error) {
	if action != "approved" && action != "rejected" {
		return nil, fmt.Errorf("invalid review action %q", action)
	}
	sub, err := s.GetByID(id)
	if err != nil {
		return nil, err
	}
	if sub == nil {
		return nil, fmt.Errorf("kyc submission %s not found", id)
	}
	now := time.Now().UnixNano()
	res, err := s.db.Exec(
		`UPDATE kyc_submissions SET status=$2, reject_reason=$3, reviewer_id=$4, reviewed_at=$5
		 WHERE id=$1 AND status='pending'`,
		id, action, reason, reviewerID, now)
	if err != nil {
		return nil, fmt.Errorf("kyc review: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, errors.New("submission is not pending review (already reviewed)")
	}
	if action == "approved" {
		if _, err := s.db.Exec(
			`UPDATE users SET kyc_level=1, updated_at=$2 WHERE id=$1`, sub.UserID, now); err != nil {
			return nil, fmt.Errorf("raise kyc_level: %w", err)
		}
	}
	sub.Status = action
	sub.RejectReason = reason
	sub.ReviewerID = reviewerID
	sub.ReviewedAt = now
	return sub, nil
}

// GetByID loads one submission by ID (nil when absent).
func (s *PGKycStore) GetByID(id string) (*api.KycSubmission, error) {
	row := s.db.QueryRow(`SELECT `+kycSelectCols+` FROM kyc_submissions WHERE id=$1`, id)
	sub, err := scanKycSubmission(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("kyc get: %w", err)
	}
	return sub, nil
}

// LoadPlatformLimits reads the KYC-tier daily withdrawal limits
// (wallet.PlatformLimitLoader). Missing table or rows degrade to an empty
// map; callers apply their own fallback.
func (s *PGKycStore) LoadPlatformLimits() (map[int]*big.Float, error) {
	rows, err := s.db.Query(`SELECT kyc_level, daily_limit_usdt FROM platform_limits`)
	if err != nil {
		return nil, fmt.Errorf("platform limits: %w", err)
	}
	defer rows.Close()
	out := make(map[int]*big.Float)
	for rows.Next() {
		var level int
		var limitStr string
		if err := rows.Scan(&level, &limitStr); err != nil {
			return nil, err
		}
		f, ok := new(big.Float).SetString(limitStr)
		if !ok || f.Sign() <= 0 {
			continue
		}
		out[level] = f
	}
	return out, rows.Err()
}
