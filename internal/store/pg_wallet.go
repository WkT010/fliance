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
	row := s.db.QueryRow(`SELECT id,user_id,asset,balance,locked,COALESCE(address,''),created_at,updated_at FROM wallets WHERE user_id=$1 AND asset=$2`, userID, asset)
	w := &wallet.Wallet{Balance: new(big.Float), Locked: new(big.Float)}
	var b,l string
	if err := row.Scan(&w.ID, &w.UserID, &w.Asset, &b, &l, &w.Address, &w.CreatedAt, &w.UpdatedAt); err != nil { return nil, fmt.Errorf("get wallet: %w", err) }
	w.Balance.Parse(b,10); w.Locked.Parse(l,10)
	return w, nil
}

func (s *PGWalletStore) GetWallets(userID string) ([]*wallet.Wallet, error) {
	rows, _ := s.db.Query(`SELECT id,user_id,asset,balance,locked,COALESCE(address,''),created_at,updated_at FROM wallets WHERE user_id=$1`, userID)
	defer rows.Close()
	var ws []*wallet.Wallet
	for rows.Next() {
		w := &wallet.Wallet{Balance: new(big.Float), Locked: new(big.Float)}
		var b,l string
		rows.Scan(&w.ID,&w.UserID,&w.Asset,&b,&l,&w.Address,&w.CreatedAt,&w.UpdatedAt)
		w.Balance.Parse(b,10); w.Locked.Parse(l,10)
		ws = append(ws, w)
	}
	return ws, nil
}

func (s *PGWalletStore) UpdateBalance(id string, d *big.Float) error { _, e := s.db.Exec(`UPDATE wallets SET balance = balance + $1, updated_at = $2 WHERE id=$3`, d.Text('f',18), time.Now().UnixNano(), id); return e }
func (s *PGWalletStore) LockBalance(id string, a *big.Float) error { _, e := s.db.Exec(`UPDATE wallets SET locked = locked + $1, updated_at = $2 WHERE id=$3`, a.Text('f',18), time.Now().UnixNano(), id); return e }
func (s *PGWalletStore) UnlockBalance(id string, a *big.Float) error { _, e := s.db.Exec(`UPDATE wallets SET locked = locked - $1, updated_at = $2 WHERE id=$3`, a.Text('f',18), time.Now().UnixNano(), id); return e }

func (s *PGWalletStore) SaveTx(tx *wallet.Transaction) error {
	_, e := s.db.Exec(`INSERT INTO transactions (id,user_id,wallet_id,type,asset,amount,fee,status,tx_hash,confirmations,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		tx.ID,tx.UserID,tx.WalletID,tx.Type,tx.Asset,tx.Amount.Text('f',18),tx.Fee.Text('f',18),tx.Status,tx.TxHash,tx.Confirmations,tx.CreatedAt)
	return e
}

func (s *PGWalletStore) GetTx(id string) (*wallet.Transaction, error) {
	tx := &wallet.Transaction{Amount: new(big.Float), Fee: new(big.Float)}
	var a,f string
	row := s.db.QueryRow(`SELECT id,user_id,wallet_id,type,asset,amount,fee,status,COALESCE(tx_hash,''),confirmations,created_at FROM transactions WHERE id=$1`, id)
	if err := row.Scan(&tx.ID,&tx.UserID,&tx.WalletID,&tx.Type,&tx.Asset,&a,&f,&tx.Status,&tx.TxHash,&tx.Confirmations,&tx.CreatedAt); err != nil { return nil, fmt.Errorf("get tx: %w", err) }
	tx.Amount.Parse(a,10); tx.Fee.Parse(f,10)
	return tx, nil
}

func (s *PGWalletStore) ListTx(userID string, limit, offset int) ([]*wallet.Transaction, error) {
	rows, _ := s.db.Query(`SELECT id,user_id,wallet_id,type,asset,amount,fee,status,COALESCE(tx_hash,''),confirmations,created_at FROM transactions WHERE user_id=$1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`, userID, limit, offset)
	defer rows.Close()
	var txs []*wallet.Transaction
	for rows.Next() {
		tx := &wallet.Transaction{Amount: new(big.Float), Fee: new(big.Float)}
		var a,f string
		rows.Scan(&tx.ID,&tx.UserID,&tx.WalletID,&tx.Type,&tx.Asset,&a,&f,&tx.Status,&tx.TxHash,&tx.Confirmations,&tx.CreatedAt)
		tx.Amount.Parse(a,10); tx.Fee.Parse(f,10)
		txs = append(txs, tx)
	}
	return txs, nil
}
