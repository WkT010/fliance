package api

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"math/big"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/WkT010/nexa-exchange/internal/audit"
	"github.com/WkT010/nexa-exchange/internal/wallet"
	"github.com/gin-gonic/gin"
)

// supportedPairsForWallet is the list of trading pairs the platform offers,
// used by ListSupportedAssets to include every base token in the supported
// asset list so users can deposit/withdraw them even when no on-chain
// blockchain client is configured.
var supportedPairsForWallet = []string{
	"BTC/USDT", "ETH/USDT", "SOL/USDT", "BNB/USDT", "ADA/USDT",
	"BTC/USDC", "ETH/USDC",
}

// WalletService is the subset of wallet.Service used by the HTTP layer. It is
// satisfied by *wallet.Service in production and by a fake in tests.
type WalletService interface {
	GetBalance(userID, asset string) (*wallet.Wallet, error)
	GetBalances(userID string) ([]*wallet.Wallet, error)
	Deposit(userID, asset string, amount *big.Float, txHash string) error
	Withdraw(userID, asset, address string, amount *big.Float) error
	ListTransactions(userID string, limit, offset int) ([]*wallet.Transaction, error)
	// ReserveForOrder locks `amount` of `asset` without registering a
	// per-order reservation. It must only be used for off-orderbook locks
	// (futures margin, AMM trades); spot orders must use ReserveOrder so
	// cancel/fill paths can release exactly what was locked.
	ReserveForOrder(userID, asset string, amount *big.Float) (*wallet.Wallet, error)
	// ReserveOrder locks the funds an order needs AND registers the
	// reservation under orderID so ReleaseOrder/SettleFill can unwind it.
	// Market buys are not pre-locked (price unknown) and settle on fill.
	ReserveOrder(orderID, userID, pair string, side int, orderType int, price, qty *big.Float) error
	// Transfer atomically moves amount of asset between two of the user's
	// internal accounts (spot/futures/funding).
	Transfer(userID, from, to, asset string, amount *big.Float) error
}

// DepositUserLookup resolves users by ID so the admin deposit endpoint can
// validate an explicit target user before crediting their wallet.
type DepositUserLookup interface {
	GetByID(id string) (*User, error)
}

// DepositAddressStore persists the deposit address assigned to a user on
// their spot wallet row (wallets.address). The deposit-claim auto-verifier
// later compares the on-chain recipient against exactly this value, so the
// address handed out by the deposit-address endpoint MUST be written to the
// database — an unpersisted address can never verify and would route every
// claim to manual review. AssignDepositAddress is idempotent: it returns the
// address actually persisted (an address assigned by an earlier call wins).
type DepositAddressStore interface {
	GetWallet(userID, asset string) (*wallet.Wallet, error)
	AssignDepositAddress(userID, asset, address string) (string, error)
}

// WalletHandler exposes the wallet HTTP API: balances, deposit address, withdraw
// and transaction history.
type WalletHandler struct {
	svc     WalletService
	clients map[string]wallet.BlockchainClient
	lookup  DepositUserLookup
	// addrStore persists generated deposit addresses on the user's spot
	// wallet row. Optional: without it the endpoint keeps its legacy
	// behaviour (generate + return without persistence).
	addrStore DepositAddressStore

	// Audit trail for the admin deposit endpoint. Optional: nil disables
	// auditing (the logger's methods are nil-safe).
	audit *audit.Logger
}

func NewWalletHandler(svc WalletService, clients map[string]wallet.BlockchainClient) *WalletHandler {
	return &WalletHandler{svc: svc, clients: clients}
}

// SetUserLookup wires the user store used to validate an explicit target user
// on the admin deposit endpoint. Optional: without it, deposits can only
// target the caller.
func (h *WalletHandler) SetUserLookup(l DepositUserLookup) { h.lookup = l }

// SetDepositAddressStore wires the persistence for generated deposit
// addresses. Optional: without it the endpoint answers without persisting
// (legacy behaviour) and every deposit claim falls back to manual review.
func (h *WalletHandler) SetDepositAddressStore(s DepositAddressStore) { h.addrStore = s }

// SetAuditLogger wires the asynchronous admin audit logger used by the admin
// deposit endpoint. Optional.
func (h *WalletHandler) SetAuditLogger(l *audit.Logger) { h.audit = l }

// GetBalances returns all wallet balances for the authenticated user.
// GET /api/v2/wallet/balances
func (h *WalletHandler) GetBalances(c *gin.Context) {
	uid, _ := c.Get("user_id")
	userID, _ := uid.(string)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}
	ws, err := h.svc.GetBalances(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load balances"})
		return
	}
	if ws == nil {
		ws = []*wallet.Wallet{}
	}
	result := make([]gin.H, len(ws))
	for i, w := range ws {
		result[i] = walletToJSON(w)
	}
	c.JSON(http.StatusOK, gin.H{"balances": result})
}

// GetBalance returns the wallet for a single asset.
// GET /api/v2/wallet/balances/:asset
func (h *WalletHandler) GetBalance(c *gin.Context) {
	uid, _ := c.Get("user_id")
	userID, _ := uid.(string)
	asset := c.Param("asset")
	if asset == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "asset required"})
		return
	}
	w, err := h.svc.GetBalance(userID, asset)
	if err != nil {
		if errors.Is(err, wallet.ErrWalletNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "wallet not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load wallet"})
		return
	}
	c.JSON(http.StatusOK, walletToJSON(w))
}

// evmDepositAssets are the assets whose deposits live on EVM chains and
// therefore use standard 0x-prefixed 20-byte deposit addresses.
var evmDepositAssets = map[string]bool{"ETH": true, "USDT": true, "USDC": true, "POLYGON": true}

// randomEVMDepositAddress generates a random 0x-prefixed EVM address used as
// a user's deposit address when the asset's on-chain client cannot derive
// addresses yet (HD-wallet key derivation is a separate milestone; USDT/USDC
// have no client at all). The address is unique per call and uncontrolled:
// claim auto-verification binds an on-chain transfer to the claimant only via
// an exact recipient match against this value, and no two users are ever
// assigned the same address (collision probability ~2^-160), so a mismatch
// keeps the claim pending for manual review — the flow stays fail-closed.
func randomEVMDepositAddress() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "0x" + hex.EncodeToString(b), nil
}

// GetDepositAddress generates (or returns) a deposit address for an asset.
// POST /api/v2/wallet/deposit/address  { "asset": "BTC" }
//
// In production the address is generated by the on-chain wallet-service and
// watched for incoming deposits; this endpoint returns a fresh address from the
// configured blockchain client. For assets without a client (e.g. USDT issued
// internally), the asset symbol itself is returned as a placeholder.
//
// Idempotent + persisted: the generated address is written to the user's spot
// wallet row (wallets.address), and a repeat call returns the already
// assigned address instead of generating a new one. Persisting the address
// is what lets the on-chain deposit verifier bind a claim's transaction to
// its claimant; the response shape is unchanged for the frontend.
func (h *WalletHandler) GetDepositAddress(c *gin.Context) {
	uid, _ := c.Get("user_id")
	userID, _ := uid.(string)
	var r struct {
		Asset string `json:"asset" binding:"required"`
	}
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "asset required"})
		return
	}
	asset := strings.TrimSpace(strings.ToUpper(r.Asset))
	// Idempotency: an address already assigned to this user is returned
	// verbatim — never regenerated (generation is skipped entirely).
	if h.addrStore != nil {
		if w, err := h.addrStore.GetWallet(userID, asset); err == nil && w != nil && w.Address != "" {
			c.JSON(http.StatusOK, gin.H{"asset": asset, "address": w.Address, "user_id": userID})
			return
		}
	}
	var addr string
	if client, ok := h.clients[asset]; ok {
		generated, err := client.GenerateAddress()
		if err != nil {
			// HD-wallet derivation is unimplemented for the RPC clients;
			// fall back to a placeholder for EVM assets instead of 500ing.
			if !evmDepositAssets[asset] {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "address generation failed"})
				return
			}
		}
		addr = generated
	}
	if addr == "" && evmDepositAssets[asset] {
		// No client (USDT/USDC) or client cannot derive addresses (ETH):
		// issue a random EVM-format placeholder so the claim verifier has a
		// recipient to bind transfers against.
		generated, err := randomEVMDepositAddress()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "address generation failed"})
			return
		}
		addr = generated
	}
	if addr == "" {
		c.JSON(http.StatusOK, gin.H{"asset": asset, "address": "", "user_id": userID, "note": "manual deposit only"})
		return
	}
	if h.addrStore != nil {
		// Persist atomically; the store resolves concurrent/duplicate
		// assignments in favour of the first address ever written, so
		// return what is actually stored, not the local candidate.
		persisted, err := h.addrStore.AssignDepositAddress(userID, asset, addr)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "address persistence failed"})
			return
		}
		addr = persisted
	}
	c.JSON(http.StatusOK, gin.H{"asset": asset, "address": addr, "user_id": userID})
}

// Deposit credits a wallet. Admin-only endpoint (deposits are normally
// detected by the wallet-service); kept for manual credits and integration.
// POST /api/v2/admin/wallet/deposit
//
// { "asset":"BTC","amount":"1.0","tx_hash":"...","user_id":"usr_..." }
//
// user_id is optional: when omitted the caller's own wallet is credited;
// when present the target user must exist.
func (h *WalletHandler) Deposit(c *gin.Context) {
	uid, _ := c.Get("user_id")
	userID, _ := uid.(string)
	var r struct {
		Asset    string `json:"asset" binding:"required"`
		Amount   string `json:"amount" binding:"required"`
		TxHash   string `json:"tx_hash"`
		TargetID string `json:"user_id"`
	}
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "asset and amount required"})
		return
	}
	amt := new(big.Float)
	if _, _, err := amt.Parse(r.Amount, 10); err != nil || amt.Sign() <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid amount"})
		return
	}
	target := userID
	if r.TargetID != "" {
		if h.lookup == nil {
			h.audit.Log(c, "admin.deposit", "wallet", r.TargetID, depositDetails(r.Asset, r.Amount, r.TxHash, r.TargetID), errors.New("user lookup unavailable"))
			c.JSON(http.StatusBadRequest, gin.H{"error": "user lookup unavailable, cannot target another user"})
			return
		}
		u, err := h.lookup.GetByID(r.TargetID)
		if err != nil || u == nil {
			h.audit.Log(c, "admin.deposit", "wallet", r.TargetID, depositDetails(r.Asset, r.Amount, r.TxHash, r.TargetID), errors.New("target user not found"))
			c.JSON(http.StatusBadRequest, gin.H{"error": "target user not found"})
			return
		}
		target = u.ID
	}
	if err := h.svc.Deposit(target, r.Asset, amt, r.TxHash); err != nil {
		h.audit.Log(c, "admin.deposit", "wallet", target, depositDetails(r.Asset, r.Amount, r.TxHash, r.TargetID), err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.audit.Log(c, "admin.deposit", "wallet", target, depositDetails(r.Asset, r.Amount, r.TxHash, r.TargetID), nil)
	c.JSON(http.StatusOK, gin.H{"status": "ok", "user_id": target, "asset": r.Asset, "amount": r.Amount})
}

// depositDetails builds the audit details payload for a deposit attempt.
func depositDetails(asset, amount, txHash, targetID string) gin.H {
	return gin.H{"asset": asset, "amount": amount, "tx_hash": txHash, "target_user_id": targetID}
}

// DepositGone answers the legacy user-facing deposit path with 410 Gone and a
// migration note. Crediting balances is admin-only now
// (POST /api/v2/admin/wallet/deposit); regular deposits are detected and
// credited automatically by the wallet-service.
func (h *WalletHandler) DepositGone(c *gin.Context) {
	c.JSON(http.StatusGone, gin.H{
		"error": "endpoint removed",
		"message": "POST /api/v2/wallet/deposit is no longer available. " +
			"On-chain deposits are credited automatically by the wallet-service; " +
			"manual balance credits are admin-only via POST /api/v2/admin/wallet/deposit.",
	})
}

// Withdraw locks the funds and queues a withdrawal for the wallet-service to
// broadcast on-chain.
// POST /api/v2/wallet/withdraw  { "asset":"BTC","address":"...","amount":"0.5" }
func (h *WalletHandler) Withdraw(c *gin.Context) {
	uid, _ := c.Get("user_id")
	userID, _ := uid.(string)
	var r struct {
		Asset   string `json:"asset" binding:"required"`
		Address string `json:"address" binding:"required"`
		Amount  string `json:"amount" binding:"required"`
	}
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "asset, address and amount required"})
		return
	}
	amt := new(big.Float)
	if _, _, err := amt.Parse(r.Amount, 10); err != nil || amt.Sign() <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid amount"})
		return
	}
	if err := h.svc.Withdraw(userID, r.Asset, r.Address, amt); err != nil {
		switch {
		case errors.Is(err, wallet.ErrInsufficientBalance):
			c.JSON(http.StatusPaymentRequired, gin.H{"error": "insufficient balance"})
		case errors.Is(err, wallet.ErrInvalidAddress):
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid address"})
		case errors.Is(err, wallet.ErrUnsupportedAsset):
			c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported asset"})
		case errors.Is(err, wallet.ErrDailyLimitExceeded):
			// Business-rule rejection, not a server fault: surface as 4xx so
			// clients can show the limit message instead of a generic 500.
			c.JSON(http.StatusBadRequest, gin.H{"error": "daily withdrawal limit exceeded"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "pending", "asset": r.Asset, "amount": r.Amount, "address": r.Address})
}

// Transfer moves funds between the user's internal accounts
// (spot / futures / funding). Both legs settle atomically in one database
// transaction and never count against the withdrawal daily limit.
// POST /api/v2/wallet/transfer  {"from":"spot","to":"futures","asset":"USDT","amount":"100"}
func (h *WalletHandler) Transfer(c *gin.Context) {
	uid, _ := c.Get("user_id")
	userID, _ := uid.(string)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}
	var r struct {
		From   string `json:"from" binding:"required"`
		To     string `json:"to" binding:"required"`
		Asset  string `json:"asset" binding:"required"`
		Amount string `json:"amount" binding:"required"`
	}
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "from, to, asset and amount required"})
		return
	}
	details := gin.H{"from": r.From, "to": r.To, "asset": r.Asset, "amount": r.Amount}
	if !wallet.ValidAccountType(r.From) || !wallet.ValidAccountType(r.To) {
		h.audit.Log(c, "wallet.transfer", "wallet", userID, details, wallet.ErrInvalidAccount)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid account"})
		return
	}
	if r.From == r.To {
		h.audit.Log(c, "wallet.transfer", "wallet", userID, details, wallet.ErrSameAccountTransfer)
		c.JSON(http.StatusBadRequest, gin.H{"error": wallet.ErrSameAccountTransfer.Error()})
		return
	}
	amt := new(big.Float)
	if _, _, err := amt.Parse(r.Amount, 10); err != nil || amt.Sign() <= 0 {
		h.audit.Log(c, "wallet.transfer", "wallet", userID, details, wallet.ErrNegativeAmount)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid amount"})
		return
	}
	r.Asset = strings.TrimSpace(strings.ToUpper(r.Asset))
	err := h.svc.Transfer(userID, r.From, r.To, r.Asset, amt)
	h.audit.Log(c, "wallet.transfer", "wallet", userID, details, err)
	if err != nil {
		switch {
		case errors.Is(err, wallet.ErrInsufficientBalance):
			c.JSON(http.StatusBadRequest, gin.H{"error": "insufficient balance"})
		case errors.Is(err, wallet.ErrUnsupportedAsset):
			c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported asset"})
		case errors.Is(err, wallet.ErrNegativeAmount), errors.Is(err, wallet.ErrInvalidAccount), errors.Is(err, wallet.ErrSameAccountTransfer):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "transfer failed"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ListTransactions returns the user's transaction history.
// GET /api/v2/wallet/transactions?limit=50&offset=0
func (h *WalletHandler) ListTransactions(c *gin.Context) {
	uid, _ := c.Get("user_id")
	userID, _ := uid.(string)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if offset < 0 {
		offset = 0
	}
	txs, err := h.svc.ListTransactions(userID, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list transactions"})
		return
	}
	if txs == nil {
		txs = []*wallet.Transaction{}
	}
	result := make([]gin.H, len(txs))
	for i, t := range txs {
		result[i] = txToJSON(t)
	}
	c.JSON(http.StatusOK, gin.H{"transactions": result, "limit": limit, "offset": offset})
}

// ListSupportedAssets returns every asset for which the platform supports
// deposit and withdrawal. This includes both on-chain assets (those with a
// registered blockchain client) and internally-issued assets (USDT, USDC and
// every base token from the supported trading pairs), so users can deposit or
// withdraw them via the simulated/manual flow even when no chain client is
// configured. The frontend uses this list to populate the asset selector on
// the wallet page, so excluding internally-issued assets would prevent users
// from ever withdrawing their balance.
func (h *WalletHandler) ListSupportedAssets(c *gin.Context) {
	assets := make(map[string]struct{})
	for a := range h.clients {
		assets[a] = struct{}{}
	}
	// Internally-issued: USDT, USDC and every base token traded on the platform.
	assets["USDT"] = struct{}{}
	assets["USDC"] = struct{}{}
	for _, pair := range supportedPairsForWallet {
		parts := strings.SplitN(pair, "/", 2)
		if len(parts) == 2 {
			assets[parts[0]] = struct{}{}
		}
	}
	out := make([]string, 0, len(assets))
	for a := range assets {
		out = append(out, a)
	}
	sort.Strings(out)
	c.JSON(http.StatusOK, gin.H{"assets": out})
}

func walletToJSON(w *wallet.Wallet) gin.H {
	return gin.H{
		"id":           w.ID,
		"user_id":      w.UserID,
		"asset":        w.Asset,
		"address":      w.Address,
		"balance":      safeFloatStr(w.Balance),
		"locked":       safeFloatStr(w.Locked),
		"available":    safeFloatStr(new(big.Float).Sub(w.Balance, w.Locked)),
		"account_type": wallet.NormalizeAccountType(w.AccountType),
		"created_at":   w.CreatedAt,
		"updated_at":   w.UpdatedAt,
	}
}

func txToJSON(t *wallet.Transaction) gin.H {
	return gin.H{
		"id":            t.ID,
		"user_id":       t.UserID,
		"wallet_id":     t.WalletID,
		"type":          t.Type.String(),
		"asset":         t.Asset,
		"amount":        safeFloatStr(t.Amount),
		"fee":           safeFloatStr(t.Fee),
		"status":        t.Status.String(),
		"tx_hash":       t.TxHash,
		"confirmations": t.Confirmations,
		"created_at":    t.CreatedAt,
	}
}
