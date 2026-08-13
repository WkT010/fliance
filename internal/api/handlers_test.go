package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"github.com/WkT010/nexa-exchange/internal/auth"
	"github.com/WkT010/nexa-exchange/internal/cache"
	"github.com/WkT010/nexa-exchange/internal/matching"
	"github.com/WkT010/nexa-exchange/internal/wallet"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// ---------------------------------------------------------------------------
// mocks
// ---------------------------------------------------------------------------

// mockUserStore is an in-memory UserStore.
type mockUserStore struct {
	mu      sync.Mutex
	byID    map[string]*User
	byEmail map[string]*User
}

func newMockUserStore() *mockUserStore {
	return &mockUserStore{byID: map[string]*User{}, byEmail: map[string]*User{}}
}

func (m *mockUserStore) GetByEmail(email string) (*User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.byEmail[email]
	if !ok {
		return nil, errors.New("not found")
	}
	return u, nil
}

func (m *mockUserStore) GetByID(id string) (*User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.byID[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return u, nil
}

func (m *mockUserStore) Create(u *User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.byEmail[u.Email]; exists {
		return errors.New("duplicate email")
	}
	m.byID[u.ID] = u
	m.byEmail[u.Email] = u
	return nil
}

func (m *mockUserStore) Update(u *User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.byID[u.ID] = u
	m.byEmail[u.Email] = u
	return nil
}

// seedAdmin inserts a user with the admin role directly.
func (m *mockUserStore) seedAdmin(t *testing.T, email, password string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	u := &User{
		ID: "usr_admin_test", Email: email, PasswordHash: string(hash),
		Role: "admin", CreatedAt: time.Now().UnixNano(), UpdatedAt: time.Now().UnixNano(),
	}
	if err := m.Create(u); err != nil {
		t.Fatal(err)
	}
	return u.ID
}

// mockOrderStore is an in-memory OrderStore; only Get is exercised by the
// tests but the full interface must be satisfied.
type mockOrderStore struct {
	mu     sync.Mutex
	orders map[string]*matching.Order
}

func newMockOrderStore() *mockOrderStore {
	return &mockOrderStore{orders: map[string]*matching.Order{}}
}

func (m *mockOrderStore) Save(o *matching.Order) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.orders[o.ID] = o
	return nil
}

func (m *mockOrderStore) Get(id string) (*matching.Order, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.orders[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return o, nil
}

func (m *mockOrderStore) ListByUser(userID, pair string, status matching.OrderStatus, limit, offset int) ([]*matching.Order, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*matching.Order, 0)
	for _, o := range m.orders {
		if o.UserID == userID {
			out = append(out, o)
		}
	}
	return out, nil
}

func (m *mockOrderStore) UpdateOrderStatus(id string, status matching.OrderStatus) error { return nil }
func (m *mockOrderStore) SaveTrade(t *matching.Trade) error                              { return nil }
func (m *mockOrderStore) GetTrades(pair string, limit int) ([]*matching.Trade, error) {
	return nil, nil
}

// fakeWalletService implements WalletService and records deposits.
type fakeWalletService struct {
	mu       sync.Mutex
	deposits []fakeDeposit
}

type fakeDeposit struct {
	userID, asset string
	amount        *big.Float
}

func (f *fakeWalletService) GetBalance(userID, asset string) (*wallet.Wallet, error) {
	return &wallet.Wallet{UserID: userID, Asset: asset, Balance: new(big.Float), Locked: new(big.Float)}, nil
}
func (f *fakeWalletService) GetBalances(userID string) ([]*wallet.Wallet, error) {
	return nil, nil
}
func (f *fakeWalletService) Deposit(userID, asset string, amount *big.Float, txHash string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deposits = append(f.deposits, fakeDeposit{userID: userID, asset: asset, amount: new(big.Float).Copy(amount)})
	return nil
}
func (f *fakeWalletService) Withdraw(userID, asset, address string, amount *big.Float) error {
	return nil
}
func (f *fakeWalletService) ListTransactions(userID string, limit, offset int) ([]*wallet.Transaction, error) {
	return nil, nil
}
func (f *fakeWalletService) ReserveForOrder(userID, asset string, amount *big.Float) (*wallet.Wallet, error) {
	return &wallet.Wallet{UserID: userID, Asset: asset, Balance: new(big.Float), Locked: new(big.Float)}, nil
}
func (f *fakeWalletService) ReserveOrder(orderID, userID, pair string, side int, orderType int, price, qty *big.Float) error {
	return nil
}
func (f *fakeWalletService) Transfer(userID, from, to, asset string, amount *big.Float) error {
	return nil
}

// ---------------------------------------------------------------------------
// test server
// ---------------------------------------------------------------------------

type testServer struct {
	engine    *gin.Engine
	authH     *AuthHandler
	users     *mockUserStore
	orders    *mockOrderStore
	walletSvc *fakeWalletService
}

// newTestServer wires a minimal router mirroring the production routes that
// are under test: auth endpoints, a protected probe, protected order lookup
// and the admin-only deposit path.
func newTestServer(t *testing.T) *testServer {
	t.Helper()
	users := newMockUserStore()
	orders := newMockOrderStore()
	ws := &fakeWalletService{}

	jm := auth.NewJWTManager("test-secret-key", "nexa-test")
	authH := NewAuthHandler(jm, users)
	// Exercise the SetCache wiring (cache-backed lockout + blacklist
	// write-through). The generous policy keeps lockout from interfering
	// with the auth-flow assertions; only failed logins consume lockout
	// quota, and this suite triggers at most one failure per account.
	authH.SetCache(cache.NewMemoryCache(time.Minute))
	authH.SetLockoutPolicy(10000, time.Minute)

	oh := NewOrderHandlerWithExchange(nil, orders, nil)
	wh := NewWalletHandler(ws, nil)

	r := gin.New()
	api := r.Group("/api/v2")

	a := api.Group("/auth")
	a.POST("/register", authH.Register)
	a.POST("/login", authH.Login)
	a.POST("/refresh", authH.RefreshToken)
	a.POST("/logout", authH.AuthMiddleware(), authH.Logout)

	prot := api.Group("", authH.AuthMiddleware())
	prot.GET("/me", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"user_id": c.GetString("user_id"), "role": c.GetString("role")})
	})
	prot.GET("/order/:id", oh.GetOrder)

	admin := api.Group("/admin")
	admin.Use(authH.AuthMiddleware(), AdminOnly())
	admin.POST("/wallet/deposit", wh.Deposit)

	return &testServer{engine: r, authH: authH, users: users, orders: orders, walletSvc: ws}
}

// do issues a JSON request and returns the recorder plus the decoded body.
func (s *testServer) do(t *testing.T, method, path, token string, body any) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	s.engine.ServeHTTP(w, req)
	out := map[string]any{}
	if w.Body.Len() > 0 {
		_ = json.Unmarshal(w.Body.Bytes(), &out)
	}
	return w, out
}

func (s *testServer) register(t *testing.T, email, password string) (userID, token string) {
	t.Helper()
	w, body := s.do(t, http.MethodPost, "/api/v2/auth/register", "", map[string]string{
		"email": email, "password": password,
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("register %s: status %d body %s", email, w.Code, w.Body.String())
	}
	uid, _ := body["user_id"].(string)
	tok, _ := body["token"].(string)
	if uid == "" || tok == "" {
		t.Fatalf("register response missing user_id/token: %v", body)
	}
	return uid, tok
}

func (s *testServer) login(t *testing.T, email, password string) (access, refresh string, status int) {
	t.Helper()
	w, body := s.do(t, http.MethodPost, "/api/v2/auth/login", "", map[string]string{
		"email": email, "password": password,
	})
	a, _ := body["access_token"].(string)
	r, _ := body["refresh_token"].(string)
	return a, r, w.Code
}

// ---------------------------------------------------------------------------
// tests
// ---------------------------------------------------------------------------

// TestAuthLifecycle covers the full token flow: register -> access protected
// endpoint -> refresh rotation -> logout -> blacklisted token rejected.
func TestAuthLifecycle(t *testing.T) {
	s := newTestServer(t)
	email, password := "alice@example.com", "password123"

	// Register.
	uidA, tokA := s.register(t, email, password)

	// Duplicate registration is rejected.
	w, _ := s.do(t, http.MethodPost, "/api/v2/auth/register", "", map[string]string{
		"email": email, "password": password,
	})
	if w.Code != http.StatusConflict {
		t.Errorf("duplicate register: status %d, want 409", w.Code)
	}

	// Protected endpoint requires a token.
	w, _ = s.do(t, http.MethodGet, "/api/v2/me", "", nil)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("no token: status %d, want 401", w.Code)
	}

	// Register token authenticates.
	w, body := s.do(t, http.MethodGet, "/api/v2/me", tokA, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("register token rejected: %d %s", w.Code, w.Body.String())
	}
	if body["user_id"] != uidA {
		t.Errorf("me user_id = %v, want %s", body["user_id"], uidA)
	}

	// Garbage token rejected.
	w, _ = s.do(t, http.MethodGet, "/api/v2/me", "not-a-jwt", nil)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("garbage token: status %d, want 401", w.Code)
	}

	// Login with a wrong password fails.
	_, _, status := s.login(t, email, "wrong-password")
	if status != http.StatusUnauthorized {
		t.Errorf("wrong password login: status %d, want 401", status)
	}

	// Correct login yields access + refresh tokens.
	access, refresh, status := s.login(t, email, password)
	if status != http.StatusOK || access == "" || refresh == "" {
		t.Fatalf("login failed: status %d", status)
	}

	// Refresh rotation: a valid refresh token returns a fresh pair.
	w, body = s.do(t, http.MethodPost, "/api/v2/auth/refresh", "", map[string]string{"refresh_token": refresh})
	if w.Code != http.StatusOK {
		t.Fatalf("refresh: status %d body %s", w.Code, w.Body.String())
	}
	newAccess, _ := body["access_token"].(string)
	newRefresh, _ := body["refresh_token"].(string)
	if newAccess == "" || newRefresh == "" {
		t.Fatalf("refresh missing tokens: %v", body)
	}
	w, _ = s.do(t, http.MethodGet, "/api/v2/me", newAccess, nil)
	if w.Code != http.StatusOK {
		t.Errorf("rotated access token rejected: %d", w.Code)
	}

	// The rotated-out refresh token is single-use: replay must fail.
	w, _ = s.do(t, http.MethodPost, "/api/v2/auth/refresh", "", map[string]string{"refresh_token": refresh})
	if w.Code != http.StatusUnauthorized {
		t.Errorf("replayed refresh token: status %d, want 401", w.Code)
	}

	// An access token must not work on the refresh endpoint.
	w, _ = s.do(t, http.MethodPost, "/api/v2/auth/refresh", "", map[string]string{"refresh_token": newAccess})
	if w.Code != http.StatusUnauthorized {
		t.Errorf("access token as refresh: status %d, want 401", w.Code)
	}

	// A refresh token must not authenticate API requests.
	w, _ = s.do(t, http.MethodGet, "/api/v2/me", newRefresh, nil)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("refresh token on API: status %d, want 401", w.Code)
	}

	// Logout blacklists the presented token.
	w, _ = s.do(t, http.MethodPost, "/api/v2/auth/logout", newAccess, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("logout: status %d", w.Code)
	}
	w, _ = s.do(t, http.MethodGet, "/api/v2/me", newAccess, nil)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("logged-out token still works: status %d, want 401", w.Code)
	}

	// Logout itself requires authentication.
	w, _ = s.do(t, http.MethodPost, "/api/v2/auth/logout", "", nil)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated logout: status %d, want 401", w.Code)
	}
}

// TestGetOrderOwnership verifies IDOR protection on order lookup: owners get
// 200, everyone else gets 404 (never 403) so existence is not leaked.
func TestGetOrderOwnership(t *testing.T) {
	s := newTestServer(t)

	uidA, tokA := s.register(t, "owner@example.com", "password123")
	_, tokB := s.register(t, "stranger@example.com", "password123")

	price, _ := new(big.Float).SetString("50000")
	qty, _ := new(big.Float).SetString("0.1")
	order := matching.NewOrder(uidA, "BTC/USDT", matching.Buy, matching.Limit, price, qty)
	if err := s.orders.Save(order); err != nil {
		t.Fatal(err)
	}

	// Owner can read the order.
	w, body := s.do(t, http.MethodGet, "/api/v2/order/"+order.ID, tokA, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("owner GetOrder: status %d body %s", w.Code, w.Body.String())
	}
	if body["id"] != order.ID || body["user_id"] != uidA {
		t.Errorf("owner GetOrder wrong payload: %v", body)
	}

	// Another user sees 404, not 403, so existence is not leaked.
	w, _ = s.do(t, http.MethodGet, "/api/v2/order/"+order.ID, tokB, nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("foreign GetOrder: status %d, want 404", w.Code)
	}

	// Unknown ids are also 404.
	w, _ = s.do(t, http.MethodGet, "/api/v2/order/does-not-exist", tokA, nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("unknown order: status %d, want 404", w.Code)
	}

	// Unauthenticated requests are rejected before lookup.
	w, _ = s.do(t, http.MethodGet, "/api/v2/order/"+order.ID, "", nil)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated GetOrder: status %d, want 401", w.Code)
	}
}

// TestAdminDepositAccess verifies the privilege-escalation fix: crediting
// balances moved from the user group to the admin group.
func TestAdminDepositAccess(t *testing.T) {
	s := newTestServer(t)

	adminEmail, adminPass := "admin@example.com", "adminpass123"
	adminID := s.users.seedAdmin(t, adminEmail, adminPass)
	_, userTok := s.register(t, "pleb@example.com", "password123")

	depositBody := map[string]string{"asset": "USDT", "amount": "1000", "tx_hash": "0xabc"}

	// Ordinary user hitting the admin route gets 403.
	w, _ := s.do(t, http.MethodPost, "/api/v2/admin/wallet/deposit", userTok, depositBody)
	if w.Code != http.StatusForbidden {
		t.Errorf("user on admin deposit: status %d, want 403", w.Code)
	}

	// Anonymous gets 401 first (auth runs before AdminOnly).
	w, _ = s.do(t, http.MethodPost, "/api/v2/admin/wallet/deposit", "", depositBody)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("anonymous on admin deposit: status %d, want 401", w.Code)
	}

	// The old user-facing path no longer exists.
	w, _ = s.do(t, http.MethodPost, "/api/v2/wallet/deposit", userTok, depositBody)
	if w.Code != http.StatusNotFound {
		t.Errorf("legacy user deposit path: status %d, want 404", w.Code)
	}

	// Admin succeeds and the credit is applied.
	adminTok, _, status := s.login(t, adminEmail, adminPass)
	if status != http.StatusOK || adminTok == "" {
		t.Fatalf("admin login failed: %d", status)
	}
	w, body := s.do(t, http.MethodPost, "/api/v2/admin/wallet/deposit", adminTok, depositBody)
	if w.Code != http.StatusOK {
		t.Fatalf("admin deposit: status %d body %s", w.Code, w.Body.String())
	}
	if body["status"] != "ok" {
		t.Errorf("admin deposit payload: %v", body)
	}
	if len(s.walletSvc.deposits) != 1 {
		t.Fatalf("deposits = %v, want exactly 1", s.walletSvc.deposits)
	}
	d := s.walletSvc.deposits[0]
	if d.userID != adminID || d.asset != "USDT" || d.amount.Cmp(big.NewFloat(1000)) != 0 {
		t.Errorf("deposit = %+v, want {%s USDT 1000}", d, adminID)
	}

	// Invalid amounts are rejected even for admins.
	w, _ = s.do(t, http.MethodPost, "/api/v2/admin/wallet/deposit", adminTok, map[string]string{"asset": "USDT", "amount": "-5"})
	if w.Code != http.StatusBadRequest {
		t.Errorf("negative deposit: status %d, want 400", w.Code)
	}
}

// TestLoginLockout uses a dedicated server with a tight in-memory lockout
// policy (no cache, so only failures count) and verifies the account locks
// after repeated bad passwords and recovers afterwards.
func TestLoginLockout(t *testing.T) {
	s := newTestServer(t)
	// Replace the cache-backed limiter with the in-memory failure counter so
	// successful logins do not consume lockout slots.
	s.authH.SetCache(nil)
	s.authH.SetLockoutPolicy(3, time.Minute)

	email, password := "lockme@example.com", "password123"
	s.register(t, email, password)

	for i := 0; i < 3; i++ {
		if _, _, status := s.login(t, email, "bad-password-1"); status != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status %d, want 401", i, status)
		}
	}

	// Locked: even the correct password is refused while locked.
	_, _, status := s.login(t, email, password)
	if status != http.StatusLocked {
		t.Errorf("locked account login: status %d, want 423", status)
	}
}
