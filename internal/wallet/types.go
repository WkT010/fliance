package wallet

import "math/big"

type TxType int8

const (
	Deposit    TxType = 1
	Withdrawal TxType = 2
	Transfer   TxType = 3
	TradeBuy   TxType = 4
	TradeSell  TxType = 5
	Fee        TxType = 6
)

func (t TxType) String() string {
	switch t {
	case Deposit:
		return "deposit"
	case Withdrawal:
		return "withdrawal"
	case Transfer:
		return "transfer"
	case TradeBuy:
		return "trade_buy"
	case TradeSell:
		return "trade_sell"
	case Fee:
		return "fee"
	default:
		return "unknown"
	}
}

type TxStatus int8

const (
	Pending     TxStatus = 1
	Confirming  TxStatus = 2
	Completed   TxStatus = 3
	Failed      TxStatus = 4
	Reviewing   TxStatus = 5
	Approved    TxStatus = 6
	Broadcast   TxStatus = 7
	Rejected    TxStatus = 8
)

func (s TxStatus) String() string {
	switch s {
	case Pending:
		return "pending"
	case Confirming:
		return "confirming"
	case Completed:
		return "completed"
	case Failed:
		return "failed"
	case Reviewing:
		return "reviewing"
	case Approved:
		return "approved"
	case Broadcast:
		return "broadcast"
	case Rejected:
		return "rejected"
	default:
		return "unknown"
	}
}

type Wallet struct {
	ID, UserID, Asset, Address string
	Balance, Locked            *big.Float
	CreatedAt, UpdatedAt       int64
}

type Transaction struct {
	ID, UserID, WalletID, Asset, TxHash string
	Type                                 TxType
	Status                               TxStatus
	Amount, Fee                          *big.Float
	Confirmations                        int
	CreatedAt                            int64
	ToAddress                            string // withdrawal destination
}
