package wallet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
	// ErrDailyLimitExceeded is returned when a withdrawal would push the
	// user's USDT-equivalent daily usage past the KYC-tier limit.
	ErrDailyLimitExceeded = errors.New("daily withdrawal limit exceeded")
	// ErrInvalidAccount is returned when a transfer references an account
	// type outside {spot, futures, funding}.
	ErrInvalidAccount = errors.New("invalid account")
	// ErrSameAccountTransfer is returned when a transfer's source and
	// destination accounts are identical.
	ErrSameAccountTransfer = errors.New("source and destination accounts must differ")
)

const (
	// defaultProcessedFillCapacity bounds the processed-fill dedup cache.
	defaultProcessedFillCapacity = 100000
	// defaultProcessedFillTTL expires dedup records after 7 days; a fill
	// replayed after this window is treated as new (the ledger/DB layer
	// remains the ultimate source of truth).
	defaultProcessedFillTTL = 7 * 24 * time.Hour
	// defaultReservationTTL bounds L2 reservation records; reservations are
	// rewritten on every mutation, so live orders never expire in practice.
	defaultReservationTTL = 30 * 24 * time.Hour
)

// WalletStore is the persistence interface for wallet balances, locks and the
// ledger of transactions. Implementations must make the Settle / Reserve /
// Release operations atomic at the database level (single transaction with row
// locking) to prevent race conditions and double-spends.
type WalletStore interface {
	GetWallet(userID, asset string) (*Wallet, error)
	GetWallets(userID string) ([]*Wallet, error)
	// SaveWallet inserts a new wallet row.
	SaveWallet(*Wallet) error
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
	// UpdateTxStatus updates the status of a transaction.
	UpdateTxStatus(id string, status TxStatus) error
	// UpdateTxStatusFrom atomically flips a transaction's status only when
	// the current status is one of from. It reports whether the update was
	// applied (false = the row no longer has an acceptable status, i.e. a
	// concurrent or repeated review), which makes review actions idempotent.
	UpdateTxStatusFrom(id string, from []TxStatus, to TxStatus) (bool, error)
	// ListTxByStatus returns transactions filtered by status.
	ListTxByStatus(status TxStatus, limit, offset int) ([]*Transaction, error)

	// ReserveForOrder atomically checks that the wallet has enough available
	// (balance - locked) balance and locks amt. Returns ErrInsufficientBalance
	// if not. This closes the TOCTOU window between the availability check and
	// the lock. Operates on the spot account.
	ReserveForOrder(userID, asset string, amt *big.Float) (*Wallet, error)
	// GetWalletForAccount returns the wallet row for one account type
	// (spot/futures/funding).
	GetWalletForAccount(userID, asset, accountType string) (*Wallet, error)
	// ReserveForAccount is ReserveForOrder scoped to one account type; the
	// wallet row is created on first use so futures margin can be locked
	// before the user ever touched that account.
	ReserveForAccount(userID, asset, accountType string, amt *big.Float) (*Wallet, error)
	// Settle applies a set of atomic (unlock + balance delta + optional lock)
	// operations across multiple wallets in a single database transaction, and
	// records the provided ledger entries. Either all operations succeed or
	// none do.
	Settle(ops []SettleOp, txns []*Transaction) error
}

// SettleOp describes one atomic wallet mutation performed as part of a trade
// settlement or order release. Exactly one wallet (identified by
// UserID+Asset+AccountType) is touched per op.
type SettleOp struct {
	UserID string
	Asset  string
	// AccountType selects the account (spot/futures/funding). Empty means
	// the spot account, keeping pre-account-dimension callers unchanged.
	AccountType string
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
	// It is a bounded LRU (container/list + map): eviction removes the
	// least-recently-accessed entries, never recent ones, and entries older
	// than the TTL expire. Guarded by mu.
	processedFills *fillLRU
	// reservations tracks per-order reserved funds so that cancellations and
	// price-improvement leftovers can be released precisely. Keyed by order ID.
	// In-memory (L1); when a shared store is injected the records are written
	// through to L2 for cross-instance consistency (plan 6.2).
	reservations map[string]*reservation

	// shared is the optional L2 store for cross-instance consistency
	// (plan 6.2). nil = pure-local behaviour.
	shared RedisLike
	// sharedWarned ensures the L2 degradation warning is logged only once.
	sharedWarned bool
	// instanceID identifies this process in L2 fill claims.
	instanceID string
}

// reservation records the asset and remaining locked amount for one order.
type reservation struct {
	asset     string
	remaining *big.Float
}

// sharedReservation is the JSON wire format of a reservation in the L2 store.
type sharedReservation struct {
	Asset     string     `json:"asset"`
	Remaining *big.Float `json:"remaining"`
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
	s := &Service{
		store:          store,
		clients:        clients,
		confThresholds: map[string]int{"BTC": 12, "ETH": 24, "POLYGON": 24},
		fees:           fees,
		processedFills: newFillLRU(defaultProcessedFillCapacity, defaultProcessedFillTTL),
		reservations:   make(map[string]*reservation),
		instanceID:     uuid.NewString(),
	}
	// Package-level env wiring (plan 6.2): if WALLET_SHARED_STORE_ADDR is set,
	// the L2 store attaches automatically; cmd/ needs no change. Explicit
	// SetSharedStore calls still override this.
	if rs, err := SharedStoreFromEnv(); err != nil {
		slog.Warn("wallet shared store env config invalid, staying local-only", "err", err)
	} else if rs != nil {
		s.shared = rs
		slog.Info("wallet L2 shared store attached", "addr", EnvSharedStoreAddr)
	}
	return s
}

// SetSharedStore injects the optional L2 shared store used for cross-instance
// consistency of processed fills and reservations (horizontal scaling, plan
// 6.2). Pass nil to return to pure-local behaviour. Safe for concurrent use;
// intended to be called once at startup:
//
//	rs, _ := wallet.SharedStoreFromEnv() // WALLET_SHARED_STORE_ADDR=...
//	walletSvc.SetSharedStore(rs)
func (s *Service) SetSharedStore(rs RedisLike) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shared = rs
	s.sharedWarned = false
}

// SetProcessedFillCapacity overrides the capacity of the processed-fill
// dedup cache. Least-recently-accessed entries are evicted when the new
// capacity is exceeded. Safe for concurrent use.
func (s *Service) SetProcessedFillCapacity(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.processedFills.setCapacity(n)
}

// SetProcessedFillTTL overrides the expiry window of processed-fill dedup
// records (d <= 0 disables time-based expiry). Safe for concurrent use.
func (s *Service) SetProcessedFillTTL(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.processedFills.setTTL(d)
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

// GetBalanceForAccount returns the user's wallet for an asset inside one
// account (spot/futures/funding). An empty accountType resolves to spot.
func (s *Service) GetBalanceForAccount(userID, asset, accountType string) (*Wallet, error) {
	if s.store == nil {
		return nil, ErrWalletNotFound
	}
	accountType = NormalizeAccountType(accountType)
	if !ValidAccountType(accountType) {
		return nil, ErrInvalidAccount
	}
	w, err := s.store.GetWalletForAccount(userID, asset, accountType)
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
		if err := s.store.SaveWallet(w); err != nil {
			return fmt.Errorf("create wallet: %w", err)
		}
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
	// Strict chain-specific format validation applies regardless of the
	// underlying client implementation (including the development mock).
	if err := ValidateWithdrawalAddress(asset, address); err != nil {
		return err
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
	// L2 write-through so another instance can release this reservation.
	s.syncReservationsL2(orderID)
	return nil
}

// ReleaseOrder releases any remaining reserved funds for an order (e.g. on
// cancel, or after the order has fully filled). It is a no-op if the order had
// no reservation (e.g. a market buy). With an L2 store attached, reservations
// created on another instance are loaded from L2, so any instance can release
// any order (horizontal scaling, plan 6.2).
func (s *Service) ReleaseOrder(orderID, userID string) error {
	if s.store == nil {
		return nil
	}
	s.mu.Lock()
	r, ok := s.reservations[orderID]
	if ok {
		delete(s.reservations, orderID)
	}
	shared := s.shared
	s.mu.Unlock()
	if !ok && shared != nil {
		// Cross-instance release: the reservation may exist only in L2.
		if loaded, err := loadReservationL2(shared, orderID); err != nil {
			s.warnSharedOnce(fmt.Errorf("load reservation %s from L2: %w", orderID, err))
		} else if loaded != nil {
			r, ok = loaded, true
		}
	}
	if !ok || r == nil || r.remaining.Sign() <= 0 {
		return nil
	}
	// Use the atomic Settle path with a single unlock op (no balance delta) so
	// the release is consistent with how settlements mutate locked balances.
	ops := []SettleOp{{UserID: userID, Asset: r.asset, Unlock: r.remaining}}
	if err := s.store.Settle(ops, nil); err != nil {
		return err
	}
	// Release committed: drop the L2 record (best-effort).
	if shared != nil {
		if err := shared.Del(context.Background(), sharedResPrefix+orderID); err != nil && !errors.Is(err, ErrSharedKeyMissing) {
			s.warnSharedOnce(fmt.Errorf("del reservation %s from L2: %w", orderID, err))
		}
	}
	return nil
}

// loadReservationL2 fetches one reservation record from the L2 store. It
// returns (nil, nil) when the key does not exist.
func loadReservationL2(rs RedisLike, orderID string) (*reservation, error) {
	data, err := rs.Get(context.Background(), sharedResPrefix+orderID)
	if err != nil {
		if errors.Is(err, ErrSharedKeyMissing) {
			return nil, nil
		}
		return nil, err
	}
	var sr sharedReservation
	if err := json.Unmarshal([]byte(data), &sr); err != nil || sr.Remaining == nil || sr.Asset == "" {
		return nil, nil // corrupt record: treat as absent rather than fail
	}
	return &reservation{asset: sr.Asset, remaining: sr.Remaining}, nil
}

// syncReservationsL2 writes the current state of the given order reservations
// through to the L2 store. Deleted/exhausted reservations are removed. All
// failures are best-effort: a single warning is logged and local behaviour
// continues unchanged (graceful degradation, plan 6.2).
func (s *Service) syncReservationsL2(orderIDs ...string) {
	s.mu.Lock()
	shared := s.shared
	type snap struct {
		id     string
		res    *reservation
		exists bool
	}
	snaps := make([]snap, 0, len(orderIDs))
	for _, id := range orderIDs {
		if r, ok := s.reservations[id]; ok && r != nil {
			snaps = append(snaps, snap{id, &reservation{asset: r.asset, remaining: new(big.Float).Copy(r.remaining)}, true})
		} else {
			snaps = append(snaps, snap{id, nil, false})
		}
	}
	s.mu.Unlock()
	if shared == nil {
		return
	}
	ctx := context.Background()
	for _, sn := range snaps {
		if !sn.exists {
			if err := shared.Del(ctx, sharedResPrefix+sn.id); err != nil && !errors.Is(err, ErrSharedKeyMissing) {
				s.warnSharedOnce(fmt.Errorf("del reservation %s: %w", sn.id, err))
			}
			continue
		}
		data, err := json.Marshal(sharedReservation{Asset: sn.res.asset, Remaining: sn.res.remaining})
		if err != nil {
			continue
		}
		if err := shared.Set(ctx, sharedResPrefix+sn.id, string(data), defaultReservationTTL); err != nil {
			s.warnSharedOnce(fmt.Errorf("set reservation %s: %w", sn.id, err))
		}
	}
}

// warnSharedOnce logs the L2 degradation warning exactly once per attachment.
func (s *Service) warnSharedOnce(err error) {
	s.mu.Lock()
	if s.sharedWarned {
		s.mu.Unlock()
		return
	}
	s.sharedWarned = true
	s.mu.Unlock()
	slog.Warn("wallet L2 shared store unavailable, degrading to local-only consistency", "err", err)
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

	// Idempotency on fillID: L1 (local LRU) is the fast path; when an L2
	// shared store is attached, SetNX acts as the atomic cross-instance claim
	// so two instances consuming the same fill settle it exactly once.
	s.mu.Lock()
	if s.processedFills.contains(fillID) {
		s.mu.Unlock()
		return nil
	}
	shared := s.shared
	s.mu.Unlock()

	claimedL2 := false
	if shared != nil {
		ok, err := shared.SetNX(context.Background(), sharedFillPrefix+fillID, s.instanceID, defaultProcessedFillTTL)
		switch {
		case err != nil:
			// Degradation: proceed with local-only dedup (warning logged once).
			s.warnSharedOnce(fmt.Errorf("claim fill %s in L2: %w", fillID, err))
		case !ok:
			// Another instance already claimed this fill: dedup no-op.
			s.mu.Lock()
			s.processedFills.add(fillID)
			s.mu.Unlock()
			return nil
		default:
			claimedL2 = true
		}
	}

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
	// Cross-instance fills: hydrate reservations this instance never saw from
	// the L2 store first.
	if shared != nil {
		for _, oid := range []string{buyerOrderID, sellerOrderID} {
			s.mu.Lock()
			_, local := s.reservations[oid]
			s.mu.Unlock()
			if local {
				continue
			}
			r, err := loadReservationL2(shared, oid)
			if err != nil {
				s.warnSharedOnce(fmt.Errorf("hydrate reservation %s from L2: %w", oid, err))
				continue
			}
			if r != nil {
				s.mu.Lock()
				if _, dup := s.reservations[oid]; !dup {
					s.reservations[oid] = r
				}
				s.mu.Unlock()
			}
		}
	}
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
	// Write the updated reservation state through to L2 (best-effort).
	s.syncReservationsL2(buyerOrderID, sellerOrderID)

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
		s.syncReservationsL2(buyerOrderID, sellerOrderID)
		if claimedL2 {
			// Release the L2 claim so another instance (or a retry) can settle.
			if err := shared.Del(context.Background(), sharedFillPrefix+fillID); err != nil && !errors.Is(err, ErrSharedKeyMissing) {
				s.warnSharedOnce(fmt.Errorf("release L2 claim for fill %s: %w", fillID, err))
			}
		}
		return fmt.Errorf("settle fill: %w", err)
	}

	s.mu.Lock()
	s.processedFills.add(fillID)
	s.mu.Unlock()
	return nil
}

// ReserveForOrder atomically checks available balance and locks amt for an
// off-exchange position or AMM trade. Thin wrapper over WalletStore; returns
// ErrInsufficientBalance if funds are unavailable. If there is no store, the
// reservation succeeds in best-effort mode.
func (s *Service) ReserveForOrder(userID, asset string, amt *big.Float) (*Wallet, error) {
	if s.store == nil {
		return &Wallet{UserID: userID, Asset: asset, Balance: big.NewFloat(0), Locked: big.NewFloat(0)}, nil
	}
	if amt == nil || amt.Sign() <= 0 {
		return nil, ErrNegativeAmount
	}
	return s.store.ReserveForOrder(userID, asset, amt)
}

// ReserveForAccount is ReserveForOrder scoped to one account
// (spot/futures/funding). Used by the futures engine to lock margin inside
// the futures account instead of the spot wallet.
func (s *Service) ReserveForAccount(userID, asset, accountType string, amt *big.Float) (*Wallet, error) {
	accountType = NormalizeAccountType(accountType)
	if !ValidAccountType(accountType) {
		return nil, ErrInvalidAccount
	}
	if s.store == nil {
		return &Wallet{UserID: userID, Asset: asset, AccountType: accountType, Balance: big.NewFloat(0), Locked: big.NewFloat(0)}, nil
	}
	if amt == nil || amt.Sign() <= 0 {
		return nil, ErrNegativeAmount
	}
	return s.store.ReserveForAccount(userID, asset, accountType, amt)
}

// Transfer atomically moves amount of asset between two of the user's
// internal accounts (spot / futures / funding). Both legs run inside one
// Settle transaction: the source account is debited and the destination
// credited, and one type=Transfer ledger entry is written per leg (debit
// negative, credit positive). Transfers never touch the withdrawal daily
// limit meter. Returns ErrInvalidAccount, ErrSameAccountTransfer,
// ErrNegativeAmount, ErrUnsupportedAsset or ErrInsufficientBalance.
func (s *Service) Transfer(userID, from, to, asset string, amount *big.Float) error {
	if s.store == nil {
		return ErrWalletNotFound
	}
	if !ValidAccountType(from) || !ValidAccountType(to) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidAccount, from, to)
	}
	if from == to {
		return ErrSameAccountTransfer
	}
	if strings.TrimSpace(asset) == "" {
		return fmt.Errorf("%w: empty asset", ErrUnsupportedAsset)
	}
	if amount == nil || amount.Sign() <= 0 {
		return ErrNegativeAmount
	}
	now := time.Now().UnixNano()
	debit := new(big.Float).Neg(amount)
	credit := new(big.Float).Copy(amount)
	ops := []SettleOp{
		// Debit first: on insufficient balance the whole batch rolls back
		// before the credit leg could commit anywhere.
		{UserID: userID, Asset: asset, AccountType: from, Delta: debit},
		{UserID: userID, Asset: asset, AccountType: to, Delta: credit},
	}
	txns := []*Transaction{
		{ID: "tf_" + uuid.NewString(), UserID: userID, Asset: asset, AccountType: from, Type: Transfer, Amount: new(big.Float).Copy(debit), Fee: big.NewFloat(0), Status: Completed, CreatedAt: now},
		{ID: "tf_" + uuid.NewString(), UserID: userID, Asset: asset, AccountType: to, Type: Transfer, Amount: credit, Fee: big.NewFloat(0), Status: Completed, CreatedAt: now},
	}
	return s.store.Settle(ops, txns)
}

// Settle applies a batch of atomic wallet operations (unlock + balance delta)
// and records ledger entries. Thin wrapper over WalletStore.
func (s *Service) Settle(ops []SettleOp, txns []*Transaction) error {
	if s.store == nil {
		return nil
	}
	return s.store.Settle(ops, txns)
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
