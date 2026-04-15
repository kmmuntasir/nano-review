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

func waitForCondition(t *testing.T, interval, timeout time.Duration, fn func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(interval)
	}
	t.Fatalf("waitForCondition timed out: %s", msg)
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

	waitForCondition(t, 5*time.Millisecond, 500*time.Millisecond, func() bool {
		return srv.hub.ClientCount() == 1
	}, "expected hub client count 1")
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

	waitForCondition(t, 5*time.Millisecond, 500*time.Millisecond, func() bool {
		return srv.hub.ClientCount() == 1
	}, "expected hub client count 1")
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
