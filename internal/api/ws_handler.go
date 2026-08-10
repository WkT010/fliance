package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/WkT010/nexa-exchange/internal/auth"
	"github.com/WkT010/nexa-exchange/internal/observability"
	"github.com/WkT010/nexa-exchange/pkg/websocket"
	"github.com/gin-gonic/gin"
	gorilla "github.com/gorilla/websocket"
)

const wsAuthTokenTimeout = 10 * time.Second

// wsAuthRequest is the first-message authentication contract:
// {"op":"auth","token":"<jwt>"}. Replies are {"op":"auth_ok"} or
// {"op":"auth_error"}; private channels stay rejected until auth succeeds.
type wsAuthRequest struct {
	Op    string `json:"op"`
	Token string `json:"token"`
}

type WSHandler struct {
	hub       *websocket.Hub
	jwtMgr    *auth.JWTManager
	upgrader  gorilla.Upgrader
	origin    *OriginChecker
	blacklist *TokenBlacklist
}

// NewWSHandler constructs a WebSocket handler. Identity is only derived from
// a valid JWT (Authorization header or "token" query parameter); anonymous
// connections are allowed but restricted to public market-data channels.
func NewWSHandler(hub *websocket.Hub, jwtMgr *auth.JWTManager) *WSHandler {
	h := &WSHandler{
		hub:    hub,
		jwtMgr: jwtMgr,
		origin: NewOriginChecker(nil),
	}
	h.upgrader = gorilla.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		// Strict origin check shared with the CORS middleware. Requests with
		// no Origin header (non-browser clients) are permitted.
		CheckOrigin: func(r *http.Request) bool {
			return h.origin.Allowed(r.Header.Get("Origin"))
		},
	}
	return h
}

// SetJWTManager replaces the JWT manager at runtime.
func (h *WSHandler) SetJWTManager(m *auth.JWTManager) {
	h.jwtMgr = m
}

// SetOriginChecker installs the shared CORS allow-list used by CheckOrigin.
func (h *WSHandler) SetOriginChecker(oc *OriginChecker) {
	if oc != nil {
		h.origin = oc
	}
}

// SetTokenBlacklist wires the revocation store so revoked JWTs cannot be used
// for WebSocket authentication either.
func (h *WSHandler) SetTokenBlacklist(bl *TokenBlacklist) {
	h.blacklist = bl
}

// SetMaxConnections propagates the global WS connection cap to the hub.
func (h *WSHandler) SetMaxConnections(n int) {
	if h.hub != nil {
		h.hub.SetMaxConnections(n)
	}
}

// validateWSToken checks a JWT for WebSocket authentication: signature,
// expiry, revocation, and token type (refresh tokens are not identities).
func (h *WSHandler) validateWSToken(tok string) (*auth.Claims, error) {
	cl, err := h.jwtMgr.ValidateToken(tok)
	if err != nil {
		return nil, err
	}
	if h.blacklist != nil && h.blacklist.IsRevoked(tok) {
		return nil, errTokenRevoked
	}
	if tt, _ := parseExtraClaims(tok); tt == "refresh" {
		return nil, errTokenType
	}
	return cl, nil
}

var (
	errTokenRevoked = &wsAuthError{"token revoked"}
	errTokenType    = &wsAuthError{"invalid token type"}
)

type wsAuthError struct{ reason string }

func (e *wsAuthError) Error() string { return e.reason }

func (h *WSHandler) HandleWebSocket(c *gin.Context) {
	// Global connection cap: reject before spending an upgrade.
	if h.hub.AtCapacity() {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "server connection limit reached"})
		return
	}
	// Identity is only ever derived from a valid JWT — the legacy "user_id"
	// query parameter is ignored so clients cannot impersonate other users.
	uid := ""
	if h.jwtMgr != nil {
		if tok := wsTokenFromRequest(c); tok != "" {
			cl, err := h.validateWSToken(tok)
			if err != nil {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
				return
			}
			uid = cl.UserID
		}
	}
	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		observability.WithRequestID(c.Request.Context()).Error("ws upgrade error", "err", err)
		return
	}
	cl := websocket.NewClient(conn, h.hub, uid)
	if err := h.hub.Register(cl); err != nil {
		_ = conn.WriteJSON(gin.H{"op": "error", "error": "server connection limit reached"})
		_ = conn.Close()
		return
	}
	go cl.WritePump()
	h.serveConn(conn, cl)
}

// serveConn runs the read loop for one connection. It implements the auth
// contract ({"op":"auth"} as the first message), enforces that private
// channels require authentication, and otherwise mirrors the subscription
// handling of websocket.Client.ReadPump.
func (h *WSHandler) serveConn(conn *gorilla.Conn, cl *websocket.Client) {
	defer func() {
		h.hub.Unregister(cl)
		conn.Close()
	}()
	authed := cl.UserID != "" && cl.UserID != "anon"
	// Authenticated connections are auto-subscribed to their private room so
	// fills/order updates arrive without an explicit subscribe message.
	if authed {
		cl.JoinRoom(websocket.ChannelUser + ":" + cl.UserID)
	}
	conn.SetReadLimit(16384)
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error { conn.SetReadDeadline(time.Now().Add(60 * time.Second)); return nil })

	send := func(v gin.H) {
		conn.SetWriteDeadline(time.Now().Add(wsAuthTokenTimeout))
		_ = conn.WriteJSON(v)
	}

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			if gorilla.IsUnexpectedCloseError(err, gorilla.CloseGoingAway, gorilla.CloseNormalClosure) {
				slog.Warn("ws read error", "client_id", cl.ID, "err", err)
			}
			return
		}

		// Connection-level ops.
		var opMsg wsAuthRequest
		if json.Unmarshal(raw, &opMsg) == nil && opMsg.Op != "" {
			switch opMsg.Op {
			case websocket.MsgAuth:
				if authed {
					send(gin.H{"op": "auth_ok"})
					continue
				}
				if h.jwtMgr == nil {
					send(gin.H{"op": "auth_error"})
					continue
				}
				cl2, err := h.validateWSToken(opMsg.Token)
				if err != nil || cl2 == nil {
					send(gin.H{"op": "auth_error"})
					continue
				}
				cl.UserID = cl2.UserID
				authed = true
				cl.JoinRoom(websocket.ChannelUser + ":" + cl.UserID)
				send(gin.H{"op": "auth_ok"})
			default:
				send(gin.H{"op": "error", "error": "unknown op"})
			}
			continue
		}

		// Multi-pair subscription messages.
		var sub websocket.Subscription
		if json.Unmarshal(raw, &sub) == nil && sub.Type != "" {
			switch sub.Type {
			case websocket.MsgSubscribe:
				ch := sub.Channel
				if ch == "" {
					ch = websocket.ChannelTrades
				}
				for _, p := range sub.Pairs {
					if !h.maybeJoin(cl, ch, p, authed, send) {
						continue
					}
				}
			case websocket.MsgUnsubscribe:
				ch := sub.Channel
				if ch == "" {
					ch = websocket.ChannelTrades
				}
				for _, p := range sub.Pairs {
					h.leaveRoom(cl, ch, p)
				}
			}
			continue
		}

		// Single-pair messages.
		var msg websocket.Message
		if json.Unmarshal(raw, &msg) != nil {
			continue
		}
		switch msg.Type {
		case websocket.MsgSubscribe:
			h.maybeJoin(cl, msg.Channel, msg.Pair, authed, send)
		case websocket.MsgUnsubscribe:
			h.leaveRoom(cl, msg.Channel, msg.Pair)
		default:
			// Free-form broadcast from unauthenticated clients is refused.
			if !authed {
				send(gin.H{"type": websocket.MsgError, "error": "authentication required"})
			}
		}
	}
}

// maybeJoin joins channel:pair when permitted; private channels require an
// authenticated connection and produce an explicit error message otherwise.
func (h *WSHandler) maybeJoin(cl *websocket.Client, ch, pair string, authed bool, send func(gin.H)) bool {
	if websocket.IsPrivateChannel(ch) && !authed {
		send(gin.H{"type": websocket.MsgError, "channel": ch, "error": "authentication required"})
		return false
	}
	cl.JoinRoom(ch + ":" + pair)
	return true
}

func (h *WSHandler) leaveRoom(cl *websocket.Client, ch, pair string) {
	h.hub.LeaveRoom(ch+":"+pair, cl)
}

// wsTokenFromRequest extracts a bearer token from the Authorization header or
// the "token" query parameter (the latter is required for browser WebSocket
// clients which cannot set headers on the upgrade request).
func wsTokenFromRequest(c *gin.Context) string {
	if a := c.GetHeader("Authorization"); a != "" {
		if p := strings.SplitN(a, " ", 2); len(p) == 2 && strings.EqualFold(p[0], "bearer") {
			return p[1]
		}
	}
	return c.Query("token")
}
