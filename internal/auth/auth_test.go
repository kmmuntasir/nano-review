package auth

import (
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
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
