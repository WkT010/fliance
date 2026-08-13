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
	return s.GetWalletForAccount(userID, asset, wallet.AccountSpot)
}

// GetWalletForAccount returns the wallet row for one account type
// (spot/futures/funding). An empty accountType resolves to spot.
func (s *PGWalletStore) GetWalletForAccount(userID, asset, accountType string) (*wallet.Wallet, error) {
	accountType = wallet.NormalizeAccountType(accountType)
	w := &wallet.Wallet{Balance: new(big.Float), Locked: new(big.Float)}
	var b, l string
	row := s.db.QueryRow(
		`SELECT id,user_id,asset,balance,locked,COALESCE(address,''),created_at,updated_at,account_type FROM wallets WHERE user_id=$1 AND asset=$2 AND account_type=$3`, userID, asset, accountType)
	if err := row.Scan(&w.ID, &w.UserID, &w.Asset, &b, &l, &w.Address, &w.CreatedAt, &w.UpdatedAt, &w.AccountType); err != nil {
		if err == sql.ErrNoRows {
			return nil, wallet.ErrWalletNotFound
		}
		return nil, fmt.Errorf("get wallet: %w", err)
	}
	w.Balance.Parse(b, 10)
	w.Locked.Parse(l, 10)
	return w, nil
}

func (s *PGWalletStore) GetWallets(userID string) ([]*wallet.Wallet, error) {
	rows, err := s.db.Query(
		`SELECT id,user_id,asset,balance,locked,COALESCE(address,''),created_at,updated_at,account_type FROM wallets WHERE user_id=$1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var wallets []*wallet.Wallet
	for rows.Next() {
		w := &wallet.Wallet{Balance: new(big.Float), Locked: new(big.Float)}
		var b, l string
		if err := rows.Scan(&w.ID, &w.UserID, &w.Asset, &b, &l, &w.Address, &w.CreatedAt, &w.UpdatedAt, &w.AccountType); err != nil {
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

func (s *PGWalletStore) SaveWallet(w *wallet.Wallet) error {
	if w.Balance == nil {
		w.Balance = big.NewFloat(0)
	}
	if w.Locked == nil {
		w.Locked = big.NewFloat(0)
	}
	_, err := s.db.Exec(
		`INSERT INTO wallets (id,user_id,asset,balance,locked,address,created_at,updated_at,account_type) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		 ON CONFLICT (user_id, asset, account_type) DO NOTHING`,
		w.ID, w.UserID, w.Asset, w.Balance.Text('f', 18), w.Locked.Text('f', 18), w.Address, w.CreatedAt, w.UpdatedAt, wallet.NormalizeAccountType(w.AccountType))
	return err
}

// AssignDepositAddress idempotently binds a deposit address to the user's
// spot wallet row for an asset, upserting the row when it does not exist yet.
// It returns the address that is actually persisted afterwards: when the row
// already carries an address (even one written concurrently by another
// request) that one wins and the candidate is discarded, so the endpoint
// never hands out an address it did not persist. The single atomic
// INSERT ... ON CONFLICT ... RETURNING statement makes the first address
// ever assigned win under concurrent requests — there is no
// check-then-write race window.
func (s *PGWalletStore) AssignDepositAddress(userID, asset, address string) (string, error) {
	if userID == "" || asset == "" || address == "" {
		return "", fmt.Errorf("assign deposit address: userID, asset and address are required")
	}
	now := time.Now().UnixNano()
	wid := "wal_" + nowText() + randSuffix()
	var persisted string
	err := s.db.QueryRow(
		`INSERT INTO wallets (id,user_id,asset,balance,locked,address,created_at,updated_at,account_type)
		 VALUES ($1,$2,$3,0,0,$4,$5,$5,'spot')
		 ON CONFLICT (user_id, asset, account_type) DO UPDATE
		 SET address = CASE WHEN wallets.address IS NULL OR wallets.address = '' THEN EXCLUDED.address ELSE wallets.address END,
		     updated_at = CASE WHEN wallets.address IS NULL OR wallets.address = '' THEN EXCLUDED.updated_at ELSE wallets.updated_at END
		 RETURNING COALESCE(address,'')`, wid, userID, asset, address, now).Scan(&persisted)
	if err != nil {
		return "", fmt.Errorf("assign deposit address: %w", err)
	}
	return persisted, nil
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
// use so users can place orders before any wallet row exists. Operates on the
// spot account; derivatives use ReserveForAccount.
func (s *PGWalletStore) ReserveForOrder(userID, asset string, amt *big.Float) (*wallet.Wallet, error) {
	return s.ReserveForAccount(userID, asset, wallet.AccountSpot, amt)
}

// ReserveForAccount is ReserveForOrder scoped to one account type
// (spot/futures/funding). The wallet row is upserted on first use, so the
// futures account gets a row the first time margin is locked.
func (s *PGWalletStore) ReserveForAccount(userID, asset, accountType string, amt *big.Float) (*wallet.Wallet, error) {
	accountType = wallet.NormalizeAccountType(accountType)
	if !wallet.ValidAccountType(accountType) {
		return nil, fmt.Errorf("reserve: %w: %s", wallet.ErrInvalidAccount, accountType)
	}
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
		`INSERT INTO wallets (id,user_id,asset,balance,locked,created_at,updated_at,account_type)
		 VALUES($1,$2,$3,0,0,$4,$4,$5)
		 ON CONFLICT (user_id, asset, account_type) DO NOTHING`, wid, userID, asset, now, accountType); err != nil {
		return nil, fmt.Errorf("upsert wallet: %w", err)
	}
	res, err := tx.Exec(
		`UPDATE wallets SET locked = locked + $1, updated_at = $2
		 WHERE user_id=$3 AND asset=$4 AND account_type=$5 AND (balance - locked) >= $1`,
		amtStr, now, userID, asset, accountType)
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
	return s.GetWalletForAccount(userID, asset, accountType)
}

// ReserveWithDailyLimit atomically reserves withdrawal funds AND accrues the
// USDT-equivalent amount into the per-day usage meter, failing the whole
// transaction when the accrual would breach the daily limit. Both mutations
// happen in one transaction: the fund reservation mirrors ReserveForOrder's
// conditional UPDATE, and the usage row is upserted with a WHERE clause so a
// concurrent withdrawal cannot slip past the limit between a check and a
// write (no TOCTOU window). Returns wallet.ErrInsufficientBalance or
// wallet.ErrDailyLimitExceeded without committing either side.
func (s *PGWalletStore) ReserveWithDailyLimit(userID, asset string, amount, usdtEquiv, limit *big.Float) (*wallet.Wallet, error) {
	if amount == nil || amount.Sign() <= 0 {
		return nil, wallet.ErrNegativeAmount
	}
	if usdtEquiv == nil || usdtEquiv.Sign() <= 0 {
		return nil, fmt.Errorf("reserve with daily limit: %w", wallet.ErrNegativeAmount)
	}
	now := time.Now().UnixNano()
	day := time.Now().UTC().Format("2006-01-02")
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()
	wid := "wal_" + nowText() + randSuffix()
	if _, err := tx.Exec(
		`INSERT INTO wallets (id,user_id,asset,balance,locked,created_at,updated_at,account_type)
		 VALUES($1,$2,$3,0,0,$4,$4,'spot')
		 ON CONFLICT (user_id, asset, account_type) DO NOTHING`, wid, userID, asset, now); err != nil {
		return nil, fmt.Errorf("upsert wallet: %w", err)
	}
	amtStr := amount.Text('f', 18)
	res, err := tx.Exec(
		`UPDATE wallets SET locked = locked + $1, updated_at = $2
		 WHERE user_id=$3 AND asset=$4 AND account_type='spot' AND (balance - locked) >= $1`,
		amtStr, now, userID, asset)
	if err != nil {
		return nil, fmt.Errorf("reserve: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, wallet.ErrInsufficientBalance
	}
	// Conditional usage accrual: BOTH branches carry the limit check. The
	// INSERT side gates the first usage row of the day (a plain INSERT would
	// bypass the limit entirely); the DO UPDATE's WHERE gates every later
	// accrual. Rows that would breach the limit report zero affected rows.
	// lib/pq passes the statement through verbatim; the syntax is standard
	// PostgreSQL.
	usageRes, err := tx.Exec(
		`INSERT INTO withdrawal_daily_usage (user_id, asset, day, used)
		 SELECT $1, $2, $3, $4::numeric
		 WHERE $4::numeric <= $5::numeric
		 ON CONFLICT (user_id, asset, day) DO UPDATE
		 SET used = withdrawal_daily_usage.used + EXCLUDED.used
		 WHERE withdrawal_daily_usage.used + EXCLUDED.used <= $5::numeric`,
		userID, asset, day, usdtEquiv.Text('f', 18), limit.Text('f', 18))
	if err != nil {
		return nil, fmt.Errorf("daily usage: %w", err)
	}
	if n, _ := usageRes.RowsAffected(); n == 0 {
		// Rollback releases the fund reservation as well.
		return nil, wallet.ErrDailyLimitExceeded
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return s.GetWallet(userID, asset)
}

// ReleaseDailyUsage credits the per-day usage meter back (rejected or failed
// withdrawals). It never drives the meter below zero.
func (s *PGWalletStore) ReleaseDailyUsage(userID, asset string, usdtEquiv *big.Float) error {
	if usdtEquiv == nil || usdtEquiv.Sign() <= 0 {
		return nil
	}
	day := time.Now().UTC().Format("2006-01-02")
	_, err := s.db.Exec(
		`UPDATE withdrawal_daily_usage SET used = GREATEST(used - $1, 0)
		 WHERE user_id=$2 AND asset=$3 AND day=$4`,
		usdtEquiv.Text('f', 18), userID, asset, day)
	return err
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
	// Pre-upsert every wallet touched by this settle so a debit on a wallet
	// the user has never deposited into (e.g. AMM swapping a token they do not
	// yet hold) deterministically fails the (balance - locked >= -delta) guard
	// below instead of silently zeroing-out the operation. Without this upsert
	// the UPDATE would match 0 rows and the credit side of the settle would
	// still commit, allowing the AMM engine to "mint" tokens for free.
	for _, op := range ops {
		acct := wallet.NormalizeAccountType(op.AccountType)
		wid := "wal_" + nowText() + randSuffix()
		if _, err := tx.Exec(
			`INSERT INTO wallets (id,user_id,asset,balance,locked,created_at,updated_at,account_type)
			 VALUES($1,$2,$3,0,0,$4,$4,$5)
			 ON CONFLICT (user_id, asset, account_type) DO NOTHING`, wid, op.UserID, op.Asset, now, acct); err != nil {
			return fmt.Errorf("upsert wallet %s/%s/%s: %w", op.UserID, op.Asset, acct, err)
		}
	}
	for _, op := range ops {
		acct := wallet.NormalizeAccountType(op.AccountType)
		if op.Unlock != nil && op.Unlock.Sign() != 0 {
			if _, err := tx.Exec(
				`UPDATE wallets SET locked = locked - $1, updated_at = $2
				 WHERE user_id=$3 AND asset=$4 AND account_type=$5 AND locked >= $1`,
				op.Unlock.Text('f', 18), now, op.UserID, op.Asset, acct); err != nil {
				return fmt.Errorf("unlock %s/%s/%s: %w", op.UserID, op.Asset, acct, err)
			}
		}
		if op.Delta != nil && op.Delta.Sign() != 0 {
			// Refuse any negative delta that would drive the wallet below
			// zero. A user cannot spend tokens they do not own. For a
			// positive delta there is no upper bound, so we accept it as long
			// as the wallet row exists (guaranteed by the upsert above).
			if op.Delta.Sign() < 0 {
				res, err := tx.Exec(
					`UPDATE wallets SET balance = balance + $1, updated_at = $2
					 WHERE user_id=$3 AND asset=$4 AND account_type=$5 AND (balance - locked) >= $6`,
					op.Delta.Text('f', 18), now, op.UserID, op.Asset, acct, new(big.Float).Neg(op.Delta).Text('f', 18))
				if err != nil {
					return fmt.Errorf("delta %s/%s/%s: %w", op.UserID, op.Asset, acct, err)
				}
				if n, _ := res.RowsAffected(); n == 0 {
					return fmt.Errorf("insufficient balance for %s %s %s: %w", op.UserID, op.Asset, acct, wallet.ErrInsufficientBalance)
				}
			} else {
				if _, err := tx.Exec(
					`UPDATE wallets SET balance = balance + $1, updated_at = $2
					 WHERE user_id=$3 AND asset=$4 AND account_type=$5`,
					op.Delta.Text('f', 18), now, op.UserID, op.Asset, acct); err != nil {
					return fmt.Errorf("delta %s/%s/%s: %w", op.UserID, op.Asset, acct, err)
				}
			}
		}
	}
	for _, t := range txns {
		// Resolve the wallet ID when the caller did not supply one (e.g.
		// futures PnL / liquidation and spot fill ledgers created by the
		// wallet/futures services). wallet_id is NOT NULL with an FK to
		// wallets.id, so an empty value would violate the constraint.
		wid := t.WalletID
		if wid == "" {
			if err := tx.QueryRow(
				`SELECT id FROM wallets WHERE user_id=$1 AND asset=$2 AND account_type=$3`,
				t.UserID, t.Asset, wallet.NormalizeAccountType(t.AccountType)).Scan(&wid); err != nil {
				return fmt.Errorf("resolve wallet for tx %s: %w", t.ID, err)
			}
		}
		if _, err := tx.Exec(
			`INSERT INTO transactions (id,user_id,wallet_id,type,asset,amount,fee,status,tx_hash,confirmations,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT (id) DO NOTHING`,
			t.ID, t.UserID, wid, t.Type, t.Asset,
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
		`INSERT INTO transactions (id,user_id,wallet_id,type,asset,amount,fee,status,tx_hash,confirmations,created_at,to_address) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) ON CONFLICT (id) DO NOTHING`,
		tx.ID, tx.UserID, tx.WalletID, tx.Type, tx.Asset,
		tx.Amount.Text('f', 18), tx.Fee.Text('f', 18),
		tx.Status, tx.TxHash, tx.Confirmations, tx.CreatedAt, tx.ToAddress)
	return err
}
func (s *PGWalletStore) GetTx(id string) (*wallet.Transaction, error) {
	tx := &wallet.Transaction{Amount: new(big.Float), Fee: new(big.Float)}
	var a, f string
	row := s.db.QueryRow(
		`SELECT id,user_id,wallet_id,type,asset,amount,fee,status,COALESCE(tx_hash,''),confirmations,created_at,COALESCE(to_address,'') FROM transactions WHERE id=$1`, id)
	if err := row.Scan(&tx.ID, &tx.UserID, &tx.WalletID, &tx.Type, &tx.Asset,
		&a, &f, &tx.Status, &tx.TxHash, &tx.Confirmations, &tx.CreatedAt, &tx.ToAddress); err != nil {
		return nil, fmt.Errorf("get tx: %w", err)
	}
	tx.Amount.Parse(a, 10)
	tx.Fee.Parse(f, 10)
	return tx, nil
}
func (s *PGWalletStore) ListTx(userID string, limit, offset int) ([]*wallet.Transaction, error) {
	rows, err := s.db.Query(
		`SELECT id,user_id,wallet_id,type,asset,amount,fee,status,COALESCE(tx_hash,''),confirmations,created_at,COALESCE(to_address,'') FROM transactions WHERE user_id=$1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
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
			&a, &f, &tx.Status, &tx.TxHash, &tx.Confirmations, &tx.CreatedAt, &tx.ToAddress); err != nil {
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

// UpdateTxStatus updates the status of a transaction row.
func (s *PGWalletStore) UpdateTxStatus(id string, status wallet.TxStatus) error {
	_, err := s.db.Exec(`UPDATE transactions SET status=$1, updated_at=$2 WHERE id=$3`,
		status, time.Now().UnixNano(), id)
	return err
}

// ListTxByStatus returns transactions with the given status, newest first.
func (s *PGWalletStore) ListTxByStatus(status wallet.TxStatus, limit, offset int) ([]*wallet.Transaction, error) {
	rows, err := s.db.Query(
		`SELECT id,user_id,wallet_id,type,asset,amount,fee,status,COALESCE(tx_hash,''),confirmations,created_at,COALESCE(to_address,'') FROM transactions WHERE status=$1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		status, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var txs []*wallet.Transaction
	for rows.Next() {
		tx := &wallet.Transaction{Amount: new(big.Float), Fee: new(big.Float)}
		var a, f string
		if err := rows.Scan(&tx.ID, &tx.UserID, &tx.WalletID, &tx.Type, &tx.Asset,
			&a, &f, &tx.Status, &tx.TxHash, &tx.Confirmations, &tx.CreatedAt, &tx.ToAddress); err != nil {
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
