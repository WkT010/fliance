package websocket

import (
	"encoding/json"
	"log"
	"time"
	"github.com/gorilla/websocket"
)

const (
	writeWait = 10 * time.Second
	pongWait  = 60 * time.Second
	pingPeriod = 54 * time.Second
	maxMsgSize = 4096
)

func NewClient(conn *websocket.Conn, hub *Hub, userID string) *Client {
	return &Client{ID: userID + "-" + time.Now().Format("150405.000"), UserID: userID, Conn: conn, Send: make(chan []byte, 256), Hub: hub}
}

func (c *Client) ReadPump() {
	defer func() { c.Hub.Unregister(c); c.Conn.Close() }()
	c.Conn.SetReadLimit(maxMsgSize)
	c.Conn.SetReadDeadline(time.Now().Add(pongWait))
	c.Conn.SetPongHandler(func(string) error { c.Conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	for {
		_, raw, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("ws read err: %v", err)
			}
			break
		}
		var sub Subscription
		if json.Unmarshal(raw, &sub) == nil && sub.Type != "" {
			switch sub.Type {
			case MsgSubscribe:
				for _, p := range sub.Pairs { c.Hub.JoinRoom(sub.Channel+":"+p, c) }
			case MsgUnsubscribe:
				for _, p := range sub.Pairs { c.Hub.LeaveRoom(sub.Channel+":"+p, c) }
			}
			continue
		}
		var msg Message
		if json.Unmarshal(raw, &msg) != nil { continue }
		switch msg.Type {
		case MsgSubscribe: c.Hub.JoinRoom(msg.Channel+":"+msg.Pair, c)
		case MsgUnsubscribe: c.Hub.LeaveRoom(msg.Channel+":"+msg.Pair, c)
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
			for i := len(c.Send); i > 0; i-- { w.Write([]byte("\n")); w.Write(<-c.Send) }
			w.Close()
		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil { return }
		}
	}
}
