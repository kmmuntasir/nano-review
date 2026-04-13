package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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

	req := httptest.NewRequest(http.MethodGet, "/auth/login?state=/dashboard", nil)
	w := httptest.NewRecorder()

	HandleGoogleLogin(cfg)(w, req)

	if w.Code != http.StatusTemporaryRedirect {
		t.Errorf("expected status 307, got %d", w.Code)
	}

	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "state=") {
		t.Fatalf("expected state parameter in redirect URL, got %s", loc)
	}

	// Verify nano_oauth_state cookie is set with the combined state value.
	cookies := w.Result().Cookies()
	var stateCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == oauthStateCookieName {
			stateCookie = c
			break
		}
	}
	if stateCookie == nil {
		t.Fatal("expected nano_oauth_state cookie to be set")
	}
	if !strings.Contains(stateCookie.Value, ":/dashboard") {
		t.Errorf("expected cookie to contain ':/dashboard', got %s", stateCookie.Value)
	}

	// Verify the URL-decoded state in the redirect matches the cookie value.
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("failed to parse redirect URL: %v", err)
	}
	stateFromURL := u.Query().Get("state")
	if stateFromURL != stateCookie.Value {
		t.Errorf("URL state %q should match cookie value %q", stateFromURL, stateCookie.Value)
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

func TestIsEmailAllowed(t *testing.T) {
	tests := []struct {
		name            string
		email           string
		allowedDomains  []string
		want            bool
	}{
		{
			name:           "empty allowed list allows all",
			email:          "user@anywhere.com",
			allowedDomains: nil,
			want:           true,
		},
		{
			name:           "matching domain",
			email:          "user@example.com",
			allowedDomains: []string{"example.com"},
			want:           true,
		},
		{
			name:           "non-matching domain",
			email:          "user@other.org",
			allowedDomains: []string{"example.com"},
			want:           false,
		},
		{
			name:           "case insensitive match",
			email:          "user@Example.COM",
			allowedDomains: []string{"example.com"},
			want:           true,
		},
		{
			name:           "case insensitive allowed domain",
			email:          "user@example.com",
			allowedDomains: []string{"Example.Com"},
			want:           true,
		},
		{
			name:           "multiple allowed domains",
			email:          "user@other.org",
			allowedDomains: []string{"example.com", "other.org"},
			want:           true,
		},
		{
			name:           "email without @ sign",
			email:          "invalid-email",
			allowedDomains: []string{"example.com"},
			want:           false,
		},
		{
			name:           "empty email",
			email:          "",
			allowedDomains: []string{"example.com"},
			want:           false,
		},
		{
			name:           "domain with @ prefix in allowed list",
			email:          "user@example.com",
			allowedDomains: []string{"@example.com"},
			want:           true,
		},
		{
			name:           "subdomain does not match parent",
			email:          "user@sub.example.com",
			allowedDomains: []string{"example.com"},
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isEmailAllowed(tt.email, tt.allowedDomains)
			if got != tt.want {
				t.Errorf("isEmailAllowed(%q, %v) = %v, want %v", tt.email, tt.allowedDomains, got, tt.want)
			}
		})
	}
}

// --- OAuth State CSRF protection ---

func TestOAuthStateCSRF(t *testing.T) {
	t.Run("state cookie is set on login", func(t *testing.T) {
		cfg := &OAuthConfig{
			ClientID:     "test-client-id",
			ClientSecret: "test-client-secret",
			RedirectURL:  "http://localhost:8080/auth/callback",
			SessionManager: NewSessionManager([]byte(strings.Repeat("x", 32)), 24, nil),
		}

		req := httptest.NewRequest(http.MethodGet, "/auth/login?state=/reviews", nil)
		w := httptest.NewRecorder()

		HandleGoogleLogin(cfg)(w, req)

		var stateCookie *http.Cookie
		for _, c := range w.Result().Cookies() {
			if c.Name == oauthStateCookieName {
				stateCookie = c
				break
			}
		}
		if stateCookie == nil {
			t.Fatal("expected nano_oauth_state cookie")
		}
		if !stateCookie.HttpOnly {
			t.Error("expected HttpOnly=true")
		}
		if stateCookie.SameSite != http.SameSiteLaxMode {
			t.Errorf("expected SameSite=Lax, got %v", stateCookie.SameSite)
		}
		if stateCookie.MaxAge != 300 {
			t.Errorf("expected MaxAge=300, got %d", stateCookie.MaxAge)
		}
		if !strings.HasSuffix(stateCookie.Value, ":/reviews") {
			t.Errorf("expected cookie value to end with ':/reviews', got %s", stateCookie.Value)
		}
	})

	t.Run("state cookie uses Secure flag from SessionManager", func(t *testing.T) {
		cfg := &OAuthConfig{
			ClientID:     "test-client-id",
			ClientSecret: "test-client-secret",
			RedirectURL:  "http://localhost:8080/auth/callback",
			SessionManager: NewSessionManager([]byte(strings.Repeat("x", 32)), 24, nil),
		}

		req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
		w := httptest.NewRecorder()

		HandleGoogleLogin(cfg)(w, req)

		var stateCookie *http.Cookie
		for _, c := range w.Result().Cookies() {
			if c.Name == oauthStateCookieName {
				stateCookie = c
				break
			}
		}
		if stateCookie == nil {
			t.Fatal("expected nano_oauth_state cookie")
		}
		if stateCookie.Secure != cfg.SessionManager.Secure() {
			t.Errorf("expected Secure=%v (from SessionManager), got %v", cfg.SessionManager.Secure(), stateCookie.Secure)
		}
	})

	t.Run("callback rejects missing state param", func(t *testing.T) {
		cfg := &OAuthConfig{
			ClientID:     "test-id",
			ClientSecret: "test-secret",
			RedirectURL:  "http://localhost:8080/auth/callback",
			SessionManager: NewSessionManager([]byte(strings.Repeat("x", 32)), 24, nil),
		}

		req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=auth-code", nil)
		w := httptest.NewRecorder()

		HandleOAuthCallback(cfg)(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "missing state parameter") {
			t.Errorf("expected 'missing state parameter' error, got %s", w.Body.String())
		}
	})

	t.Run("callback rejects missing state cookie", func(t *testing.T) {
		cfg := &OAuthConfig{
			ClientID:     "test-id",
			ClientSecret: "test-secret",
			RedirectURL:  "http://localhost:8080/auth/callback",
			SessionManager: NewSessionManager([]byte(strings.Repeat("x", 32)), 24, nil),
		}

		req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=auth-code&state=some-token", nil)
		w := httptest.NewRecorder()

		HandleOAuthCallback(cfg)(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "missing state cookie") {
			t.Errorf("expected 'missing state cookie' error, got %s", w.Body.String())
		}
	})

	t.Run("callback rejects mismatched state", func(t *testing.T) {
		cfg := &OAuthConfig{
			ClientID:     "test-id",
			ClientSecret: "test-secret",
			RedirectURL:  "http://localhost:8080/auth/callback",
			SessionManager: NewSessionManager([]byte(strings.Repeat("x", 32)), 24, nil),
		}

		req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=auth-code&state=attacker-csrf:/dashboard", nil)
		req.AddCookie(&http.Cookie{Name: oauthStateCookieName, Value: "legitimate-csrf:/dashboard"})
		w := httptest.NewRecorder()

		HandleOAuthCallback(cfg)(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "invalid state parameter") {
			t.Errorf("expected 'invalid state parameter' error, got %s", w.Body.String())
		}
	})

	t.Run("callback clears state cookie after verification", func(t *testing.T) {
		cfg := &OAuthConfig{
			ClientID:     "test-id",
			ClientSecret: "test-secret",
			RedirectURL:  "http://localhost:8080/auth/callback",
			SessionManager: NewSessionManager([]byte(strings.Repeat("x", 32)), 24, nil),
		}

		// Valid state with matching CSRF token — will proceed past state verification
		// and fail later on token exchange (which is expected; we just want to check
		// that the cookie is cleared before the token exchange attempt).
		req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=auth-code&state=valid-token:/reviews", nil)
		req.AddCookie(&http.Cookie{Name: oauthStateCookieName, Value: "valid-token:/reviews"})
		w := httptest.NewRecorder()

		HandleOAuthCallback(cfg)(w, req)

		// The response should fail (bad gateway from token exchange), not from state verification.
		if w.Code == http.StatusBadRequest {
			// If it's a 400, it should NOT be a state-related error.
			if strings.Contains(w.Body.String(), "state") {
				t.Errorf("state verification should have passed, got error: %s", w.Body.String())
			}
		}

		// Verify the state cookie was cleared.
		cookies := w.Result().Cookies()
		for _, c := range cookies {
			if c.Name == oauthStateCookieName {
				if c.MaxAge != -1 {
					t.Errorf("expected cleared cookie MaxAge=-1, got %d", c.MaxAge)
				}
				if c.Value != "" {
					t.Errorf("expected cleared cookie value to be empty, got %q", c.Value)
				}
				return
			}
		}
		t.Error("expected nano_oauth_state cookie to be cleared")
	})
}

func newTestSessionManager(t *testing.T) *SessionManager {
	t.Helper()
	return NewSessionManager([]byte(strings.Repeat("x", 32)), 24, nil)
}

func TestHandleSessionInfoPublic(t *testing.T) {
	sm := newTestSessionManager(t)
	sm.authEnabled = true
	handler := HandleSessionInfo(sm)

	t.Run("returns empty object without cookie", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
		w := httptest.NewRecorder()

		handler(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}
		ct := w.Header().Get("Content-Type")
		if ct != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", ct)
		}

		var body map[string]interface{}
		if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if len(body) != 0 {
			t.Errorf("expected empty object, got %v", body)
		}
	})

	t.Run("returns user info with valid cookie", func(t *testing.T) {
		token := sm.CreateToken("user-123", TokenUserInfo{
			Email:   "test@example.com",
			Name:    "Test User",
			Picture: "https://example.com/pic.jpg",
		})

		req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
		req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
		w := httptest.NewRecorder()

		handler(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}

		var body map[string]string
		if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["id"] != "user-123" {
			t.Errorf("expected id=user-123, got %s", body["id"])
		}
		if body["email"] != "test@example.com" {
			t.Errorf("expected email=test@example.com, got %s", body["email"])
		}
		if body["name"] != "Test User" {
			t.Errorf("expected name=Test User, got %s", body["name"])
		}
		if body["picture"] != "https://example.com/pic.jpg" {
			t.Errorf("expected picture URL, got %s", body["picture"])
		}
	})

	t.Run("returns empty object with invalid token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
		req.AddCookie(&http.Cookie{Name: cookieName, Value: "invalid.token.value"})
		w := httptest.NewRecorder()

		handler(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}

		var body map[string]interface{}
		if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if len(body) != 0 {
			t.Errorf("expected empty object for invalid token, got %v", body)
		}
	})

	t.Run("method not allowed for POST", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/auth/me", nil)
		w := httptest.NewRecorder()

		handler(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected status 405, got %d", w.Code)
		}
	})
}

func TestHandleLogout(t *testing.T) {
	sm := newTestSessionManager(t)

	req := httptest.NewRequest(http.MethodGet, "/auth/logout", nil)
	w := httptest.NewRecorder()

	HandleLogout(sm)(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("expected status 302, got %d", w.Code)
	}

	loc := w.Header().Get("Location")
	if loc != "/" {
		t.Errorf("expected redirect to %q, got %q", "/", loc)
	}

	// Verify both cookies are cleared.
	cookies := w.Result().Cookies()
	found := make(map[string]bool)
	for _, c := range cookies {
		if c.Name == cookieName || c.Name == tokenCookieName {
			if c.MaxAge != -1 {
				t.Errorf("cookie %q: expected MaxAge=-1, got %d", c.Name, c.MaxAge)
			}
			if c.Value != "" {
				t.Errorf("cookie %q: expected empty value, got %q", c.Name, c.Value)
			}
			found[c.Name] = true
		}
	}
	if !found[cookieName] {
		t.Error("expected nano_session cookie to be cleared")
	}
	if !found[tokenCookieName] {
		t.Error("expected nano_session_token cookie to be cleared")
	}
}

func TestHandleSessionInfoWhenAuthDisabled(t *testing.T) {
	sm := newTestSessionManager(t)
	sm.authEnabled = false
	handler := HandleSessionInfo(sm)

	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	v, ok := body["auth_enabled"]
	if !ok {
		t.Fatal("expected auth_enabled field in response")
	}
	if v != false {
		t.Errorf("expected auth_enabled=false, got %v", v)
	}
	// Should not contain user fields
	for _, field := range []string{"id", "email", "name", "picture"} {
		if _, exists := body[field]; exists {
			t.Errorf("unexpected field %q in auth-disabled response", field)
		}
	}
}
