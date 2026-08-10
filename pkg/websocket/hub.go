package websocket

import (
	"encoding/json"
	"errors"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
)

// ErrHubFull is returned by Register when the global connection cap is hit.
var ErrHubFull = errors.New("websocket hub at capacity")

// defaultMaxConnections bounds the total number of live WebSocket clients
// process-wide so a connection flood cannot exhaust memory/fds.
const defaultMaxConnections = 10000

// maxConsecutiveDrops is the number of back-to-back broadcast drops a client
// may accumulate (its send buffer staying full) before the hub disconnects it
// to protect the server from slow/dead consumers.
const maxConsecutiveDrops = 100

// broadcastBatchSize bounds how many clients a single fan-out pass serves
// before the hub yields the scheduler; large rooms are drained in batches so
// one broadcast cannot monopolise the CPU or hold snapshots too long.
const broadcastBatchSize = 512

// HubStats is a consistent snapshot of hub-level broadcast counters.
type HubStats struct {
	Broadcasts          int64 `json:"broadcasts"`            // broadcast fan-outs started
	Delivered           int64 `json:"delivered"`             // per-client deliveries enqueued
	Dropped             int64 `json:"dropped"`               // per-client drops (buffer full)
	SlowDisconnected    int64 `json:"slow_disconnected"`     // clients cut off after drop flood
	LiveClients         int   `json:"live_clients"`          // registered connections
	Rooms               int   `json:"rooms"`                 // non-empty rooms
	MaxConsecutiveDrops int   `json:"max_consecutive_drops"` // policy threshold
}

type Hub struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
	rooms      map[string]map[*Client]bool
	mu         sync.RWMutex
	maxConn    int64 // global connection cap (<=0 = unlimited)
	count      int64 // live registered connections (atomic)

	// Broadcast observability counters (atomic).
	broadcasts       int64
	delivered        int64
	dropped          int64
	slowDisconnected int64
}

func NewHub() *Hub {
	return &Hub{clients: make(map[*Client]bool), broadcast: make(chan []byte, 256), register: make(chan *Client), unregister: make(chan *Client), rooms: make(map[string]map[*Client]bool), maxConn: defaultMaxConnections}
}

// SetMaxConnections overrides the global connection cap (<=0 = unlimited).
func (h *Hub) SetMaxConnections(n int) { atomic.StoreInt64(&h.maxConn, int64(n)) }

// AtCapacity reports whether the hub has reached its connection cap.
func (h *Hub) AtCapacity() bool {
	max := atomic.LoadInt64(&h.maxConn)
	return max > 0 && atomic.LoadInt64(&h.count) >= max
}

// Stats returns a snapshot of hub-level broadcast statistics.
func (h *Hub) Stats() HubStats {
	h.mu.RLock()
	clients, rooms := len(h.clients), len(h.rooms)
	h.mu.RUnlock()
	return HubStats{
		Broadcasts:          atomic.LoadInt64(&h.broadcasts),
		Delivered:           atomic.LoadInt64(&h.delivered),
		Dropped:             atomic.LoadInt64(&h.dropped),
		SlowDisconnected:    atomic.LoadInt64(&h.slowDisconnected),
		LiveClients:         clients,
		Rooms:               rooms,
		MaxConsecutiveDrops: maxConsecutiveDrops,
	}
}

// ClientCount returns the number of currently registered clients.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

func (h *Hub) Run() {
	for {
		select {
		case c := <-h.register:
			h.mu.Lock()
			h.clients[c] = true
			h.mu.Unlock()
			slog.Debug("ws registered", "client_id", c.ID, "user_id", c.UserID)
		case c := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[c]; ok {
				for room, members := range h.rooms {
					if _, in := members[c]; in {
						delete(members, c)
						if len(members) == 0 {
							delete(h.rooms, room)
						}
					}
				}
				delete(h.clients, c)
				c.closeSend()
				atomic.AddInt64(&h.count, -1)
			}
			h.mu.Unlock()
			slog.Debug("ws unregistered", "client_id", c.ID)
		case msg := <-h.broadcast:
			h.mu.RLock()
			clients := make([]*Client, 0, len(h.clients))
			for c := range h.clients {
				clients = append(clients, c)
			}
			h.mu.RUnlock()
			h.fanout(clients, msg)
		}
	}
}

// fanout delivers one shared, read-only payload to every client in batches.
// The lock is never held here: callers pass a snapshot taken under RLock,
// and each batch yields the scheduler so huge rooms stay responsive.
func (h *Hub) fanout(clients []*Client, msg []byte) {
	atomic.AddInt64(&h.broadcasts, 1)
	for i := 0; i < len(clients); i += broadcastBatchSize {
		end := i + broadcastBatchSize
		if end > len(clients) {
			end = len(clients)
		}
		for _, c := range clients[i:end] {
			h.deliver(c, msg)
		}
		if end < len(clients) {
			runtime.Gosched()
		}
	}
}

// deliver performs the non-blocking per-client send and enforces the slow
// client policy: drops are counted per client, and a client whose buffer
// stays full for maxConsecutiveDrops consecutive broadcasts is disconnected.
func (h *Hub) deliver(c *Client, msg []byte) {
	active, drops := c.offer(msg)
	if !active {
		return
	} // already closed; stale snapshot entry
	if drops == 0 {
		atomic.AddInt64(&h.delivered, 1)
		return
	}
	atomic.AddInt64(&h.dropped, 1)
	if drops > maxConsecutiveDrops {
		h.disconnectSlowClient(c)
	}
}

// disconnectSlowClient cuts off a consumer that cannot keep up. Counted once
// per client; the actual teardown runs through the normal unregister path.
func (h *Hub) disconnectSlowClient(c *Client) {
	if !c.markCutOff() {
		return // another broadcaster already initiated the disconnect
	}
	atomic.AddInt64(&h.slowDisconnected, 1)
	slog.Warn("ws disconnecting slow client", "client_id", c.ID, "drops_threshold", maxConsecutiveDrops)
	go h.Unregister(c)
}

// Register admits a client into the hub. It reserves a slot atomically up
// front and returns ErrHubFull when the global cap is reached, so callers can
// reject the connection before pumping goroutines are started.
func (h *Hub) Register(c *Client) error {
	atomic.AddInt64(&h.count, 1)
	if max := atomic.LoadInt64(&h.maxConn); max > 0 && atomic.LoadInt64(&h.count) > max {
		atomic.AddInt64(&h.count, -1)
		return ErrHubFull
	}
	h.register <- c
	return nil
}
func (h *Hub) Unregister(c *Client) { h.unregister <- c }

// IsPrivateChannel reports whether a channel carries account-private data and
// must only be served to authenticated clients. Everything outside the
// explicit public market-data set is treated as private (default deny).
func IsPrivateChannel(ch string) bool {
	switch ch {
	case ChannelTicker, ChannelOrderbook, ChannelTrades, "candles", "klines", "depth":
		return false
	}
	return true
}

// isPrivateRoom applies IsPrivateChannel to a "channel:pair" room name.
func isPrivateRoom(room string) bool {
	ch := room
	if i := strings.Index(room, ":"); i >= 0 {
		ch = room[:i]
	}
	return IsPrivateChannel(ch)
}

func (h *Hub) JoinRoom(room string, c *Client) {
	// Defense in depth: unauthenticated (anonymous) clients can never join
	// private rooms, regardless of how the subscribe request was handled.
	if isPrivateRoom(room) && (c.UserID == "" || c.UserID == "anon") {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.rooms[room] == nil {
		h.rooms[room] = make(map[*Client]bool)
	}
	h.rooms[room][c] = true
}

func (h *Hub) LeaveRoom(room string, c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if members, ok := h.rooms[room]; ok {
		delete(members, c)
		if len(members) == 0 {
			delete(h.rooms, room)
		}
	}
}

// Broadcast delivers msg to every registered client. msg is a shared,
// read-only payload: it must be serialised exactly once by the caller and is
// never copied or mutated per client or by the hub.
func (h *Hub) Broadcast(msg []byte) {
	h.mu.RLock()
	clients := make([]*Client, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.RUnlock()
	h.fanout(clients, msg)
}

// BroadcastToRoom delivers msg to every member of room. The payload is
// serialised once upstream and shared read-only across all members; delivery
// is non-blocking and large rooms are served in batches so the hub lock is
// never held while sending.
func (h *Hub) BroadcastToRoom(room string, msg []byte) {
	h.mu.RLock()
	members := h.rooms[room]
	if members == nil {
		h.mu.RUnlock()
		return
	}
	clients := make([]*Client, 0, len(members))
	for c := range members {
		clients = append(clients, c)
	}
	h.mu.RUnlock()
	h.fanout(clients, msg)
}

// BroadcastToRoomAny serialises v exactly once and fans the resulting bytes
// out to every member of room (convenience wrapper keeping the "marshal once,
// share everywhere" invariant explicit for structured payloads).
func (h *Hub) BroadcastToRoomAny(room string, v any) error {
	msg, err := json.Marshal(v)
	if err != nil {
		return err
	}
	h.BroadcastToRoom(room, msg)
	return nil
}

// BroadcastAny serialises v exactly once and fans it out to all clients.
func (h *Hub) BroadcastAny(v any) error {
	msg, err := json.Marshal(v)
	if err != nil {
		return err
	}
	h.Broadcast(msg)
	return nil
}

func (h *Hub) BroadcastToUser(userID string, msg []byte) {
	h.mu.RLock()
	clients := make([]*Client, 0)
	for c := range h.clients {
		if c.UserID == userID {
			clients = append(clients, c)
		}
	}
	h.mu.RUnlock()
	h.fanout(clients, msg)
}
