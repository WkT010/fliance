package store

import (
	"database/sql"
	"fmt"
	"math/big"
	"time"

	"github.com/WkT010/nexa-exchange/internal/wallet"
)

type PGWalletStore struct{ db *sql.DB }

func NewPGWalletStore(db *sql.DB) *PGWalletStore { return &PGWalletStore{db: db} }

func (s *PGWalletStore) GetWallet(userID, asset string) (*wallet.Wallet, error) {
	w := &wallet.Wallet{Balance: new(big.Float), Locked: new(big.Float)}
	var b, l string
	row := s.db.QueryRow(
		`SELECT id,user_id,asset,balance,locked,COALESCE(address,''),created_at,updated_at FROM wallets WHERE user_id=$1 AND asset=$2`, userID, asset)
	if err := row.Scan(&w.ID, &w.UserID, &w.Asset, &b, &l, &w.Address, &w.CreatedAt, &w.UpdatedAt); err != nil {
		return nil, fmt.Errorf("get wallet: %w", err)
	}
	w.Balance.Parse(b, 10)
	w.Locked.Parse(l, 10)
	return w, nil
}

func (s *PGWalletStore) GetWallets(userID string) ([]*wallet.Wallet, error) {
	rows, err := s.db.Query(
		`SELECT id,user_id,asset,balance,locked,COALESCE(address,''),created_at,updated_at FROM wallets WHERE user_id=$1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var wallets []*wallet.Wallet
	for rows.Next() {
		w := &wallet.Wallet{Balance: new(big.Float), Locked: new(big.Float)}
		var b, l string
		if err := rows.Scan(&w.ID, &w.UserID, &w.Asset, &b, &l, &w.Address, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, err
		}
		w.Balance.Parse(b, 10)
		w.Locked.Parse(l, 10)
		wallets = append(wallets, w)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return wallets, nil
}

func (s *PGWalletStore) UpdateBalance(id string, delta *big.Float) error {
	_, err := s.db.Exec(`UPDATE wallets SET balance = balance + $1, updated_at = $2 WHERE id=$3`,
		delta.Text('f', 18), time.Now().UnixNano(), id)
	return err
}
func (s *PGWalletStore) LockBalance(id string, amt *big.Float) error {
	_, err := s.db.Exec(`UPDATE wallets SET locked = locked + $1, updated_at = $2 WHERE id=$3`,
		amt.Text('f', 18), time.Now().UnixNano(), id)
	return err
}
func (s *PGWalletStore) UnlockBalance(id string, amt *big.Float) error {
	_, err := s.db.Exec(`UPDATE wallets SET locked = locked - $1, updated_at = $2 WHERE id=$3`,
		amt.Text('f', 18), time.Now().UnixNano(), id)
	return err
}

// ReserveForOrder atomically verifies the wallet has enough available balance
// (balance - locked >= amt) and increments locked by amt, all within a single
// row update guarded by a WHERE clause. This closes the TOCTOU window that a
// separate SELECT-then-UPDATE would create. The wallet row is upserted on first
// use so users can place orders before any wallet row exists.
func (s *PGWalletStore) ReserveForOrder(userID, asset string, amt *big.Float) (*wallet.Wallet, error) {
	if amt == nil || amt.Sign() <= 0 {
		return nil, wallet.ErrNegativeAmount
	}
	amtStr := amt.Text('f', 18)
	now := time.Now().UnixNano()
	// Upsert wallet row (balance 0, locked 0) if missing, then reserve in the
	// same atomic UPDATE. The WHERE clause guarantees we never go negative on
	// available balance.
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()
	wid := "wal_" + nowText() + randSuffix()
	if _, err := tx.Exec(
		`INSERT INTO wallets (id,user_id,asset,balance,locked,created_at,updated_at)
		 VALUES($1,$2,$3,0,0,$4,$4)
		 ON CONFLICT (user_id, asset) DO NOTHING`, wid, userID, asset, now); err != nil {
		return nil, fmt.Errorf("upsert wallet: %w", err)
	}
	res, err := tx.Exec(
		`UPDATE wallets SET locked = locked + $1, updated_at = $2
		 WHERE user_id=$3 AND asset=$4 AND (balance - locked) >= $1`,
		amtStr, now, userID, asset)
	if err != nil {
		return nil, fmt.Errorf("reserve: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, wallet.ErrInsufficientBalance
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return s.GetWallet(userID, asset)
}

// Settle applies a batch of atomic wallet mutations inside a single database
// transaction with row-level locking (SELECT ... FOR UPDATE), and records the
// provided ledger entries. Either every op and every ledger write succeeds, or
// the whole transaction is rolled back. This is the financial-consistency
// core: a trade fill touches up to 4 wallets (buyer quote/base, seller
// base/quote) plus fees, and they must all commit together.
func (s *PGWalletStore) Settle(ops []wallet.SettleOp, txns []*wallet.Transaction) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()
	now := time.Now().UnixNano()
	for _, op := range ops {
		if op.Unlock != nil && op.Unlock.Sign() != 0 {
			if _, err := tx.Exec(
				`UPDATE wallets SET locked = locked - $1, updated_at = $2
				 WHERE user_id=$3 AND asset=$4 AND locked >= $1`,
				op.Unlock.Text('f', 18), now, op.UserID, op.Asset); err != nil {
				return fmt.Errorf("unlock %s/%s: %w", op.UserID, op.Asset, err)
			}
		}
		if op.Delta != nil && op.Delta.Sign() != 0 {
			if _, err := tx.Exec(
				`UPDATE wallets SET balance = balance + $1, updated_at = $2
				 WHERE user_id=$3 AND asset=$4`,
				op.Delta.Text('f', 18), now, op.UserID, op.Asset); err != nil {
				return fmt.Errorf("delta %s/%s: %w", op.UserID, op.Asset, err)
			}
		}
	}
	for _, t := range txns {
		if _, err := tx.Exec(
			`INSERT INTO transactions (id,user_id,wallet_id,type,asset,amount,fee,status,tx_hash,confirmations,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT (id) DO NOTHING`,
			t.ID, t.UserID, t.WalletID, t.Type, t.Asset,
			t.Amount.Text('f', 18), t.Fee.Text('f', 18),
			t.Status, t.TxHash, t.Confirmations, t.CreatedAt); err != nil {
			return fmt.Errorf("save tx: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func (s *PGWalletStore) SaveTx(tx *wallet.Transaction) error {
	_, err := s.db.Exec(
		`INSERT INTO transactions (id,user_id,wallet_id,type,asset,amount,fee,status,tx_hash,confirmations,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT (id) DO NOTHING`,
		tx.ID, tx.UserID, tx.WalletID, tx.Type, tx.Asset,
		tx.Amount.Text('f', 18), tx.Fee.Text('f', 18),
		tx.Status, tx.TxHash, tx.Confirmations, tx.CreatedAt)
	return err
}
func (s *PGWalletStore) GetTx(id string) (*wallet.Transaction, error) {
	tx := &wallet.Transaction{Amount: new(big.Float), Fee: new(big.Float)}
	var a, f string
	row := s.db.QueryRow(
		`SELECT id,user_id,wallet_id,type,asset,amount,fee,status,COALESCE(tx_hash,''),confirmations,created_at FROM transactions WHERE id=$1`, id)
	if err := row.Scan(&tx.ID, &tx.UserID, &tx.WalletID, &tx.Type, &tx.Asset,
		&a, &f, &tx.Status, &tx.TxHash, &tx.Confirmations, &tx.CreatedAt); err != nil {
		return nil, fmt.Errorf("get tx: %w", err)
	}
	tx.Amount.Parse(a, 10)
	tx.Fee.Parse(f, 10)
	return tx, nil
}
func (s *PGWalletStore) ListTx(userID string, limit, offset int) ([]*wallet.Transaction, error) {
	rows, err := s.db.Query(
		`SELECT id,user_id,wallet_id,type,asset,amount,fee,status,COALESCE(tx_hash,''),confirmations,created_at FROM transactions WHERE user_id=$1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var txs []*wallet.Transaction
	for rows.Next() {
		tx := &wallet.Transaction{Amount: new(big.Float), Fee: new(big.Float)}
		var a, f string
		if err := rows.Scan(&tx.ID, &tx.UserID, &tx.WalletID, &tx.Type, &tx.Asset,
			&a, &f, &tx.Status, &tx.TxHash, &tx.Confirmations, &tx.CreatedAt); err != nil {
			return nil, err
		}
		tx.Amount.Parse(a, 10)
		tx.Fee.Parse(f, 10)
		txs = append(txs, tx)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return txs, nil
}
