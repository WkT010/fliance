package wallet

import (
	"errors"
	"fmt"
	"math/big"
	"time"
)

var (
	ErrInsufficientBalance = errors.New("insufficient balance")
	ErrWalletNotFound      = errors.New("wallet not found")
	ErrInvalidAddress      = errors.New("invalid address")
	ErrNegativeAmount      = errors.New("positive amount required")
)

type WalletStore interface {
	GetWallet(userID, asset string) (*Wallet, error)
	GetWallets(userID string) ([]*Wallet, error)
	UpdateBalance(id string, delta *big.Float) error
	LockBalance(id string, amt *big.Float) error
	UnlockBalance(id string, amt *big.Float) error
	SaveTx(*Transaction) error
	GetTx(string) (*Transaction, error)
	ListTx(userID string, limit, offset int) ([]*Transaction, error)
}

type Service struct {
	store          WalletStore
	clients        map[string]BlockchainClient
	confThresholds map[string]int
}

func NewService(store WalletStore, clients map[string]BlockchainClient) *Service {
	return &Service{store: store, clients: clients, confThresholds: map[string]int{"BTC": 12, "ETH": 24, "POLYGON": 24}}
}
func (s *Service) clientFor(asset string) BlockchainClient { return s.clients[asset] }
func (s *Service) RegisterClient(asset string, c BlockchainClient) { s.clients[asset] = c }

func (s *Service) GetBalance(userID, asset string) (*Wallet, error) {
	w, err := s.store.GetWallet(userID, asset)
	if err != nil { return nil, fmt.Errorf("get wallet: %w", err) }
	return w, nil
}

func (s *Service) Deposit(userID, asset string, amount *big.Float, txHash string) error {
	if amount == nil || amount.Sign() <= 0 { return ErrNegativeAmount }
	w, err := s.store.GetWallet(userID, asset)
	if err != nil {
		w = &Wallet{UserID: userID, Asset: asset, Balance: big.NewFloat(0), Locked: big.NewFloat(0), CreatedAt: time.Now().UnixNano()}
	}
	s.store.UpdateBalance(w.ID, amount)
	return s.store.SaveTx(&Transaction{UserID: userID, WalletID: w.ID, Type: Deposit, Asset: asset, Amount: new(big.Float).Copy(amount), Fee: big.NewFloat(0), Status: Completed, TxHash: txHash, CreatedAt: time.Now().UnixNano()})
}

func (s *Service) Withdraw(userID, asset, address string, amount *big.Float) error {
	if amount == nil || amount.Sign() <= 0 { return ErrNegativeAmount }
	c := s.clientFor(asset)
	if c == nil { return fmt.Errorf("unsupported asset: %s", asset) }
	if !c.IsValidAddress(address) { return ErrInvalidAddress }
	w, err := s.store.GetWallet(userID, asset)
	if err != nil { return ErrWalletNotFound }
	avail := new(big.Float).Sub(w.Balance, w.Locked)
	if avail.Cmp(amount) < 0 { return fmt.Errorf("%w: have=%s want=%s", ErrInsufficientBalance, avail.Text('f', 8), amount.Text('f', 8)) }
	s.store.LockBalance(w.ID, amount)
	return s.store.SaveTx(&Transaction{UserID: userID, WalletID: w.ID, Type: Withdrawal, Asset: asset, Amount: new(big.Float).Copy(amount), Fee: big.NewFloat(0), Status: Pending, CreatedAt: time.Now().UnixNano()})
}

func (s *Service) SettleTrade(buyerID, sellerID, asset string, amount, price *big.Float) error {
	if amount.Sign() <= 0 { return ErrNegativeAmount }
	total := new(big.Float).Mul(amount, price)
	bw, _ := s.store.GetWallet(buyerID, asset)
	if bw != nil { s.store.UnlockBalance(bw.ID, total); s.store.UpdateBalance(bw.ID, new(big.Float).Neg(total)) }
	sw, _ := s.store.GetWallet(sellerID, "USDT")
	if sw != nil { s.store.UpdateBalance(sw.ID, amount) }
	return nil
}
