package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// accountTestContext builds a gin context carrying the authenticated
// user_id, as AuthMiddleware would set it.
func accountTestContext(t *testing.T, userID string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v2/account", nil)
	if userID != "" {
		c.Set("user_id", userID)
	}
	return c, w
}

// Regression: when Postgres is unreachable the gateway boots in
// best-effort mode with a nil UserStore. GetAccount must answer 503
// instead of panicking on the nil interface (which gin.Recovery would
// turn into an opaque 500).
func TestGetAccountNilUserStore(t *testing.T) {
	h := NewAccountHandler(nil, nil, nil, nil)
	c, w := accountTestContext(t, "usr_test")
	h.GetAccount(c)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d (body %s)", w.Code, http.StatusServiceUnavailable, w.Body.String())
	}
}

// Same guard for the profile endpoint.
func TestGetProfileNilUserStore(t *testing.T) {
	h := NewAccountHandler(nil, nil, nil, nil)
	c, w := accountTestContext(t, "usr_test")
	h.GetProfile(c)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d (body %s)", w.Code, http.StatusServiceUnavailable, w.Body.String())
	}
}

// Missing user_id stays a 401 even when the store is nil, so auth errors
// are never masked by the availability check.
func TestGetAccountUnauthenticatedPrecedesAvailability(t *testing.T) {
	h := NewAccountHandler(nil, nil, nil, nil)
	c, w := accountTestContext(t, "")
	h.GetAccount(c)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// Happy path: valid store returns 200 with profile fields and an
// (empty) balances array.
func TestGetAccountOK(t *testing.T) {
	users := newMockUserStore()
	users.byID["usr_test"] = &User{ID: "usr_test", Email: "alice@example.com", Role: "user"}
	h := NewAccountHandler(users, &fakeWalletService{}, nil, nil)
	c, w := accountTestContext(t, "usr_test")
	h.GetAccount(c)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", w.Code, http.StatusOK, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON body: %v", err)
	}
	if body["user_id"] != "usr_test" || body["email"] != "alice@example.com" {
		t.Fatalf("unexpected body: %v", body)
	}
	if _, ok := body["balances"].([]any); !ok {
		t.Fatalf("balances missing or wrong type: %v", body["balances"])
	}
}
