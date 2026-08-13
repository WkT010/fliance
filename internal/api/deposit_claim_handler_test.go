package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/WkT010/nexa-exchange/internal/wallet"
	"github.com/gin-gonic/gin"
)

// ---------------------------------------------------------------------------
// in-memory fake DepositClaimStore (mirrors the PG store's semantics: global
// txid uniqueness, pending-only review)
// ---------------------------------------------------------------------------

type fakeDepositClaimStore struct {
	mu     sync.Mutex
	claims map[string]*DepositClaim
	txids  map[string]bool
	emails map[string]string // user_id -> email (admin listing enrichment)
}

func newFakeDepositClaimStore() *fakeDepositClaimStore {
	return &fakeDepositClaimStore{
		claims: map[string]*DepositClaim{},
		txids:  map[string]bool{},
		emails: map[string]string{},
	}
}

func (f *fakeDepositClaimStore) SubmitClaim(cl *DepositClaim) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.txids[cl.TxID] {
		return ErrDepositClaimTxidDuplicate
	}
	f.txids[cl.TxID] = true
	f.claims[cl.ID] = cl
	return nil
}

func (f *fakeDepositClaimStore) GetClaimByID(id string) (*DepositClaim, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.claims[id], nil
}

func (f *fakeDepositClaimStore) ListClaimsByUser(userID string) ([]*DepositClaim, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*DepositClaim
	for _, cl := range f.claims {
		if cl.UserID == userID {
			out = append(out, cl)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out, nil
}

func (f *fakeDepositClaimStore) ListClaimsForAdmin(status string, limit, offset int) ([]*DepositClaim, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*DepositClaim
	for _, cl := range f.claims {
		if status == "" || cl.Status == status {
			cp := *cl
			cp.UID = cl.UserID
			cp.Email = f.emails[cl.UserID]
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out, nil
}

func (f *fakeDepositClaimStore) ReviewClaim(id, reviewerID, action, reason string) (*DepositClaim, error) {
	if action != "approved" && action != "rejected" {
		return nil, fmt.Errorf("invalid review action %q", action)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	cl := f.claims[id]
	if cl == nil {
		return nil, fmt.Errorf("deposit claim %s not found", id)
	}
	if cl.Status != "pending" {
		return nil, fmt.Errorf("claim is not pending review (already reviewed)")
	}
	cl.Status = action
	cl.RejectReason = reason
	cl.ReviewerID = reviewerID
	cl.ReviewedAt = 1700000001000000000
	return cl, nil
}

func (f *fakeDepositClaimStore) AutoApproveClaim(id, note, reviewer string) (*DepositClaim, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cl := f.claims[id]
	if cl == nil {
		return nil, fmt.Errorf("deposit claim %s not found", id)
	}
	if cl.Status != "pending" {
		return nil, fmt.Errorf("claim is not pending review (already reviewed)")
	}
	cl.Status = "approved"
	cl.RejectReason = note
	cl.ReviewerID = reviewer
	cl.ReviewedAt = 1700000001000000000
	cl.AutoVerified = true
	cl.VerifyNote = note
	cl.VerifiedAt = 1700000001000000000
	return cl, nil
}

func (f *fakeDepositClaimStore) RecordVerifyNote(id, note string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if cl := f.claims[id]; cl != nil {
		cl.VerifyNote = note
		cl.VerifiedAt = 1700000001000000000
	}
	return nil
}

// fakeDepositVerifier stubs the on-chain verifier for handler tests.
type fakeDepositVerifier struct {
	mu        sync.Mutex
	res       *wallet.VerifyResult
	err       error
	calls     int
	lastAsset string
	lastTxid  string
}

func (f *fakeDepositVerifier) VerifyDeposit(ctx context.Context, asset, txid string, amount *big.Float) (*wallet.VerifyResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.lastAsset, f.lastTxid = asset, txid
	if f.err != nil {
		return nil, f.err
	}
	return f.res, nil
}

// ---------------------------------------------------------------------------
// test wiring
// ---------------------------------------------------------------------------

type claimTestServer struct {
	engine *gin.Engine
	store  *fakeDepositClaimStore
	h      *DepositClaimHandler
}

func newClaimTestServer(t *testing.T) *claimTestServer {
	t.Helper()
	st := newFakeDepositClaimStore()
	h := NewDepositClaimHandler(st, t.TempDir())
	r := gin.New()
	// Stand-in for the JWT middleware: the test picks the identity via the
	// X-User header.
	mw := func(c *gin.Context) {
		c.Set("user_id", c.GetHeader("X-User"))
		c.Next()
	}
	r.POST("/api/v2/wallet/deposit/claim", mw, h.Submit)
	r.GET("/api/v2/wallet/deposit/claims", mw, h.List)
	r.GET("/api/v2/admin/deposit/claims", mw, h.AdminList)
	r.GET("/api/v2/admin/deposit/claims/:id/screenshot", mw, h.AdminScreenshot)
	r.POST("/api/v2/admin/deposit/claims/:id/review", mw, h.AdminReview)
	return &claimTestServer{engine: r, store: st, h: h}
}

func (s *claimTestServer) do(t *testing.T, method, path, user string, body any) (*httptest.ResponseRecorder, map[string]any) {
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
	req.Header.Set("X-User", user)
	w := httptest.NewRecorder()
	s.engine.ServeHTTP(w, req)
	out := map[string]any{}
	if w.Body.Len() > 0 {
		_ = json.Unmarshal(w.Body.Bytes(), &out)
	}
	return w, out
}

// pngDataURL returns a data URL whose decoded bytes carry the PNG magic
// prefix (decodeKycDoc validates magic bytes, not full image structure).
func pngDataURL() string {
	raw := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x01, 0x02}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw)
}

// ---------------------------------------------------------------------------
// submission
// ---------------------------------------------------------------------------

func TestDepositClaimSubmitHappyPath(t *testing.T) {
	s := newClaimTestServer(t)
	w, body := s.do(t, http.MethodPost, "/api/v2/wallet/deposit/claim", "usr_alice", map[string]string{
		"asset": "usdt", "amount": "1000", "txid": "0xabc123", "screenshot": pngDataURL(),
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("submit: status %d body %s", w.Code, w.Body.String())
	}
	id, _ := body["id"].(string)
	if !strings.HasPrefix(id, "dep_") || body["status"] != "pending" {
		t.Fatalf("payload = %v, want dep_* id + pending status", body)
	}
	// Screenshot persisted on disk.
	cl := s.store.claims[id]
	if cl == nil {
		t.Fatal("claim not stored")
	}
	if cl.ScreenshotPath == "" {
		t.Error("screenshot path not stored")
	} else if _, err := os.Stat(cl.ScreenshotPath); err != nil {
		t.Errorf("screenshot file missing: %v", err)
	}
	if cl.Asset != "USDT" {
		t.Errorf("asset = %q, want normalized USDT", cl.Asset)
	}
	if cl.Amount.Cmp(big.NewFloat(1000)) != 0 {
		t.Errorf("amount = %v, want 1000", cl.Amount)
	}
}

func TestDepositClaimSubmitValidation(t *testing.T) {
	s := newClaimTestServer(t)
	cases := []struct {
		name string
		body map[string]string
	}{
		{"missing txid", map[string]string{"asset": "USDT", "amount": "10"}},
		{"missing amount", map[string]string{"asset": "USDT", "txid": "0x1"}},
		{"missing asset", map[string]string{"amount": "10", "txid": "0x1"}},
		{"zero amount", map[string]string{"asset": "USDT", "amount": "0", "txid": "0x1"}},
		{"negative amount", map[string]string{"asset": "USDT", "amount": "-5", "txid": "0x1"}},
		{"garbage amount", map[string]string{"asset": "USDT", "amount": "abc", "txid": "0x1"}},
		{"bad screenshot", map[string]string{"asset": "USDT", "amount": "1", "txid": "0x1", "screenshot": "data:image/png;base64,!!!"}},
		{"gif screenshot", map[string]string{"asset": "USDT", "amount": "1", "txid": "0x1",
			"screenshot": "data:image/gif;base64," + base64.StdEncoding.EncodeToString([]byte("GIF89a...."))}},
	}
	for _, tc := range cases {
		w, _ := s.do(t, http.MethodPost, "/api/v2/wallet/deposit/claim", "usr_alice", tc.body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400 (body %s)", tc.name, w.Code, w.Body.String())
		}
	}
	if len(s.store.claims) != 0 {
		t.Errorf("claims stored = %d, want 0", len(s.store.claims))
	}
}

func TestDepositClaimSubmitDuplicateTxid(t *testing.T) {
	s := newClaimTestServer(t)
	body := map[string]string{"asset": "USDT", "amount": "10", "txid": "0xdup"}
	w, _ := s.do(t, http.MethodPost, "/api/v2/wallet/deposit/claim", "usr_alice", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("first submit: status %d", w.Code)
	}
	// Same txid — even from another user or after a rejection — must 409.
	w, out := s.do(t, http.MethodPost, "/api/v2/wallet/deposit/claim", "usr_bob", body)
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate txid: status %d, want 409", w.Code)
	}
	if out["error"] != "txid already submitted" {
		t.Errorf("error = %v, want %q", out["error"], "txid already submitted")
	}
	// A different txid still goes through.
	w, _ = s.do(t, http.MethodPost, "/api/v2/wallet/deposit/claim", "usr_bob",
		map[string]string{"asset": "USDT", "amount": "10", "txid": "0xother"})
	if w.Code != http.StatusCreated {
		t.Errorf("fresh txid: status %d, want 201", w.Code)
	}
}

// ---------------------------------------------------------------------------
// user listing
// ---------------------------------------------------------------------------

func TestDepositClaimListOwnOnly(t *testing.T) {
	s := newClaimTestServer(t)
	s.do(t, http.MethodPost, "/api/v2/wallet/deposit/claim", "usr_alice",
		map[string]string{"asset": "USDT", "amount": "1", "txid": "0xa"})
	// Distinct created_at nanos so the newest-first ordering is observable.
	time.Sleep(time.Millisecond)
	s.do(t, http.MethodPost, "/api/v2/wallet/deposit/claim", "usr_alice",
		map[string]string{"asset": "BTC", "amount": "0.5", "txid": "0xb"})
	s.do(t, http.MethodPost, "/api/v2/wallet/deposit/claim", "usr_bob",
		map[string]string{"asset": "USDT", "amount": "2", "txid": "0xc"})

	w, body := s.do(t, http.MethodGet, "/api/v2/wallet/deposit/claims", "usr_alice", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list: status %d", w.Code)
	}
	claims, _ := body["claims"].([]any)
	if len(claims) != 2 {
		t.Fatalf("claims = %d, want 2 (own only)", len(claims))
	}
	// Newest first and contract fields present.
	first, _ := claims[0].(map[string]any)
	for _, k := range []string{"id", "asset", "amount", "txid", "status", "reject_reason", "created_at", "reviewed_at"} {
		if _, ok := first[k]; !ok {
			t.Errorf("claim JSON missing key %q: %v", k, first)
		}
	}
	if first["txid"] != "0xb" {
		t.Errorf("first claim txid = %v, want newest (0xb)", first["txid"])
	}
	if amt, _ := first["amount"].(string); amt == "" {
		t.Errorf("amount not serialized as string: %v", first["amount"])
	}
}

// ---------------------------------------------------------------------------
// admin review
// ---------------------------------------------------------------------------

func TestDepositClaimAdminReviewLifecycle(t *testing.T) {
	s := newClaimTestServer(t)
	s.store.emails["usr_alice"] = "alice@example.com"
	_, body := s.do(t, http.MethodPost, "/api/v2/wallet/deposit/claim", "usr_alice",
		map[string]string{"asset": "USDT", "amount": "1000", "txid": "0xr1", "screenshot": pngDataURL()})
	id, _ := body["id"].(string)

	// Invalid action refused.
	w, _ := s.do(t, http.MethodPost, "/api/v2/admin/deposit/claims/"+id+"/review", "usr_admin",
		map[string]string{"action": "maybe"})
	if w.Code != http.StatusBadRequest {
		t.Errorf("invalid action: status %d, want 400", w.Code)
	}

	// Unknown id → 400 with an error message.
	w, out := s.do(t, http.MethodPost, "/api/v2/admin/deposit/claims/dep_missing/review", "usr_admin",
		map[string]string{"action": "approve"})
	if w.Code != http.StatusBadRequest {
		t.Errorf("unknown claim review: status %d, want 400", w.Code)
	} else if out["error"] == "" {
		t.Error("unknown claim review: missing error message")
	}

	// Approve succeeds.
	w, out = s.do(t, http.MethodPost, "/api/v2/admin/deposit/claims/"+id+"/review", "usr_admin",
		map[string]string{"action": "approve"})
	if w.Code != http.StatusOK || out["ok"] != true {
		t.Fatalf("approve: status %d body %s", w.Code, w.Body.String())
	}
	if cl := s.store.claims[id]; cl.Status != "approved" || cl.ReviewerID != "usr_admin" {
		t.Errorf("claim = %+v, want approved by usr_admin", cl)
	}

	// Second review of the same claim is rejected (state machine guard).
	w, out = s.do(t, http.MethodPost, "/api/v2/admin/deposit/claims/"+id+"/review", "usr_admin",
		map[string]string{"action": "reject", "reason": "changed my mind"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("double review: status %d, want 400", w.Code)
	}
	if !strings.Contains(fmt.Sprint(out["error"]), "already reviewed") {
		t.Errorf("double review error = %v, want already-reviewed", out["error"])
	}
	if cl := s.store.claims[id]; cl.Status != "approved" {
		t.Errorf("double review mutated claim: %+v", cl)
	}

	// Reject path records the reason.
	_, body = s.do(t, http.MethodPost, "/api/v2/wallet/deposit/claim", "usr_alice",
		map[string]string{"asset": "USDT", "amount": "5", "txid": "0xr2"})
	id2, _ := body["id"].(string)
	w, out = s.do(t, http.MethodPost, "/api/v2/admin/deposit/claims/"+id2+"/review", "usr_admin",
		map[string]string{"action": "reject", "reason": "tx not found on chain"})
	if w.Code != http.StatusOK || out["ok"] != true {
		t.Fatalf("reject: status %d body %s", w.Code, w.Body.String())
	}
	if cl := s.store.claims[id2]; cl.Status != "rejected" || cl.RejectReason != "tx not found on chain" {
		t.Errorf("claim = %+v, want rejected with reason", cl)
	}
}

// ---------------------------------------------------------------------------
// admin listing + screenshot
// ---------------------------------------------------------------------------

func TestDepositClaimAdminListAndScreenshot(t *testing.T) {
	s := newClaimTestServer(t)
	s.store.emails["usr_alice"] = "alice@example.com"
	_, body := s.do(t, http.MethodPost, "/api/v2/wallet/deposit/claim", "usr_alice",
		map[string]string{"asset": "USDT", "amount": "7", "txid": "0xshot", "screenshot": pngDataURL()})
	id, _ := body["id"].(string)
	s.do(t, http.MethodPost, "/api/v2/wallet/deposit/claim", "usr_alice",
		map[string]string{"asset": "USDT", "amount": "8", "txid": "0xplain"})

	// Listing carries uid/email/user_id/reviewer_id on top of the base shape.
	w, body := s.do(t, http.MethodGet, "/api/v2/admin/deposit/claims?status=pending&limit=100&offset=0", "usr_admin", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("admin list: status %d", w.Code)
	}
	claims, _ := body["claims"].([]any)
	if len(claims) != 2 {
		t.Fatalf("admin claims = %d, want 2", len(claims))
	}
	item, _ := claims[0].(map[string]any)
	for _, k := range []string{"id", "user_id", "uid", "email", "asset", "amount", "txid", "status", "reject_reason", "created_at", "reviewed_at", "reviewer_id"} {
		if _, ok := item[k]; !ok {
			t.Errorf("admin claim JSON missing key %q: %v", k, item)
		}
	}

	// Screenshot endpoint streams the stored png bytes.
	w = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v2/admin/deposit/claims/"+id+"/screenshot", nil)
	req.Header.Set("X-User", "usr_admin")
	s.engine.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("screenshot: status %d body %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("content-type = %q, want image/png", ct)
	}
	if !bytes.HasPrefix(w.Body.Bytes(), []byte{0x89, 'P', 'N', 'G'}) {
		t.Error("screenshot bytes do not start with the PNG magic")
	}

	// Claim without a screenshot → 404.
	var plainID string
	for cid, cl := range s.store.claims {
		if cl.TxID == "0xplain" {
			plainID = cid
		}
	}
	w, out := s.do(t, http.MethodGet, "/api/v2/admin/deposit/claims/"+plainID+"/screenshot", "usr_admin", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("no-screenshot: status %d, want 404", w.Code)
	}
	// Unknown claim → 404 as well.
	w, _ = s.do(t, http.MethodGet, "/api/v2/admin/deposit/claims/dep_missing/screenshot", "usr_admin", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("unknown claim screenshot: status %d, want 404", w.Code)
	}
	_ = out
}

// ---------------------------------------------------------------------------
// on-chain auto-verification (Alchemy)
// ---------------------------------------------------------------------------

func TestDepositClaimAutoVerifyApproves(t *testing.T) {
	s := newClaimTestServer(t)
	v := &fakeDepositVerifier{res: &wallet.VerifyResult{
		OK:            true,
		Note:          "auto-verified via Alchemy: USDT transfer of 1000 confirmed in tx 0xok (12 confirmations)",
		MatchedAmount: big.NewFloat(1000),
		TxHash:        "0xok",
	}}
	s.h.SetDepositVerifier(v)

	w, body := s.do(t, http.MethodPost, "/api/v2/wallet/deposit/claim", "usr_alice",
		map[string]string{"asset": "USDT", "amount": "900", "txid": "0xok"})
	if w.Code != http.StatusCreated {
		t.Fatalf("submit: status %d body %s", w.Code, w.Body.String())
	}
	// The response lets the frontend see the auto-approval immediately.
	if body["status"] != "approved" || body["auto_verified"] != true {
		t.Fatalf("payload = %v, want approved + auto_verified", body)
	}
	if note, _ := body["verify_note"].(string); !strings.Contains(note, "auto-verified") {
		t.Errorf("verify_note = %q, want alchemy rationale", note)
	}
	// Store reflects the system review.
	id, _ := body["id"].(string)
	cl := s.store.claims[id]
	if cl.Status != "approved" || !cl.AutoVerified {
		t.Fatalf("claim = %+v, want approved + auto-verified", cl)
	}
	if cl.ReviewerID != "alchemy" {
		t.Errorf("reviewer = %q, want alchemy", cl.ReviewerID)
	}
	if v.calls != 1 || v.lastAsset != "USDT" || v.lastTxid != "0xok" {
		t.Errorf("verifier usage = %+v, want one USDT/0xok call", v)
	}
}

func TestDepositClaimAutoVerifyInsufficientAmountStaysPending(t *testing.T) {
	s := newClaimTestServer(t)
	s.h.SetDepositVerifier(&fakeDepositVerifier{res: &wallet.VerifyResult{
		OK:   false,
		Note: "on-chain USDT transfer value 100 is below the claimed amount (claimed >= 900 required)",
	}})

	w, body := s.do(t, http.MethodPost, "/api/v2/wallet/deposit/claim", "usr_alice",
		map[string]string{"asset": "USDT", "amount": "900", "txid": "0xshort"})
	if w.Code != http.StatusCreated {
		t.Fatalf("submit: status %d body %s", w.Code, w.Body.String())
	}
	if body["status"] != "pending" || body["auto_verified"] != false {
		t.Fatalf("payload = %v, want pending + not auto-verified", body)
	}
	id, _ := body["id"].(string)
	cl := s.store.claims[id]
	if cl.Status != "pending" {
		t.Fatalf("claim status = %q, want pending (never auto-reject)", cl.Status)
	}
	if !strings.Contains(cl.VerifyNote, "below the claimed amount") {
		t.Errorf("verify_note = %q, want insufficiency reason", cl.VerifyNote)
	}
}

func TestDepositClaimAutoVerifyFailedReceiptStaysPending(t *testing.T) {
	s := newClaimTestServer(t)
	s.h.SetDepositVerifier(&fakeDepositVerifier{res: &wallet.VerifyResult{
		OK:   false,
		Note: "transaction reverted on chain (receipt status 0x0)",
	}})

	w, body := s.do(t, http.MethodPost, "/api/v2/wallet/deposit/claim", "usr_alice",
		map[string]string{"asset": "ETH", "amount": "1", "txid": "0xreverted"})
	if w.Code != http.StatusCreated {
		t.Fatalf("submit: status %d body %s", w.Code, w.Body.String())
	}
	if body["status"] != "pending" || body["auto_verified"] != false {
		t.Fatalf("payload = %v, want pending + not auto-verified", body)
	}
	id, _ := body["id"].(string)
	if note := s.store.claims[id].VerifyNote; !strings.Contains(note, "reverted") {
		t.Errorf("verify_note = %q, want receipt-failure reason", note)
	}
}

func TestDepositClaimAutoVerifySkipsUnverifiableAssets(t *testing.T) {
	s := newClaimTestServer(t)
	v := &fakeDepositVerifier{res: &wallet.VerifyResult{OK: true, Note: "should never be used"}}
	s.h.SetDepositVerifier(v)

	// BTC/ADA/SOL/BNB are not covered by the Ethereum verifier and must go
	// straight to the manual queue without consulting the chain.
	for i, asset := range []string{"BTC", "ADA", "SOL", "BNB"} {
		w, body := s.do(t, http.MethodPost, "/api/v2/wallet/deposit/claim", "usr_alice",
			map[string]string{"asset": asset, "amount": "1", "txid": fmt.Sprintf("0xnv%d", i)})
		if w.Code != http.StatusCreated {
			t.Fatalf("%s submit: status %d", asset, w.Code)
		}
		if body["status"] != "pending" || body["auto_verified"] != false {
			t.Errorf("%s payload = %v, want pending + not auto-verified", asset, body)
		}
		if note, _ := body["verify_note"].(string); note != "" {
			t.Errorf("%s verify_note = %q, want empty (no attempt)", asset, note)
		}
	}
	if v.calls != 0 {
		t.Errorf("verifier called %d times, want 0 for unverifiable assets", v.calls)
	}
}

func TestDepositClaimAutoVerifyGracefulWithoutVerifier(t *testing.T) {
	// No ALCHEMY_API_KEY → no verifier wired → plain manual flow, no error.
	s := newClaimTestServer(t)
	w, body := s.do(t, http.MethodPost, "/api/v2/wallet/deposit/claim", "usr_alice",
		map[string]string{"asset": "ETH", "amount": "2", "txid": "0xnokey"})
	if w.Code != http.StatusCreated {
		t.Fatalf("submit: status %d body %s", w.Code, w.Body.String())
	}
	if body["status"] != "pending" || body["auto_verified"] != false {
		t.Fatalf("payload = %v, want pending + not auto-verified", body)
	}
}

func TestDepositClaimAutoVerifyInfraErrorStaysPending(t *testing.T) {
	s := newClaimTestServer(t)
	s.h.SetDepositVerifier(&fakeDepositVerifier{err: errors.New("rpc eth_getTransactionReceipt: connection refused")})

	w, body := s.do(t, http.MethodPost, "/api/v2/wallet/deposit/claim", "usr_alice",
		map[string]string{"asset": "USDC", "amount": "10", "txid": "0xnetdown"})
	if w.Code != http.StatusCreated {
		t.Fatalf("submit: status %d body %s", w.Code, w.Body.String())
	}
	if body["status"] != "pending" || body["auto_verified"] != false {
		t.Fatalf("payload = %v, want pending + not auto-verified", body)
	}
	id, _ := body["id"].(string)
	if note := s.store.claims[id].VerifyNote; !strings.Contains(note, "auto-verify unavailable") {
		t.Errorf("verify_note = %q, want unavailable reason", note)
	}
}
