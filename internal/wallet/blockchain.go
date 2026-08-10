package wallet

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"math/big"
)

// BlockchainClient abstracts the on-chain backend for one asset.
type BlockchainClient interface {
	GenerateAddress() (string, error)
	GetBalance(address string) (*big.Float, error)
	SendTransaction(to string, amount *big.Float) (string, error)
	GetConfirmations(txHash string) (int, error)
	IsValidAddress(address string) bool
}

// MockBlockchainClient is a DEVELOPMENT-ONLY stub that fakes all on-chain
// operations. It must NEVER be registered in production deployments:
// withdrawals "broadcast" by this client do not move real funds. Production
// must use the real RPC clients (see btc_client.go / eth_client.go).
//
// Address validation still goes through the strict chain-specific validators
// (validate.go), so even with the mock registered, malformed withdrawal
// addresses are rejected.
type MockBlockchainClient struct{ Asset string }

// NewMockBlockchainClient constructs the development-only client and logs a
// loud warning so its accidental use in production is easy to spot.
func NewMockBlockchainClient(asset string) *MockBlockchainClient {
	slog.Warn("MockBlockchainClient registered — DEVELOPMENT MODE ONLY; no real on-chain operations will be performed. Replace with a real RPC client in production.", "asset", asset)
	return &MockBlockchainClient{Asset: asset}
}

// GenerateAddress returns a synthetic, chain-invalid placeholder address. It
// is deliberately NOT a valid on-chain address; use it only in tests/dev.
func (m *MockBlockchainClient) GenerateAddress() (string, error) {
	b := make([]byte, 20)
	rand.Read(b)
	return m.Asset + "_" + hex.EncodeToString(b), nil
}
func (m *MockBlockchainClient) GetBalance(_ string) (*big.Float, error) {
	return big.NewFloat(1000.0), nil
}
func (m *MockBlockchainClient) SendTransaction(_ string, _ *big.Float) (string, error) {
	b := make([]byte, 32)
	rand.Read(b)
	return "0x" + hex.EncodeToString(b), nil
}
func (m *MockBlockchainClient) GetConfirmations(_ string) (int, error) { return 12, nil }

// BroadcastSignedTx simulates broadcasting a cold-signed payload so the
// cold-wallet flow can be exercised in development. Implements
// SignedTxBroadcaster.
func (m *MockBlockchainClient) BroadcastSignedTx(signedRawTx string) (string, error) {
	if signedRawTx == "" {
		return "", fmt.Errorf("mock: empty signed raw tx")
	}
	sum := sha256.Sum256([]byte(signedRawTx))
	return "0x" + hex.EncodeToString(sum[:]), nil
}

// IsValidAddress delegates to the strict chain-specific validators so the mock
// cannot be used to bypass address format checks. Assets without a registered
// strict format fall back to the legacy non-empty check.
func (m *MockBlockchainClient) IsValidAddress(a string) bool {
	if a == "" {
		return false
	}
	return ValidateWithdrawalAddress(m.Asset, a) == nil
}
