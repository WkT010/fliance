package api

import (
	"net/http"
	"github.com/gin-gonic/gin"
	gorilla "github.com/gorilla/websocket"
	"github.com/WkT010/nexa-exchange/pkg/websocket"
)

type WSHandler struct{ hub *websocket.Hub }
func NewWSHandler(hub *websocket.Hub) *WSHandler { return &WSHandler{hub: hub} }

var upgrader = gorilla.Upgrader{ReadBufferSize: 4096, WriteBufferSize: 4096, CheckOrigin: func(r *http.Request) bool { return true }}

func (h *WSHandler) HandleWebSocket(c *gin.Context) {
	uid := c.Query("user_id"); if uid == "" { uid = "anon" }
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil { return }
	cl := websocket.NewClient(conn, h.hub, uid)
	h.hub.Register(cl)
	go cl.WritePump(); go cl.ReadPump()
}