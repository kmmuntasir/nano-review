package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleGoogleLogin_MethodNotAllowed(t *testing.T) {
	cfg := &OAuthConfig{
		ClientID:     "test-id",
		ClientSecret: "test-secret",
		RedirectURL:  "http://localhost:8080/auth/callback",
		SessionManager: NewSessionManager([]byte(strings.Repeat("x", 32)), 24, nil),
	}

	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	w := httptest.NewRecorder()

	HandleGoogleLogin(cfg)(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}
}

func TestHandleGoogleLogin_OAuthNotConfigured(t *testing.T) {
	cfg := &OAuthConfig{
		SessionManager: NewSessionManager([]byte(strings.Repeat("x", 32)), 24, nil),
	}

	req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	w := httptest.NewRecorder()

	HandleGoogleLogin(cfg)(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Errorf("expected status 501, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "OAuth is not configured") {
		t.Errorf("expected error message about OAuth not configured, got %s", body)
	}
}

func TestHandleGoogleLogin_RedirectsToGoogle(t *testing.T) {
	cfg := &OAuthConfig{
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		RedirectURL:  "http://localhost:8080/auth/callback",
		SessionManager: NewSessionManager([]byte(strings.Repeat("x", 32)), 24, nil),
	}

	req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	w := httptest.NewRecorder()

	HandleGoogleLogin(cfg)(w, req)

	if w.Code != http.StatusTemporaryRedirect {
		t.Errorf("expected status 307, got %d", w.Code)
	}

	loc := w.Header().Get("Location")
	if loc == "" {
		t.Fatal("expected Location header to be set")
	}
	if !strings.HasPrefix(loc, "https://accounts.google.com/o/oauth2/auth") {
		t.Errorf("expected redirect to Google OAuth, got %s", loc)
	}
	if !strings.Contains(loc, "client_id=test-client-id") {
		t.Errorf("expected client_id in redirect URL, got %s", loc)
	}
	if !strings.Contains(loc, "redirect_uri=http") {
		t.Errorf("expected redirect_uri in URL, got %s", loc)
	}
	if !strings.Contains(loc, "access_type=offline") {
		t.Errorf("expected access_type=offline in URL, got %s", loc)
	}
}

func TestHandleGoogleLogin_WithState(t *testing.T) {
	cfg := &OAuthConfig{
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		RedirectURL:  "http://localhost:8080/auth/callback",
		SessionManager: NewSessionManager([]byte(strings.Repeat("x", 32)), 24, nil),
	}

	req := httptest.NewRequest(http.MethodGet, "/auth/login?state=csrf-token-123", nil)
	w := httptest.NewRecorder()

	HandleGoogleLogin(cfg)(w, req)

	if w.Code != http.StatusTemporaryRedirect {
		t.Errorf("expected status 307, got %d", w.Code)
	}

	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "state=csrf-token-123") {
		t.Errorf("expected state=csrf-token-123 in redirect URL, got %s", loc)
	}
}

func TestOAuthEndpoint_NilWhenUnconfigured(t *testing.T) {
	tests := []struct {
		name  string
		cfg   OAuthConfig
		want  bool
	}{
		{
			name:  "both empty",
			cfg:   OAuthConfig{},
			want:  false,
		},
		{
			name:  "only client ID",
			cfg:   OAuthConfig{ClientID: "id"},
			want:  false,
		},
		{
			name:  "only client secret",
			cfg:   OAuthConfig{ClientSecret: "secret"},
			want:  false,
		},
		{
			name:  "both set",
			cfg:   OAuthConfig{ClientID: "id", ClientSecret: "secret", RedirectURL: "http://localhost/cb"},
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.OAuthEndpoint() != nil
			if got != tt.want {
				t.Errorf("OAuthEndpoint() nil = %v, want nil = %v", !got, !tt.want)
			}
		})
	}
}
