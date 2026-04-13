package auth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
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

// --- Test Infrastructure (shared helpers for Track A tests) ---

// testOAuthConfig returns a ready-to-use *OAuthConfig for tests.
// The config has ClientID, ClientSecret, RedirectURL set, and a non-nil
// SessionManager. Override individual fields as needed.
func testOAuthConfig() *OAuthConfig {
	return &OAuthConfig{
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		RedirectURL:  "http://localhost:8080/auth/callback",
		SessionManager: NewSessionManager([]byte(strings.Repeat("x", 32)), 24, nil),
	}
}

// validStateToken generates a valid base64url-encoded 32-byte CSRF token,
// matching the format produced by HandleGoogleLogin.
func validStateToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("validStateToken: crypto/rand.Read failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// validUserInfo returns a struct matching the Google userinfo endpoint
// JSON response shape decoded in HandleOAuthCallback.
type validUserInfo struct {
	ID      string `json:"id"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
}

// defaultTestUserInfo returns a validUserInfo populated with sensible defaults.
func defaultTestUserInfo() validUserInfo {
	return validUserInfo{
		ID:      "google-12345",
		Email:   "test@example.com",
		Name:    "Test User",
		Picture: "https://example.com/pic.jpg",
	}
}

// mockRoundTripper implements http.RoundTripper, intercepting HTTP calls
// so tests can control responses for token exchange and userinfo endpoints
// without hitting real Google servers.
type mockRoundTripper struct {
	// TokenResponse is the JSON body returned for token exchange requests
	// (POST to Google's token endpoint).
	TokenResponse string

	// UserInfoResponse is the JSON body returned for userinfo GET requests.
	UserInfoResponse string

	// TokenStatus is the HTTP status code for token exchange (default 200).
	TokenStatus int

	// UserInfoStatus is the HTTP status code for userinfo requests (default 200).
	UserInfoStatus int

	// RequestLog records every request URL for assertion in tests.
	RequestLog []string
}

func newMockRoundTripper() *mockRoundTripper {
	return &mockRoundTripper{
		TokenResponse:    `{"access_token":"mock-access-token","token_type":"Bearer","expires_in":3600}`,
		UserInfoResponse: func() string {
			info := defaultTestUserInfo()
			b, _ := json.Marshal(info)
			return string(b)
		}(),
		TokenStatus:    http.StatusOK,
		UserInfoStatus: http.StatusOK,
	}
}

// RoundTrip routes requests to the appropriate mock response based on the URL path.
func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	m.RequestLog = append(m.RequestLog, req.URL.String())

	body := io.NopCloser(strings.NewReader(""))
	status := http.StatusOK

	switch {
	case req.URL.Path == "/token" || strings.Contains(req.URL.Host, "token"):
		body = io.NopCloser(strings.NewReader(m.TokenResponse))
		status = m.TokenStatus
	case strings.Contains(req.URL.Path, "userinfo"):
		body = io.NopCloser(strings.NewReader(m.UserInfoResponse))
		status = m.UserInfoStatus
	default:
		body = io.NopCloser(strings.NewReader("{}"))
	}

	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       body,
		Request:    req,
	}, nil
}

// mockHTTPClient creates an *http.Client backed by a new mockRoundTripper.
// Assign the returned client to cfg.HTTPClient before calling handlers.
func mockHTTPClient() *http.Client {
	return &http.Client{Transport: newMockRoundTripper()}
}

// mockHTTPClientWithConfig creates an *OAuthConfig with a mock HTTP client
// already wired in. The mock is also returned for customization of responses.
func mockHTTPClientWithConfig() (*OAuthConfig, *mockRoundTripper) {
	rt := newMockRoundTripper()
	cfg := testOAuthConfig()
	cfg.HTTPClient = &http.Client{Transport: rt}
	return cfg, rt
}

func newTestSessionManager(t *testing.T) *SessionManager {
	t.Helper()
	return NewSessionManager([]byte(strings.Repeat("x", 32)), 24, nil)
}

// --- Task A1: HandleGoogleLogin comprehensive tests ---

func TestHandleGoogleLoginSuccess(t *testing.T) {
	cfg := testOAuthConfig()

	req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	w := httptest.NewRecorder()

	HandleGoogleLogin(cfg)(w, req)

	// Must redirect with 307.
	if w.Code != http.StatusTemporaryRedirect {
		t.Fatalf("expected status 307, got %d", w.Code)
	}

	// Redirect URL must point to Google.
	loc := w.Header().Get("Location")
	if loc == "" {
		t.Fatal("expected Location header")
	}
	if !strings.HasPrefix(loc, "https://accounts.google.com/o/oauth2/auth") {
		t.Errorf("expected Google OAuth redirect, got %s", loc)
	}

	// State cookie must be set.
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

	// State value must be a base64url-encoded 32-byte CSRF token (no redirect path).
	// base64.RawURLEncoding of 32 bytes = 43 characters.
	if len(stateCookie.Value) != 43 {
		t.Errorf("expected CSRF token length 43 (base64 of 32 bytes), got %d: %q", len(stateCookie.Value), stateCookie.Value)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(stateCookie.Value)
	if err != nil {
		t.Fatalf("expected valid base64url-encoded CSRF token, got error: %v", err)
	}
	if len(decoded) != 32 {
		t.Errorf("expected 32 decoded bytes, got %d", len(decoded))
	}

	// The state parameter in the redirect URL must match the cookie value.
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("failed to parse redirect URL: %v", err)
	}
	stateFromURL := u.Query().Get("state")
	if stateFromURL != stateCookie.Value {
		t.Errorf("URL state %q != cookie value %q", stateFromURL, stateCookie.Value)
	}
}

func TestHandleGoogleLoginWithRedirectPath(t *testing.T) {
	cfg := testOAuthConfig()

	req := httptest.NewRequest(http.MethodGet, "/auth/login?state=/dashboard", nil)
	w := httptest.NewRecorder()

	HandleGoogleLogin(cfg)(w, req)

	if w.Code != http.StatusTemporaryRedirect {
		t.Fatalf("expected status 307, got %d", w.Code)
	}

	// Verify cookie contains the redirect path.
	cookies := w.Result().Cookies()
	var stateCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == oauthStateCookieName {
			stateCookie = c
			break
		}
	}
	if stateCookie == nil {
		t.Fatal("expected nano_oauth_state cookie")
	}

	// State format is csrfToken:redirectPath.
	parts := strings.SplitN(stateCookie.Value, ":", 2)
	if len(parts) != 2 {
		t.Fatalf("expected state in format 'token:path', got %q", stateCookie.Value)
	}
	if parts[1] != "/dashboard" {
		t.Errorf("expected redirect path '/dashboard', got %q", parts[1])
	}

	// CSRF token portion must be valid base64url of 32 bytes.
	decoded, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("expected valid base64url-encoded CSRF token, got error: %v", err)
	}
	if len(decoded) != 32 {
		t.Errorf("expected 32 decoded bytes in CSRF token, got %d", len(decoded))
	}

	// Verify URL state matches cookie.
	loc := w.Header().Get("Location")
	u, _ := url.Parse(loc)
	stateFromURL := u.Query().Get("state")
	if stateFromURL != stateCookie.Value {
		t.Errorf("URL state %q != cookie value %q", stateFromURL, stateCookie.Value)
	}
}

func TestHandleGoogleLoginSetsSecureCookieWhenHTTPS(t *testing.T) {
	cfg := testOAuthConfig()
	cfg.SessionManager.secure = true

	req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	w := httptest.NewRecorder()

	HandleGoogleLogin(cfg)(w, req)

	cookies := w.Result().Cookies()
	var stateCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == oauthStateCookieName {
			stateCookie = c
			break
		}
	}
	if stateCookie == nil {
		t.Fatal("expected nano_oauth_state cookie")
	}
	if !stateCookie.Secure {
		t.Error("expected Secure=true when SessionManager.Secure() is true")
	}
}

func TestHandleGoogleLoginSetsInsecureCookieWhenHTTP(t *testing.T) {
	cfg := testOAuthConfig()
	cfg.SessionManager.secure = false

	req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	w := httptest.NewRecorder()

	HandleGoogleLogin(cfg)(w, req)

	cookies := w.Result().Cookies()
	var stateCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == oauthStateCookieName {
			stateCookie = c
			break
		}
	}
	if stateCookie == nil {
		t.Fatal("expected nano_oauth_state cookie")
	}
	if stateCookie.Secure {
		t.Error("expected Secure=false when SessionManager.Secure() is false")
	}
}

func TestHandleGoogleLoginStateCookieAttributes(t *testing.T) {
	cfg := testOAuthConfig()

	req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	w := httptest.NewRecorder()

	HandleGoogleLogin(cfg)(w, req)

	cookies := w.Result().Cookies()
	var stateCookie *http.Cookie
	for _, c := range cookies {
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
	if stateCookie.Path != "/" {
		t.Errorf("expected Path='/', got %q", stateCookie.Path)
	}
}

func TestHandleGoogleLoginStateFormat(t *testing.T) {
	t.Run("without redirect path is bare csrf token", func(t *testing.T) {
		cfg := testOAuthConfig()

		req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
		w := httptest.NewRecorder()

		HandleGoogleLogin(cfg)(w, req)

		cookies := w.Result().Cookies()
		var stateCookie *http.Cookie
		for _, c := range cookies {
			if c.Name == oauthStateCookieName {
				stateCookie = c
				break
			}
		}
		if stateCookie == nil {
			t.Fatal("expected nano_oauth_state cookie")
		}

		// No ":" means no redirect path.
		if strings.Contains(stateCookie.Value, ":") {
			t.Errorf("expected bare CSRF token without ':', got %q", stateCookie.Value)
		}

		// Must be valid base64url.
		decoded, err := base64.RawURLEncoding.DecodeString(stateCookie.Value)
		if err != nil {
			t.Fatalf("invalid base64url: %v", err)
		}
		if len(decoded) != 32 {
			t.Errorf("expected 32 bytes, got %d", len(decoded))
		}
	})

	t.Run("with redirect path is csrfToken:path", func(t *testing.T) {
		cfg := testOAuthConfig()

		req := httptest.NewRequest(http.MethodGet, "/auth/login?state=/settings", nil)
		w := httptest.NewRecorder()

		HandleGoogleLogin(cfg)(w, req)

		cookies := w.Result().Cookies()
		var stateCookie *http.Cookie
		for _, c := range cookies {
			if c.Name == oauthStateCookieName {
				stateCookie = c
				break
			}
		}
		if stateCookie == nil {
			t.Fatal("expected nano_oauth_state cookie")
		}

		if !strings.Contains(stateCookie.Value, ":") {
			t.Fatalf("expected 'token:path' format with redirect, got %q", stateCookie.Value)
		}

		parts := strings.SplitN(stateCookie.Value, ":", 2)
		if parts[1] != "/settings" {
			t.Errorf("expected redirect path '/settings', got %q", parts[1])
		}

		// Token portion must still be valid 32-byte base64url.
		decoded, err := base64.RawURLEncoding.DecodeString(parts[0])
		if err != nil {
			t.Fatalf("invalid base64url in CSRF token: %v", err)
		}
		if len(decoded) != 32 {
			t.Errorf("expected 32 bytes in CSRF token, got %d", len(decoded))
		}
	})

	t.Run("with empty redirect query param is bare token", func(t *testing.T) {
		cfg := testOAuthConfig()

		req := httptest.NewRequest(http.MethodGet, "/auth/login?state=", nil)
		w := httptest.NewRecorder()

		HandleGoogleLogin(cfg)(w, req)

		cookies := w.Result().Cookies()
		var stateCookie *http.Cookie
		for _, c := range cookies {
			if c.Name == oauthStateCookieName {
				stateCookie = c
				break
			}
		}
		if stateCookie == nil {
			t.Fatal("expected nano_oauth_state cookie")
		}

		// Empty state param means no redirect path appended.
		if strings.Contains(stateCookie.Value, ":") {
			t.Errorf("expected bare CSRF token for empty state param, got %q", stateCookie.Value)
		}
	})
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

// createExpiredToken builds a validly-signed token whose timestamp is older
// than maxAge, causing ValidateToken to return ErrExpiredToken.
func createExpiredToken(sm *SessionManager) string {
	sessionID := "expired-user"
	info := TokenUserInfo{Email: "old@example.com", Name: "Old User", Picture: "https://example.com/old.jpg"}
	userInfoJSON, _ := json.Marshal(info)

	ts := make([]byte, 8)
	binary.BigEndian.PutUint64(ts, uint64(time.Now().Add(-48*time.Hour).UTC().Unix()))

	randBytes := make([]byte, randomBytesLength)
	rand.Read(randBytes)

	sig := sm.computeSignature(sessionID, ts, randBytes, userInfoJSON)

	return encodeSegment(sessionID) + "." +
		encodeSegment(string(ts)) +
		"." + encodeSegment(string(randBytes)) +
		"." + encodeSegment(string(userInfoJSON)) +
		"." + encodeSegment(string(sig))
}

func TestHandleSessionInfoAuthDisabled(t *testing.T) {
	sm := newTestSessionManager(t)
	sm.authEnabled = false
	handler := HandleSessionInfo(sm)

	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
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
	if len(body) != 1 {
		t.Errorf("expected only auth_enabled field, got %v", body)
	}
}

func TestHandleSessionInfoNoCredentials(t *testing.T) {
	sm := newTestSessionManager(t)
	sm.authEnabled = true
	handler := HandleSessionInfo(sm)

	t.Run("no cookie or query param", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
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
			t.Errorf("expected empty object, got %v", body)
		}
	})

	t.Run("empty token query param", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/auth/me?token=", nil)
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
			t.Errorf("expected empty object for empty token query param, got %v", body)
		}
	})
}

func TestHandleSessionInfoInvalidToken(t *testing.T) {
	sm := newTestSessionManager(t)
	sm.authEnabled = true
	handler := HandleSessionInfo(sm)

	t.Run("garbage token in cookie", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
		req.AddCookie(&http.Cookie{Name: cookieName, Value: "not.a.valid.token"})
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
			t.Errorf("expected empty object for garbage token, got %v", body)
		}
	})

	t.Run("garbage token in query param", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/auth/me?token=garbage", nil)
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
			t.Errorf("expected empty object for garbage query token, got %v", body)
		}
	})

	t.Run("tampered signature", func(t *testing.T) {
		validToken := sm.CreateToken("user-1", TokenUserInfo{
			Email: "test@example.com", Name: "Test", Picture: "https://example.com/p.jpg",
		})
		// Flip last character of the signature segment.
		parts := strings.Split(validToken, ".")
		sig := []byte(parts[4])
		sig[len(sig)-1] ^= 0xFF
		tampered := strings.Join([]string{parts[0], parts[1], parts[2], parts[3], string(sig)}, ".")

		req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
		req.AddCookie(&http.Cookie{Name: cookieName, Value: tampered})
		w := httptest.NewRecorder()

		handler(w, req)

		var body map[string]interface{}
		if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if len(body) != 0 {
			t.Errorf("expected empty object for tampered signature, got %v", body)
		}
	})
}

func TestHandleSessionInfoExpiredToken(t *testing.T) {
	sm := newTestSessionManager(t)
	sm.authEnabled = true
	handler := HandleSessionInfo(sm)

	token := createExpiredToken(sm)

	t.Run("expired token in cookie", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
		req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
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
			t.Errorf("expected empty object for expired token, got %v", body)
		}
	})

	t.Run("expired token in query param", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/auth/me?token="+token, nil)
		w := httptest.NewRecorder()

		handler(w, req)

		var body map[string]interface{}
		if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if len(body) != 0 {
			t.Errorf("expected empty object for expired query token, got %v", body)
		}
	})
}

func TestHandleSessionInfoValidTokenCookie(t *testing.T) {
	sm := newTestSessionManager(t)
	sm.authEnabled = true
	handler := HandleSessionInfo(sm)

	token := sm.CreateToken("google-abc", TokenUserInfo{
		Email:   "alice@example.com",
		Name:    "Alice Johnson",
		Picture: "https://example.com/alice.jpg",
	})

	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["id"] != "google-abc" {
		t.Errorf("expected id=google-abc, got %s", body["id"])
	}
	if body["email"] != "alice@example.com" {
		t.Errorf("expected email=alice@example.com, got %s", body["email"])
	}
	if body["name"] != "Alice Johnson" {
		t.Errorf("expected name=Alice Johnson, got %s", body["name"])
	}
	if body["picture"] != "https://example.com/alice.jpg" {
		t.Errorf("expected picture=https://example.com/alice.jpg, got %s", body["picture"])
	}
}

func TestHandleSessionInfoValidTokenQueryParam(t *testing.T) {
	sm := newTestSessionManager(t)
	sm.authEnabled = true
	handler := HandleSessionInfo(sm)

	token := sm.CreateToken("google-xyz", TokenUserInfo{
		Email:   "bob@example.com",
		Name:    "Bob Smith",
		Picture: "https://example.com/bob.jpg",
	})

	req := httptest.NewRequest(http.MethodGet, "/auth/me?token="+token, nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["id"] != "google-xyz" {
		t.Errorf("expected id=google-xyz, got %s", body["id"])
	}
	if body["email"] != "bob@example.com" {
		t.Errorf("expected email=bob@example.com, got %s", body["email"])
	}
	if body["name"] != "Bob Smith" {
		t.Errorf("expected name=Bob Smith, got %s", body["name"])
	}
	if body["picture"] != "https://example.com/bob.jpg" {
		t.Errorf("expected picture URL, got %s", body["picture"])
	}
}

func TestHandleSessionInfoCookiePrecedence(t *testing.T) {
	sm := newTestSessionManager(t)
	sm.authEnabled = true
	handler := HandleSessionInfo(sm)

	cookieToken := sm.CreateToken("cookie-user", TokenUserInfo{
		Email: "cookie@example.com", Name: "Cookie User", Picture: "https://example.com/cookie.jpg",
	})
	queryToken := sm.CreateToken("query-user", TokenUserInfo{
		Email: "query@example.com", Name: "Query User", Picture: "https://example.com/query.jpg",
	})

	req := httptest.NewRequest(http.MethodGet, "/auth/me?token="+queryToken, nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: cookieToken})
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	// Cookie should take precedence over query param.
	if body["id"] != "cookie-user" {
		t.Errorf("expected cookie to take precedence, got id=%s", body["id"])
	}
	if body["email"] != "cookie@example.com" {
		t.Errorf("expected cookie email, got %s", body["email"])
	}
}

func TestHandleSessionInfoResponseStructure(t *testing.T) {
	sm := newTestSessionManager(t)
	sm.authEnabled = true
	handler := HandleSessionInfo(sm)

	token := sm.CreateToken("struct-user", TokenUserInfo{
		Email:   "struct@example.com",
		Name:    "Struct Test",
		Picture: "https://example.com/struct.jpg",
	})

	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}

	requiredFields := []string{"id", "email", "name", "picture"}
	for _, field := range requiredFields {
		val, ok := body[field]
		if !ok {
			t.Errorf("missing required field %q in response", field)
			continue
		}
		strVal, ok := val.(string)
		if !ok || strVal == "" {
			t.Errorf("field %q should be a non-empty string, got %v", field, val)
		}
	}

	// Verify no extra fields.
	if len(body) != len(requiredFields) {
		t.Errorf("expected exactly %d fields, got %d: %v", len(requiredFields), len(body), body)
	}
}

func TestHandleSessionInfoContentType(t *testing.T) {
	sm := newTestSessionManager(t)
	sm.authEnabled = true
	handler := HandleSessionInfo(sm)

	tests := []struct {
		name    string
		setup   func(*http.Request)
	}{
		{"unauthenticated GET", func(r *http.Request) {}},
		{"authenticated GET", func(r *http.Request) {
			token := sm.CreateToken("ct-user", TokenUserInfo{
				Email: "ct@example.com", Name: "CT", Picture: "https://example.com/ct.jpg",
			})
			r.AddCookie(&http.Cookie{Name: cookieName, Value: token})
		}},
		{"auth disabled", func(r *http.Request) {
			sm.authEnabled = false
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset authEnabled between subtests.
			sm.authEnabled = true
			req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
			tt.setup(req)
			w := httptest.NewRecorder()

			handler(w, req)

			ct := w.Header().Get("Content-Type")
			if ct != "application/json" {
				t.Errorf("expected Content-Type application/json, got %s", ct)
			}
		})
	}
}

func TestHandleSessionInfoMethodNotAllowed(t *testing.T) {
	sm := newTestSessionManager(t)
	sm.authEnabled = true
	handler := HandleSessionInfo(sm)

	methods := []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch}
	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/auth/me", nil)
			w := httptest.NewRecorder()

			handler(w, req)

			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("expected status 405 for %s, got %d", method, w.Code)
			}
		})
	}
}
