package audit

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// fakeStore is an in-memory AuditStore for tests. It can be switched to
// failing mode and can block Record calls until released (or until the
// write context expires) to exercise backpressure.
type fakeStore struct {
	mu       sync.Mutex
	entries  []Entry
	attempts int
	fail     bool
	block    chan struct{} // when non-nil and open, Record blocks
}

func (f *fakeStore) Record(ctx context.Context, e Entry) error {
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attempts++
	if f.fail {
		return errors.New("boom")
	}
	f.entries = append(f.entries, e)
	return nil
}

func (f *fakeStore) snapshot() ([]Entry, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Entry, len(f.entries))
	copy(out, f.entries)
	return out, f.attempts
}

func testLogger(store AuditStore) *Logger {
	// Shrink the flush interval so tests don't depend on the 1s default.
	return NewLogger(store, WithBatchSize(8), WithFlushInterval(10*time.Millisecond))
}

func TestAsyncWriteFlushesOnClose(t *testing.T) {
	fs := &fakeStore{}
	l := testLogger(fs)

	l.Record(Entry{Action: "admin.deposit", ActorUserID: "u1", Success: true})
	l.Record(Entry{Action: "admin.pair.pause", ActorUserID: "u2", Success: false, ErrorMsg: "x"})
	l.Close()

	entries, _ := fs.snapshot()
	if len(entries) != 2 {
		t.Fatalf("expected 2 persisted entries, got %d", len(entries))
	}
	if entries[0].Action != "admin.deposit" || entries[1].Action != "admin.pair.pause" {
		t.Fatalf("entries out of order: %+v", entries)
	}
	if entries[0].Timestamp.IsZero() {
		t.Fatal("timestamp should be auto-assigned")
	}
	// Close must be idempotent.
	l.Close()
}

func TestStoreFailureDegradesWithoutBlocking(t *testing.T) {
	fs := &fakeStore{fail: true}
	l := testLogger(fs)

	l.Record(Entry{Action: "admin.deposit"})
	l.Close()

	entries, attempts := fs.snapshot()
	if len(entries) != 0 {
		t.Fatalf("failing store must not persist entries, got %d", len(entries))
	}
	if attempts == 0 {
		t.Fatal("store.Record must have been attempted (fallback happens after the failure)")
	}
}

func TestNilStoreDegradesToLocalLog(t *testing.T) {
	l := testLogger(nil)
	// Must not panic or deadlock; the entry is written to the local log.
	l.Record(Entry{Action: "admin.deposit"})
	done := make(chan struct{})
	go func() { l.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close deadlocked with nil store")
	}
}

func TestRecordDoesNotBlockUnderBackpressure(t *testing.T) {
	fs := &fakeStore{block: make(chan struct{})}
	// Tiny buffer + huge batch so the channel saturates quickly.
	l := NewLogger(fs, WithBatchSize(10000), WithFlushInterval(time.Hour))

	start := time.Now()
	// 5000 > channel buffer (4096): once the writer goroutine blocks, the
	// buffer saturates and the excess must degrade to the local log instead
	// of blocking the caller.
	for i := 0; i < 5000; i++ {
		l.Record(Entry{Action: "admin.deposit"})
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Record blocked under backpressure: %v", elapsed)
	}

	// Release the blocked writer so Close can drain and exit.
	close(fs.block)
	done := make(chan struct{})
	go func() { l.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Close deadlocked after backpressure")
	}
}

func TestLogExtractsGinContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fs := &fakeStore{}
	l := testLogger(fs)
	defer l.Close()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v2/admin/wallet/deposit", strings.NewReader(""))
	c.Request.RemoteAddr = "203.0.113.9:12345"
	c.Request.Header.Set("User-Agent", "nexa-test-agent/1.0")
	c.Set("user_id", "usr_admin1")
	c.Set("email", "admin@nexa.test")

	l.Log(c, "admin.deposit", "wallet", "usr_target", gin.H{"asset": "BTC", "amount": "1.5"}, nil)
	l.Close()

	entries, _ := fs.snapshot()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Action != "admin.deposit" || e.TargetType != "wallet" || e.TargetID != "usr_target" {
		t.Fatalf("unexpected identity fields: %+v", e)
	}
	if e.ActorUserID != "usr_admin1" || e.ActorEmail != "admin@nexa.test" {
		t.Fatalf("actor not extracted: %+v", e)
	}
	if e.IPAddress != "203.0.113.9" {
		t.Fatalf("ip not extracted: %q", e.IPAddress)
	}
	if e.UserAgent != "nexa-test-agent/1.0" {
		t.Fatalf("user agent not extracted: %q", e.UserAgent)
	}
	if !e.Success || e.ErrorMsg != "" {
		t.Fatalf("success entry mis-flagged: %+v", e)
	}
	if !strings.Contains(e.Details, `"asset":"BTC"`) {
		t.Fatalf("details not marshaled: %q", e.Details)
	}
}

func TestLogRecordsFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fs := &fakeStore{}
	l := testLogger(fs)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("user_id", "usr_admin1")
	l.Log(c, "admin.withdrawal.approve", "withdrawal", "tx1", nil, errors.New("not reviewing"))
	l.Close()

	entries, _ := fs.snapshot()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Success {
		t.Fatal("entry must be flagged as failure")
	}
	if e.ErrorMsg != "not reviewing" {
		t.Fatalf("error message lost: %q", e.ErrorMsg)
	}
}

func TestNilLoggerIsSafe(t *testing.T) {
	var l *Logger
	l.Record(Entry{Action: "admin.deposit"})
	l.Log(nil, "admin.deposit", "wallet", "u1", nil, nil)
	l.Close() // must not panic
}
