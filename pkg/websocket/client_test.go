package websocket

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
)

// wsTestPair establishes a real WebSocket connection pair backed by an
// httptest server and returns both ends plus a teardown function.
func wsTestPair(t *testing.T) (client, server *gorilla.Conn) {
	t.Helper()
	serverConn := make(chan *gorilla.Conn, 1)
	up := gorilla.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		serverConn <- c
	}))
	t.Cleanup(srv.Close)

	cc, _, err := gorilla.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return cc, <-serverConn
}

// TestWritePumpDeadConnDoesNotPanic is the regression test for the gateway
// crash: NextWriter on a dead connection returns (nil, err); the old code
// ignored the error and dereferenced the nil writer, killing the process.
// WritePump must now exit cleanly instead of panicking.
func TestWritePumpDeadConnDoesNotPanic(t *testing.T) {
	cc, sc := wsTestPair(t)
	// Kill the connection abruptly from both ends so any write fails.
	sc.UnderlyingConn().Close()
	cc.UnderlyingConn().Close()

	c := &Client{ID: "dead", Conn: cc, Send: make(chan []byte, 4)}
	c.Send <- []byte(`{"type":"update"}`)

	done := make(chan struct{})
	go func() { defer close(done); c.WritePump() }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("WritePump did not exit on a dead connection")
	}
}

// TestWritePumpNilConnAndChannel guards the degenerate constructions: pumps
// must bail out instead of dereferencing nil fields.
func TestWritePumpNilConnAndChannel(t *testing.T) {
	(&Client{ID: "nil-conn", Send: make(chan []byte, 1)}).WritePump() // would hang/panic before
	(&Client{ID: "nil-chan", Conn: nil}).WritePump()
	h := NewHub()
	go h.Run() // keeps the unregister channel served
	(&Client{ID: "nil-conn-read", Send: make(chan []byte, 1), Hub: h}).ReadPump()
}

// TestCloseSendSafeOnNilChannel ensures closeSend never panics on a client
// built without a Send channel (e.g. partially initialised).
func TestCloseSendSafeOnNilChannel(t *testing.T) {
	c := &Client{ID: "nil-send"}
	c.closeSend()
	c.closeSend() // idempotent

	c2 := &Client{ID: "ok", Send: make(chan []byte, 1)}
	c2.closeSend()
	c2.closeSend()
	if _, open := <-c2.Send; open {
		t.Fatal("send channel not closed")
	}
}

// TestWritePumpDeliversBatchedMessages verifies the normal broadcast path is
// untouched: queued messages are flushed in one frame and the pump exits on
// channel close.
func TestWritePumpDeliversBatchedMessages(t *testing.T) {
	cc, sc := wsTestPair(t)
	c := &Client{ID: "live", Conn: cc, Send: make(chan []byte, 8)}
	c.Send <- []byte("one")
	c.Send <- []byte("two")

	done := make(chan struct{})
	go func() { defer close(done); c.WritePump() }()

	_, payload, err := sc.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(payload) != "one\ntwo" {
		t.Fatalf("payload = %q, want batched %q", payload, "one\ntwo")
	}

	c.closeSend() // closed channel -> WritePump sends CloseMessage and exits
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("WritePump did not exit after send channel close")
	}
}
