package handlers

import (
	"net/http"

	"pulse-backend/internal/wsocket"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// Allow all origins for development; tighten in production
	CheckOrigin: func(r *http.Request) bool { return true },
}

// WSHandler handles WebSocket upgrade and delegates to the Hub
type WSHandler struct {
	hub *wsocket.Hub
}

// NewWSHandler constructs a WSHandler
func NewWSHandler(hub *wsocket.Hub) *WSHandler {
	return &WSHandler{hub: hub}
}

// ServeWS godoc
// GET /ws
// Upgrades the HTTP connection to a WebSocket and registers the client with the Hub.
// Clients will receive real-time JSON events whenever a new incident is detected.
func (h *WSHandler) ServeWS(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Error().Err(err).Msg("WebSocket upgrade failed")
		return
	}
	h.hub.ServeClient(conn)
}
