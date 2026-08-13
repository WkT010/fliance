package store

import (
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

// newWalletAddrMock returns a PGWalletStore backed by sqlmock.
func newWalletAddrMock(t *testing.T) (*PGWalletStore, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewPGWalletStore(db), mock
}

// TestAssignDepositAddressUpsertPersistsCandidate: with no address on the
// row yet, the upsert RETURNING clause yields the candidate — the store
// writes and reports exactly what it persisted.
func TestAssignDepositAddressUpsertPersistsCandidate(t *testing.T) {
	s, mock := newWalletAddrMock(t)
	mock.ExpectQuery("INSERT INTO wallets").
		WithArgs(sqlmock.AnyArg(), "usr_alice", "USDT", "0xCandidate", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"address"}).AddRow("0xCandidate"))

	got, err := s.AssignDepositAddress("usr_alice", "USDT", "0xCandidate")
	if err != nil {
		t.Fatalf("assign: %v", err)
	}
	if got != "0xCandidate" {
		t.Fatalf("persisted = %q, want 0xCandidate", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestAssignDepositAddressKeepsExistingWinner: when the row already carries
// an address, the upsert's CASE keeps it and RETURNING reports the winner —
// the caller (handler) must return that value, never the fresh candidate.
// This is the idempotency guarantee under concurrent/duplicate requests.
func TestAssignDepositAddressKeepsExistingWinner(t *testing.T) {
	s, mock := newWalletAddrMock(t)
	mock.ExpectQuery("INSERT INTO wallets").
		WithArgs(sqlmock.AnyArg(), "usr_alice", "USDT", "0xFreshCandidate", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"address"}).AddRow("0xEarlierWinner"))

	got, err := s.AssignDepositAddress("usr_alice", "USDT", "0xFreshCandidate")
	if err != nil {
		t.Fatalf("assign: %v", err)
	}
	if got != "0xEarlierWinner" {
		t.Fatalf("persisted = %q, want the existing winner 0xEarlierWinner", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestAssignDepositAddressRejectsEmptyInput: empty inputs never reach the
// database (an empty address must never overwrite or be persisted).
func TestAssignDepositAddressRejectsEmptyInput(t *testing.T) {
	s, mock := newWalletAddrMock(t)
	for _, tc := range []struct{ user, asset, addr string }{
		{"", "USDT", "0x1"},
		{"usr_a", "", "0x1"},
		{"usr_a", "USDT", ""},
	} {
		if _, err := s.AssignDepositAddress(tc.user, tc.asset, tc.addr); err == nil {
			t.Errorf("AssignDepositAddress(%q,%q,%q) expected error", tc.user, tc.asset, tc.addr)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}
