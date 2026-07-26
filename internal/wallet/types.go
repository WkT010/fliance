package wallet

import "math/big"

type TxType int8
const (Deposit TxType = 1; Withdrawal TxType = 2; Transfer TxType = 3; TradeBuy TxType = 4; TradeSell TxType = 5; Fee TxType = 6)

type TxStatus int8
const (Pending TxStatus = 1; Confirming TxStatus = 2; Completed TxStatus = 3; Failed TxStatus = 4)

type Wallet struct {
	ID, UserID, Asset, Address string
	Balance, Locked *big.Float
	CreatedAt, UpdatedAt int64
}

type Transaction struct {
	ID, UserID, WalletID, Asset, TxHash string
	Type TxType; Status TxStatus
	Amount, Fee *big.Float
	Confirmations int; CreatedAt int64
}
