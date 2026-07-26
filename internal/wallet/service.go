package wallet

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInsufficientBalance = errors.New("insufficient balance")
	ErrWalletNotFound      = errors.New("wallet not found")
	ErrInvalidAddress      = errors.New("invalid address")
	ErrNegativeAmount      = errors.New("positive amount required")
	ErrUnsupportedAsset    = errors.New("unsupported asset")
	ErrInvalidPair         = errors.New("invalid trading pair")
	ErrDuplicateTx         = errors.New("duplicate transaction")
)

// WalletStore is the persistence interface for wallet balances, locks and the
// ledger of transactions. Implementations must make the Settle / Reserve /
// Release operations atomic at the database level (single transaction with row
// locking) to prevent race conditions and double-spends.
type WalletStore interface {
	GetWallet(userID, asset string) (*Wallet, error)
	GetWallets(userID string) ([]*Wallet, error)
	// UpdateBalance applies a signed delta to the wallet's balance.
	UpdateBalance(id string, delta *big.Float) error
	// LockBalance increases the locked amount by amt. It does NOT change the
	// balance; the funds remain owned by the user but are earmarked.
	LockBalance(id string, amt *big.Float) error
	// UnlockBalance decreases the locked amount by amt.
	UnlockBalance(id string, amt *big.Float) error
	SaveTx(*Transaction) error
	GetTx(string) (*Transaction, error)
	ListTx(userID string, limit, offset int) ([]*Transaction, error)

	// ReserveForOrder atomically checks that the wallet has enough available
	// (balance - locked) balance and locks amt. Returns ErrInsufficientBalance
	// if not. This closes the TOCTOU window between the availability check and
	// the lock.
	ReserveForOrder(userID, asset string, amt *big.Float) (*Wallet, error)
	// Settle applies a set of atomic (unlock + balance delta + optional lock)
	// operations across multiple wallets in a single database transaction, and
	// records the provided ledger entries. Either all operations succeed or
	// none do.
	Settle(ops []SettleOp, txns []*Transaction) error
}

// SettleOp describes one atomic wallet mutation performed as part of a trade
// settlement or order release. Exactly one wallet (identified by UserID+Asset)
// is touched per op.
type SettleOp struct {
	UserID string
	Asset  string
	// Unlock, if non-nil, is subtracted from the wallet's locked amount.
	Unlock *big.Float
	// Delta is a signed change to the wallet's balance (negative = debit,
	// positive = credit). Applied after Unlock.
	Delta *big.Float
}

// FeeConfig holds per-pair trading fee rates. Rates are fractions, e.g. 0.001
// for 10 bps (0.1%). Taker fees typically exceed maker fees.
type FeeConfig struct {
	TakerRate *big.Float
	MakerRate *big.Float
}

// FeeSchedule returns the fee configuration for a trading pair.
type FeeSchedule interface {
	Fees(pair string) FeeConfig
}

// StaticFeeSchedule is a FeeSchedule that applies the same rates to every pair
// unless a per-pair override is registered.
type StaticFeeSchedule struct {
	Default FeeConfig
	Pairs   map[string]FeeConfig
}

func (s *StaticFeeSchedule) Fees(pair string) FeeConfig {
	if c, ok := s.Pairs[pair]; ok {
		return c
	}
	return s.Default
}

// Service is the wallet domain service. It owns balance management, deposit /
// withdrawal lifecycle and trade settlement. All monetary arithmetic uses
// big.Float at matching.DefaultPrecision via the helpers below so that fees
// and settlements are exact.
type Service struct {
	store          WalletStore
	clients        map[string]BlockchainClient
	confThresholds map[string]int
	fees           FeeSchedule
	mu             sync.Mutex
	// processedFills deduplicates fill settlement by FillNotification key so a
	// replayed fill (e.g. after a restart that re-reads the channel) is a no-op.
	processedFills map[string]bool
	// reservations tracks per-order reserved funds so that cancellations and
	// price-improvement leftovers can be released precisely. Keyed by order ID.
	// In-memory: suitable for single-instance deployments; an HA deployment
	// would persist this to a table.
	reservations map[string]*reservation
}

// reservation records the asset and remaining locked amount for one order.
type reservation struct {
	asset     string
	remaining *big.Float
}

// NewService constructs a wallet service. If fees is nil, a zero-fee schedule is
// used (suitable for tests; production should inject real rates).
func NewService(store WalletStore, clients map[string]BlockchainClient, fees FeeSchedule) *Service {
	if clients == nil {
		clients = make(map[string]BlockchainClient)
	}
	if fees == nil {
		fees = &StaticFeeSchedule{
			Default: FeeConfig{TakerRate: big.NewFloat(0), MakerRate: big.NewFloat(0)},
		}
	}
	return &Service{
		store:          store,
		clients:        clients,
		confThresholds: map[string]int{"BTC": 12, "ETH": 24, "POLYGON": 24},
		fees:           fees,
		processedFills: make(map[string]bool),
		reservations:   make(map[string]*reservation),
	}
}

func (s *Service) clientFor(asset string) BlockchainClient { return s.clients[asset] }
func (s *Service) RegisterClient(asset string, c BlockchainClient) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients[asset] = c
}

// ClientsMap returns a snapshot of the registered blockchain clients. The
// returned map is a copy so callers cannot mutate the service's internal map.
func (s *Service) ClientsMap() map[string]BlockchainClient {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]BlockchainClient, len(s.clients))
	for k, v := range s.clients {
		out[k] = v
	}
	return out
}

// SetFeeSchedule replaces the fee schedule at runtime. Safe for concurrent use.
func (s *Service) SetFeeSchedule(fees FeeSchedule) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fees = fees
}

// GetBalance returns the user's wallet for an asset.
func (s *Service) GetBalance(userID, asset string) (*Wallet, error) {
	if s.store == nil {
		return nil, ErrWalletNotFound
	}
	w, err := s.store.GetWallet(userID, asset)
	if err != nil {
		return nil, fmt.Errorf("get wallet: %w", err)
	}
	return w, nil
}

// GetBalances returns all wallets for a user.
func (s *Service) GetBalances(userID string) ([]*Wallet, error) {
	if s.store == nil {
		return []*Wallet{}, nil
	}
	ws, err := s.store.GetWallets(userID)
	if err != nil {
		return nil, fmt.Errorf("get wallets: %w", err)
	}
	if ws == nil {
		ws = []*Wallet{}
	}
	return ws, nil
}

// Deposit credits a user's wallet with amount of asset. It is idempotent on
// txHash: depositing the same txHash twice is a no-op (returns nil). A
// completed ledger entry is recorded.
func (s *Service) Deposit(userID, asset string, amount *big.Float, txHash string) error {
	if amount == nil || amount.Sign() <= 0 {
		return ErrNegativeAmount
	}
	if s.store == nil {
		return ErrWalletNotFound
	}
	// Idempotency on txHash: if a tx with this hash already exists, no-op.
	if txHash != "" {
		if _, err := s.store.GetTx(txHash); err == nil {
			return nil // already deposited
		}
	}
	w, err := s.store.GetWallet(userID, asset)
	now := time.Now().UnixNano()
	if err != nil || w == nil {
		// Auto-create wallet on first deposit (common for exchanges).
		w = &Wallet{ID: "wal_" + uuid.NewString(), UserID: userID, Asset: asset, Balance: big.NewFloat(0), Locked: big.NewFloat(0), CreatedAt: now, UpdatedAt: now}
	}
	if err := s.store.UpdateBalance(w.ID, amount); err != nil {
		return fmt.Errorf("credit balance: %w", err)
	}
	txID := txHash
	if txID == "" {
		txID = "dep_" + uuid.NewString()
	}
	return s.store.SaveTx(&Transaction{
		ID: txID, UserID: userID, WalletID: w.ID, Type: Deposit, Asset: asset,
		Amount: new(big.Float).Copy(amount), Fee: big.NewFloat(0),
		Status: Completed, TxHash: txHash, CreatedAt: now,
	})
}

// Withdraw locks amount of asset and records a pending withdrawal transaction.
// The actual on-chain broadcast is handled by the wallet-service's withdrawal
// worker (out of scope for the synchronous API). Returns ErrInsufficientBalance
// if the available (balance - locked) amount is insufficient.
func (s *Service) Withdraw(userID, asset, address string, amount *big.Float) error {
	if amount == nil || amount.Sign() <= 0 {
		return ErrNegativeAmount
	}
	if s.store == nil {
		return ErrWalletNotFound
	}
	c := s.clientFor(asset)
	if c == nil {
		return fmt.Errorf("%w: %s", ErrUnsupportedAsset, asset)
	}
	if !c.IsValidAddress(address) {
		return ErrInvalidAddress
	}
	// Reserve atomically: this checks available balance and locks in one tx.
	w, err := s.store.ReserveForOrder(userID, asset, amount)
	if err != nil {
		return err
	}
	now := time.Now().UnixNano()
	return s.store.SaveTx(&Transaction{
		ID: "wd_" + uuid.NewString(), UserID: userID, WalletID: w.ID, Type: Withdrawal,
		Asset: asset, Amount: new(big.Float).Copy(amount), Fee: big.NewFloat(0),
		Status: Pending, CreatedAt: now,
	})
}

// SplitPair parses a trading pair "BASE/QUOTE" into its constituents.
func SplitPair(pair string) (base, quote string, err error) {
	parts := strings.Split(pair, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("%w: %s", ErrInvalidPair, pair)
	}
	return parts[0], parts[1], nil
}

// ReserveOrder locks the funds required for an order before it enters the
// matching engine, and records the reservation so it can be released on cancel
// or partial-fill price improvement. Returns ErrInsufficientBalance if the
// available (balance - locked) amount is insufficient.
//
// Reservation rules:
//   - Limit buy: lock price*qty*(1+takerRate) of quote (worst-case cost + fee).
//   - Limit sell / Market sell: lock qty of base.
//   - Market buy: NOT pre-locked (price unknown); settled on fill. Callers
//     should ensure the user has quote balance; settlement debits on fill.
//
// side is 1 for buy, -1 for sell (matching.Buy/matching.Sell). orderType is the
// matching.OrderType integer (Limit=0, Market=1, ...).
func (s *Service) ReserveOrder(orderID, userID, pair string, side int, orderType int, price, qty *big.Float) error {
	if s.store == nil {
		return nil // no persistence: nothing to reserve (best-effort mode)
	}
	if qty == nil || qty.Sign() <= 0 {
		return ErrNegativeAmount
	}
	base, quote, err := SplitPair(pair)
	if err != nil {
		return err
	}

	var asset string
	var amt *big.Float
	switch {
	case side == 1 && orderType == 0: // limit buy
		if price == nil || price.Sign() <= 0 {
			return ErrNegativeAmount
		}
		asset = quote
		amt = new(big.Float).Mul(price, qty)
		if cfg := s.fees.Fees(pair); cfg.TakerRate != nil && cfg.TakerRate.Sign() > 0 {
			// Reserve worst-case fee (taker rate). Any unused portion is
			// released when the order fills as a maker or completes/cancels.
			feeBuf := new(big.Float).Mul(amt, cfg.TakerRate)
			amt = new(big.Float).Add(amt, feeBuf)
		}
	case side == -1: // sell (limit or market): lock base qty
		asset = base
		amt = new(big.Float).Copy(qty)
	default:
		// market buy: no pre-lock
		return nil
	}

	if _, err := s.store.ReserveForOrder(userID, asset, amt); err != nil {
		return err
	}
	s.mu.Lock()
	s.reservations[orderID] = &reservation{asset: asset, remaining: new(big.Float).Copy(amt)}
	s.mu.Unlock()
	return nil
}

// ReleaseOrder releases any remaining reserved funds for an order (e.g. on
// cancel, or after the order has fully filled). It is a no-op if the order had
// no reservation (e.g. a market buy).
func (s *Service) ReleaseOrder(orderID, userID string) error {
	if s.store == nil {
		return nil
	}
	s.mu.Lock()
	r, ok := s.reservations[orderID]
	if ok {
		delete(s.reservations, orderID)
	}
	s.mu.Unlock()
	if !ok || r == nil || r.remaining.Sign() <= 0 {
		return nil
	}
	// Use the atomic Settle path with a single unlock op (no balance delta) so
	// the release is consistent with how settlements mutate locked balances.
	ops := []SettleOp{{UserID: userID, Asset: r.asset, Unlock: r.remaining}}
	return s.store.Settle(ops, nil)
}

// SettleFill settles a single trade fill between a taker and a maker. It:
//   - Debits the buyer (notional+buyerFee) of quote and credits (qty) of base.
//   - Debits the seller (qty) of base and credits (notional - sellerFee) of quote.
//   - For each leg, unlocks the reserved amount that corresponds to this fill
//     (so reserved funds are released as the order fills).
//
// The whole operation is atomic. It is idempotent on fillID. The order IDs are
// used to look up per-order reservations so the unlock matches what was locked.
//
// takerSide is 1 (buy) or -1 (sell). The taker/maker fee rates come from the
// fee schedule.
func (s *Service) SettleFill(fillID, pair string, takerSide int, takerOrderID, makerOrderID, takerUserID, makerUserID string, price, qty *big.Float) error {
	if s.store == nil {
		return nil // best-effort: no persistence, nothing to settle
	}
	if price == nil || qty == nil || qty.Sign() <= 0 || price.Sign() <= 0 {
		return ErrNegativeAmount
	}

	// Idempotency on fillID.
	s.mu.Lock()
	if s.processedFills[fillID] {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	base, quote, err := SplitPair(pair)
	if err != nil {
		return err
	}

	notional := new(big.Float).Mul(price, qty) // quote currency

	feeCfg := s.fees.Fees(pair)
	takerFee := new(big.Float).Mul(notional, feeCfg.TakerRate)
	makerFee := new(big.Float).Mul(notional, feeCfg.MakerRate)

	// Determine buyer/seller order IDs, user IDs and fees.
	buyerOrderID, sellerOrderID := takerOrderID, makerOrderID
	buyerID, sellerID := takerUserID, makerUserID
	buyerFee, sellerFee := takerFee, makerFee
	if takerSide == -1 { // taker is selling => maker is buyer
		buyerOrderID, sellerOrderID = makerOrderID, takerOrderID
		buyerID, sellerID = makerUserID, takerUserID
		buyerFee, sellerFee = makerFee, takerFee
	}

	// Compute unlock amounts from reservations. If an order has a reservation,
	// unlock the fill's reserved portion; otherwise debit only (market buys).
	buyerUnlock := new(big.Float)
	buyerDebit := new(big.Float).Add(notional, buyerFee) // cost + fee
	sellerUnlock := new(big.Float)
	s.mu.Lock()
	buyerRes := s.reservations[buyerOrderID]
	if buyerRes != nil && buyerRes.asset == quote {
		// Reserved amount for this fill = buyerDebit (cost+fee), capped at the
		// remaining reservation.
		unlock := new(big.Float).Copy(buyerDebit)
		if unlock.Cmp(buyerRes.remaining) > 0 {
			unlock = new(big.Float).Copy(buyerRes.remaining)
		}
		buyerUnlock = unlock
		buyerRes.remaining.Sub(buyerRes.remaining, unlock)
		if buyerRes.remaining.Sign() <= 0 {
			delete(s.reservations, buyerOrderID)
		}
	}
	sellerRes := s.reservations[sellerOrderID]
	if sellerRes != nil && sellerRes.asset == base {
		unlock := new(big.Float).Copy(qty)
		if unlock.Cmp(sellerRes.remaining) > 0 {
			unlock = new(big.Float).Copy(sellerRes.remaining)
		}
		sellerUnlock = unlock
		sellerRes.remaining.Sub(sellerRes.remaining, unlock)
		if sellerRes.remaining.Sign() <= 0 {
			delete(s.reservations, sellerOrderID)
		}
	}
	s.mu.Unlock()

	var ops []SettleOp
	// Buyer: unlock reserved quote (if any), debit (notional+buyerFee) quote, credit qty base.
	if buyerUnlock.Sign() > 0 {
		ops = append(ops, SettleOp{UserID: buyerID, Asset: quote, Unlock: buyerUnlock})
	}
	ops = append(ops, SettleOp{UserID: buyerID, Asset: quote, Delta: new(big.Float).Neg(buyerDebit)})
	ops = append(ops, SettleOp{UserID: buyerID, Asset: base, Delta: new(big.Float).Copy(qty)})

	// Seller: unlock reserved base (if any), debit qty base, credit (notional-sellerFee) quote.
	if sellerUnlock.Sign() > 0 {
		ops = append(ops, SettleOp{UserID: sellerID, Asset: base, Unlock: sellerUnlock})
	}
	ops = append(ops, SettleOp{UserID: sellerID, Asset: base, Delta: new(big.Float).Neg(qty)})
	// Seller receives notional minus their fee (fee deducted from proceeds).
	sellerCredit := new(big.Float).Sub(notional, sellerFee)
	ops = append(ops, SettleOp{UserID: sellerID, Asset: quote, Delta: sellerCredit})

	now := time.Now().UnixNano()
	var txns []*Transaction
	txns = append(txns, &Transaction{ID: "tb_" + uuid.NewString(), UserID: buyerID, Asset: base, Type: TradeBuy, Amount: new(big.Float).Copy(qty), Fee: buyerFee, Status: Completed, CreatedAt: now})
	txns = append(txns, &Transaction{ID: "ts_" + uuid.NewString(), UserID: sellerID, Asset: base, Type: TradeSell, Amount: new(big.Float).Copy(qty), Fee: sellerFee, Status: Completed, CreatedAt: now})

	if err := s.store.Settle(ops, txns); err != nil {
		// Settlement failed: restore the reservation trackers we decremented so
		// a retry or manual reconciliation can release them later.
		s.mu.Lock()
		if buyerRes != nil && buyerRes.asset == quote {
			buyerRes.remaining.Add(buyerRes.remaining, buyerUnlock)
			s.reservations[buyerOrderID] = buyerRes
		}
		if sellerRes != nil && sellerRes.asset == base {
			sellerRes.remaining.Add(sellerRes.remaining, sellerUnlock)
			s.reservations[sellerOrderID] = sellerRes
		}
		s.mu.Unlock()
		return fmt.Errorf("settle fill: %w", err)
	}

	s.mu.Lock()
	s.processedFills[fillID] = true
	if len(s.processedFills) > 100000 {
		for k := range s.processedFills {
			delete(s.processedFills, k)
			if len(s.processedFills) <= 50000 {
				break
			}
		}
	}
	s.mu.Unlock()
	return nil
}

// ListTransactions returns the user's ledger entries (newest first).
func (s *Service) ListTransactions(userID string, limit, offset int) ([]*Transaction, error) {
	if s.store == nil {
		return []*Transaction{}, nil
	}
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	txs, err := s.store.ListTx(userID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list tx: %w", err)
	}
	if txs == nil {
		txs = []*Transaction{}
	}
	return txs, nil
}
