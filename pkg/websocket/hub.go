package websocket

import (
	"log"
	"sync"
)

type Hub struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
	rooms      map[string]map[*Client]bool
	mu         sync.RWMutex
}

func NewHub() *Hub {
	return &Hub{clients: make(map[*Client]bool), broadcast: make(chan []byte, 256), register: make(chan *Client), unregister: make(chan *Client), rooms: make(map[string]map[*Client]bool)}
}

func (h *Hub) Run() {
	for {
		select {
		case c := <-h.register:
			h.mu.Lock(); h.clients[c] = true; h.mu.Unlock()
			log.Printf("ws registered: %s (user=%s)", c.ID, c.UserID)
		case c := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[c]; ok {
				for room, members := range h.rooms {
					if _, in := members[c]; in { delete(members, c); if len(members) == 0 { delete(h.rooms, room) } }
				}
				delete(h.clients, c); close(c.Send)
			}
			h.mu.Unlock()
			log.Printf("ws unregistered: %s", c.ID)
		case msg := <-h.broadcast:
			h.mu.RLock()
			for c := range h.clients {
				select { case c.Send <- msg: default: go h.Unregister(c) }
			}
			h.mu.RUnlock()
		}
	}
}

func (h *Hub) Register(c *Client) { h.register <- c }
func (h *Hub) Unregister(c *Client) { h.unregister <- c }

func (h *Hub) JoinRoom(room string, c *Client) {
	h.mu.Lock(); defer h.mu.Unlock()
	if h.rooms[room] == nil { h.rooms[room] = make(map[*Client]bool) }
	h.rooms[room][c] = true
}

func (h *Hub) LeaveRoom(room string, c *Client) {
	h.mu.Lock(); defer h.mu.Unlock()
	if members, ok := h.rooms[room]; ok { delete(members, c); if len(members) == 0 { delete(h.rooms, room) } }
}

func (h *Hub) BroadcastToRoom(room string, msg []byte) {
	h.mu.RLock()
	members := h.rooms[room]
	if members == nil { h.mu.RUnlock(); return }
	clients := make([]*Client, 0, len(members))
	for c := range members { clients = append(clients, c) }
	h.mu.RUnlock()
	for _, c := range clients {
		select { case c.Send <- msg: default: go h.Unregister(c) }
	}
}

func (h *Hub) BroadcastToUser(userID string, msg []byte) {
	h.mu.RLock()
	clients := make([]*Client, 0)
	for c := range h.clients { if c.UserID == userID { clients = append(clients, c) } }
	h.mu.RUnlock()
	for _, c := range clients {
		select { case c.Send <- msg: default: }
	}
}
