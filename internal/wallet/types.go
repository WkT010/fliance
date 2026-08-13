package wallet

import "math/big"

type TxType int8

const (
	Deposit            TxType = 1
	Withdrawal         TxType = 2
	Transfer           TxType = 3
	TradeBuy           TxType = 4
	TradeSell          TxType = 5
	Fee                TxType = 6
	FuturesMargin      TxType = 7
	FuturesPnl         TxType = 8
	FuturesLiquidation TxType = 9
	FuturesFunding     TxType = 10
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
	case FuturesMargin:
		return "futures_margin"
	case FuturesPnl:
		return "futures_pnl"
	case FuturesLiquidation:
		return "futures_liquidation"
	case FuturesFunding:
		return "futures_funding"
	default:
		return "unknown"
	}
}

type TxStatus int8

const (
	Pending    TxStatus = 1
	Confirming TxStatus = 2
	Completed  TxStatus = 3
	Failed     TxStatus = 4
	Reviewing  TxStatus = 5
	Approved   TxStatus = 6
	Broadcast  TxStatus = 7
	Rejected   TxStatus = 8
	// ColdSigning marks a large withdrawal whose unsigned tx description was
	// queued to the cold (offline/HSM) signer and awaits the signed payload.
	ColdSigning TxStatus = 9
	// ColdSigned marks a withdrawal whose cold-signed payload was picked up
	// and is ready for on-chain broadcast.
	ColdSigned TxStatus = 10
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
	case ColdSigning:
		return "cold_signing"
	case ColdSigned:
		return "cold_signed"
	default:
		return "unknown"
	}
}

// Account types for multi-account wallets. Every user holds one wallet row
// per (asset, account_type): spot for trading/deposits/withdrawals, futures
// for derivatives margin, funding for the internal funding account.
const (
	AccountSpot    = "spot"
	AccountFutures = "futures"
	AccountFunding = "funding"
)

// ValidAccountType reports whether s is a known account type.
func ValidAccountType(s string) bool {
	switch s {
	case AccountSpot, AccountFutures, AccountFunding:
		return true
	}
	return false
}

// NormalizeAccountType maps the empty string to the spot account so code
// paths that predate the account dimension keep working unchanged.
func NormalizeAccountType(s string) string {
	if s == "" {
		return AccountSpot
	}
	return s
}

type Wallet struct {
	ID, UserID, Asset, Address string
	// AccountType is the account dimension of this wallet row
	// (spot/futures/funding). Empty means the spot account.
	AccountType              string
	Balance, Locked          *big.Float
	CreatedAt, UpdatedAt     int64
}

type Transaction struct {
	ID, UserID, WalletID, Asset, TxHash string
	Type                                TxType
	Status                              TxStatus
	Amount, Fee                         *big.Float
	Confirmations                       int
	CreatedAt                           int64
	ToAddress                           string // withdrawal destination
	// AccountType identifies which account's wallet row this entry belongs
	// to. In-memory only (used to resolve wallet_id on persistence); the
	// transactions table carries no account_type column.
	AccountType string
	// ColdRef is the cold-signer reference ID for withdrawals routed through
	// the cold wallet flow (status cold_signing / cold_signed). In-memory
	// only; persistence is a store-layer concern (see internal/store).
	ColdRef string
}
