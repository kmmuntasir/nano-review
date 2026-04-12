package api

import (
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/gorilla/websocket"
)

// WSBroadcast represents a message to be broadcast to subscribers of a topic.
type WSBroadcast struct {
	Topic string
	Data  []byte
}

// WSSubscription represents a client subscribing/unsubscribing from a topic.
type WSSubscription struct {
	Client *WSClient
	Topic  string
}

// WSClient represents a single WebSocket connection managed by the Hub.
type WSClient struct {
	hub    *Hub
	conn   *websocket.Conn
	send   chan []byte
	userID string
}

// Hub manages WebSocket clients and topic-based message broadcasting.
type Hub struct {
	mu          sync.RWMutex
	clients     map[*WSClient]bool
	topics      map[string]map[*WSClient]bool
	register    chan *WSClient
	unregister  chan *WSClient
	subscribe   chan WSSubscription
	unsubscribe chan WSSubscription
	broadcast   chan WSBroadcast
}

// NewHub creates a new Hub and starts its run loop in a background goroutine.
func NewHub() *Hub {
	h := &Hub{
		clients:     make(map[*WSClient]bool),
		topics:      make(map[string]map[*WSClient]bool),
		register:    make(chan *WSClient),
		unregister:  make(chan *WSClient),
		subscribe:   make(chan WSSubscription),
		unsubscribe: make(chan WSSubscription),
		broadcast:   make(chan WSBroadcast, 256),
	}
	go h.run()
	return h
}

// Run processes all Hub channel events. It must be run in its own goroutine.
func (h *Hub) run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			slog.Debug("websocket client registered", "clients", h.clientCount())

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
				// Remove from all topics
				for topic, subs := range h.topics {
					delete(subs, client)
					if len(subs) == 0 {
						delete(h.topics, topic)
					}
				}
			}
			h.mu.Unlock()
			slog.Debug("websocket client unregistered", "clients", h.clientCount())

		case sub := <-h.subscribe:
			h.mu.Lock()
			if h.topics[sub.Topic] == nil {
				h.topics[sub.Topic] = make(map[*WSClient]bool)
			}
			h.topics[sub.Topic][sub.Client] = true
			h.mu.Unlock()
			slog.Debug("websocket client subscribed", "topic", sub.Topic)

		case sub := <-h.unsubscribe:
			h.mu.Lock()
			if subs, ok := h.topics[sub.Topic]; ok {
				delete(subs, sub.Client)
				if len(subs) == 0 {
					delete(h.topics, sub.Topic)
				}
			}
			h.mu.Unlock()
			slog.Debug("websocket client unsubscribed", "topic", sub.Topic)

		case msg := <-h.broadcast:
			h.mu.RLock()
			subs := h.topics[msg.Topic]
			for client := range subs {
				select {
				case client.send <- msg.Data:
				default:
					// Client send buffer full — skip to avoid blocking broadcast
				}
			}
			h.mu.RUnlock()
		}
	}
}

// Register adds a client to the hub.
func (h *Hub) Register(client *WSClient) {
	h.register <- client
}

// Unregister removes a client from the hub.
func (h *Hub) Unregister(client *WSClient) {
	h.unregister <- client
}

// Subscribe adds a client to a topic.
func (h *Hub) Subscribe(client *WSClient, topic string) {
	h.subscribe <- WSSubscription{Client: client, Topic: topic}
}

// Unsubscribe removes a client from a topic.
func (h *Hub) Unsubscribe(client *WSClient, topic string) {
	h.unsubscribe <- WSSubscription{Client: client, Topic: topic}
}

// Broadcast sends data to all subscribers of a topic.
// It is non-blocking: if the broadcast channel is full, the message is dropped.
func (h *Hub) Broadcast(topic string, data []byte) {
	select {
	case h.broadcast <- WSBroadcast{Topic: topic, Data: data}:
	default:
		slog.Warn("websocket broadcast channel full, dropping message", "topic", topic)
	}
}

// BroadcastJSON marshals v as JSON and broadcasts to all subscribers of a topic.
func (h *Hub) BroadcastJSON(topic string, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		slog.Error("failed to marshal broadcast message", "error", err)
		return
	}
	h.Broadcast(topic, data)
}

// ReadPump reads messages from the WebSocket connection and dispatches commands.
// It must be run in its own goroutine for each client.
func (c *WSClient) ReadPump() {
	defer func() {
		c.hub.Unregister(c)
		c.conn.Close()
	}()

	c.conn.SetReadLimit(512)
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			break
		}

		var cmd struct {
			Type  string `json:"type"`
			Topic string `json:"topic"`
			RunID string `json:"run_id"`
		}
		if err := json.Unmarshal(message, &cmd); err != nil {
			continue
		}

		switch cmd.Type {
		case "subscribe":
			topic := cmd.Topic
			if topic == "" && cmd.RunID != "" {
				topic = "run:" + cmd.RunID
			}
			if topic == "" {
				continue
			}
			c.hub.Subscribe(c, topic)
		case "unsubscribe":
			topic := cmd.Topic
			if topic == "" && cmd.RunID != "" {
				topic = "run:" + cmd.RunID
			}
			if topic == "" {
				continue
			}
			c.hub.Unsubscribe(c, topic)
		case "ping":
			c.hub.Subscribe(c, "all")
		}
	}
}

// WritePump writes messages from the send channel to the WebSocket connection.
// It must be run in its own goroutine for each client.
func (c *WSClient) WritePump() {
	defer c.conn.Close()

	for {
		message, ok := <-c.send
		if !ok {
			// Channel closed by hub.
			c.conn.WriteMessage(websocket.CloseMessage, []byte{})
			return
		}
		if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
			return
		}
	}
}

func (h *Hub) clientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// ClientCount returns the current number of connected WebSocket clients.
func (h *Hub) ClientCount() int {
	return h.clientCount()
}
