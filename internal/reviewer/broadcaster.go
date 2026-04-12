package reviewer

// Broadcaster allows the worker to push events to WebSocket subscribers
// without depending on the api package directly.
type Broadcaster interface {
	Broadcast(topic string, data []byte)
}
