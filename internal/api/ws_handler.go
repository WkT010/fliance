package api

import (
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	gorilla "github.com/gorilla/websocket"
	"github.com/WkT010/nexa-exchange/internal/auth"
	"github.com/WkT010/nexa-exchange/pkg/websocket"
)

type WSHandler struct {
	hub      *websocket.Hub
	jwtMgr   *auth.JWTManager
	upgrader gorilla.Upgrader
}

// NewWSHandler constructs a WebSocket handler. If jwtMgr is non-nil, the
// connecting client must supply a valid Bearer token via the
// "Authorization" header or the "token" query parameter; otherwise anonymous
// access is allowed (useful for public market-data feeds).
func NewWSHandler(hub *websocket.Hub, jwtMgr *auth.JWTManager) *WSHandler {
	return &WSHandler{
		hub:    hub,
		jwtMgr: jwtMgr,
		upgrader: gorilla.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			// Origin is validated by the CORS middleware on the HTTP upgrade
			// request; the upgrader itself accepts any origin so that
			// browser clients from configured origins can connect.
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

// SetJWTManager replaces the JWT manager at runtime.
func (h *WSHandler) SetJWTManager(m *auth.JWTManager) {
	h.jwtMgr = m
}

func (h *WSHandler) HandleWebSocket(c *gin.Context) {
	uid := c.Query("user_id")
	// If a JWT manager is configured, prefer the token-derived user id so a
	// client cannot impersonate another user by setting ?user_id=...
	if h.jwtMgr != nil {
		if tok := wsTokenFromRequest(c); tok != "" {
			if cl, err := h.jwtMgr.ValidateToken(tok); err == nil {
				uid = cl.UserID
			} else {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
				return
			}
		}
	}
	if uid == "" {
		uid = "anon"
	}
	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("ws upgrade error: %v", err)
		return
	}
	cl := websocket.NewClient(conn, h.hub, uid)
	h.hub.Register(cl)
	// Authenticated clients are automatically subscribed to their private
	// notification room so fills/order updates arrive without an explicit
	// subscribe message.
	if uid != "" && uid != "anon" {
		cl.JoinRoom(websocket.ChannelUser + ":" + uid)
	}
	go cl.WritePump()
	go cl.ReadPump()
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
