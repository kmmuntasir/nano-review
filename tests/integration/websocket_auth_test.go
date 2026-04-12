//go:build integration

package integration

import (
	"crypto/tls"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// wsDialer is a WebSocket dialer that skips TLS verification for the
// self-signed test server certificate.
var wsDialer = &websocket.Dialer{
	TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
}

func TestWebSocket_ConnectsWithCookie(t *testing.T) {
	srv := newIntegrationServer(t)
	defer srv.Close()

	token := srv.CreateToken("test-user-cookie")

	wsURL := "wss" + strings.TrimPrefix(srv.baseURL, "https") + "/ws"
	header := http.Header{}
	header.Set("Cookie", "nano_session="+token)

	conn, resp, err := wsDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("dial failed: %v (status: %d)", err, resp.StatusCode)
	}
	defer conn.Close()

	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusSwitchingProtocols)
	}

	time.Sleep(100 * time.Millisecond)

	count := srv.hub.ClientCount()
	if count != 1 {
		t.Errorf("hub client count = %d, want 1", count)
	}
}

func TestWebSocket_ConnectsWithQueryParam(t *testing.T) {
	srv := newIntegrationServer(t)
	defer srv.Close()

	token := srv.CreateToken("test-user-query")

	wsURL := "wss" + strings.TrimPrefix(srv.baseURL, "https") + "/ws?token=" + token

	conn, resp, err := wsDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v (status: %d)", err, resp.StatusCode)
	}
	defer conn.Close()

	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusSwitchingProtocols)
	}

	time.Sleep(100 * time.Millisecond)

	count := srv.hub.ClientCount()
	if count != 1 {
		t.Errorf("hub client count = %d, want 1", count)
	}
}

func TestWebSocket_401WithoutAuth(t *testing.T) {
	srv := newIntegrationServer(t)
	defer srv.Close()

	wsURL := "wss" + strings.TrimPrefix(srv.baseURL, "https") + "/ws"

	_, resp, err := wsDialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("expected dial to fail without auth, but it succeeded")
	}

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestWebSocket_401InvalidToken(t *testing.T) {
	srv := newIntegrationServer(t)
	defer srv.Close()

	wsURL := "wss" + strings.TrimPrefix(srv.baseURL, "https") + "/ws?token=garbage-invalid"

	_, resp, err := wsDialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("expected dial to fail with invalid token, but it succeeded")
	}

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}
