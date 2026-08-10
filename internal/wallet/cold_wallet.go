package wallet

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Cold wallet policy (plan 6.1)
// ---------------------------------------------------------------------------
//
// Hot/cold separation: withdrawals whose amount meets or exceeds a per-asset
// threshold are NOT broadcast from the hot wallet. After approval they are
// routed to a cold signer (offline HSM in production) which produces a signed
// payload; only the signed payload is broadcast. Small withdrawals keep the
// original hot-wallet path unchanged.

var (
	// ErrColdSignerUnavailable is returned when no cold signer is configured
	// but a withdrawal requires the cold flow.
	ErrColdSignerUnavailable = errors.New("wallet: cold signer not configured")
	// ErrColdTxNotSignedYet indicates the cold signer has not produced the
	// signed payload for a queued transaction yet.
	ErrColdTxNotSignedYet = errors.New("wallet: cold tx not signed yet")
)

// DefaultColdWalletThreshold is the fallback threshold (in the asset's own
// units / configured equivalent value) used when no per-asset override and no
// environment variable is present. Overridable via COLD_WALLET_THRESHOLD.
const DefaultColdWalletThreshold = 10000

// ColdWalletPolicy decides whether a withdrawal must go through the cold
// signing flow. Thresholds are per-asset; a nil/absent entry falls back to
// Default. Safe for concurrent use.
type ColdWalletPolicy struct {
	mu sync.RWMutex
	// Default applies to assets without an explicit threshold.
	Default *big.Float
	// Thresholds maps asset (upper-cased) -> threshold. Amount >= threshold
	// routes the withdrawal to the cold flow.
	Thresholds map[string]*big.Float
}

// NewColdWalletPolicy builds a policy with the package default threshold.
func NewColdWalletPolicy() *ColdWalletPolicy {
	return &ColdWalletPolicy{
		Default:    big.NewFloat(DefaultColdWalletThreshold),
		Thresholds: make(map[string]*big.Float),
	}
}

// ColdWalletPolicyFromEnv builds a policy from the environment:
//   - COLD_WALLET_THRESHOLD: default threshold (float), overrides the
//     package default of 10000.
//   - COLD_WALLET_THRESHOLD_<ASSET>: per-asset override, e.g.
//     COLD_WALLET_THRESHOLD_BTC=1, COLD_WALLET_THRESHOLD_ETH=50.
func ColdWalletPolicyFromEnv() *ColdWalletPolicy {
	p := NewColdWalletPolicy()
	if v := os.Getenv("COLD_WALLET_THRESHOLD"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 {
			p.Default = big.NewFloat(f)
		}
	}
	for _, kv := range os.Environ() {
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 {
			continue
		}
		k, v := kv[:eq], kv[eq+1:]
		if !strings.HasPrefix(k, "COLD_WALLET_THRESHOLD_") {
			continue
		}
		asset := strings.TrimPrefix(k, "COLD_WALLET_THRESHOLD_")
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 {
			p.SetThreshold(asset, big.NewFloat(f))
		}
	}
	return p
}

// SetThreshold registers a per-asset threshold.
func (p *ColdWalletPolicy) SetThreshold(asset string, threshold *big.Float) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Thresholds[strings.ToUpper(asset)] = newBigFloatCopy(threshold)
}

// ThresholdFor returns the effective threshold for an asset.
func (p *ColdWalletPolicy) ThresholdFor(asset string) *big.Float {
	if p == nil {
		return big.NewFloat(DefaultColdWalletThreshold)
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if t, ok := p.Thresholds[strings.ToUpper(asset)]; ok && t != nil {
		return new(big.Float).Copy(t)
	}
	if p.Default != nil {
		return new(big.Float).Copy(p.Default)
	}
	return big.NewFloat(DefaultColdWalletThreshold)
}

// RequiresCold reports whether a withdrawal of amount of asset must be routed
// through the cold signing flow (amount >= threshold).
func (p *ColdWalletPolicy) RequiresCold(asset string, amount *big.Float) bool {
	if p == nil || amount == nil {
		return false
	}
	return amount.Cmp(p.ThresholdFor(asset)) >= 0
}

// ---------------------------------------------------------------------------
// Cold signer abstraction
// ---------------------------------------------------------------------------

// ColdTxDesc is the JSON-serialisable description of an unsigned withdrawal
// handed to the cold signer. It deliberately contains no private-key material
// and no change-address secrets: the offline signer owns key management.
type ColdTxDesc struct {
	RefID      string `json:"ref_id"`
	WithdrawID string `json:"withdrawal_id"`
	Asset      string `json:"asset"`
	ToAddress  string `json:"to_address"`
	// Amount is the decimal amount in the asset's native units.
	Amount string `json:"amount"`
	// FeeStrategy describes how the signer should price fees, e.g.
	// "eip1559:priority=auto" or "btc:satvbyte=auto". The signer applies its
	// own policy limits.
	FeeStrategy string `json:"fee_strategy"`
	QueuedAt    int64  `json:"queued_at"`
}

// ColdSignStatus is the lifecycle state of a queued cold transaction.
type ColdSignStatus int8

const (
	// ColdQueued: description queued, awaiting offline signature.
	ColdQueued ColdSignStatus = iota
	// ColdSignedOk: signed payload available for pickup.
	ColdSignedOk
	// ColdSignFailed: the signer rejected or failed the transaction.
	ColdSignFailed
)

func (s ColdSignStatus) String() string {
	switch s {
	case ColdQueued:
		return "queued"
	case ColdSignedOk:
		return "signed"
	case ColdSignFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// SignedColdTx is the payload produced by the cold signer.
type SignedColdTx struct {
	RefID       string `json:"ref_id"`
	SignedRawTx string `json:"signed_raw_tx"` // raw signed tx (hex), broadcast-ready
	SignerID    string `json:"signer_id"`     // HSM / offline signer identity
	SignedAt    int64  `json:"signed_at"`
	Error       string `json:"error,omitempty"` // set on failure
}

// ColdSignState is the result of polling a queued transaction.
type ColdSignState struct {
	Status ColdSignStatus
	Signed *SignedColdTx
}

// ColdSigner abstracts the offline signing backend. Queue hands over the
// unsigned description; Status polls for the signed result. In production the
// implementation wraps a real HSM / air-gapped signer; FileBasedColdSigner
// simulates that flow with directories.
type ColdSigner interface {
	Queue(desc ColdTxDesc) (refID string, err error)
	Status(refID string) (ColdSignState, error)
}

// SignedTxBroadcaster is implemented by blockchain clients able to broadcast
// a pre-signed raw transaction (cold flow). Hot-only clients do not implement
// it; the cold flow then refuses to broadcast rather than falling back to the
// hot wallet (which would double-spend the signer's intent).
type SignedTxBroadcaster interface {
	BroadcastSignedTx(signedRawTx string) (txHash string, err error)
}

// ---------------------------------------------------------------------------
// FileBasedColdSigner: directory-based simulation of an offline HSM flow
// ---------------------------------------------------------------------------

// FileBasedColdSigner simulates an offline signing pipeline:
//
//	Queue()  -> writes <pendingDir>/<refID>.json   (unsigned description)
//	Status() -> reads  <signedDir>/<refID>.json    (signed payload, if ready)
//
// An external (offline) process picks up pending files, signs them, and drops
// the SignedColdTx JSON into the signed directory. Production deployments
// replace this implementation with a real HSM-backed signer; the ColdSigner
// interface is the seam.
type FileBasedColdSigner struct {
	pendingDir string
	signedDir  string
	signerID   string
}

// NewFileBasedColdSigner creates the directories if missing.
func NewFileBasedColdSigner(pendingDir, signedDir string) (*FileBasedColdSigner, error) {
	for _, d := range []string{pendingDir, signedDir} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			return nil, fmt.Errorf("cold signer dir %s: %w", d, err)
		}
	}
	return &FileBasedColdSigner{pendingDir: pendingDir, signedDir: signedDir, signerID: "file-based-sim"}, nil
}

// refFilePath maps a reference ID to a file inside dir, rejecting refIDs that
// could escape the base directory (path traversal defence: refIDs originate
// from persisted tx records, so treat them as untrusted input).
func refFilePath(dir, refID string) (string, error) {
	if refID == "" || strings.ContainsAny(refID, `/\`) || strings.Contains(refID, "..") || refID != filepath.Base(refID) {
		return "", fmt.Errorf("invalid cold ref id %q", refID)
	}
	path := filepath.Join(dir, refID+".json")
	if rel, err := filepath.Rel(dir, path); err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("invalid cold ref id %q", refID)
	}
	return path, nil
}

// Queue persists the unsigned description and returns the reference ID.
func (f *FileBasedColdSigner) Queue(desc ColdTxDesc) (string, error) {
	if desc.RefID == "" {
		desc.RefID = "cold_" + uuid.NewString()
	}
	path, err := refFilePath(f.pendingDir, desc.RefID)
	if err != nil {
		return "", err
	}
	if desc.QueuedAt == 0 {
		desc.QueuedAt = time.Now().UnixNano()
	}
	data, err := json.MarshalIndent(desc, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal cold tx desc: %w", err)
	}
	if err := os.WriteFile(path, data, 0o640); err != nil {
		return "", fmt.Errorf("queue cold tx: %w", err)
	}
	return desc.RefID, nil
}

// Status polls the signed directory for the payload. Absence means the
// offline signer has not finished yet (ColdQueued).
func (f *FileBasedColdSigner) Status(refID string) (ColdSignState, error) {
	if refID == "" {
		return ColdSignState{}, errors.New("empty ref id")
	}
	path, err := refFilePath(f.signedDir, refID)
	if err != nil {
		return ColdSignState{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ColdSignState{Status: ColdQueued}, nil
		}
		return ColdSignState{}, fmt.Errorf("poll cold tx %s: %w", refID, err)
	}
	var signed SignedColdTx
	if err := json.Unmarshal(data, &signed); err != nil {
		return ColdSignState{}, fmt.Errorf("corrupt signed file %s: %w", path, err)
	}
	if signed.Error != "" || signed.SignedRawTx == "" {
		return ColdSignState{Status: ColdSignFailed, Signed: &signed}, nil
	}
	return ColdSignState{Status: ColdSignedOk, Signed: &signed}, nil
}
