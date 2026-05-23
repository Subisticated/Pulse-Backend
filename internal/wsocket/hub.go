package wsocket

import (
	"sync"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

// Client represents a single connected WebSocket client
type Client struct {
	conn *websocket.Conn
	send chan []byte
}

// Hub manages all active WebSocket clients and broadcast messages
type Hub struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
	mu         sync.RWMutex
}

// NewHub creates and returns a new Hub instance
func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

// Run starts the hub event loop — must be called in a goroutine
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			log.Info().Msg("WebSocket client connected")

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
			log.Info().Msg("WebSocket client disconnected")

		case message := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					// Client send buffer full — drop and unregister
					close(client.send)
					delete(h.clients, client)
				}
			}
			h.mu.RUnlock()
		}
	}
}

// Broadcast sends a JSON payload to all connected clients
func (h *Hub) Broadcast(payload []byte) {
	h.broadcast <- payload
}

// ServeClient pumps messages from hub to a single WebSocket connection
func (h *Hub) ServeClient(conn *websocket.Conn) {
	client := &Client{
		conn: conn,
		send: make(chan []byte, 64),
	}

	h.register <- client

	// Pump writes in a separate goroutine
	go func() {
		defer func() {
			h.unregister <- client
			conn.Close()
		}()
		for msg := range client.send {
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		}
	}()

	// Read loop (keeps connection alive, discards incoming messages)
	defer func() {
		h.unregister <- client
		conn.Close()
	}()
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			return
		}
	}
}
