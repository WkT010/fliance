package store

import (
	"database/sql"
	"errors"
	"math/big"
	"strings"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"

	"github.com/WkT010/nexa-exchange/internal/api"
)

// newClaimMock returns a PGDepositClaimStore backed by sqlmock. Query
// expectations use short regex-safe fragments (the real SQL spans multiple
// lines).
func newClaimMock(t *testing.T) (*PGDepositClaimStore, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewPGDepositClaimStore(db), mock
}

func pendingClaim() *api.DepositClaim {
	return &api.DepositClaim{
		ID: "dep_test", UserID: "usr_alice", Asset: "USDT",
		Amount: big.NewFloat(1000), TxID: "0xabc123", Status: "pending", CreatedAt: 1700000000000000000,
	}
}

// TestPGDepositClaimSubmitDuplicateTxid verifies the global UNIQUE(txid)
// violation is surfaced as api.ErrDepositClaimTxidDuplicate (the handler maps
// it to 409), protecting against double crediting of one on-chain tx.
func TestPGDepositClaimSubmitDuplicateTxid(t *testing.T) {
	s, mock := newClaimMock(t)

	mock.ExpectExec("INSERT INTO deposit_claims").
		WillReturnError(&pq.Error{Code: "23505", Message: `duplicate key value violates unique constraint "deposit_claims_txid_key"`})

	err := s.SubmitClaim(pendingClaim())
	if err == nil || !errors.Is(err, api.ErrDepositClaimTxidDuplicate) {
		t.Fatalf("err = %v, want ErrDepositClaimTxidDuplicate", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestPGDepositClaimSubmitOK covers the happy path insert.
func TestPGDepositClaimSubmitOK(t *testing.T) {
	s, mock := newClaimMock(t)
	mock.ExpectExec("INSERT INTO deposit_claims").WillReturnResult(sqlmock.NewResult(0, 1))
	if err := s.SubmitClaim(pendingClaim()); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// claimRow builds the result row matching depositClaimSelectCols.
func claimRow(status string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "user_id", "asset", "amount", "txid",
		"screenshot_path", "status", "reject_reason", "reviewer_id",
		"created_at", "reviewed_at",
	}).AddRow("dep_test", "usr_alice", "USDT", "1000.000000000000000000", "0xabc123",
		"", status, "", "", int64(1700000000000000000), int64(0))
}

// TestPGDepositClaimReviewApprove asserts the full approval transaction: the
// guarded status UPDATE, the spot-wallet upsert + credit, and the type=1
// deposit ledger entry all run before the commit.
func TestPGDepositClaimReviewApprove(t *testing.T) {
	s, mock := newClaimMock(t)

	mock.ExpectBegin()
	mock.ExpectQuery("FROM deposit_claims WHERE id").WillReturnRows(claimRow("pending"))
	mock.ExpectExec("UPDATE deposit_claims SET status").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO wallets").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE wallets SET balance").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT id FROM wallets").WillReturnRows(
		sqlmock.NewRows([]string{"id"}).AddRow("wal_spot_1"))
	mock.ExpectExec("INSERT INTO transactions").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	cl, err := s.ReviewClaim("dep_test", "usr_admin", "approved", "")
	if err != nil {
		t.Fatalf("review approve: %v", err)
	}
	if cl.Status != "approved" || cl.ReviewerID != "usr_admin" || cl.ReviewedAt == 0 {
		t.Errorf("reviewed claim = %+v, want approved by usr_admin with ReviewedAt set", cl)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestPGDepositClaimReviewRejectRecordsReason verifies the reject path skips
// the wallet credit entirely (no wallet/ledger expectations are queued, so
// sqlmock fails the test if any crediting SQL runs).
func TestPGDepositClaimReviewRejectRecordsReason(t *testing.T) {
	s, mock := newClaimMock(t)

	mock.ExpectBegin()
	mock.ExpectQuery("FROM deposit_claims WHERE id").WillReturnRows(claimRow("pending"))
	mock.ExpectExec("UPDATE deposit_claims SET status").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	cl, err := s.ReviewClaim("dep_test", "usr_admin", "rejected", "tx not found on chain")
	if err != nil {
		t.Fatalf("review reject: %v", err)
	}
	if cl.Status != "rejected" || cl.RejectReason != "tx not found on chain" {
		t.Errorf("reviewed claim = %+v, want rejected with reason", cl)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestPGDepositClaimReviewAlreadyReviewed covers the state-machine guard: the
// `WHERE status='pending'` UPDATE affects zero rows for a claim that was
// already decided, so the second reviewer's decision is refused.
func TestPGDepositClaimReviewAlreadyReviewed(t *testing.T) {
	s, mock := newClaimMock(t)

	mock.ExpectBegin()
	mock.ExpectQuery("FROM deposit_claims WHERE id").WillReturnRows(claimRow("approved"))
	mock.ExpectExec("UPDATE deposit_claims SET status").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	_, err := s.ReviewClaim("dep_test", "usr_admin2", "approved", "")
	if err == nil || !strings.Contains(err.Error(), "already reviewed") {
		t.Fatalf("err = %v, want already-reviewed error", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestPGDepositClaimReviewNotFound returns an error for unknown ids.
func TestPGDepositClaimReviewNotFound(t *testing.T) {
	s, mock := newClaimMock(t)

	mock.ExpectBegin()
	mock.ExpectQuery("FROM deposit_claims WHERE id").WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	_, err := s.ReviewClaim("dep_missing", "usr_admin", "approved", "")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v, want not-found error", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestPGDepositClaimReviewInvalidAction refuses unknown actions before any
// database interaction (no expectations queued at all).
func TestPGDepositClaimReviewInvalidAction(t *testing.T) {
	s, mock := newClaimMock(t)
	if _, err := s.ReviewClaim("dep_test", "usr_admin", "maybe", ""); err == nil {
		t.Fatal("invalid action accepted, want error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}
