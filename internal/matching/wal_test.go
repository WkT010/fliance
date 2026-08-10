package matching

import (
	"path/filepath"
	"testing"
	"time"
)

func walTestOrder(id string) *Order {
	p := newBigFloat()
	p.SetFloat64(50000)
	q := newBigFloat()
	q.SetFloat64(1)
	o := NewOrder("u1", "BTC/USDT", Buy, Limit, p, q)
	o.ID = id
	return o
}

// TestWALSyncPolicyDefault verifies the safe default: fsync every record and
// no time-based interval when no env vars are set.
func TestWALSyncPolicyDefault(t *testing.T) {
	t.Setenv("WAL_SYNC_EVERY", "")
	t.Setenv("WAL_SYNC_INTERVAL_MS", "")
	every, interval := defaultWALSyncPolicy()
	if every != 1 {
		t.Errorf("expected default syncEvery=1, got %d", every)
	}
	if interval != 0 {
		t.Errorf("expected default syncInterval=0, got %v", interval)
	}
}

// TestWALSyncPolicyFromEnv verifies the env-driven configuration.
func TestWALSyncPolicyFromEnv(t *testing.T) {
	t.Setenv("WAL_SYNC_EVERY", "32")
	t.Setenv("WAL_SYNC_INTERVAL_MS", "5")
	every, interval := defaultWALSyncPolicy()
	if every != 32 {
		t.Errorf("expected syncEvery=32, got %d", every)
	}
	if interval != 5*time.Millisecond {
		t.Errorf("expected syncInterval=5ms, got %v", interval)
	}

	// Invalid values fall back to the safe default.
	t.Setenv("WAL_SYNC_EVERY", "not-a-number")
	t.Setenv("WAL_SYNC_INTERVAL_MS", "-3")
	every, interval = defaultWALSyncPolicy()
	if every != 1 || interval != 0 {
		t.Errorf("expected fallback (1, 0), got (%d, %v)", every, interval)
	}
}

// TestWALBatchModeReplayCompatible writes records in batched-fsync mode and
// verifies the on-disk format is unchanged: every record replays with
// monotonically increasing Seq and intact fields.
func TestWALBatchModeReplayCompatible(t *testing.T) {
	path := filepath.Join(t.TempDir(), "BTC-USDT.wal")
	w, err := NewWALWriter(path)
	if err != nil {
		t.Fatalf("open wal: %v", err)
	}
	// Batch mode: fsync every 10 records (never reached mid-test for some).
	w.SetSyncPolicy(10, 0)
	const n = 25
	for i := 0; i < n; i++ {
		if err := w.AppendOrder(walTestOrder("ord-" + string(rune('A'+i)))); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	// Explicit Sync must always make everything durable.
	if err := w.Sync(); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	var count uint64
	var lastSeq uint64
	err = NewWALReader(path).Replay(func(rec WALRecord) error {
		count++
		if rec.Seq != count {
			t.Errorf("expected contiguous seq %d, got %d", count, rec.Seq)
		}
		if rec.Op != "order" || rec.UserID != "u1" || rec.Pair != "BTC/USDT" {
			t.Errorf("corrupt record fields: %+v", rec)
		}
		lastSeq = rec.Seq
		return nil
	})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if count != n || lastSeq != n {
		t.Errorf("expected %d replayed records (last seq %d), got %d (last %d)", n, n, count, lastSeq)
	}
}

// TestWALSyncEveryRecord verifies the default writer fsyncs per record (the
// pending counter never accumulates) and Seq restarts correctly on reopen.
func TestWALSyncEveryRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ETH-USDT.wal")
	w, err := NewWALWriter(path)
	if err != nil {
		t.Fatalf("open wal: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := w.AppendCancel("o1", "u1"); err != nil {
			t.Fatalf("append cancel: %v", err)
		}
		if w.pending != 0 {
			t.Errorf("per-record fsync mode should keep pending=0, got %d", w.pending)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// Reopen: sequence number must continue from existing records.
	w2, err := NewWALWriter(path)
	if err != nil {
		t.Fatalf("reopen wal: %v", err)
	}
	defer w2.Close()
	if w2.Seq() != 3 {
		t.Errorf("expected seq to resume at 3, got %d", w2.Seq())
	}
}

// TestWALBatchPendingAccumulates checks that in batch mode records accumulate
// until the batch boundary, and an explicit Sync resets the counter.
func TestWALBatchPendingAccumulates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "SOL-USDT.wal")
	w, err := NewWALWriter(path)
	if err != nil {
		t.Fatalf("open wal: %v", err)
	}
	defer w.Close()
	w.SetSyncPolicy(3, 0)
	for i := 0; i < 2; i++ {
		if err := w.AppendCancel("o1", "u1"); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	if w.pending != 2 {
		t.Errorf("expected 2 pending records before batch boundary, got %d", w.pending)
	}
	if err := w.AppendCancel("o1", "u1"); err != nil { // third record triggers fsync
		t.Fatalf("append: %v", err)
	}
	if w.pending != 0 {
		t.Errorf("expected pending reset after batch fsync, got %d", w.pending)
	}
	if err := w.Sync(); err != nil {
		t.Fatalf("explicit sync: %v", err)
	}
	if w.pending != 0 {
		t.Errorf("expected pending reset after explicit Sync, got %d", w.pending)
	}
}
