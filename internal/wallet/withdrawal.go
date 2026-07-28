package wallet

import (
	"errors"
	"fmt"
	"log"
	"math/big"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Withdrawal lifecycle statuses.
const (
	WithdrawalPending    TxStatus = Pending
	WithdrawalReviewing  TxStatus = Reviewing   // manual review / AML check
	WithdrawalApproved   TxStatus = Approved    // ready for broadcast
	WithdrawalBroadcast  TxStatus = Broadcast   // tx sent to network
	WithdrawalConfirming TxStatus = Confirming  // waiting for confirmations
	WithdrawalCompleted  TxStatus = Completed
	WithdrawalRejected   TxStatus = Rejected
	WithdrawalFailed     TxStatus = Failed
)

// AddressBookEntry is a whitelisted withdrawal address for a user.
type AddressBookEntry struct {
	ID        string
	UserID    string
	Asset     string
	Address   string
	Label     string
	CreatedAt int64
}

// WithdrawalLimit is the per-user per-asset daily withdrawal limit.
type WithdrawalLimit struct {
	UserID      string
	Asset       string
	DailyLimit  *big.Float
	WindowHours int
}

// WithdrawalService adds production-grade withdrawal controls on top of the
// wallet Service: address whitelisting, manual review thresholds, daily limits
// and a state-machine lifecycle.
type WithdrawalService struct {
	*Service
	mu sync.RWMutex

	// addressBook maps userID:asset -> list of allowed addresses.
	addressBook map[string][]AddressBookEntry

	// limits maps userID:asset -> limit.
	limits map[string]*WithdrawalLimit

	// dailyWithdrawn maps userID:asset:yyyy-mm-dd -> amount.
	dailyWithdrawn map[string]*big.Float

	// reviewThreshold triggers manual review for withdrawals >= threshold.
	reviewThreshold *big.Float
}

// NewWithdrawalService wraps a wallet Service.
func NewWithdrawalService(svc *Service) *WithdrawalService {
	return &WithdrawalService{
		Service:         svc,
		addressBook:     make(map[string][]AddressBookEntry),
		limits:          make(map[string]*WithdrawalLimit),
		dailyWithdrawn:  make(map[string]*big.Float),
		reviewThreshold: big.NewFloat(10000), // quote-units; override in prod
	}
}

// SetReviewThreshold sets the amount above which a withdrawal requires review.
func (ws *WithdrawalService) SetReviewThreshold(threshold *big.Float) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.reviewThreshold = newBigFloatCopy(threshold)
}

// SetLimit registers a daily withdrawal limit for a user/asset.
func (ws *WithdrawalService) SetLimit(limit *WithdrawalLimit) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	key := limit.UserID + ":" + limit.Asset
	ws.limits[key] = limit
}

// AddAddress whitelists a withdrawal address for a user/asset.
func (ws *WithdrawalService) AddAddress(entry AddressBookEntry) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if entry.ID == "" {
		entry.ID = uuid.NewString()
	}
	if entry.CreatedAt == 0 {
		entry.CreatedAt = time.Now().UnixNano()
	}
	key := entry.UserID + ":" + entry.Asset
	ws.addressBook[key] = append(ws.addressBook[key], entry)
}

// IsWhitelisted reports whether an address is in the user's whitelist.
func (ws *WithdrawalService) IsWhitelisted(userID, asset, address string) bool {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	key := userID + ":" + asset
	for _, e := range ws.addressBook[key] {
		if e.Address == address {
			return true
		}
	}
	return false
}

// ListAddresses returns the whitelisted withdrawal addresses for a user/asset.
func (ws *WithdrawalService) ListAddresses(userID, asset string) []AddressBookEntry {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	key := userID + ":" + asset
	out := make([]AddressBookEntry, len(ws.addressBook[key]))
	copy(out, ws.addressBook[key])
	return out
}

// Withdraw is a convenience wrapper around RequestWithdrawal so that
// *WithdrawalService satisfies the same interface as *Service while enforcing
// the whitelist, limits and review workflow.
func (ws *WithdrawalService) Withdraw(userID, asset, address string, amount *big.Float) error {
	_, err := ws.RequestWithdrawal(userID, asset, address, amount)
	return err
}

// RequestWithdrawal validates, reserves funds and creates a pending withdrawal
// transaction. If amount >= reviewThreshold the status becomes "reviewing".
func (ws *WithdrawalService) RequestWithdrawal(userID, asset, address string, amount *big.Float) (*Transaction, error) {
	if amount == nil || amount.Sign() <= 0 {
		return nil, ErrNegativeAmount
	}
	c := ws.clientFor(asset)
	if c == nil {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedAsset, asset)
	}
	if !c.IsValidAddress(address) {
		return nil, ErrInvalidAddress
	}
	if !ws.IsWhitelisted(userID, asset, address) {
		return nil, errors.New("withdrawal address not whitelisted")
	}
	if err := ws.checkDailyLimit(userID, asset, amount); err != nil {
		return nil, err
	}

	w, err := ws.store.ReserveForOrder(userID, asset, amount)
	if err != nil {
		return nil, err
	}

	status := WithdrawalPending
	if ws.reviewThreshold != nil && ws.reviewThreshold.Sign() > 0 && amount.Cmp(ws.reviewThreshold) >= 0 {
		status = WithdrawalReviewing
	}

	tx := &Transaction{
		ID:        "wd_" + uuid.NewString(),
		UserID:    userID,
		WalletID:  w.ID,
		Type:      Withdrawal,
		Asset:     asset,
		Amount:    new(big.Float).Copy(amount),
		Fee:       big.NewFloat(0),
		Status:    status,
		ToAddress: address,
		CreatedAt: time.Now().UnixNano(),
	}
	if err := ws.store.SaveTx(tx); err != nil {
		// Release the reservation if we cannot record the tx.
		_ = ws.store.Settle([]SettleOp{{UserID: userID, Asset: asset, Unlock: amount}}, nil)
		return nil, fmt.Errorf("save withdrawal: %w", err)
	}
	ws.recordDailyWithdrawn(userID, asset, amount)
	return tx, nil
}

// ApproveWithdrawal moves a reviewing withdrawal to approved. Only call after
// manual KYC/AML review.
func (ws *WithdrawalService) ApproveWithdrawal(txID string) error {
	// In a real system this would update the DB; here we rely on the caller
	// persisting the status change via the store.
	return ws.updateWithdrawalStatus(txID, WithdrawalApproved)
}

// RejectWithdrawal cancels a withdrawal and releases reserved funds.
func (ws *WithdrawalService) RejectWithdrawal(txID string) error {
	tx, err := ws.store.GetTx(txID)
	if err != nil {
		return err
	}
	if tx.Type != Withdrawal {
		return errors.New("not a withdrawal")
	}
	if tx.Status != WithdrawalPending && tx.Status != WithdrawalReviewing {
		return errors.New("withdrawal cannot be rejected")
	}
	if err := ws.store.Settle([]SettleOp{{UserID: tx.UserID, Asset: tx.Asset, Unlock: tx.Amount}}, nil); err != nil {
		return fmt.Errorf("release withdrawal funds: %w", err)
	}
	return ws.updateWithdrawalStatus(txID, WithdrawalRejected)
}

// GetWithdrawal loads a single withdrawal transaction.
func (ws *WithdrawalService) GetWithdrawal(txID string) (*Transaction, error) {
	return ws.store.GetTx(txID)
}

// ListByStatus returns withdrawals with the given status.
func (ws *WithdrawalService) ListByStatus(status TxStatus, limit, offset int) ([]*Transaction, error) {
	return ws.store.ListTxByStatus(status, limit, offset)
}

// ListPendingWithdrawals returns withdrawals awaiting manual review or broadcast.
func (ws *WithdrawalService) ListPendingWithdrawals(limit int) ([]*Transaction, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	pending, err := ws.store.ListTxByStatus(WithdrawalPending, limit, 0)
	if err != nil {
		return nil, err
	}
	reviewing, err := ws.store.ListTxByStatus(WithdrawalReviewing, limit, 0)
	if err != nil {
		return nil, err
	}
	approved, err := ws.store.ListTxByStatus(WithdrawalApproved, limit, 0)
	if err != nil {
		return nil, err
	}
	broadcast, err := ws.store.ListTxByStatus(WithdrawalBroadcast, limit, 0)
	if err != nil {
		return nil, err
	}
	out := make([]*Transaction, 0, len(pending)+len(reviewing)+len(approved)+len(broadcast))
	out = append(out, pending...)
	out = append(out, reviewing...)
	out = append(out, approved...)
	out = append(out, broadcast...)
	return out, nil
}

// ListUserWithdrawals returns withdrawals for a specific user.
func (ws *WithdrawalService) ListUserWithdrawals(userID string, limit, offset int) ([]*Transaction, error) {
	all, err := ws.store.ListTx(userID, limit, offset)
	if err != nil {
		return nil, err
	}
	out := make([]*Transaction, 0, len(all))
	for _, t := range all {
		if t.Type == Withdrawal {
			out = append(out, t)
		}
	}
	return out, nil
}

// ProcessApprovedWithdrawals broadcasts all approved withdrawals on-chain.
func (ws *WithdrawalService) ProcessApprovedWithdrawals(limit int) error {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	txs, err := ws.store.ListTxByStatus(WithdrawalApproved, limit, 0)
	if err != nil {
		return err
	}
	for _, tx := range txs {
		if err := ws.BroadcastWithdrawal(tx.ID); err != nil {
			log.Printf("[withdrawal] broadcast %s failed: %v", tx.ID, err)
		}
	}
	return nil
}

// BroadcastWithdrawal sends an approved withdrawal to the network and records
// the transaction hash.
func (ws *WithdrawalService) BroadcastWithdrawal(txID string) error {
	tx, err := ws.store.GetTx(txID)
	if err != nil {
		return err
	}
	if tx.Type != Withdrawal {
		return errors.New("not a withdrawal")
	}
	if tx.Status != WithdrawalApproved {
		return errors.New("withdrawal not approved")
	}
	c := ws.clientFor(tx.Asset)
	if c == nil {
		return fmt.Errorf("%w: %s", ErrUnsupportedAsset, tx.Asset)
	}
	txHash, err := c.SendTransaction(tx.ToAddress, tx.Amount)
	if err != nil {
		return fmt.Errorf("broadcast: %w", err)
	}
	tx.TxHash = txHash
	tx.Status = WithdrawalBroadcast
	return ws.store.SaveTx(tx)
}

// ProcessBroadcastWithdrawals checks confirmations for broadcast withdrawals and
// finalises them when the confirmation threshold is reached.
func (ws *WithdrawalService) ProcessBroadcastWithdrawals(limit int) error {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	txs, err := ws.store.ListTxByStatus(WithdrawalBroadcast, limit, 0)
	if err != nil {
		return err
	}
	for _, tx := range txs {
		if err := ws.ConfirmWithdrawal(tx.ID); err != nil {
			log.Printf("[withdrawal] confirm %s failed: %v", tx.ID, err)
		}
	}
	return nil
}

// ConfirmWithdrawal checks on-chain confirmations and finalises the withdrawal.
func (ws *WithdrawalService) ConfirmWithdrawal(txID string) error {
	tx, err := ws.store.GetTx(txID)
	if err != nil {
		return err
	}
	if tx.Type != Withdrawal {
		return errors.New("not a withdrawal")
	}
	if tx.Status != WithdrawalBroadcast {
		return errors.New("withdrawal not broadcast")
	}
	c := ws.clientFor(tx.Asset)
	if c == nil {
		return fmt.Errorf("%w: %s", ErrUnsupportedAsset, tx.Asset)
	}
	conf, err := c.GetConfirmations(tx.TxHash)
	if err != nil {
		return fmt.Errorf("confirmations: %w", err)
	}
	threshold := ws.confThreshold(tx.Asset)
	if conf < threshold {
		return nil // wait for more confirmations
	}
	// Finalise: move locked funds out of the wallet permanently.
	ops := []SettleOp{
		{UserID: tx.UserID, Asset: tx.Asset, Unlock: tx.Amount, Delta: new(big.Float).Neg(tx.Amount)},
	}
	if err := ws.store.Settle(ops, nil); err != nil {
		return fmt.Errorf("finalise withdrawal: %w", err)
	}
	tx.Confirmations = conf
	tx.Status = WithdrawalCompleted
	return ws.store.SaveTx(tx)
}

func (ws *WithdrawalService) confThreshold(asset string) int {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	if threshold, ok := ws.confThresholds[asset]; ok {
		return threshold
	}
	return 6
}

func (ws *WithdrawalService) updateWithdrawalStatus(txID string, status TxStatus) error {
	return ws.store.UpdateTxStatus(txID, status)
}

func (ws *WithdrawalService) checkDailyLimit(userID, asset string, amount *big.Float) error {
	ws.mu.RLock()
	limit, ok := ws.limits[userID+":"+asset]
	ws.mu.RUnlock()
	if !ok || limit == nil || limit.DailyLimit == nil || limit.DailyLimit.Sign() <= 0 {
		return nil
	}
	today := time.Now().UTC().Format("2006-01-02")
	ws.mu.Lock()
	used := ws.dailyWithdrawn[userID+":"+asset+":"+today]
	if used == nil {
		used = big.NewFloat(0)
	}
	newTotal := new(big.Float).Add(used, amount)
	ws.mu.Unlock()
	if newTotal.Cmp(limit.DailyLimit) > 0 {
		return errors.New("daily withdrawal limit exceeded")
	}
	return nil
}

func (ws *WithdrawalService) recordDailyWithdrawn(userID, asset string, amount *big.Float) {
	today := time.Now().UTC().Format("2006-01-02")
	key := userID + ":" + asset + ":" + today
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if ws.dailyWithdrawn[key] == nil {
		ws.dailyWithdrawn[key] = big.NewFloat(0)
	}
	ws.dailyWithdrawn[key].Add(ws.dailyWithdrawn[key], amount)
}

func newBigFloatCopy(f *big.Float) *big.Float {
	if f == nil {
		return nil
	}
	x := new(big.Float)
	x.SetPrec(128)
	x.Set(f)
	return x
}
