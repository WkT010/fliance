package api

import (
	"context"
	"errors"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/WkT010/nexa-exchange/internal/audit"
	"github.com/WkT010/nexa-exchange/internal/wallet"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ErrDepositClaimTxidDuplicate is returned when a claim's txid was already
// submitted (any status — including rejected ones). The handler maps it to
// 409 so one on-chain transaction can never be credited twice.
var ErrDepositClaimTxidDuplicate = errors.New("txid already submitted")

// DepositClaim is one manual deposit proof: the user asserts they sent
// amount of asset on-chain under txid and (optionally) attaches a
// screenshot. An admin reviews it; approval credits the spot wallet.
type DepositClaim struct {
	ID             string
	UserID         string
	Asset          string
	Amount         *big.Float
	TxID           string
	ScreenshotPath string // on-disk path; empty when no screenshot was attached
	Status         string // pending|approved|rejected
	RejectReason   string
	ReviewerID     string
	CreatedAt      int64 // unix nanos
	ReviewedAt     int64
	// Auto-verification bookkeeping (migration 013). AutoVerified is true
	// only when the on-chain verifier approved the claim itself; VerifyNote
	// carries the outcome / failure reason of the verification attempt.
	AutoVerified bool
	VerifyNote   string
	VerifiedAt   int64
	// Admin-listing enrichment (populated only by the admin list endpoint).
	UID   string
	Email string
}

// DepositClaimStore is the persistence contract for manual deposit claims.
type DepositClaimStore interface {
	// SubmitClaim records a new pending claim; returns
	// ErrDepositClaimTxidDuplicate when the txid was already submitted.
	SubmitClaim(cl *DepositClaim) error
	// GetClaimByID loads one claim by ID (nil when absent).
	GetClaimByID(id string) (*DepositClaim, error)
	// ListClaimsByUser returns the user's own claims, newest first.
	ListClaimsByUser(userID string) ([]*DepositClaim, error)
	// ListClaimsForAdmin returns claims filtered by status (newest first,
	// empty status = all), enriched with the claimant's uid/email.
	ListClaimsForAdmin(status string, limit, offset int) ([]*DepositClaim, error)
	// ReviewClaim transitions a pending claim. Approval credits the spot
	// wallet and writes the deposit ledger entry atomically with the status
	// change; reviewing an already-reviewed claim is an error.
	ReviewClaim(id, reviewerID, action, reason string) (*DepositClaim, error)
	// AutoApproveClaim is the automated-reviewer variant of ReviewClaim:
	// same atomic credit semantics, plus it stamps auto_verified=true,
	// verify_note and verified_at in the same transaction.
	AutoApproveClaim(id, note, reviewer string) (*DepositClaim, error)
	// RecordVerifyNote stores the auto-verification outcome on a claim that
	// stays pending (failed / unverifiable attempts). Never changes status.
	RecordVerifyNote(id, note string) error
}

const (
	// depositClaimDir is where claim screenshots are stored (0700 dirs,
	// 0600 files) — same layout and permissions as the KYC document store.
	depositClaimDir = "data/deposit-claims"
	// depositClaimMaxTxIDLen caps the accepted txid length (longest common
	// chain hashes are 64 hex chars; generous headroom, still bounded).
	depositClaimMaxTxIDLen = 256
	// depositAutoReviewer is the reviewer_id recorded when the on-chain
	// verifier approves a claim without human intervention.
	depositAutoReviewer = "alchemy"
	// depositVerifyTimeout bounds the synchronous on-chain verification at
	// submission time; on expiry the claim simply stays pending for manual
	// review (the verifier's own HTTP timeout is tighter). Generous because
	// the verification includes one retry on infra errors.
	depositVerifyTimeout = 55 * time.Second
)

// DepositTxVerifier checks a claimed deposit transaction on-chain. The
// wallet.DepositVerifier (Alchemy, Ethereum mainnet) implements it; the
// interface exists so tests can fake the chain.
type DepositTxVerifier interface {
	VerifyDeposit(ctx context.Context, asset, txid string, amount *big.Float) (*wallet.VerifyResult, error)
}

// DepositClaimHandler serves the user-facing deposit-claim endpoints and the
// admin review endpoints (registered on separate route groups).
type DepositClaimHandler struct {
	store    DepositClaimStore
	dataDir  string
	audit    *audit.Logger
	verifier DepositTxVerifier
}

// NewDepositClaimHandler constructs a deposit-claim handler rooted at
// dataDir (defaults to data/deposit-claims).
func NewDepositClaimHandler(store DepositClaimStore, dataDir string) *DepositClaimHandler {
	if dataDir == "" {
		dataDir = depositClaimDir
	}
	return &DepositClaimHandler{store: store, dataDir: dataDir}
}

// SetAuditLogger wires the asynchronous admin audit logger. Optional: without
// it review decisions simply do not record audit entries (nil-safe).
func (h *DepositClaimHandler) SetAuditLogger(l *audit.Logger) { h.audit = l }

// SetDepositVerifier wires the on-chain auto-verifier (Alchemy). Optional:
// without it — or without an ALCHEMY_API_KEY at startup — every claim stays
// pending for manual review.
func (h *DepositClaimHandler) SetDepositVerifier(v DepositTxVerifier) { h.verifier = v }

type depositClaimSubmitReq struct {
	Asset      string `json:"asset" binding:"required"`
	Amount     string `json:"amount" binding:"required"`
	TxID       string `json:"txid" binding:"required"`
	Screenshot string `json:"screenshot"` // optional data URL, png/jpeg, <=5MB
}

// Submit handles POST /api/v2/wallet/deposit/claim: the user files a deposit
// proof (txid + optional screenshot) for manual review. No balance is touched
// until an admin approves the claim.
func (h *DepositClaimHandler) Submit(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	if h.store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "deposit claims not available"})
		return
	}
	var r depositClaimSubmitReq
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "asset, amount and txid required"})
		return
	}
	asset := strings.TrimSpace(strings.ToUpper(r.Asset))
	if asset == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "asset required"})
		return
	}
	amt := new(big.Float)
	if _, _, err := amt.Parse(strings.TrimSpace(r.Amount), 10); err != nil || amt.Sign() <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid amount"})
		return
	}
	txid := strings.TrimSpace(r.TxID)
	if txid == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "txid required"})
		return
	}
	if len(txid) > depositClaimMaxTxIDLen {
		c.JSON(http.StatusBadRequest, gin.H{"error": "txid too long"})
		return
	}

	claim := &DepositClaim{
		ID:        "dep_" + uuid.NewString(),
		UserID:    userID,
		Asset:     asset,
		Amount:    amt,
		TxID:      txid,
		Status:    "pending",
		CreatedAt: time.Now().UnixNano(),
	}

	// Optional screenshot: same validation and storage style as KYC docs.
	if strings.TrimSpace(r.Screenshot) != "" {
		img, ext, err := decodeKycDoc(r.Screenshot)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "screenshot: " + err.Error()})
			return
		}
		userDir, ok := h.safeUserDir(userID)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
			return
		}
		if err := os.MkdirAll(userDir, 0700); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "storage unavailable"})
			return
		}
		shotPath := filepath.Join(userDir, claim.ID+ext)
		if err := os.WriteFile(shotPath, img, 0600); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "storage unavailable"})
			return
		}
		claim.ScreenshotPath = filepath.ToSlash(shotPath)
	}

	if err := h.store.SubmitClaim(claim); err != nil {
		if claim.ScreenshotPath != "" {
			_ = os.Remove(claim.ScreenshotPath)
		}
		if errors.Is(err, ErrDepositClaimTxidDuplicate) {
			c.JSON(http.StatusConflict, gin.H{"error": "txid already submitted"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "deposit claim submit failed"})
		return
	}

	// On-chain auto-verification (Alchemy, ETH/USDT/USDC on Ethereum
	// mainnet). Strictly best-effort: every failure mode — missing verifier
	// (no ALCHEMY_API_KEY), unverifiable asset, network error, mismatched
	// amount — leaves the claim pending for manual review. A claim is NEVER
	// auto-rejected and NEVER auto-approved on anything but a full match.
	autoVerified, verifyNote := h.autoVerify(c, claim)

	c.JSON(http.StatusCreated, gin.H{
		"id":            claim.ID,
		"status":        claim.Status,
		"auto_verified": autoVerified,
		"verify_note":   verifyNote,
	})
}

// autoVerify runs the synchronous on-chain check for a freshly submitted
// claim and applies the outcome. Returns the final auto_verified flag and a
// human-readable note (empty when verification was not attempted). It never
// fails the request: worst case the claim stays pending.
func (h *DepositClaimHandler) autoVerify(c *gin.Context, claim *DepositClaim) (bool, string) {
	if h.verifier == nil || !wallet.IsAutoVerifiableAsset(claim.Asset) {
		return false, ""
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), depositVerifyTimeout)
	defer cancel()
	res, err := h.verifier.VerifyDeposit(ctx, claim.Asset, claim.TxID, claim.Amount)
	if err != nil && ctx.Err() == nil {
		// One retry on infra errors (TLS handshake blips / slow responses);
		// approval semantics are unaffected — errors still mean "pending".
		time.Sleep(time.Second)
		res, err = h.verifier.VerifyDeposit(ctx, claim.Asset, claim.TxID, claim.Amount)
	}
	now := time.Now().UnixNano()
	if err != nil {
		note := "auto-verify unavailable (manual review required): " + err.Error()
		_ = h.store.RecordVerifyNote(claim.ID, note)
		claim.VerifyNote, claim.VerifiedAt = note, now
		return false, note
	}
	if !res.OK {
		_ = h.store.RecordVerifyNote(claim.ID, res.Note)
		claim.VerifyNote, claim.VerifiedAt = res.Note, now
		return false, res.Note
	}
	approved, aerr := h.store.AutoApproveClaim(claim.ID, res.Note, depositAutoReviewer)
	if aerr != nil {
		// Verification passed but the credit transaction failed (e.g. the
		// claim raced with a manual review): keep it pending, leave a note.
		note := "auto-verify passed but automatic credit failed: " + aerr.Error()
		_ = h.store.RecordVerifyNote(claim.ID, note)
		claim.VerifyNote, claim.VerifiedAt = note, now
		h.audit.Log(c, "deposit.claim.auto_approve_failed", "deposit_claim", claim.ID, gin.H{
			"txid": claim.TxID, "asset": claim.Asset, "error": aerr.Error(),
		}, aerr)
		return false, note
	}
	*claim = *approved
	h.audit.Log(c, "deposit.claim.auto_approved", "deposit_claim", claim.ID, gin.H{
		"txid": claim.TxID, "asset": claim.Asset, "note": res.Note,
	}, nil)
	return true, res.Note
}

// List handles GET /api/v2/wallet/deposit/claims: the caller's own claims,
// newest first.
func (h *DepositClaimHandler) List(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	if h.store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "deposit claims not available"})
		return
	}
	claims, err := h.store.ListClaimsByUser(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list deposit claims"})
		return
	}
	out := make([]gin.H, len(claims))
	for i, cl := range claims {
		out[i] = depositClaimToJSON(cl)
	}
	c.JSON(http.StatusOK, gin.H{"claims": out})
}

// AdminList handles GET /api/v2/admin/deposit/claims?status=pending&limit=100&offset=0.
func (h *DepositClaimHandler) AdminList(c *gin.Context) {
	if h.store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "deposit claims not available"})
		return
	}
	status := c.Query("status")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if offset < 0 {
		offset = 0
	}
	claims, err := h.store.ListClaimsForAdmin(status, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list deposit claims", "detail": err.Error()})
		return
	}
	out := make([]gin.H, len(claims))
	for i, cl := range claims {
		item := depositClaimToJSON(cl)
		item["user_id"] = cl.UserID
		item["uid"] = cl.UID
		item["email"] = cl.Email
		item["reviewer_id"] = cl.ReviewerID
		out[i] = item
	}
	c.JSON(http.StatusOK, gin.H{"claims": out, "limit": limit, "offset": offset})
}

// AdminScreenshot serves a claim's screenshot image so the review UI can
// display the proof instead of a filesystem path.
// GET /api/v2/admin/deposit/claims/:id/screenshot
// Only .png/.jpg files are served; claims without a screenshot answer 404.
func (h *DepositClaimHandler) AdminScreenshot(c *gin.Context) {
	if h.store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "deposit claims not available"})
		return
	}
	id := c.Param("id")
	cl, err := h.store.GetClaimByID(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load deposit claim"})
		return
	}
	if cl == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "claim not found"})
		return
	}
	if cl.ScreenshotPath == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "screenshot not stored"})
		return
	}
	var contentType string
	switch strings.ToLower(filepath.Ext(cl.ScreenshotPath)) {
	case ".png":
		contentType = "image/png"
	case ".jpg", ".jpeg":
		contentType = "image/jpeg"
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "unsupported screenshot type"})
		return
	}
	data, err := os.ReadFile(cl.ScreenshotPath)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "screenshot file missing"})
		return
	}
	c.Header("Cache-Control", "private, max-age=300")
	c.Data(http.StatusOK, contentType, data)
}

type depositClaimReviewReq struct {
	Action string `json:"action" binding:"required"`
	Reason string `json:"reason"`
}

// AdminReview approves or rejects a pending claim.
// POST /api/v2/admin/deposit/claims/:id/review { "action":"approve|reject", "reason":"..." }
// Approval credits the spot wallet + writes the deposit ledger entry in the
// same database transaction as the status change (see the store layer).
func (h *DepositClaimHandler) AdminReview(c *gin.Context) {
	if h.store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "deposit claims not available"})
		return
	}
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "claim id required"})
		return
	}
	var r depositClaimReviewReq
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "action required (approve|reject)"})
		return
	}
	var action string
	switch r.Action {
	case "approve", "approved":
		action = "approved"
	case "reject", "rejected":
		action = "rejected"
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "action must be approve or reject"})
		return
	}
	reviewerID := c.GetString("user_id")
	_, err := h.store.ReviewClaim(id, reviewerID, action, r.Reason)
	h.audit.Log(c, "admin.deposit.claim.review", "deposit_claim", id, gin.H{
		"action": action, "reason": r.Reason,
	}, err)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// safeUserDir resolves the per-user screenshot directory and verifies it
// stays inside the claim data root (userID comes from the JWT, but
// defense-in-depth applies since it becomes a filesystem path segment).
func (h *DepositClaimHandler) safeUserDir(userID string) (string, bool) {
	seg := filepath.Base(filepath.Clean("/" + userID))
	if seg == "" || seg == "." || seg == ".." || strings.ContainsAny(seg, "/\\") {
		return "", false
	}
	root, err := filepath.Abs(h.dataDir)
	if err != nil {
		return "", false
	}
	dir := filepath.Join(root, seg)
	if !strings.HasPrefix(dir, root+string(filepath.Separator)) {
		return "", false
	}
	return dir, true
}

// depositClaimToJSON renders the user-visible claim shape (the admin listing
// adds user_id/uid/email/reviewer_id on top).
func depositClaimToJSON(cl *DepositClaim) gin.H {
	return gin.H{
		"id":            cl.ID,
		"asset":         cl.Asset,
		"amount":        safeFloatStr(cl.Amount),
		"txid":          cl.TxID,
		"status":        cl.Status,
		"reject_reason": cl.RejectReason,
		"auto_verified": cl.AutoVerified,
		"verify_note":   cl.VerifyNote,
		"verified_at":   cl.VerifiedAt,
		"created_at":    cl.CreatedAt,
		"reviewed_at":   cl.ReviewedAt,
	}
}
