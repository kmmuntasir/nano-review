package api

import (
	"encoding/json"
	"testing"
	"time"
)

// mockWSConn implements the parts of websocket.Conn used by WSClient.
type mockWSConn struct {
	readMessages  chan []byte
	writeMessages chan []byte
	closed        chan struct{}
}

func newMockWSConn() *mockWSConn {
	return &mockWSConn{
		readMessages:  make(chan []byte, 64),
		writeMessages: make(chan []byte, 64),
		closed:        make(chan struct{}),
	}
}

func TestHub_RegisterAndClientCount(t *testing.T) {
	h := NewHub()
	defer close(h.register)

	c := &WSClient{hub: h, send: make(chan []byte, 16)}

	h.Register(c)

	// Give the hub time to process
	time.Sleep(50 * time.Millisecond)

	h.mu.RLock()
	count := len(h.clients)
	h.mu.RUnlock()

	if count != 1 {
		t.Errorf("client count = %d, want 1", count)
	}
}

func TestHub_BroadcastToSubscribers(t *testing.T) {
	h := NewHub()

	c := &WSClient{hub: h, send: make(chan []byte, 16)}
	h.Register(c)
	time.Sleep(50 * time.Millisecond)

	h.Subscribe(c, "run:test-123")
	time.Sleep(50 * time.Millisecond)

	msg := []byte(`{"type":"stream","run_id":"test-123","data":"hello"}`)
	h.Broadcast("run:test-123", msg)

	select {
	case received := <-c.send:
		if string(received) != string(msg) {
			t.Errorf("received = %s, want %s", string(received), string(msg))
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for broadcast message")
	}
}

func TestHub_BroadcastNonSubscriberGetsNothing(t *testing.T) {
	h := NewHub()

	c := &WSClient{hub: h, send: make(chan []byte, 16)}
	h.Register(c)
	time.Sleep(50 * time.Millisecond)

	// Broadcast to a topic c is NOT subscribed to
	h.Broadcast("run:other-id", []byte(`{"type":"stream"}`))

	select {
	case <-c.send:
		t.Error("non-subscriber should not receive message")
	case <-time.After(100 * time.Millisecond):
		// Expected: no message received
	}
}

func TestHub_Unsubscribe(t *testing.T) {
	h := NewHub()

	c := &WSClient{hub: h, send: make(chan []byte, 16)}
	h.Register(c)
	time.Sleep(50 * time.Millisecond)

	h.Subscribe(c, "run:test-123")
	time.Sleep(50 * time.Millisecond)

	h.Unsubscribe(c, "run:test-123")
	time.Sleep(50 * time.Millisecond)

	h.Broadcast("run:test-123", []byte(`{"type":"stream"}`))

	select {
	case <-c.send:
		t.Error("unsubscribed client should not receive message")
	case <-time.After(100 * time.Millisecond):
		// Expected
	}
}

func TestHub_UnregisterRemovesFromTopics(t *testing.T) {
	h := NewHub()

	c := &WSClient{hub: h, send: make(chan []byte, 16)}
	h.Register(c)
	time.Sleep(50 * time.Millisecond)

	h.Subscribe(c, "run:test-123")
	h.Subscribe(c, "all")
	time.Sleep(50 * time.Millisecond)

	h.Unregister(c)
	time.Sleep(50 * time.Millisecond)

	// Channel should be closed after unregister
	_, ok := <-c.send
	if ok {
		t.Error("send channel should be closed after unregister")
	}
}

func TestHub_BroadcastJSON(t *testing.T) {
	h := NewHub()

	c := &WSClient{hub: h, send: make(chan []byte, 16)}
	h.Register(c)
	time.Sleep(50 * time.Millisecond)

	h.Subscribe(c, "all")
	time.Sleep(50 * time.Millisecond)

	h.BroadcastJSON("all", map[string]string{
		"type":   "review_update",
		"run_id": "abc",
		"status": "completed",
	})

	select {
	case received := <-c.send:
		var msg map[string]string
		if err := json.Unmarshal(received, &msg); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		if msg["type"] != "review_update" {
			t.Errorf("type = %q, want %q", msg["type"], "review_update")
		}
		if msg["run_id"] != "abc" {
			t.Errorf("run_id = %q, want %q", msg["run_id"], "abc")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for broadcast JSON")
	}
}

func TestHub_MultipleSubscribers(t *testing.T) {
	h := NewHub()

	c1 := &WSClient{hub: h, send: make(chan []byte, 16)}
	c2 := &WSClient{hub: h, send: make(chan []byte, 16)}
	c3 := &WSClient{hub: h, send: make(chan []byte, 16)}

	h.Register(c1)
	h.Register(c2)
	h.Register(c3)
	time.Sleep(50 * time.Millisecond)

	// Only c1 and c2 subscribe
	h.Subscribe(c1, "run:test")
	h.Subscribe(c2, "run:test")
	time.Sleep(50 * time.Millisecond)

	msg := []byte(`{"type":"stream","data":"test"}`)
	h.Broadcast("run:test", msg)

	for i, c := range []*WSClient{c1, c2, c3} {
		if i < 2 {
			// c1 and c2 should receive
			select {
			case <-c.send:
			case <-time.After(time.Second):
				t.Errorf("client %d: timed out waiting for message", i)
			}
		} else {
			// c3 should NOT receive
			select {
			case <-c.send:
				t.Errorf("client %d (non-subscriber) should not receive message", i)
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
}

func TestHub_BroadcastToNonexistentTopic(t *testing.T) {
	h := NewHub()
	// Broadcasting to a topic with no subscribers should not panic or block
	done := make(chan struct{})
	go func() {
		h.Broadcast("run:nonexistent", []byte(`{}`))
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("broadcast to nonexistent topic blocked")
	}
}
