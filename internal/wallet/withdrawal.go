package wallet

import (
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Withdrawal lifecycle statuses.
const (
	WithdrawalPending    TxStatus = Pending
	WithdrawalReviewing  TxStatus = Reviewing  // manual review / AML check
	WithdrawalApproved   TxStatus = Approved   // ready for broadcast
	WithdrawalBroadcast  TxStatus = Broadcast  // tx sent to network
	WithdrawalConfirming TxStatus = Confirming // waiting for confirmations
	WithdrawalCompleted  TxStatus = Completed
	WithdrawalRejected   TxStatus = Rejected
	WithdrawalFailed     TxStatus = Failed

	// Cold wallet separation statuses (plan 6.1): large withdrawals queued to
	// the offline signer, then picked up signed for broadcast.
	WithdrawalColdSigning TxStatus = ColdSigning
	WithdrawalColdSigned  TxStatus = ColdSigned
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

// PriceGetter resolves a pair's market price. BestPrice is implemented by the
// api.PriceHandler; the withdrawal flow uses it to fold every asset into a
// USDT-equivalent amount for the KYC-tier daily limit.
type PriceGetter interface {
	BestPrice(pair string) (*big.Float, string, error)
}

// KycLevelLookup returns a user's KYC verification level.
type KycLevelLookup interface {
	KycLevel(userID string) (int, error)
}

// PlatformLimitLoader loads the KYC-tier daily limits (platform_limits).
type PlatformLimitLoader interface {
	LoadPlatformLimits() (map[int]*big.Float, error)
}

// dailyLimitStore is the optional store capability for atomic fund-reservation
// plus daily-usage accounting. Stores without it (e.g. the in-memory test
// store) fall back to the legacy two-step path.
type dailyLimitStore interface {
	ReserveWithDailyLimit(userID, asset string, amount, usdtEquiv, limit *big.Float) (*Wallet, error)
	ReleaseDailyUsage(userID, asset string, usdtEquiv *big.Float) error
}

// defaultDailyLimitUSDT applies when no platform_limits row matches the
// user's KYC level (including a missing table/level 0 row).
const defaultDailyLimitUSDT = 1000

// stableAssets count 1:1 towards the USDT-equivalent daily meter.
var stableAssets = map[string]bool{"USDT": true, "USDC": true, "DAI": true, "BUSD": true, "TUSD": true}

type kycCacheEntry struct {
	level   int
	expires time.Time
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

	// Cold wallet separation (plan 6.1): withdrawals >= the per-asset cold
	// threshold are queued to coldSigner instead of being broadcast from the
	// hot wallet. A nil coldSigner disables the cold flow entirely (legacy
	// behaviour), in which case large withdrawals follow the hot path — set a
	// signer in production.
	coldSigner ColdSigner
	coldPolicy *ColdWalletPolicy
	// coldFeeStrategies maps asset -> fee strategy hint embedded in the
	// unsigned tx description (the offline signer applies its own limits).
	coldFeeStrategies map[string]string

	// KYC-tiered daily withdrawal limits. priceGetter folds assets into
	// USDT-equivalent; kycLookup resolves the user's tier (cached 60s,
	// invalidated on admin review); platformLimits is the tier table loaded
	// at boot and reloadable after admin changes.
	priceGetter    PriceGetter
	kycLookup      KycLevelLookup
	limitLoader    PlatformLimitLoader
	platformLimits map[int]*big.Float
	kycCache       map[string]kycCacheEntry
}

// NewWithdrawalService wraps a wallet Service.
func NewWithdrawalService(svc *Service) *WithdrawalService {
	return &WithdrawalService{
		Service:           svc,
		addressBook:       make(map[string][]AddressBookEntry),
		limits:            make(map[string]*WithdrawalLimit),
		dailyWithdrawn:    make(map[string]*big.Float),
		reviewThreshold:   big.NewFloat(10000), // quote-units; override in prod
		coldFeeStrategies: map[string]string{"BTC": "satvbyte=auto", "ETH": "eip1559", "POLYGON": "eip1559"},
		platformLimits:    make(map[int]*big.Float),
		kycCache:          make(map[string]kycCacheEntry),
	}
}

// SetPriceGetter wires the market-price source used to fold withdrawal
// amounts into USDT-equivalent for the daily limit. When unset, withdrawals
// fall back to the legacy per-user in-memory limit path.
func (ws *WithdrawalService) SetPriceGetter(pg PriceGetter) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.priceGetter = pg
}

// SetKycLevelLookup wires the user KYC-level lookup (cached for 60s).
func (ws *WithdrawalService) SetKycLevelLookup(l KycLevelLookup) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.kycLookup = l
}

// SetPlatformLimitLoader wires the platform_limits loader without loading.
func (ws *WithdrawalService) SetPlatformLimitLoader(l PlatformLimitLoader) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.limitLoader = l
}

// ReloadPlatformLimits refreshes the in-memory KYC-tier limit table from the
// store (boot-time and after admin changes).
func (ws *WithdrawalService) ReloadPlatformLimits() error {
	ws.mu.RLock()
	loader := ws.limitLoader
	ws.mu.RUnlock()
	if loader == nil {
		return nil
	}
	limits, err := loader.LoadPlatformLimits()
	if err != nil {
		return err
	}
	if limits == nil {
		limits = make(map[int]*big.Float)
	}
	ws.mu.Lock()
	ws.platformLimits = limits
	ws.mu.Unlock()
	return nil
}

// InvalidateKycLevelCache drops one user's cached KYC level (called when an
// admin approves a KYC submission so the new tier applies immediately).
func (ws *WithdrawalService) InvalidateKycLevelCache(userID string) {
	ws.mu.Lock()
	delete(ws.kycCache, userID)
	ws.mu.Unlock()
}

// SetColdSigner enables hot/cold wallet separation. Withdrawals whose amount
// reaches the policy threshold for their asset are queued to the cold signer
// after approval instead of being broadcast from the hot wallet. Pass a nil
// policy to use the default (env-overridable) policy.
func (ws *WithdrawalService) SetColdSigner(signer ColdSigner, policy *ColdWalletPolicy) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.coldSigner = signer
	ws.coldPolicy = policy
	if ws.coldPolicy == nil {
		ws.coldPolicy = ColdWalletPolicyFromEnv()
	}
}

// SetColdFeeStrategy overrides the fee-strategy hint embedded in unsigned
// cold tx descriptions for an asset.
func (ws *WithdrawalService) SetColdFeeStrategy(asset, strategy string) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.coldFeeStrategies[strings.ToUpper(asset)] = strategy
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
	// Strict chain-specific format validation applies regardless of the
	// underlying client implementation (including the development mock).
	if err := ValidateWithdrawalAddress(asset, address); err != nil {
		return nil, err
	}
	if !c.IsValidAddress(address) {
		return nil, ErrInvalidAddress
	}
	if !ws.IsWhitelisted(userID, asset, address) {
		return nil, errors.New("withdrawal address not whitelisted")
	}

	// Fund reservation + daily-limit accounting. When the store supports it
	// and a price source is wired, both happen in ONE transaction against the
	// USDT-equivalent amount and the user's KYC-tier limit (no TOCTOU).
	// Otherwise the legacy in-memory limit path applies.
	var usdtEq *big.Float
	atomicUsage := false
	ds, dsOK := ws.store.(dailyLimitStore)
	ws.mu.RLock()
	pg := ws.priceGetter
	ws.mu.RUnlock()
	var w *Wallet
	if dsOK && pg != nil {
		limit := ws.resolveDailyLimitUSDT(userID, asset)
		eq, err := ws.usdtEquivalent(asset, amount)
		if err != nil {
			// Fail-closed: without a trustworthy price we cannot compare
			// against the USDT limit, so refuse the withdrawal.
			return nil, fmt.Errorf("withdrawal refused, price unavailable (fail-closed): %w", err)
		}
		w, err = ds.ReserveWithDailyLimit(userID, asset, amount, eq, limit)
		if err != nil {
			return nil, err
		}
		usdtEq = eq
		atomicUsage = true
	} else {
		if err := ws.checkDailyLimit(userID, asset, amount); err != nil {
			return nil, err
		}
		var err error
		w, err = ws.store.ReserveForOrder(userID, asset, amount)
		if err != nil {
			return nil, err
		}
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
		if atomicUsage && usdtEq != nil {
			if rErr := ds.ReleaseDailyUsage(userID, asset, usdtEq); rErr != nil {
				slog.Warn("withdrawal daily-usage release failed", "user_id", userID, "asset", asset, "err", rErr)
			}
		}
		return nil, fmt.Errorf("save withdrawal: %w", err)
	}
	if !atomicUsage {
		ws.recordDailyWithdrawn(userID, asset, amount)
	}
	return tx, nil
}

// Review-lifecycle errors surfaced to the admin API. They make repeated
// approve/reject calls idempotent-friendly: instead of silently re-applying
// a decision, the caller gets a clear, actionable message.
var (
	ErrWithdrawalAlreadyApproved = errors.New("withdrawal already approved")
	ErrWithdrawalAlreadyRejected = errors.New("withdrawal already rejected")
	ErrWithdrawalNotApprovable   = errors.New("withdrawal cannot be approved in its current state")
	ErrWithdrawalNotRejectable   = errors.New("withdrawal cannot be rejected in its current state")
)

// ApproveWithdrawal moves a pending/reviewing withdrawal to approved.
// Only call after manual KYC/AML review. Idempotent-safe: a tx that has
// already been reviewed (or is beyond review) yields a descriptive error
// instead of a silent re-apply.
func (ws *WithdrawalService) ApproveWithdrawal(txID string) error {
	tx, err := ws.store.GetTx(txID)
	if err != nil {
		return fmt.Errorf("withdrawal %s not found", txID)
	}
	if tx.Type != Withdrawal {
		return errors.New("not a withdrawal")
	}
	switch tx.Status {
	case WithdrawalPending, WithdrawalReviewing:
		// Reviewable.
	case WithdrawalApproved, WithdrawalBroadcast, WithdrawalConfirming,
		WithdrawalCompleted, WithdrawalColdSigning, WithdrawalColdSigned:
		return ErrWithdrawalAlreadyApproved
	case WithdrawalRejected:
		return ErrWithdrawalAlreadyRejected
	default:
		return ErrWithdrawalNotApprovable
	}
	// Conditional flip: only applies while the row is still pending/reviewing,
	// so concurrent admins or retries cannot double-approve.
	ok, err := ws.store.UpdateTxStatusFrom(txID, []TxStatus{WithdrawalPending, WithdrawalReviewing}, WithdrawalApproved)
	if err != nil {
		return err
	}
	if !ok {
		return ErrWithdrawalNotApprovable
	}
	return nil
}

// RejectWithdrawal cancels a withdrawal and releases reserved funds. The
// status flip is conditional and happens BEFORE the fund release, so the
// unlock runs exactly once even under concurrent/duplicate calls; if the
// release itself fails the status is rolled back so the admin can retry.
func (ws *WithdrawalService) RejectWithdrawal(txID string) error {
	tx, err := ws.store.GetTx(txID)
	if err != nil {
		return fmt.Errorf("withdrawal %s not found", txID)
	}
	if tx.Type != Withdrawal {
		return errors.New("not a withdrawal")
	}
	if tx.Status == WithdrawalRejected {
		return ErrWithdrawalAlreadyRejected
	}
	if tx.Status != WithdrawalPending && tx.Status != WithdrawalReviewing && tx.Status != WithdrawalColdSigning {
		return ErrWithdrawalNotRejectable
	}
	prevStatus := tx.Status
	ok, err := ws.store.UpdateTxStatusFrom(txID,
		[]TxStatus{WithdrawalPending, WithdrawalReviewing, WithdrawalColdSigning}, WithdrawalRejected)
	if err != nil {
		return err
	}
	if !ok {
		return ErrWithdrawalNotRejectable
	}
	if err := ws.store.Settle([]SettleOp{{UserID: tx.UserID, Asset: tx.Asset, Unlock: tx.Amount}}, nil); err != nil {
		// Roll the status back: the funds are still locked and the admin
		// must be able to retry the rejection.
		if rbErr := ws.store.UpdateTxStatus(txID, prevStatus); rbErr != nil {
			slog.Error("withdrawal reject rollback failed", "tx_id", txID, "err", rbErr)
		}
		return fmt.Errorf("release withdrawal funds: %w", err)
	}
	// Credit the daily-usage meter back (best effort; the meter resets daily).
	ws.releaseDailyUsageBestEffort(tx.UserID, tx.Asset, tx.Amount)
	return nil
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
	coldSigning, err := ws.store.ListTxByStatus(WithdrawalColdSigning, limit, 0)
	if err != nil {
		return nil, err
	}
	coldSigned, err := ws.store.ListTxByStatus(WithdrawalColdSigned, limit, 0)
	if err != nil {
		return nil, err
	}
	out := make([]*Transaction, 0, len(pending)+len(reviewing)+len(approved)+len(broadcast)+len(coldSigning)+len(coldSigned))
	out = append(out, pending...)
	out = append(out, reviewing...)
	out = append(out, approved...)
	out = append(out, broadcast...)
	out = append(out, coldSigning...)
	out = append(out, coldSigned...)
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
			slog.Warn("withdrawal broadcast failed", "tx_id", tx.ID, "err", err)
		}
	}
	return nil
}

// BroadcastWithdrawal sends an approved withdrawal to the network and records
// the transaction hash. Hot/cold separation (plan 6.1): if the amount meets
// the cold threshold for its asset and a cold signer is configured, the tx is
// NOT broadcast from the hot wallet; instead its unsigned description is
// queued to the cold signer and the status becomes cold_signing. Small
// withdrawals keep the original hot-wallet path.
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
	if ws.coldRequired(tx) {
		return ws.queueColdWithdrawal(tx)
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

// coldRequired reports whether tx must go through the cold signing flow.
func (ws *WithdrawalService) coldRequired(tx *Transaction) bool {
	ws.mu.RLock()
	signer, policy := ws.coldSigner, ws.coldPolicy
	ws.mu.RUnlock()
	return signer != nil && policy.RequiresCold(tx.Asset, tx.Amount)
}

// queueColdWithdrawal generates the unsigned tx description and hands it to
// the cold signer; the withdrawal then waits in cold_signing.
func (ws *WithdrawalService) queueColdWithdrawal(tx *Transaction) error {
	ws.mu.RLock()
	signer := ws.coldSigner
	feeStrategy := ws.coldFeeStrategies[strings.ToUpper(tx.Asset)]
	ws.mu.RUnlock()
	if signer == nil {
		// Policy says cold but no signer wired: refuse rather than silently
		// broadcast a large withdrawal from the hot wallet.
		return ErrColdSignerUnavailable
	}
	desc := ColdTxDesc{
		WithdrawID:  tx.ID,
		Asset:       tx.Asset,
		ToAddress:   tx.ToAddress,
		Amount:      tx.Amount.Text('f', -1),
		FeeStrategy: feeStrategy,
	}
	refID, err := signer.Queue(desc)
	if err != nil {
		return fmt.Errorf("queue cold signing: %w", err)
	}
	tx.ColdRef = refID
	tx.Status = WithdrawalColdSigning
	return ws.store.SaveTx(tx)
}

// ProcessColdWithdrawals advances withdrawals in the cold flow: cold_signing
// txs are polled for the signed payload, and cold_signed txs are broadcast.
func (ws *WithdrawalService) ProcessColdWithdrawals(limit int) error {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	signing, err := ws.store.ListTxByStatus(WithdrawalColdSigning, limit, 0)
	if err != nil {
		return err
	}
	signed, err := ws.store.ListTxByStatus(WithdrawalColdSigned, limit, 0)
	if err != nil {
		return err
	}
	for _, tx := range append(signing, signed...) {
		// One poll may need to carry a tx two steps (cold_signing ->
		// cold_signed -> broadcast); re-advance while it stays in the cold
		// flow and keeps progressing.
		for step := 0; step < 2; step++ {
			cur, err := ws.store.GetTx(tx.ID)
			if err != nil || (cur.Status != WithdrawalColdSigning && cur.Status != WithdrawalColdSigned) {
				break
			}
			if err := ws.AdvanceColdWithdrawal(tx.ID); err != nil {
				if !errors.Is(err, ErrColdTxNotSignedYet) {
					slog.Warn("withdrawal cold advance failed", "tx_id", tx.ID, "err", err)
				}
				break
			}
		}
	}
	return nil
}

// AdvanceColdWithdrawal moves one withdrawal forward in the cold flow:
//
//	cold_signing -> cold_signed (signed payload picked up from the signer)
//	cold_signed  -> broadcast   (signed raw tx broadcast on-chain)
//
// It returns ErrColdTxNotSignedYet while the signer has not finished.
func (ws *WithdrawalService) AdvanceColdWithdrawal(txID string) error {
	tx, err := ws.store.GetTx(txID)
	if err != nil {
		return err
	}
	if tx.Type != Withdrawal {
		return errors.New("not a withdrawal")
	}
	switch tx.Status {
	case WithdrawalColdSigning:
		ws.mu.RLock()
		signer := ws.coldSigner
		ws.mu.RUnlock()
		if signer == nil {
			return ErrColdSignerUnavailable
		}
		st, err := signer.Status(tx.ColdRef)
		if err != nil {
			return err
		}
		switch st.Status {
		case ColdQueued:
			return ErrColdTxNotSignedYet
		case ColdSignFailed:
			tx.Status = WithdrawalFailed
			if err := ws.store.SaveTx(tx); err != nil {
				return err
			}
			// Release reserved funds: the cold signer will never broadcast.
			if err := ws.store.Settle([]SettleOp{{UserID: tx.UserID, Asset: tx.Asset, Unlock: tx.Amount}}, nil); err != nil {
				return err
			}
			ws.releaseDailyUsageBestEffort(tx.UserID, tx.Asset, tx.Amount)
			return nil
		case ColdSignedOk:
			tx.Status = WithdrawalColdSigned
			return ws.store.SaveTx(tx)
		}
		return errors.New("unknown cold sign status")
	case WithdrawalColdSigned:
		return ws.broadcastColdSigned(tx)
	default:
		return fmt.Errorf("withdrawal %s not in cold flow (status %s)", txID, tx.Status)
	}
}

// broadcastColdSigned broadcasts the cold-signed payload. Only clients that
// implement SignedTxBroadcaster may broadcast; there is deliberately no
// fallback to the hot wallet's SendTransaction.
func (ws *WithdrawalService) broadcastColdSigned(tx *Transaction) error {
	ws.mu.RLock()
	signer := ws.coldSigner
	ws.mu.RUnlock()
	if signer == nil {
		return ErrColdSignerUnavailable
	}
	st, err := signer.Status(tx.ColdRef)
	if err != nil {
		return err
	}
	if st.Status != ColdSignedOk || st.Signed == nil || st.Signed.SignedRawTx == "" {
		return ErrColdTxNotSignedYet
	}
	c := ws.clientFor(tx.Asset)
	if c == nil {
		return fmt.Errorf("%w: %s", ErrUnsupportedAsset, tx.Asset)
	}
	bc, ok := c.(SignedTxBroadcaster)
	if !ok {
		return fmt.Errorf("client for %s cannot broadcast signed tx (hot-wallet fallback disabled)", tx.Asset)
	}
	txHash, err := bc.BroadcastSignedTx(st.Signed.SignedRawTx)
	if err != nil {
		return fmt.Errorf("broadcast cold-signed tx: %w", err)
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
			slog.Warn("withdrawal confirm failed", "tx_id", tx.ID, "err", err)
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

// resolveDailyLimitUSDT returns the effective daily limit (USDT-equivalent)
// for a user/asset: a personal override registered via SetLimit wins, then
// the user's KYC tier from platform_limits, then the level-0 row, then the
// built-in fallback.
func (ws *WithdrawalService) resolveDailyLimitUSDT(userID, asset string) *big.Float {
	ws.mu.RLock()
	if l, ok := ws.limits[userID+":"+asset]; ok && l != nil && l.DailyLimit != nil && l.DailyLimit.Sign() > 0 {
		lim := new(big.Float).Copy(l.DailyLimit)
		ws.mu.RUnlock()
		return lim
	}
	limits := ws.platformLimits
	ws.mu.RUnlock()
	level := ws.kycLevelCached(userID)
	if l, ok := limits[level]; ok && l != nil && l.Sign() > 0 {
		return new(big.Float).Copy(l)
	}
	if l, ok := limits[0]; ok && l != nil && l.Sign() > 0 {
		return new(big.Float).Copy(l)
	}
	return big.NewFloat(defaultDailyLimitUSDT)
}

// kycLevelCached resolves the user's KYC level with a 60s TTL cache. Lookup
// errors fail safe to level 0 (the most conservative tier).
func (ws *WithdrawalService) kycLevelCached(userID string) int {
	ws.mu.RLock()
	lookup := ws.kycLookup
	if e, ok := ws.kycCache[userID]; ok && time.Now().Before(e.expires) {
		ws.mu.RUnlock()
		return e.level
	}
	ws.mu.RUnlock()
	level := 0
	if lookup != nil {
		if l, err := lookup.KycLevel(userID); err == nil {
			level = l
		} else {
			slog.Debug("kyc level lookup failed; assuming level 0", "user_id", userID, "err", err)
		}
	}
	ws.mu.Lock()
	ws.kycCache[userID] = kycCacheEntry{level: level, expires: time.Now().Add(60 * time.Second)}
	ws.mu.Unlock()
	return level
}

// usdtEquivalent folds an asset amount into USDT. Stablecoins count 1:1; any
// other asset is priced via the injected PriceGetter and errors propagate so
// the caller can fail closed.
func (ws *WithdrawalService) usdtEquivalent(asset string, amount *big.Float) (*big.Float, error) {
	up := strings.ToUpper(asset)
	if stableAssets[up] {
		return new(big.Float).Copy(amount), nil
	}
	ws.mu.RLock()
	pg := ws.priceGetter
	ws.mu.RUnlock()
	if pg == nil {
		return nil, errors.New("no price source configured")
	}
	price, _, err := pg.BestPrice(up + "/USDT")
	if err != nil || price == nil || price.Sign() <= 0 {
		return nil, fmt.Errorf("no %s price available", up)
	}
	return new(big.Float).Mul(amount, price), nil
}

// releaseDailyUsageBestEffort credits the daily meter back after a rejected
// or permanently failed withdrawal. The release amount is re-derived from
// the current market price (an acceptable approximation; the meter resets
// every UTC day). When no price is available the release is skipped and the
// meter stays slightly overstated — the safe direction.
func (ws *WithdrawalService) releaseDailyUsageBestEffort(userID, asset string, amount *big.Float) {
	ds, ok := ws.store.(dailyLimitStore)
	if !ok {
		return
	}
	ws.mu.RLock()
	pg := ws.priceGetter
	ws.mu.RUnlock()
	if pg == nil {
		return
	}
	eq, err := ws.usdtEquivalent(asset, amount)
	if err != nil {
		slog.Warn("withdrawal daily-usage release skipped: price unavailable", "user_id", userID, "asset", asset, "err", err)
		return
	}
	if err := ds.ReleaseDailyUsage(userID, asset, eq); err != nil {
		slog.Warn("withdrawal daily-usage release failed", "user_id", userID, "asset", asset, "err", err)
	}
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
		return ErrDailyLimitExceeded
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
