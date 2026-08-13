package api

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gin-gonic/gin"
)

// KycSubmission is one identity-verification request. Document fields hold
// paths relative to the gateway's data directory (data/kyc/{user_id}/...).
type KycSubmission struct {
	ID           string `json:"id"`
	UserID       string `json:"user_id"`
	FullName     string `json:"full_name"`
	IDNumber     string `json:"id_number"`
	DocFront     string `json:"doc_front"`
	DocBack      string `json:"doc_back"`
	Status       string `json:"status"` // pending|approved|rejected
	RejectReason string `json:"reject_reason"`
	ReviewerID   string `json:"reviewer_id"`
	SubmittedAt  int64  `json:"submitted_at"`
	ReviewedAt   int64  `json:"reviewed_at"`
}

// KycStore is the persistence contract for KYC submissions.
type KycStore interface {
	Submit(s *KycSubmission) error
	GetLatestByUser(userID string) (*KycSubmission, error)
	// GetByID loads one submission by ID (nil when absent).
	GetByID(id string) (*KycSubmission, error)
	ListByStatus(status string, limit, offset int) ([]*KycSubmission, error)
	// Review transitions a pending submission (guarded against double
	// review) and returns the reviewed record.
	Review(id, reviewerID, action, reason string) (*KycSubmission, error)
}

const (
	// kycMaxDocBytes caps each decoded identity document at 5 MB.
	kycMaxDocBytes = 5 << 20
	// kycDocDir is where identity documents are stored (0700 dirs, 0600 files).
	kycDocDir = "data/kyc"
)

// KycHandler serves the user-facing KYC endpoints.
type KycHandler struct {
	store    KycStore
	dataDir  string
	kycLevel interface{ KycLevel(userID string) (int, error) }
}

// NewKycHandler constructs a KYC handler rooted at dataDir (defaults to
// data/kyc).
func NewKycHandler(store KycStore, dataDir string) *KycHandler {
	if dataDir == "" {
		dataDir = kycDocDir
	}
	return &KycHandler{store: store, dataDir: dataDir}
}

// SetKycLevelLookup wires the user kyc_level lookup for /kyc/status.
func (h *KycHandler) SetKycLevelLookup(l interface{ KycLevel(userID string) (int, error) }) {
	h.kycLevel = l
}

type kycSubmitReq struct {
	FullName string `json:"full_name" binding:"required"`
	IDNumber string `json:"id_number" binding:"required"`
	DocFront string `json:"doc_front" binding:"required"` // base64 png/jpeg
	DocBack  string `json:"doc_back" binding:"required"`  // base64 png/jpeg
}

// decodeKycDoc decodes a base64 document, enforcing the 5 MB cap and the
// png/jpeg magic bytes. Returns the bytes and an extension.
func decodeKycDoc(b64 string) ([]byte, string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		// Tolerate data URLs (data:image/png;base64,...) from browsers.
		if i := strings.Index(b64, "base64,"); i >= 0 {
			raw, err = base64.StdEncoding.DecodeString(strings.TrimSpace(b64[i+len("base64,"):]))
		}
		if err != nil {
			return nil, "", fmt.Errorf("invalid base64 image")
		}
	}
	if len(raw) == 0 {
		return nil, "", fmt.Errorf("empty image")
	}
	if len(raw) > kycMaxDocBytes {
		return nil, "", fmt.Errorf("image exceeds 5MB limit")
	}
	if bytes.HasPrefix(raw, []byte{0x89, 'P', 'N', 'G'}) {
		return raw, ".png", nil
	}
	if bytes.HasPrefix(raw, []byte{0xFF, 0xD8, 0xFF}) {
		return raw, ".jpg", nil
	}
	return nil, "", fmt.Errorf("unsupported image type (png/jpeg only)")
}

// Submit handles POST /api/v2/kyc/submit.
func (h *KycHandler) Submit(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	if h.store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "kyc not available"})
		return
	}
	var r kycSubmitReq
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "full_name, id_number, doc_front, doc_back required"})
		return
	}
	front, frontExt, err := decodeKycDoc(r.DocFront)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "doc_front: " + err.Error()})
		return
	}
	back, backExt, err := decodeKycDoc(r.DocBack)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "doc_back: " + err.Error()})
		return
	}
	submissionID := "kyc_" + uuid.NewString()
	userDir, ok := h.safeUserDir(userID)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}
	if err := os.MkdirAll(userDir, 0700); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "storage unavailable"})
		return
	}
	frontRel := filepath.Join(userDir, submissionID+"_front"+frontExt)
	backRel := filepath.Join(userDir, submissionID+"_back"+backExt)
	if err := os.WriteFile(frontRel, front, 0600); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "storage unavailable"})
		return
	}
	if err := os.WriteFile(backRel, back, 0600); err != nil {
		_ = os.Remove(frontRel)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "storage unavailable"})
		return
	}
	sub := &KycSubmission{
		ID:          submissionID,
		UserID:      userID,
		FullName:    r.FullName,
		IDNumber:    r.IDNumber,
		DocFront:    filepath.ToSlash(frontRel),
		DocBack:     filepath.ToSlash(backRel),
		Status:      "pending",
		SubmittedAt: time.Now().UnixNano(),
	}
	if err := h.store.Submit(sub); err != nil {
		_ = os.Remove(frontRel)
		_ = os.Remove(backRel)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "kyc submit failed"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"id":           sub.ID,
		"status":       sub.Status,
		"submitted_at": sub.SubmittedAt,
	})
}

// safeUserDir resolves the per-user document directory and verifies it stays
// inside the KYC data root (userID comes from the JWT, but defense-in-depth
// applies since it becomes a filesystem path segment).
func (h *KycHandler) safeUserDir(userID string) (string, bool) {
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

// Status handles GET /api/v2/kyc/status: the user's latest submission and
// current KYC level.
func (h *KycHandler) Status(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	level := 0
	if h.kycLevel != nil {
		if l, err := h.kycLevel.KycLevel(userID); err == nil {
			level = l
		}
	}
	out := gin.H{"kyc_level": level, "submission": nil}
	if h.store != nil {
		if sub, err := h.store.GetLatestByUser(userID); err == nil && sub != nil {
			out["submission"] = gin.H{
				"id":            sub.ID,
				"status":        sub.Status,
				"reject_reason": sub.RejectReason,
				"submitted_at":  sub.SubmittedAt,
				"reviewed_at":   sub.ReviewedAt,
			}
		}
	}
	c.JSON(http.StatusOK, out)
}
