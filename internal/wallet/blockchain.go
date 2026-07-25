package wallet

import (
	"crypto/rand"
	"encoding/hex"
	"math/big"
)

type BlockchainClient interface {
	GenerateAddress() (string, error)
	GetBalance(address string) (*big.Float, error)
	SendTransaction(to string, amount *big.Float) (string, error)
	GetConfirmations(txHash string) (int, error)
	IsValidAddress(address string) bool
}

type MockBlockchainClient struct{ Asset string }

func NewMockBlockchainClient(asset string) *MockBlockchainClient { return &MockBlockchainClient{Asset: asset} }
func (m *MockBlockchainClient) GenerateAddress() (string, error) { b := make([]byte, 20); rand.Read(b); return m.Asset + "_" + hex.EncodeToString(b), nil }
func (m *MockBlockchainClient) GetBalance(_ string) (*big.Float, error) { return big.NewFloat(1000.0), nil }
func (m *MockBlockchainClient) SendTransaction(_ string, _ *big.Float) (string, error) { b := make([]byte, 32); rand.Read(b); return "0x" + hex.EncodeToString(b), nil }
func (m *MockBlockchainClient) GetConfirmations(_ string) (int, error) { return 12, nil }
func (m *MockBlockchainClient) IsValidAddress(a string) bool { return a != "" }
