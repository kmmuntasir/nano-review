package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/kmmuntasir/nano-review/internal/auth"
)

func TestHandleWebSocket_ValidToken(t *testing.T) {
	tests := []struct {
		name   string
		userID string
		source string
	}{
		{
			name:   "authenticated user from cookie token",
			userID: "sess-abc-123",
			source: "cookie",
		},
		{
			name:   "authenticated user from webhook",
			userID: "webhook-source",
			source: "webhook",
		},
		{
			name:   "authenticated user with api_token source",
			userID: "api-key-42",
			source: "api_token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hub := NewHub()

			mux := http.NewServeMux()
			mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
				// Simulate the RequireAuth middleware injecting user into context
				user := auth.User{ID: tt.userID, Source: tt.source}
				ctx := auth.ContextWithUser(r.Context(), user)
				HandleWebSocket(hub)(w, r.WithContext(ctx))
			})

			server := httptest.NewServer(mux)
			defer server.Close()

			wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
			conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
			if err != nil {
				t.Fatalf("dial failed: %v (status: %d)", err, resp.StatusCode)
			}
			defer conn.Close()

			if resp.StatusCode != http.StatusSwitchingProtocols {
				t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusSwitchingProtocols)
			}

			// Give hub time to process registration
			time.Sleep(100 * time.Millisecond)

			hub.mu.RLock()
			count := len(hub.clients)
			hub.mu.RUnlock()

			if count != 1 {
				t.Errorf("client count = %d, want 1", count)
			}

			// Verify the registered client has the correct userID
			hub.mu.RLock()
			var foundUserID string
			for c := range hub.clients {
				foundUserID = c.userID
			}
			hub.mu.RUnlock()

			if foundUserID != tt.userID {
				t.Errorf("client userID = %q, want %q", foundUserID, tt.userID)
			}
		})
	}
}

func TestHandleWebSocket_NoUserInContext(t *testing.T) {
	hub := NewHub()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", HandleWebSocket(hub))

	server := httptest.NewServer(mux)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v (status: %d)", err, resp.StatusCode)
	}
	defer conn.Close()

	// Give hub time to process registration
	time.Sleep(100 * time.Millisecond)

	hub.mu.RLock()
	count := len(hub.clients)
	hub.mu.RUnlock()

	if count != 1 {
		t.Errorf("client count = %d, want 1", count)
	}

	// User with empty ID should be registered (no crash)
	hub.mu.RLock()
	var foundUserID string
	for c := range hub.clients {
		foundUserID = c.userID
	}
	hub.mu.RUnlock()

	if foundUserID != "" {
		t.Errorf("client userID = %q, want empty string", foundUserID)
	}
}

func newSessionManager(t *testing.T) *auth.SessionManager {
	t.Helper()
	hmacKey := make([]byte, 32)
	for i := range hmacKey {
		hmacKey[i] = byte(i)
	}
	return auth.NewSessionManager(hmacKey, 24, nil)
}

func dialWithCookie(t *testing.T, serverURL, path string, cookie *http.Cookie) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(serverURL, "http") + path
	dialer := &websocket.Dialer{}
	return dialer.Dial(wsURL, http.Header{
		"Cookie": []string{cookie.Name + "=" + cookie.Value},
	})
}

func TestHandleWebSocket_RequireAuthValidToken(t *testing.T) {
	sm := newSessionManager(t)
	token := sm.CreateToken("session-user-789", auth.TokenUserInfo{})

	hub := NewHub()

	handler := sm.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user.ID != "session-user-789" {
			t.Errorf("user ID = %q, want %q", user.ID, "session-user-789")
		}
		if user.Source != "cookie" {
			t.Errorf("user source = %q, want %q", user.Source, "cookie")
		}
		HandleWebSocket(hub)(w, r)
	}))

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", handler.ServeHTTP)

	server := httptest.NewServer(mux)
	defer server.Close()

	cookie := &http.Cookie{Name: sm.CookieName(), Value: token}
	conn, resp, err := dialWithCookie(t, server.URL, "/ws", cookie)
	if err != nil {
		t.Fatalf("dial with valid token failed: %v (status: %d)", err, resp.StatusCode)
	}
	defer conn.Close()

	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusSwitchingProtocols)
	}

	time.Sleep(100 * time.Millisecond)

	hub.mu.RLock()
	count := len(hub.clients)
	hub.mu.RUnlock()

	if count != 1 {
		t.Errorf("client count = %d, want 1", count)
	}
}

func TestHandleWebSocket_RequireAuthMissingCookie(t *testing.T) {
	sm := newSessionManager(t)

	hub := NewHub()

	handler := sm.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("inner handler should not be called when cookie is missing")
		HandleWebSocket(hub)(w, r)
	}))

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", handler.ServeHTTP)

	server := httptest.NewServer(mux)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	dialer := &websocket.Dialer{}
	_, resp, err := dialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("expected error when dialing without session cookie")
	}

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}

	time.Sleep(50 * time.Millisecond)

	hub.mu.RLock()
	count := len(hub.clients)
	hub.mu.RUnlock()

	if count != 0 {
		t.Errorf("client count = %d, want 0 (no client should be registered)", count)
	}
}

func TestHandleWebSocket_RequireAuthInvalidToken(t *testing.T) {
	sm := newSessionManager(t)

	hub := NewHub()

	handler := sm.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("inner handler should not be called when token is invalid")
		HandleWebSocket(hub)(w, r)
	}))

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", handler.ServeHTTP)

	server := httptest.NewServer(mux)
	defer server.Close()

	cookie := &http.Cookie{Name: sm.CookieName(), Value: "invalid.token.payload.here"}
	_, resp, err := dialWithCookie(t, server.URL, "/ws", cookie)
	if err == nil {
		t.Fatal("expected error when dialing with invalid token")
	}

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}

	time.Sleep(50 * time.Millisecond)

	hub.mu.RLock()
	count := len(hub.clients)
	hub.mu.RUnlock()

	if count != 0 {
		t.Errorf("client count = %d, want 0 (no client should be registered)", count)
	}
}

func TestHandleWebSocket_ClientWithAttributes(t *testing.T) {
	hub := NewHub()

	user := auth.User{
		ID:     "user-with-attrs",
		Source: "cookie",
		Attributes: map[string]string{
			"repo":  "owner/repo",
			"scope": "read",
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		ctx := auth.ContextWithUser(r.Context(), user)
		HandleWebSocket(hub)(w, r.WithContext(ctx))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	time.Sleep(100 * time.Millisecond)

	hub.mu.RLock()
	var foundUserID string
	for c := range hub.clients {
		foundUserID = c.userID
	}
	hub.mu.RUnlock()

	if foundUserID != "user-with-attrs" {
		t.Errorf("client userID = %q, want %q", foundUserID, "user-with-attrs")
	}
}

func TestOriginChecker_EmptyAllowedOrigins(t *testing.T) {
	checker := originChecker(nil)
	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	if !checker(req) {
		t.Error("nil allowedOrigins should allow all origins")
	}

	checker = originChecker([]string{})
	if !checker(req) {
		t.Error("empty allowedOrigins should allow all origins")
	}
}

func TestOriginChecker_ExactMatch(t *testing.T) {
	origins := []string{"https://example.com", "https://app.example.com"}
	checker := originChecker(origins)

	tests := []struct {
		name    string
		origin  string
		allowed bool
	}{
		{"exact match first", "https://example.com", true},
		{"exact match second", "https://app.example.com", true},
		{"no match", "https://other.com", false},
		{"http not matching https", "http://example.com", false},
		{"trailing slash differs", "https://example.com/", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/ws", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			if checker(req) != tt.allowed {
				t.Errorf("originChecker(%q) = %v, want %v", tt.origin, checker(req), tt.allowed)
			}
		})
	}
}

func TestOriginChecker_WildcardSubdomain(t *testing.T) {
	origins := []string{"https://*.example.com"}
	checker := originChecker(origins)

	tests := []struct {
		name    string
		origin  string
		allowed bool
	}{
		{"subdomain match", "https://sub.example.com", true},
		{"nested subdomain", "https://a.b.example.com", true},
		{"bare domain matches wildcard", "https://example.com", true},
		{"different scheme rejected", "http://sub.example.com", false},
		{"different domain rejected", "https://other.com", false},
		{"different tld rejected", "https://example.org", false},
		{"similar but different domain", "https://example.com.evil.com", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/ws", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			if checker(req) != tt.allowed {
				t.Errorf("originChecker(%q) = %v, want %v", tt.origin, checker(req), tt.allowed)
			}
		})
	}
}

func TestOriginChecker_MissingOriginHeader(t *testing.T) {
	origins := []string{"https://example.com"}
	checker := originChecker(origins)

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	// No Origin header set — simulates same-origin request
	if !checker(req) {
		t.Error("missing Origin header should be allowed (same-origin)")
	}
}

func TestOriginChecker_MixedExactAndWildcard(t *testing.T) {
	origins := []string{"https://exact.com", "https://*.wildcard.com", "http://localhost:8080"}
	checker := originChecker(origins)

	tests := []struct {
		name    string
		origin  string
		allowed bool
	}{
		{"exact match", "https://exact.com", true},
		{"wildcard subdomain", "https://sub.wildcard.com", true},
		{"wildcard bare domain", "https://wildcard.com", true},
		{"http local dev", "http://localhost:8080", true},
		{"not in list", "https://other.com", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/ws", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			if checker(req) != tt.allowed {
				t.Errorf("originChecker(%q) = %v, want %v", tt.origin, checker(req), tt.allowed)
			}
		})
	}
}

func TestOriginMatchesWildcardDomain(t *testing.T) {
	tests := []struct {
		origin        string
		wildcardDomain string
		want          bool
	}{
		{"https://sub.example.com", "example.com", true},
		{"https://example.com", "example.com", true},
		{"https://a.b.example.com", "example.com", true},
		{"http://sub.example.com", "example.com", false},
		{"https://example.org", "example.com", false},
		{"ftp://sub.example.com", "example.com", false},
		{"sub.example.com", "example.com", false},
		{"https://example.com.evil.com", "example.com", false},
		{"https://example.com:443", "example.com:443", true},
		{"https://sub.example.com:443", "example.com:443", true},
	}
	for _, tt := range tests {
		t.Run(tt.origin+" vs "+tt.wildcardDomain, func(t *testing.T) {
			got := originMatchesWildcardDomain(tt.origin, tt.wildcardDomain)
			if got != tt.want {
				t.Errorf("originMatchesWildcardDomain(%q, %q) = %v, want %v", tt.origin, tt.wildcardDomain, got, tt.want)
			}
		})
	}
}

func TestOriginChecker_ReturnsFunction(t *testing.T) {
	checker := originChecker([]string{"https://example.com"})
	if checker == nil {
		t.Fatal("originChecker should return a non-nil function")
	}

	// Verify it's a func(*http.Request) bool
	var _ func(*http.Request) bool = checker
}

func TestHandleWebSocket_AuthDisabledPassesThrough(t *testing.T) {
	t.Setenv("AUTH_ENABLED", "false")
	defer t.Setenv("AUTH_ENABLED", "")

	sm := newSessionManager(t)

	hub := NewHub()

	handler := sm.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		// When auth is disabled, user should be zero value
		if user.ID != "" {
			t.Errorf("expected empty user ID when auth disabled, got %q", user.ID)
		}
		HandleWebSocket(hub)(w, r)
	}))

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", handler.ServeHTTP)

	server := httptest.NewServer(mux)
	defer server.Close()

	// Dial without cookie — should succeed when auth is disabled
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	dialer := &websocket.Dialer{}
	conn, resp, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed when auth disabled: %v (status: %d)", err, resp.StatusCode)
	}
	defer conn.Close()

	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusSwitchingProtocols)
	}

	time.Sleep(100 * time.Millisecond)

	hub.mu.RLock()
	count := len(hub.clients)
	hub.mu.RUnlock()

	if count != 1 {
		t.Errorf("client count = %d, want 1", count)
	}
}
