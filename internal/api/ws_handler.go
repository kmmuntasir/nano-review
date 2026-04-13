package api

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"

	"github.com/kmmuntasir/nano-review/internal/auth"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// originChecker returns a CheckOrigin function based on allowedOrigins config.
// If allowedOrigins is empty, all origins are permitted (dev-friendly default).
// Supports exact matches and wildcard subdomains (e.g., https://*.example.com).
func originChecker(allowedOrigins []string) func(r *http.Request) bool {
	if len(allowedOrigins) == 0 {
		return func(r *http.Request) bool { return true }
	}

	return func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true // Same-origin requests have no Origin header
		}

		for _, allowed := range allowedOrigins {
			if strings.HasPrefix(allowed, "https://*.") {
				wildcardDomain := strings.TrimPrefix(allowed, "https://*.")
				if originMatchesWildcardDomain(origin, wildcardDomain) {
					return true
				}
			} else if origin == allowed {
				return true
			}
		}
		return false
	}
}

// originMatchesWildcardDomain checks if an origin matches a wildcard HTTPS domain pattern.
// For example, with wildcardDomain "example.com", it matches "https://sub.example.com"
// and "https://example.com" but rejects "http://example.com".
func originMatchesWildcardDomain(origin, wildcardDomain string) bool {
	if !strings.HasPrefix(origin, "https://") {
		return false
	}
	rest := strings.TrimPrefix(origin, "https://")
	return rest == wildcardDomain || strings.HasSuffix(rest, "."+wildcardDomain)
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

		userID := user.Email
		if userID == "" {
			userID = user.ID
		}

		client := &WSClient{
			hub:    hub,
			conn:   conn,
			send:   make(chan []byte, 256),
			userID: userID,
		}

		hub.Register(client)

		go client.WritePump()
		go client.ReadPump()
	}
}
