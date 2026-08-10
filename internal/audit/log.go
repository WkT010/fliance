// Package audit records administrative actions (deposits, withdrawal
// approvals, risk config changes, AMM pool management, ...) for compliance
// and forensics.
//
// Writes are asynchronous: entries are buffered on an internal channel and
// flushed to the store by a background goroutine in batches. Admin requests
// are never blocked by auditing — when the channel is full or the store is
// unavailable, entries degrade to the local process log instead.
package audit

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

// Entry is a single audit record describing one admin action.
type Entry struct {
	ID          int64     // assigned by the store (BIGSERIAL); zero until persisted
	Timestamp   time.Time // set automatically when the entry is recorded
	ActorUserID string    // admin performing the action
	ActorEmail  string    // admin email when available
	Action      string    // e.g. "admin.deposit", "admin.withdrawal.approve"
	TargetType  string    // e.g. "wallet", "withdrawal", "pair", "amm_pool"
	TargetID    string    // identifier of the affected object
	IPAddress   string    // client IP of the admin request
	UserAgent   string    // client user agent of the admin request
	Details     string    // JSON-encoded action-specific payload
	Success     bool      // whether the action succeeded
	ErrorMsg    string    // failure reason; empty on success
}

// AuditStore persists audit entries. Implemented by store.PGAuditStore in
// production; tests substitute a fake.
type AuditStore interface {
	Record(ctx context.Context, e Entry) error
}

const (
	defaultChanBuffer   = 4096
	defaultBatchSize    = 32
	defaultFlushEvery   = 1 * time.Second
	defaultWriteTimeout = 5 * time.Second
)

// Logger buffers audit entries and persists them asynchronously. All public
// methods are safe to call on a nil *Logger so handlers can invoke them
// unconditionally even when auditing was never wired.
type Logger struct {
	store AuditStore

	ch        chan Entry
	wg        sync.WaitGroup
	closed    atomic.Bool
	closeOnce sync.Once

	batchSize  int
	flushEvery time.Duration
}

// Option customizes a Logger; mainly used by tests to shrink flush timings.
type Option func(*Logger)

// WithBatchSize overrides how many buffered entries trigger an early flush.
func WithBatchSize(n int) Option {
	return func(l *Logger) {
		if n > 0 {
			l.batchSize = n
		}
	}
}

// WithFlushInterval overrides the maximum time entries may wait in the
// buffer before being flushed.
func WithFlushInterval(d time.Duration) Option {
	return func(l *Logger) {
		if d > 0 {
			l.flushEvery = d
		}
	}
}

// NewLogger starts the background flush goroutine. store may be nil (e.g.
// when Postgres is unavailable): entries then degrade to the local log so
// admin actions remain traceable without a database.
func NewLogger(store AuditStore, opts ...Option) *Logger {
	l := &Logger{
		store:      store,
		ch:         make(chan Entry, defaultChanBuffer),
		batchSize:  defaultBatchSize,
		flushEvery: defaultFlushEvery,
	}
	for _, o := range opts {
		o(l)
	}
	l.wg.Add(1)
	go l.run()
	return l
}

// Record enqueues an entry without ever blocking the caller. When the
// buffer is full or the logger is already closed, the entry is written to
// the local log instead (degraded path).
func (l *Logger) Record(e Entry) {
	if l == nil {
		return
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	if l.closed.Load() {
		localLog(e, "logger closed")
		return
	}
	defer func() {
		// Guard against the race between the closed check and Close().
		if r := recover(); r != nil {
			localLog(e, "logger closing")
		}
	}()
	select {
	case l.ch <- e:
	default:
		localLog(e, "buffer full")
	}
}

// Log builds an entry from the request context (actor user id/email, client
// IP, user agent) and records it. err == nil marks the action successful;
// otherwise its message is stored as the failure reason. details may be any
// JSON-marshalable value (nil for none).
func (l *Logger) Log(c *gin.Context, action, targetType, targetID string, details any, err error) {
	if l == nil {
		return
	}
	e := Entry{
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		Success:    err == nil,
	}
	if err != nil {
		e.ErrorMsg = err.Error()
	}
	if c != nil {
		e.ActorUserID = c.GetString("user_id")
		e.ActorEmail = c.GetString("email")
		if c.Request != nil {
			e.IPAddress = c.ClientIP()
			e.UserAgent = c.Request.UserAgent()
		}
	}
	if details != nil {
		if b, merr := json.Marshal(details); merr == nil {
			e.Details = string(b)
		}
	}
	l.Record(e)
}

// Close drains the buffer and stops the background goroutine. Safe to call
// multiple times.
func (l *Logger) Close() {
	if l == nil {
		return
	}
	l.closeOnce.Do(func() {
		l.closed.Store(true)
		close(l.ch)
	})
	l.wg.Wait()
}

// run is the background flush loop: it batches entries up to batchSize or
// until flushEvery elapses, whichever comes first, and persists them.
func (l *Logger) run() {
	defer l.wg.Done()
	buf := make([]Entry, 0, l.batchSize)
	ticker := time.NewTicker(l.flushEvery)
	defer ticker.Stop()
	flush := func() {
		for _, e := range buf {
			l.write(e)
		}
		buf = buf[:0]
	}
	for {
		select {
		case e, ok := <-l.ch:
			if !ok {
				// Logger closed: persist whatever is still buffered.
				flush()
				return
			}
			buf = append(buf, e)
			if len(buf) >= l.batchSize {
				flush()
			}
		case <-ticker.C:
			if len(buf) > 0 {
				flush()
			}
		}
	}
}

// write persists a single entry; any failure degrades to the local log so an
// audit-store outage can never break admin operations.
func (l *Logger) write(e Entry) {
	if l.store == nil {
		localLog(e, "no store configured")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultWriteTimeout)
	defer cancel()
	if err := l.store.Record(ctx, e); err != nil {
		localLog(e, "store write failed: "+err.Error())
	}
}

// localLog is the degradation sink: entries that cannot reach the store are
// preserved in the process log so no admin action silently disappears.
func localLog(e Entry, reason string) {
	slog.Warn("AUDIT-FALLBACK",
		"reason", reason,
		"action", e.Action,
		"actor_user_id", e.ActorUserID,
		"actor_email", e.ActorEmail,
		"target_type", e.TargetType,
		"target_id", e.TargetID,
		"ip", e.IPAddress,
		"success", e.Success,
		"error", e.ErrorMsg,
		"details", e.Details)
}
