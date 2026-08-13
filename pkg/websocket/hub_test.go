package websocket

import (
	"fmt"
	"io"
	"log/slog"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"
)

// startHub spins up a hub with its Run loop; the goroutine is intentionally
// left running (Hub.Run is by design a process-lifetime loop).
func startHub(t *testing.T) *Hub {
	t.Helper()
	h := NewHub()
	go h.Run()
	return h
}

// makeClient registers a pump-less test client (no real socket; tests drain
// the Send channel directly or leave it full to simulate a slow consumer).
func makeClient(t *testing.T, h *Hub, id string, buf int) *Client {
	t.Helper()
	c := &Client{ID: id, UserID: "user-" + id, Send: make(chan []byte, buf), Hub: h, rooms: make(map[string]bool)}
	if err := h.Register(c); err != nil {
		t.Fatalf("register %s: %v", id, err)
	}
	return c
}

// waitFor polls cond until it returns true or the deadline elapses.
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// drain reads n messages from c.Send and fails the test on timeout.
func drain(t *testing.T, c *Client, n int) [][]byte {
	t.Helper()
	out := make([][]byte, 0, n)
	deadline := time.After(2 * time.Second)
	for len(out) < n {
		select {
		case msg, ok := <-c.Send:
			if !ok {
				t.Fatalf("client %s: send channel closed after %d/%d messages", c.ID, len(out), n)
			}
			out = append(out, msg)
		case <-deadline:
			t.Fatalf("client %s: timeout after %d/%d messages", c.ID, len(out), n)
		}
	}
	return out
}

func TestBroadcastToRoomLargeRoomAllReceive(t *testing.T) {
	h := startHub(t)
	const n = 2000
	const room = "ticker:BTC-USDT"

	clients := make([]*Client, n)
	for i := range clients {
		clients[i] = makeClient(t, h, strconv.Itoa(i), 256)
		h.JoinRoom(room, clients[i])
	}

	msg := []byte(`{"type":"update","channel":"ticker","pair":"BTC-USDT","data":{"price":"50000"}}`)
	h.BroadcastToRoom(room, msg)

	for _, c := range clients {
		got := drain(t, c, 1)
		if string(got[0]) != string(msg) {
			t.Fatalf("client %s: payload mismatch", c.ID)
		}
		// Serialize-once invariant: every client shares the exact same
		// underlying byte array (read-only fan-out, no per-client copy).
		if &got[0][0] != &msg[0] {
			t.Fatalf("client %s: payload was copied, expected shared slice", c.ID)
		}
	}

	st := h.Stats()
	if st.Broadcasts != 1 || st.Delivered != int64(n) || st.Dropped != 0 {
		t.Fatalf("unexpected stats: %+v", st)
	}
	if st.LiveClients != n || st.Rooms != 1 {
		t.Fatalf("unexpected stats: %+v", st)
	}
}

func TestBroadcastToRoomJSONSerializesOnce(t *testing.T) {
	h := startHub(t)
	const room = "orderbook:ETH-USDT"

	clients := make([]*Client, 5)
	for i := range clients {
		clients[i] = makeClient(t, h, strconv.Itoa(i), 16)
		h.JoinRoom(room, clients[i])
	}

	payload := Message{Type: MsgUpdate, Channel: ChannelOrderbook, Pair: "ETH-USDT"}
	if err := h.BroadcastToRoomAny(room, payload); err != nil {
		t.Fatalf("BroadcastToRoomAny: %v", err)
	}

	first := drain(t, clients[0], 1)[0]
	for _, c := range clients[1:] {
		got := drain(t, c, 1)[0]
		if &got[0] != &first[0] {
			t.Fatalf("client %s: payload serialised more than once", c.ID)
		}
	}
}

func TestSlowClientDroppedAndDisconnected(t *testing.T) {
	h := startHub(t)
	const room = "trades:BTC-USDT"

	fast := makeClient(t, h, "fast", 1<<16) // drains everything
	slow := makeClient(t, h, "slow", 1)     // never drained: buffer stays full
	h.JoinRoom(room, fast)
	h.JoinRoom(room, slow)

	msg := []byte(`{"type":"update"}`)

	// Pump broadcasts until the hub cuts the slow client off. Every
	// BroadcastToRoom is a synchronous fan-out, so while the slow client's
	// buffer is full each pass deterministically bumps its consecutive-drop
	// counter. We poll for the end state (client fully unregistered) instead
	// of asserting a precise drop count: the final unregister runs on its
	// own goroutine and both the hub-wide and per-client drop counters stop
	// moving the moment the client is closed, so their exact values at any
	// observation point are inherently racy.
	const maxSent = 10 * maxConsecutiveDrops // cut-off fires after ~maxConsecutiveDrops+2 passes
	deadline := time.Now().Add(10 * time.Second)
	sent := 0
	for {
		st := h.Stats()
		if st.SlowDisconnected == 1 && st.LiveClients == 1 {
			break // slow client was cut off and fully unregistered
		}
		if sent >= maxSent || time.Now().After(deadline) {
			t.Fatalf("slow client never disconnected: sent=%d stats=%+v slowDrops=%d",
				sent, st, slow.DropCount())
		}
		h.BroadcastToRoom(room, msg)
		sent++
		if sent%64 == 0 {
			// Yield so the hub's Run loop and the async unregister goroutine
			// get a turn; otherwise the hot loop can starve them and the
			// teardown never lands before the budget runs out.
			runtime.Gosched()
		}
	}

	st := h.Stats()
	// The disconnect is initiated exactly once (CAS claim), even though many
	// broadcasts raced on the stalled client.
	if st.SlowDisconnected != 1 {
		t.Fatalf("expected exactly one slow disconnect, stats=%+v", st)
	}
	// Enough drops accumulated to cross the policy threshold; the exact
	// count is timing-dependent and intentionally not asserted.
	if st.Dropped <= maxConsecutiveDrops {
		t.Fatalf("expected > %d drops before cutoff, stats=%+v", maxConsecutiveDrops, st)
	}
	if d := slow.DropCount(); d <= maxConsecutiveDrops {
		t.Fatalf("slow client drop count %d, expected > %d", d, maxConsecutiveDrops)
	}

	// Fast client saw every single broadcast, unaffected by the slow one.
	got := drain(t, fast, sent)
	if len(got) != sent {
		t.Fatalf("fast client got %d/%d messages", len(got), sent)
	}

	// The cut-off client went through the normal unregister path, which
	// closes the send channel exactly once (LiveClients==1 above implies the
	// unregister critical section already ran closeSend).
	for range slow.Send {
		// drain the buffered message(s) until the closed channel yields
	}

	// Further broadcasts touching the stale client never panic, and the
	// disconnect is never double-counted.
	h.BroadcastToRoom(room, msg)
	drain(t, fast, 1)
	if st := h.Stats(); st.SlowDisconnected != 1 || st.LiveClients != 1 {
		t.Fatalf("post-disconnect state changed: %+v", st)
	}
}

func TestBroadcastConcurrentRegisterUnregister(t *testing.T) {
	h := startHub(t)
	const room = "ticker:ETH-USDT"
	const writers, rounds = 8, 200

	var wg sync.WaitGroup
	// Churn: continuously register and unregister clients mid-broadcast.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			c := &Client{ID: "churn-" + strconv.Itoa(i), UserID: "u", Send: make(chan []byte, 8), Hub: h, rooms: make(map[string]bool)}
			if err := h.Register(c); err != nil {
				t.Error(err)
				return
			}
			h.JoinRoom(room, c)
			h.LeaveRoom(room, c)
			h.Unregister(c)
		}
	}()
	// Broadcasters hammer the room concurrently.
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			msg := []byte(`{"w":` + strconv.Itoa(w) + `}`)
			for i := 0; i < rounds; i++ {
				h.BroadcastToRoom(room, msg)
				h.Broadcast(msg)
				h.BroadcastToUser("u", msg)
			}
		}(w)
	}
	// Stable readers keep consuming so deliveries succeed under churn.
	stable := make([]*Client, 16)
	for i := range stable {
		stable[i] = makeClient(t, h, "stable-"+strconv.Itoa(i), 1<<16)
		h.JoinRoom(room, stable[i])
	}
	wg.Wait()

	// No deadlock: hub still answers broadcasts after the storm.
	h.BroadcastToRoom(room, []byte(`{"final":true}`))
	for _, c := range stable {
		drainAtLeast(t, c, 1)
	}

	st := h.Stats()
	if st.Broadcasts == 0 || st.Delivered == 0 {
		t.Fatalf("stats not populated under concurrency: %+v", st)
	}
	// Every room broadcast either delivered or dropped for every member it
	// snapshotted; global/user fan-outs add deliveries on top, so just sanity
	// check that nothing went negative or wildly out of range.
	if st.Dropped > st.Broadcasts*int64(len(stable)+rounds) {
		t.Fatalf("implausible drop count: %+v", st)
	}
}

func drainAtLeast(t *testing.T, c *Client, n int) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for i := 0; i < n; i++ {
		select {
		case _, ok := <-c.Send:
			if !ok {
				t.Fatalf("client %s: closed", c.ID)
			}
		case <-deadline:
			t.Fatalf("client %s: timeout", c.ID)
		}
	}
}

func TestRegisterRespectsCapacity(t *testing.T) {
	h := startHub(t)
	h.SetMaxConnections(2)
	makeClient(t, h, "a", 8)
	makeClient(t, h, "b", 8)
	c := &Client{ID: "c", UserID: "u", Send: make(chan []byte, 8), Hub: h, rooms: make(map[string]bool)}
	if err := h.Register(c); err != ErrHubFull {
		t.Fatalf("expected ErrHubFull, got %v", err)
	}
}

func BenchmarkBroadcastToRoom(b *testing.B) {
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer slog.SetDefault(prev)
	for _, n := range []int{100, 1000, 5000} {
		b.Run(fmt.Sprintf("clients=%d", n), func(b *testing.B) {
			h := NewHub()
			go h.Run()
			stop := make(chan struct{})
			defer close(stop)
			const room = "orderbook:BTC-USDT"
			for i := 0; i < n; i++ {
				c := &Client{ID: "c" + strconv.Itoa(i), UserID: "u", Send: make(chan []byte, 256), Hub: h, rooms: make(map[string]bool)}
				if err := h.Register(c); err != nil {
					b.Fatal(err)
				}
				h.JoinRoom(room, c)
				// Keep the consumer draining so the benchmark exercises the
				// delivery path instead of the drop policy.
				go func(c *Client) {
					for {
						select {
						case <-c.Send:
						case <-stop:
							return
						}
					}
				}(c)
			}
			msg := []byte(`{"type":"update","channel":"orderbook","pair":"BTC-USDT","data":{"bids":[["50000.1","0.5"]],"asks":[["50000.2","0.3"]]}}`)
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				h.BroadcastToRoom(room, msg)
			}
		})
	}
}
