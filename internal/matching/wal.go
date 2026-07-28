package matching

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// WALRecord is a single durable journal entry for the matching engine. It
// captures every order submission and cancel so the engine can be reconstructed
// after a crash.
type WALRecord struct {
	Seq       uint64 `json:"seq"`
	Timestamp int64  `json:"ts"`
	Op        string `json:"op"` // "order" | "cancel"

	// Order fields
	OrderID       string  `json:"order_id,omitempty"`
	ClientOrderID string  `json:"client_order_id,omitempty"`
	UserID        string  `json:"user_id,omitempty"`
	Pair          string  `json:"pair,omitempty"`
	Side          int8    `json:"side,omitempty"`
	Type          int8    `json:"type,omitempty"`
	Price         string  `json:"price,omitempty"`
	StopPrice     string  `json:"stop_price,omitempty"`
	Quantity      string  `json:"quantity,omitempty"`
	TimeInForce   int8    `json:"tif,omitempty"`
	STP           int8    `json:"stp,omitempty"`
}

// WALWriter appends records durably to a write-ahead log. Each matching engine
// instance gets its own log file (e.g. /data/wal/BTC-USDT.wal).
type WALWriter struct {
	path string
	file *os.File
	bw   *bufio.Writer
	mu   sync.Mutex
	seq  uint64
}

// NewWALWriter opens (or creates) a WAL file at path. Records are buffered and
// fsynced on every Flush/Sync call. The writer's sequence number is initialised
// to the number of existing records so Seq() remains monotonic across restarts.
func NewWALWriter(path string) (*WALWriter, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return nil, fmt.Errorf("create wal dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0640)
	if err != nil {
		return nil, fmt.Errorf("open wal: %w", err)
	}
	seq, err := countWALRecords(path)
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("scan wal: %w", err)
	}
	return &WALWriter{path: path, file: f, bw: bufio.NewWriter(f), seq: seq}, nil
}

// countWALRecords returns the number of newline-delimited JSON records in the
// WAL file. If the file does not exist it returns 0.
func countWALRecords(path string) (uint64, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	defer f.Close()

	var count uint64
	br := bufio.NewReader(f)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			var rec WALRecord
			if json.Unmarshal(line, &rec) == nil {
				count++
			}
		}
		if err == io.EOF {
			return count, nil
		}
		if err != nil {
			return count, err
		}
	}
}

// Seq returns the sequence number of the last appended record. It is safe to
// call concurrently with Append*/Sync/Close.
func (w *WALWriter) Seq() uint64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.seq
}

// AppendOrder writes an order submission record to the WAL.
func (w *WALWriter) AppendOrder(o *Order) error {
	if w == nil || o == nil {
		return nil
	}
	rec := WALRecord{
		Op:            "order",
		OrderID:       o.ID,
		ClientOrderID: o.ClientOrderID,
		UserID:        o.UserID,
		Pair:          o.Pair,
		Side:          int8(o.Side),
		Type:          int8(o.Type),
		Price:         text(o.Price),
		StopPrice:     text(o.StopPrice),
		Quantity:      text(o.Quantity),
		TimeInForce:   int8(o.TimeInForce),
		STP:           int8(o.STP),
	}
	return w.append(rec)
}

// AppendCancel writes a cancel record to the WAL.
func (w *WALWriter) AppendCancel(orderID, userID string) error {
	if w == nil {
		return nil
	}
	return w.append(WALRecord{Op: "cancel", OrderID: orderID, UserID: userID})
}

func (w *WALWriter) append(rec WALRecord) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.seq++
	rec.Seq = w.seq
	rec.Timestamp = time.Now().UnixNano()
	b, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal wal record: %w", err)
	}
	b = append(b, '\n')
	if _, err := w.bw.Write(b); err != nil {
		return fmt.Errorf("write wal: %w", err)
	}
	return w.bw.Flush()
}

// Sync flushes the WAL to stable storage. Call after each batch or before
// acknowledging an order to the client.
func (w *WALWriter) Sync() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.bw.Flush(); err != nil {
		return err
	}
	return w.file.Sync()
}

// Close closes the WAL file.
func (w *WALWriter) Close() error {
	if w == nil {
		return nil
	}
	_ = w.Sync()
	return w.file.Close()
}

// WALReader replays records from a WAL file.
type WALReader struct {
	path string
}

// NewWALReader opens a reader for the WAL at path.
func NewWALReader(path string) *WALReader { return &WALReader{path: path} }

// Replay reads every record and invokes fn. If the file does not exist it
// returns without error.
func (r *WALReader) Replay(fn func(WALRecord) error) error {
	f, err := os.Open(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	br := bufio.NewReader(f)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			var rec WALRecord
			if jerr := json.Unmarshal(line, &rec); jerr != nil {
				log.Printf("[wal] skipping corrupt record: %v", jerr)
				continue
			}
			if fnErr := fn(rec); fnErr != nil {
				return fnErr
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func text(f *big.Float) string {
	if f == nil {
		return ""
	}
	return f.Text('f', 18)
}
