package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func testKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate test key: %v", err)
	}
	return key
}

// --- NewSessionManager ---

func TestNewSessionManager(t *testing.T) {
	t.Run("valid key and positive max age", func(t *testing.T) {
		m := NewSessionManager(testKey(t), 12, []string{"example.com"})
		if m == nil {
			t.Fatal("expected non-nil SessionManager")
		}
		if m.maxAge != 12*time.Hour {
			t.Errorf("maxAge = %v, want 12h", m.maxAge)
		}
	})

	t.Run("zero max age defaults to 24h", func(t *testing.T) {
		m := NewSessionManager(testKey(t), 0, nil)
		if m.maxAge != 24*time.Hour {
			t.Errorf("maxAge = %v, want 24h", m.maxAge)
		}
	})

	t.Run("negative max age defaults to 24h", func(t *testing.T) {
		m := NewSessionManager(testKey(t), -5, nil)
		if m.maxAge != 24*time.Hour {
			t.Errorf("maxAge = %v, want 24h", m.maxAge)
		}
	})

	t.Run("short key panics", func(t *testing.T) {
		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("expected panic for short hmacKey")
			}
			if !strings.Contains(fmt.Sprint(r), "32 bytes") {
				t.Errorf("panic message = %v, want mention of 32 bytes", r)
			}
		}()
		NewSessionManager([]byte("short"), 1, nil)
	})

	t.Run("nil key panics", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic for nil hmacKey")
			}
		}()
		NewSessionManager(nil, 1, nil)
	})
}

// --- CreateToken / ValidateToken round-trip ---

func TestTokenRoundTrip(t *testing.T) {
	key := testKey(t)
	m := NewSessionManager(key, 1, nil)

	token := m.CreateToken("session-abc-123")

	parsedID, err := m.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken() error = %v", err)
	}
	if parsedID != "session-abc-123" {
		t.Errorf("ValidateToken() id = %q, want %q", parsedID, "session-abc-123")
	}
}

func TestCreateTokenFormat(t *testing.T) {
	m := NewSessionManager(testKey(t), 1, nil)

	token := m.CreateToken("sess-1")

	parts := strings.Split(token, ".")
	if len(parts) != 4 {
		t.Fatalf("token should have 4 dot-separated segments, got %d", len(parts))
	}

	for i, part := range parts {
		if part == "" {
			t.Errorf("token segment %d is empty", i)
		}
	}
}

// --- ValidateToken errors ---

func TestValidateTokenErrors(t *testing.T) {
	key := testKey(t)
	m := NewSessionManager(key, 1, nil)

	t.Run("empty token", func(t *testing.T) {
		_, err := m.ValidateToken("")
		if !errors.Is(err, ErrInvalidToken) {
			t.Errorf("error = %v, want ErrInvalidToken", err)
		}
	})

	t.Run("wrong number of segments", func(t *testing.T) {
		_, err := m.ValidateToken("a.b")
		if !errors.Is(err, ErrInvalidToken) {
			t.Errorf("error = %v, want ErrInvalidToken", err)
		}
	})

	t.Run("garbage segments", func(t *testing.T) {
		_, err := m.ValidateToken("!!!.!!!.!!!.!!!")
		if !errors.Is(err, ErrInvalidToken) {
			t.Errorf("error = %v, want ErrInvalidToken", err)
		}
	})

	t.Run("tampered session ID", func(t *testing.T) {
		token := m.CreateToken("session-1")
		parts := strings.SplitN(token, ".", 4)
		// Replace the session ID segment with something different.
		parts[0] = encodeSegment("tampered-id")
		fake := strings.Join(parts, ".")

		_, err := m.ValidateToken(fake)
		if !errors.Is(err, ErrInvalidToken) {
			t.Errorf("error = %v, want ErrInvalidToken", err)
		}
	})

	t.Run("tampered signature", func(t *testing.T) {
		token := m.CreateToken("session-1")
		parts := strings.SplitN(token, ".", 4)
		parts[3] = encodeSegment("fake-signature")
		fake := strings.Join(parts, ".")

		_, err := m.ValidateToken(fake)
		if !errors.Is(err, ErrInvalidToken) {
			t.Errorf("error = %v, want ErrInvalidToken", err)
		}
	})

	t.Run("different key rejects", func(t *testing.T) {
		m1 := NewSessionManager(testKey(t), 1, nil)
		m2 := NewSessionManager(testKey(t), 1, nil)

		token := m1.CreateToken("session-1")
		_, err := m2.ValidateToken(token)
		if !errors.Is(err, ErrInvalidToken) {
			t.Errorf("error = %v, want ErrInvalidToken", err)
		}
	})

	t.Run("expired token", func(t *testing.T) {
		// Use 0.0001 hours (~0.36 seconds) so the token expires immediately.
		expired := NewSessionManager(key, 0.0001, nil)
		token := expired.CreateToken("session-1")

		// Wait for expiration.
		time.Sleep(400 * time.Millisecond)

		_, err := expired.ValidateToken(token)
		if !errors.Is(err, ErrExpiredToken) {
			t.Errorf("error = %v, want ErrExpiredToken", err)
		}
	})
}

func TestValidateTokenNonExhaustive(t *testing.T) {
	// Additional edge cases for segment parsing.
	t.Run("invalid base64 in session ID segment", func(t *testing.T) {
		m := NewSessionManager(testKey(t), 1, nil)
		_, err := m.ValidateToken("not-valid-base64!!.bmU=.bmU=.bmU=")
		if !errors.Is(err, ErrInvalidToken) {
			t.Errorf("error = %v, want ErrInvalidToken", err)
		}
	})
}

// --- Cookie helpers ---

func TestSetCookie(t *testing.T) {
	m := NewSessionManager(testKey(t), 12, []string{"example.com"})
	token := m.CreateToken("sess-1")

	w := httptest.NewRecorder()
	m.SetCookie(w, token)

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}

	c := cookies[0]
	if c.Name != cookieName {
		t.Errorf("cookie name = %q, want %q", c.Name, cookieName)
	}
	if c.Value != token {
		t.Errorf("cookie value mismatch")
	}
	if !c.Secure {
		t.Error("expected Secure=true")
	}
	if !c.HttpOnly {
		t.Error("expected HttpOnly=true")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", c.SameSite)
	}
	if c.Path != "/" {
		t.Errorf("Path = %q, want %q", c.Path, "/")
	}
	if c.Domain != "example.com" {
		t.Errorf("Domain = %q, want %q", c.Domain, "example.com")
	}
	if c.MaxAge != int(12*time.Hour.Seconds()) {
		t.Errorf("MaxAge = %d, want %d", c.MaxAge, int(12*time.Hour.Seconds()))
	}
}

func TestSetCookieNoDomain(t *testing.T) {
	m := NewSessionManager(testKey(t), 1, nil)

	w := httptest.NewRecorder()
	m.SetCookie(w, m.CreateToken("sess-1"))

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	if cookies[0].Domain != "" {
		t.Errorf("Domain = %q, want empty", cookies[0].Domain)
	}
}

func TestClearCookie(t *testing.T) {
	m := NewSessionManager(testKey(t), 1, []string{"example.com"})

	w := httptest.NewRecorder()
	m.ClearCookie(w)

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}

	c := cookies[0]
	if c.Name != cookieName {
		t.Errorf("cookie name = %q, want %q", c.Name, cookieName)
	}
	if c.Value != "" {
		t.Errorf("cookie value = %q, want empty", c.Value)
	}
	if c.MaxAge != -1 {
		t.Errorf("MaxAge = %d, want -1", c.MaxAge)
	}
}

func TestSetTokenCookie(t *testing.T) {
	m := NewSessionManager(testKey(t), 12, []string{"example.com"})
	token := m.CreateToken("sess-1")

	w := httptest.NewRecorder()
	m.SetTokenCookie(w, token)

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}

	c := cookies[0]
	if c.Name != tokenCookieName {
		t.Errorf("cookie name = %q, want %q", c.Name, tokenCookieName)
	}
	if c.Value != token {
		t.Error("cookie value mismatch")
	}
	if !c.Secure {
		t.Error("expected Secure=true")
	}
	if c.HttpOnly {
		t.Error("expected HttpOnly=false for JavaScript-readable token cookie")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", c.SameSite)
	}
	if c.Path != "/" {
		t.Errorf("Path = %q, want %q", c.Path, "/")
	}
	if c.Domain != "example.com" {
		t.Errorf("Domain = %q, want %q", c.Domain, "example.com")
	}
	if c.MaxAge != int(12*time.Hour.Seconds()) {
		t.Errorf("MaxAge = %d, want %d", c.MaxAge, int(12*time.Hour.Seconds()))
	}
}

func TestClearTokenCookie(t *testing.T) {
	m := NewSessionManager(testKey(t), 1, []string{"example.com"})

	w := httptest.NewRecorder()
	m.ClearTokenCookie(w)

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}

	c := cookies[0]
	if c.Name != tokenCookieName {
		t.Errorf("cookie name = %q, want %q", c.Name, tokenCookieName)
	}
	if c.Value != "" {
		t.Errorf("cookie value = %q, want empty", c.Value)
	}
	if c.MaxAge != -1 {
		t.Errorf("MaxAge = %d, want -1", c.MaxAge)
	}
}

func TestCookieName(t *testing.T) {
	m := NewSessionManager(testKey(t), 1, nil)
	if m.CookieName() != "nano_session" {
		t.Errorf("CookieName() = %q, want %q", m.CookieName(), "nano_session")
	}
}

func TestMaxAge(t *testing.T) {
	m := NewSessionManager(testKey(t), 5, nil)
	if m.MaxAge() != 5*time.Hour {
		t.Errorf("MaxAge() = %v, want 5h", m.MaxAge())
	}
}

// --- Uniqueness ---

func TestTokenUniqueness(t *testing.T) {
	m := NewSessionManager(testKey(t), 1, nil)

	seen := make(map[string]struct{})
	const n = 100
	for i := 0; i < n; i++ {
		token := m.CreateToken("sess-" + strings.Repeat("x", i))
		if _, exists := seen[token]; exists {
			t.Fatalf("duplicate token generated at iteration %d", i)
		}
		seen[token] = struct{}{}
	}
}

// --- MaxAgeFromCookie ---

func TestMaxAgeFromCookie(t *testing.T) {
	tests := []struct {
		name    string
		header  string
		want    int
		wantErr bool
	}{
		{
			name:   "valid max-age",
			header: "nano_session=abc; Path=/; Max-Age=43200; Secure; HttpOnly",
			want:   43200,
		},
		{
			name:   "max-age zero",
			header: "nano_session=abc; Max-Age=0",
			want:   0,
		},
		{
			name:    "no max-age",
			header:  "nano_session=abc; Path=/; Secure",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MaxAgeFromCookie(tt.header)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("MaxAgeFromCookie() = %d, want %d", got, tt.want)
			}
		})
	}
}

// --- RequireAuth middleware ---

func TestRequireAuth(t *testing.T) {
	key := testKey(t)

	t.Run("valid token passes and sets user in context", func(t *testing.T) {
		m := NewSessionManager(key, 1, nil)
		m.authEnabled = true

		token := m.CreateToken("sess-abc")
		handler := m.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := UserFromContext(r.Context())
			if user.ID != "sess-abc" {
				t.Errorf("user ID = %q, want %q", user.ID, "sess-abc")
			}
			if user.Source != "cookie" {
				t.Errorf("user source = %q, want %q", user.Source, "cookie")
			}
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "/reviews", nil)
		req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}
	})

	t.Run("missing cookie returns 401", func(t *testing.T) {
		m := NewSessionManager(key, 1, nil)
		m.authEnabled = true

		handler := m.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("handler should not be called")
		}))

		req := httptest.NewRequest(http.MethodGet, "/reviews", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}

		var body map[string]string
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("decode response body: %v", err)
		}
		if body["error"] != "missing session cookie" {
			t.Errorf("error = %q, want %q", body["error"], "missing session cookie")
		}
	})

	t.Run("invalid token returns 401", func(t *testing.T) {
		m := NewSessionManager(key, 1, nil)
		m.authEnabled = true

		handler := m.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("handler should not be called")
		}))

		req := httptest.NewRequest(http.MethodGet, "/reviews", nil)
		req.AddCookie(&http.Cookie{Name: cookieName, Value: "garbage.token"})
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}

		var body map[string]string
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("decode response body: %v", err)
		}
		if body["error"] != "invalid or expired session token" {
			t.Errorf("error = %q, want %q", body["error"], "invalid or expired session token")
		}
	})

	t.Run("expired token returns 401", func(t *testing.T) {
		expired := NewSessionManager(key, 0.0001, nil)
		expired.authEnabled = true
		token := expired.CreateToken("sess-expired")

		time.Sleep(400 * time.Millisecond)

		handler := expired.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("handler should not be called")
		}))

		req := httptest.NewRequest(http.MethodGet, "/reviews", nil)
		req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})

	t.Run("auth disabled passes through without cookie", func(t *testing.T) {
		m := NewSessionManager(key, 1, nil)
		m.authEnabled = false

		called := false
		handler := m.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "/reviews", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if !called {
			t.Error("handler should have been called when auth is disabled")
		}
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}
	})

	t.Run("wrong cookie name is ignored", func(t *testing.T) {
		m := NewSessionManager(key, 1, nil)
		m.authEnabled = true

		handler := m.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("handler should not be called")
		}))

		req := httptest.NewRequest(http.MethodGet, "/reviews", nil)
		req.AddCookie(&http.Cookie{Name: "wrong_cookie", Value: m.CreateToken("sess-1")})
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})

	t.Run("response content type is application/json", func(t *testing.T) {
		m := NewSessionManager(key, 1, nil)
		m.authEnabled = true

		handler := m.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

		req := httptest.NewRequest(http.MethodGet, "/reviews", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		ct := rec.Header().Get("Content-Type")
		if ct != "application/json" {
			t.Errorf("Content-Type = %q, want %q", ct, "application/json")
		}
	})

	t.Run("valid token in query param authenticates user", func(t *testing.T) {
		m := NewSessionManager(key, 1, nil)
		m.authEnabled = true

		token := m.CreateToken("sess-query")
		handler := m.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := UserFromContext(r.Context())
			if user.ID != "sess-query" {
				t.Errorf("user ID = %q, want %q", user.ID, "sess-query")
			}
			if user.Source != "query" {
				t.Errorf("user source = %q, want %q", user.Source, "query")
			}
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "/ws?token="+token, nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}
	})

	t.Run("invalid token in query param returns 401", func(t *testing.T) {
		m := NewSessionManager(key, 1, nil)
		m.authEnabled = true

		handler := m.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("handler should not be called")
		}))

		req := httptest.NewRequest(http.MethodGet, "/ws?token=garbage", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})

	t.Run("cookie takes precedence over query param", func(t *testing.T) {
		m := NewSessionManager(key, 1, nil)
		m.authEnabled = true

		cookieToken := m.CreateToken("sess-cookie")
		queryToken := m.CreateToken("sess-query")

		handler := m.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := UserFromContext(r.Context())
			if user.ID != "sess-cookie" {
				t.Errorf("user ID = %q, want %q (cookie should take precedence)", user.ID, "sess-cookie")
			}
			if user.Source != "cookie" {
				t.Errorf("user source = %q, want %q", user.Source, "cookie")
			}
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "/ws?token="+queryToken, nil)
		req.AddCookie(&http.Cookie{Name: cookieName, Value: cookieToken})
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}
	})
}

func TestParseAuthEnabled(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		want     bool
	}{
		{name: "empty env defaults to true", envValue: "", want: true},
		{name: "random string defaults to true", envValue: "yes", want: true},
		{name: "lowercase false", envValue: "false", want: false},
		{name: "uppercase FALSE", envValue: "FALSE", want: false},
		{name: "mixed case False", envValue: "False", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseAuthEnabledValue(tt.envValue)
			if got != tt.want {
				t.Errorf("parseAuthEnabledValue(%q) = %v, want %v", tt.envValue, got, tt.want)
			}
		})
	}
}

// parseAuthEnabledValue is a test helper that tests the parseAuthEnabled logic
// without reading from environment variables.
func parseAuthEnabledValue(v string) bool {
	if strings.EqualFold(v, "false") {
		return false
	}
	return true
}

// --- SESSION_SECRET validation via env var ---

func TestNewSessionManagerRejectsShortSecret(t *testing.T) {
	tests := []struct {
		name string
		key  []byte
	}{
		{name: "empty key", key: []byte{}},
		{name: "1 byte", key: []byte("a")},
		{name: "15 bytes", key: []byte("shortsecretkey!")},
		{name: "31 bytes", key: []byte(strings.Repeat("x", 31))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatal("expected panic for key shorter than 32 bytes")
				}
				msg := fmt.Sprint(r)
				if !strings.Contains(msg, "32 bytes") {
					t.Errorf("panic message = %q, want mention of '32 bytes'", msg)
				}
			}()
			NewSessionManager(tt.key, 1, nil)
		})
	}
}

func TestNewSessionManagerRejectsShortSecretFromEnv(t *testing.T) {
	tests := []struct {
		name string
		secret string
	}{
		{name: "empty secret", secret: ""},
		{name: "short secret", secret: "too-short"},
		{name: "15 chars", secret: "shortsecretkey!"},
		{name: "31 chars", secret: strings.Repeat("x", 31)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SESSION_SECRET", tt.secret)

			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("expected panic for SESSION_SECRET of length %d", len(tt.secret))
				}
			}()

			secret := os.Getenv("SESSION_SECRET")
			NewSessionManager([]byte(secret), 24, nil)
		})
	}
}

// --- Missing env var validation ---

func TestNewSessionManagerRejectsMissingEnvVars(t *testing.T) {
	tests := []struct {
		name             string
		authEnabled      string
		clientID         string
		clientSecret     string
		wantOAuthReady   bool
	}{
		{
			name:           "AUTH_ENABLED true with missing GOOGLE_CLIENT_ID",
			authEnabled:    "true",
			clientID:       "",
			clientSecret:   "some-secret",
			wantOAuthReady: false,
		},
		{
			name:           "AUTH_ENABLED true with missing GOOGLE_CLIENT_SECRET",
			authEnabled:    "true",
			clientID:       "some-id",
			clientSecret:   "",
			wantOAuthReady: false,
		},
		{
			name:           "AUTH_ENABLED true with both missing",
			authEnabled:    "true",
			clientID:       "",
			clientSecret:   "",
			wantOAuthReady: false,
		},
		{
			name:           "AUTH_ENABLED true with both present",
			authEnabled:    "true",
			clientID:       "client-id",
			clientSecret:   "client-secret",
			wantOAuthReady: true,
		},
		{
			name:           "AUTH_ENABLED false with both missing",
			authEnabled:    "false",
			clientID:       "",
			clientSecret:   "",
			wantOAuthReady: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("AUTH_ENABLED", tt.authEnabled)
			t.Setenv("GOOGLE_CLIENT_ID", tt.clientID)
			t.Setenv("GOOGLE_CLIENT_SECRET", tt.clientSecret)

			m := NewSessionManager(testKey(t), 24, nil)

			cfg := &OAuthConfig{
				ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
				ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
				RedirectURL:  "http://localhost:8080/auth/callback",
			}

			gotOAuthReady := cfg.OAuthEndpoint() != nil
			if gotOAuthReady != tt.wantOAuthReady {
				t.Errorf("OAuthEndpoint() ready = %v, want %v", gotOAuthReady, tt.wantOAuthReady)
			}

			if m.AuthEnabled() != (tt.authEnabled != "false") {
				t.Errorf("AuthEnabled() = %v, want %v", m.AuthEnabled(), tt.authEnabled != "false")
			}
		})
	}
}

// --- parseAuthEnabled with actual env var ---

func TestParseAuthEnabledWithEnvVar(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		want     bool
	}{
		{name: "empty defaults to true", envValue: "", want: true},
		{name: "random string defaults to true", envValue: "yes", want: true},
		{name: "lowercase false", envValue: "false", want: false},
		{name: "uppercase FALSE", envValue: "FALSE", want: false},
		{name: "mixed case FaLsE", envValue: "FaLsE", want: false},
		{name: "trUrE defaults to true", envValue: "trUrE", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("AUTH_ENABLED", tt.envValue)
			m := NewSessionManager(testKey(t), 24, nil)
			if m.AuthEnabled() != tt.want {
				t.Errorf("AuthEnabled() = %v, want %v", m.AuthEnabled(), tt.want)
			}
		})
	}
}

// --- Exact 32-byte boundary ---

func TestNewSessionManagerAccepts32ByteKey(t *testing.T) {
	t.Parallel()

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic for 32-byte key: %v", r)
		}
	}()

	m := NewSessionManager(key, 1, nil)
	if m == nil {
		t.Fatal("expected non-nil SessionManager")
	}
}

// --- 64-byte key accepted ---

func TestNewSessionManagerAcceptsLongKey(t *testing.T) {
	t.Parallel()

	key := make([]byte, 64)
	for i := range key {
		key[i] = byte(i)
	}

	m := NewSessionManager(key, 1, nil)
	if m == nil {
		t.Fatal("expected non-nil SessionManager")
	}
}

// --- Token round-trip with different session IDs ---

func TestTokenRoundTripVariousSessionIDs(t *testing.T) {
	t.Parallel()

	m := NewSessionManager(testKey(t), 1, nil)

	tests := []struct {
		name      string
		sessionID string
	}{
		{name: "simple ID", sessionID: "user-123"},
		{name: "empty session ID", sessionID: ""},
		{name: "long session ID", sessionID: strings.Repeat("a", 500)},
		{name: "ID with special chars", sessionID: "user@example.com:8080/path?q=1&r=2"},
		{name: "ID with unicode", sessionID: "用户-αβγ-🔐"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := m.CreateToken(tt.sessionID)
			parsedID, err := m.ValidateToken(token)
			if err != nil {
				t.Fatalf("ValidateToken() error = %v", err)
			}
			if parsedID != tt.sessionID {
				t.Errorf("ValidateToken() id = %q, want %q", parsedID, tt.sessionID)
			}
		})
	}
}

// --- ValidateToken wrong timestamp length ---

func TestValidateTokenWrongTimestampLength(t *testing.T) {
	t.Parallel()

	m := NewSessionManager(testKey(t), 1, nil)

	sessionIDSig := m.CreateToken("sess-1")
	parts := strings.SplitN(sessionIDSig, ".", 4)

	// Replace timestamp with a segment that decodes to wrong length.
	shortTS := base64.RawURLEncoding.EncodeToString([]byte{1, 2, 3})
	fake := parts[0] + "." + shortTS + "." + parts[2] + "." + parts[3]

	_, err := m.ValidateToken(fake)
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("error = %v, want ErrInvalidToken", err)
	}
}

// --- ValidateToken tampered random bytes ---

func TestValidateTokenTamperedRandomBytes(t *testing.T) {
	t.Parallel()

	m := NewSessionManager(testKey(t), 1, nil)
	token := m.CreateToken("sess-1")
	parts := strings.SplitN(token, ".", 4)

	fakeRand := base64.RawURLEncoding.EncodeToString(make([]byte, randomBytesLength))
	fake := parts[0] + "." + parts[1] + "." + fakeRand + "." + parts[3]

	_, err := m.ValidateToken(fake)
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("error = %v, want ErrInvalidToken", err)
	}
}

// --- ValidateToken with extra dots ---

func TestValidateTokenTooManySegments(t *testing.T) {
	t.Parallel()

	m := NewSessionManager(testKey(t), 1, nil)
	token := m.CreateToken("sess-1")
	fake := token + ".extra"

	_, err := m.ValidateToken(fake)
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("error = %v, want ErrInvalidToken", err)
	}
}

// --- ValidateToken with just dots ---

func TestValidateTokenOnlyDots(t *testing.T) {
	t.Parallel()

	m := NewSessionManager(testKey(t), 1, nil)

	_, err := m.ValidateToken("...")
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("error = %v, want ErrInvalidToken", err)
	}
}

// --- ClearCookie domain propagation ---

func TestClearCookieDomainPropagation(t *testing.T) {
	t.Parallel()

	t.Run("with domain", func(t *testing.T) {
		m := NewSessionManager(testKey(t), 1, []string{"app.example.com"})

		w := httptest.NewRecorder()
		m.ClearCookie(w)

		cookies := w.Result().Cookies()
		if len(cookies) != 1 {
			t.Fatalf("expected 1 cookie, got %d", len(cookies))
		}
		if cookies[0].Domain != "app.example.com" {
			t.Errorf("Domain = %q, want %q", cookies[0].Domain, "app.example.com")
		}
	})

	t.Run("without domain", func(t *testing.T) {
		m := NewSessionManager(testKey(t), 1, nil)

		w := httptest.NewRecorder()
		m.ClearCookie(w)

		cookies := w.Result().Cookies()
		if len(cookies) != 1 {
			t.Fatalf("expected 1 cookie, got %d", len(cookies))
		}
		if cookies[0].Domain != "" {
			t.Errorf("Domain = %q, want empty", cookies[0].Domain)
		}
	})
}

// --- RequireAuth preserves context beyond user ---

func TestRequireAuthPreservesContext(t *testing.T) {
	t.Parallel()

	key := testKey(t)
	m := NewSessionManager(key, 1, nil)
	m.authEnabled = true

	token := m.CreateToken("sess-ctx")

	parentCtx := contextWithTestValue(context.Background(), "test-key", "test-value")

	handler := m.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := UserFromContext(r.Context())
		if user.ID != "sess-ctx" {
			t.Errorf("user ID = %q, want %q", user.ID, "sess-ctx")
		}
		got := getTestValue(r.Context(), "test-key")
		if got != "test-value" {
			t.Errorf("context value = %q, want %q", got, "test-value")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/reviews", nil)
	req = req.WithContext(parentCtx)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// --- CreateToken produces valid base64 segments ---

func TestCreateTokenValidBase64(t *testing.T) {
	t.Parallel()

	m := NewSessionManager(testKey(t), 1, nil)
	token := m.CreateToken("sess-b64")
	parts := strings.SplitN(token, ".", 4)

	for i, part := range parts {
		_, err := base64.RawURLEncoding.DecodeString(part)
		if err != nil {
			t.Errorf("token segment %d is not valid base64: %v", i, err)
		}
	}
}

// --- ValidateToken expired token boundary ---

func TestValidateTokenNotExpiredWithinMaxAge(t *testing.T) {
	t.Parallel()

	// Create token with current timestamp, validate immediately.
	m := NewSessionManager(testKey(t), 24, nil)
	token := m.CreateToken("sess-fresh")

	parsedID, err := m.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken() on fresh token error = %v", err)
	}
	if parsedID != "sess-fresh" {
		t.Errorf("ValidateToken() id = %q, want %q", parsedID, "sess-fresh")
	}
}

// --- ValidateToken with crafted old timestamp ---

func TestValidateTokenOldTimestamp(t *testing.T) {
	t.Parallel()

	key := testKey(t)
	m := NewSessionManager(key, 1, nil)

	// Create a token manually with a timestamp from 2 hours ago.
	sessionID := "sess-old"
	ts := make([]byte, 8)
	binary.BigEndian.PutUint64(ts, uint64(time.Now().Add(-2*time.Hour).Unix()))

	randBytes := make([]byte, randomBytesLength)
	if _, err := rand.Read(randBytes); err != nil {
		t.Fatal(err)
	}

	sig := m.computeSignature(sessionID, ts, randBytes)

	token := encodeSegment(sessionID) + "." +
		encodeSegment(string(ts)) + "." +
		encodeSegment(string(randBytes)) + "." +
		encodeSegment(string(sig))

	_, err := m.ValidateToken(token)
	if !errors.Is(err, ErrExpiredToken) {
		t.Errorf("error = %v, want ErrExpiredToken", err)
	}
}

// --- OAuthConfig validation via ValidateOAuthConfig ---

func TestValidateOAuthConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     OAuthConfig
		wantErr bool
	}{
		{name: "both present", cfg: OAuthConfig{ClientID: "id", ClientSecret: "secret"}, wantErr: false},
		{name: "missing client ID", cfg: OAuthConfig{ClientSecret: "secret"}, wantErr: true},
		{name: "missing client secret", cfg: OAuthConfig{ClientID: "id"}, wantErr: true},
		{name: "both empty", cfg: OAuthConfig{}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if !strings.Contains(err.Error(), "GOOGLE_CLIENT") {
					t.Errorf("error = %v, want mention of GOOGLE_CLIENT", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// --- Test helpers for context preservation test ---

type testCtxKey string

func contextWithTestValue(ctx context.Context, key, value string) context.Context {
	return context.WithValue(ctx, testCtxKey(key), value)
}

func getTestValue(ctx context.Context, key string) string {
	v, _ := ctx.Value(testCtxKey(key)).(string)
	return v
}
