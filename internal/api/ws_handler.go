package api

import (
	"log/slog"
	"net/http"

	"github.com/gorilla/websocket"

	"github.com/kmmuntasir/nano-review/internal/auth"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// HandleWebSocket upgrades an HTTP connection to WebSocket and registers the client.
func HandleWebSocket(hub *Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			slog.Error("websocket upgrade failed", "error", err)
			return
		}

		client := &WSClient{
			hub:    hub,
			conn:   conn,
			send:   make(chan []byte, 256),
			userID: user.ID,
		}

		hub.Register(client)

		go client.WritePump()
		go client.ReadPump()
	}
}
