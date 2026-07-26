package websocket

import (
	"encoding/json"
	"log"
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
}

func NewClient(conn *websocket.Conn, hub *Hub, userID string) *Client {
	return &Client{ID: userID + "-" + time.Now().Format("150405.000"), UserID: userID, Conn: conn, Send: make(chan []byte, 256), Hub: hub, rooms: make(map[string]bool)}
}

func (c *Client) ReadPump() {
	defer func() { c.Hub.Unregister(c); c.Conn.Close() }()
	c.Conn.SetReadLimit(maxMessageSize)
	c.Conn.SetReadDeadline(time.Now().Add(pongWait))
	c.Conn.SetPongHandler(func(string) error { c.Conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	for {
		_, raw, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("ws read error (%s): %v", c.ID, err)
			}
			break
		}
		var sub Subscription
		if json.Unmarshal(raw, &sub) == nil && sub.Type != "" {
			switch sub.Type {
			case MsgSubscribe:
				ch := sub.Channel; if ch == "" { ch = ChannelTrades }
				for _, p := range sub.Pairs { room := ch + ":" + p; c.Hub.JoinRoom(room, c); c.rooms[room] = true }
			case MsgUnsubscribe:
				ch := sub.Channel; if ch == "" { ch = ChannelTrades }
				for _, p := range sub.Pairs { room := ch + ":" + p; c.Hub.LeaveRoom(room, c); delete(c.rooms, room) }
			}
			continue
		}
		var msg Message
		if json.Unmarshal(raw, &msg) != nil { continue }
		switch msg.Type {
		case MsgSubscribe: room := msg.Channel + ":" + msg.Pair; c.Hub.JoinRoom(room, c); c.rooms[room] = true
		case MsgUnsubscribe: room := msg.Channel + ":" + msg.Pair; c.Hub.LeaveRoom(room, c); delete(c.rooms, room)
		default: c.Hub.broadcast <- raw
		}
	}
}

func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() { ticker.Stop(); c.Conn.Close() }()
	for {
		select {
		case msg, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok { c.Conn.WriteMessage(websocket.CloseMessage, []byte{}); return }
			w, _ := c.Conn.NextWriter(websocket.TextMessage)
			w.Write(msg)
			n := len(c.Send)
			for i := 0; i < n; i++ { w.Write([]byte("\n")); w.Write(<-c.Send) }
			w.Close()
		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil { return }
		}
	}
}
