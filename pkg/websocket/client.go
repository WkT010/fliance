package websocket

import (
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = 54 * time.Second
	maxMessageSize = 16384
)

type Client struct {
	ID     string
	UserID string
	Conn   *websocket.Conn
	Send   chan []byte
	Hub    *Hub
	rooms  map[string]bool

	sendMu     sync.Mutex // guards closed and serialises Send close vs. offer
	closed     bool
	consecDrop int64 // consecutive broadcast drops; reset on delivery (sendMu)
	cutOff     int32 // atomic: slow-client disconnect initiated
}

func NewClient(conn *websocket.Conn, hub *Hub, userID string) *Client {
	return &Client{ID: userID + "-" + time.Now().Format("150405.000"), UserID: userID, Conn: conn, Send: make(chan []byte, 256), Hub: hub, rooms: make(map[string]bool)}
}

// DropCount returns the number of consecutive broadcast messages dropped for
// this client because its send buffer was full.
func (c *Client) DropCount() int64 {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	return c.consecDrop
}

// offer enqueues a shared (read-only) broadcast payload without blocking.
// It returns whether the client is still active and the client's consecutive
// drop count after this attempt (0 when the payload was accepted, >0 when the
// buffer was full and the message was dropped).
func (c *Client) offer(msg []byte) (active bool, consecDrops int64) {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	if c.closed {
		return false, 0
	}
	select {
	case c.Send <- msg:
		c.consecDrop = 0
		return true, 0
	default:
		c.consecDrop++
		return true, c.consecDrop
	}
}

// markCutOff atomically claims the slow-client disconnect so it is initiated
// exactly once even when many broadcasts race on the same stalled client.
func (c *Client) markCutOff() bool { return atomic.CompareAndSwapInt32(&c.cutOff, 0, 1) }

// closeSend closes the send channel exactly once; safe to call repeatedly and
// concurrently with offer.
func (c *Client) closeSend() {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	if !c.closed {
		c.closed = true
		if c.Send != nil {
			close(c.Send)
		}
	}
}

// JoinRoom joins a room and remembers it locally for clean-up.
func (c *Client) JoinRoom(room string) {
	c.Hub.JoinRoom(room, c)
	c.rooms[room] = true
}

func (c *Client) ReadPump() {
	// A bug in one connection must never take down the whole gateway: turn
	// any panic into a logged teardown of this client only.
	defer func() {
		if r := recover(); r != nil {
			slog.Error("ws read pump panic", "client_id", c.ID, "recover", r)
		}
	}()
	defer func() {
		if c.Hub != nil {
			c.Hub.Unregister(c)
		}
		if c.Conn != nil {
			c.Conn.Close()
		}
	}()
	if c.Conn == nil {
		return
	}
	c.Conn.SetReadLimit(maxMessageSize)
	c.Conn.SetReadDeadline(time.Now().Add(pongWait))
	c.Conn.SetPongHandler(func(string) error { c.Conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	for {
		_, raw, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				slog.Warn("ws read error", "client_id", c.ID, "err", err)
			}
			break
		}
		var sub Subscription
		if json.Unmarshal(raw, &sub) == nil && sub.Type != "" {
			switch sub.Type {
			case MsgSubscribe:
				ch := sub.Channel
				if ch == "" {
					ch = ChannelTrades
				}
				for _, p := range sub.Pairs {
					room := ch + ":" + p
					c.Hub.JoinRoom(room, c)
					c.rooms[room] = true
				}
			case MsgUnsubscribe:
				ch := sub.Channel
				if ch == "" {
					ch = ChannelTrades
				}
				for _, p := range sub.Pairs {
					room := ch + ":" + p
					c.Hub.LeaveRoom(room, c)
					delete(c.rooms, room)
				}
			}
			continue
		}
		var msg Message
		if json.Unmarshal(raw, &msg) != nil {
			continue
		}
		switch msg.Type {
		case MsgSubscribe:
			room := msg.Channel + ":" + msg.Pair
			c.Hub.JoinRoom(room, c)
			c.rooms[room] = true
		case MsgUnsubscribe:
			room := msg.Channel + ":" + msg.Pair
			c.Hub.LeaveRoom(room, c)
			delete(c.rooms, room)
		default:
			c.Hub.broadcast <- raw
		}
	}
}

func (c *Client) WritePump() {
	// A panic on one connection (e.g. a write after the peer half-closed the
	// socket) must tear down only this client, not the gateway process.
	defer func() {
		if r := recover(); r != nil {
			slog.Error("ws write pump panic", "client_id", c.ID, "recover", r)
		}
	}()
	if c.Conn == nil || c.Send == nil {
		return
	}
	ticker := time.NewTicker(pingPeriod)
	defer func() { ticker.Stop(); c.Conn.Close() }()
	for {
		select {
		case msg, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			w, err := c.Conn.NextWriter(websocket.TextMessage)
			if err != nil || w == nil {
				return
			}
			w.Write(msg)
			n := len(c.Send)
			for i := 0; i < n; i++ {
				w.Write([]byte("\n"))
				w.Write(<-c.Send)
			}
			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
