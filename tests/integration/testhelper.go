//go:build integration

package integration

import (
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/kmmuntasir/nano-review/internal/api"
	"github.com/kmmuntasir/nano-review/internal/auth"
	"github.com/kmmuntasir/nano-review/internal/storage"
)

// googleMockTransport implements http.RoundTripper, intercepting calls to
// Google's OAuth token and userinfo endpoints so tests run without real credentials.
type googleMockTransport struct {
	userID  string
	email   string
	name    string
	picture string
}

func (t *googleMockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	switch req.URL.Host {
	case "oauth2.googleapis.com":
		// Token exchange endpoint — return a fake access token.
		body := `{"access_token":"fake-access-token","token_type":"Bearer","expires_in":3600}`
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     http.Header{"Content-Type": {"application/json"}},
		}, nil

	case "www.googleapis.com":
		// Userinfo endpoint — return a fake user profile.
		body := `{"id":"` + t.userID + `","email":"` + t.email + `","name":"` + t.name + `","picture":"` + t.picture + `"}`
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     http.Header{"Content-Type": {"application/json"}},
		}, nil

	default:
		return http.DefaultTransport.RoundTrip(req)
	}
}

// integrationServer holds a fully wired test server with mock dependencies.
type integrationServer struct {
	server     *httptest.Server
	hub        *api.Hub
	sessionMgr *auth.SessionManager
	oauthCfg   *auth.OAuthConfig
	baseURL    string
}

// newIntegrationServer creates and returns a test server with all routes
// registered and AUTH_ENABLED=true. The server uses a mock Google OAuth
// transport so no real credentials are needed.
func newIntegrationServer(t *testing.T) *integrationServer {
	t.Helper()

	t.Setenv("AUTH_ENABLED", "true")

	hmacKey := make([]byte, 32)
	for i := range hmacKey {
		hmacKey[i] = byte(i)
	}

	sessionMgr := auth.NewSessionManager(hmacKey, 24, nil)

	mockTransport := &googleMockTransport{
		userID:  "google-user-123",
		email:   "test@example.com",
		name:    "Test User",
		picture: "https://example.com/pic.jpg",
	}

	oauthCfg := &auth.OAuthConfig{
		ClientID:       "test-client-id",
		ClientSecret:   "test-client-secret",
		RedirectURL:    "", // set after server creation
		SessionManager: sessionMgr,
		HTTPClient:     &http.Client{Transport: mockTransport},
	}

	hub := api.NewHub()

	mux := http.NewServeMux()

	// Public routes.
	mux.HandleFunc("GET /auth/login", auth.HandleGoogleLogin(oauthCfg))
	mux.HandleFunc("GET /auth/callback", auth.HandleOAuthCallback(oauthCfg))
	mux.HandleFunc("GET /auth/logout", auth.HandleLogout(sessionMgr))
	mux.HandleFunc("POST /review", api.HandleReview("test-secret", &mockReviewStarter{}))

	// Protected routes.
	mux.Handle("GET /auth/me", sessionMgr.RequireAuth(auth.HandleSessionInfo(sessionMgr)))
	mux.Handle("GET /reviews", sessionMgr.RequireAuth(api.HandleListReviews(&mockReviewGetter{})))
	mux.Handle("GET /reviews/{run_id}", sessionMgr.RequireAuth(api.HandleGetReview(&mockReviewGetter{})))
	mux.Handle("GET /ws", sessionMgr.RequireAuth(api.HandleWebSocket(hub, nil)))
	mux.Handle("GET /metrics", sessionMgr.RequireAuth(api.HandleGetMetrics(&mockReviewGetter{})))

	server := httptest.NewTLSServer(mux)

	oauthCfg.RedirectURL = server.URL + "/auth/callback"

	return &integrationServer{
		server:     server,
		hub:        hub,
		sessionMgr: sessionMgr,
		oauthCfg:   oauthCfg,
		baseURL:    server.URL,
	}
}

// Close shuts down the test server.
func (s *integrationServer) Close() {
	s.server.Close()
}

// CreateToken creates a valid session token using the server's SessionManager.
func (s *integrationServer) CreateToken(sessionID string) string {
	return s.sessionMgr.CreateToken(sessionID, auth.TokenUserInfo{})
}

// --- Mocks ---

type mockReviewStarter struct{}

func (m *mockReviewStarter) StartReview(_ context.Context, _ api.ReviewPayload) (string, error) {
	return "mock-run-id", nil
}

type mockReviewGetter struct{}

func (m *mockReviewGetter) GetReview(_ context.Context, _ string) (*storage.ReviewRecord, error) {
	return nil, storage.ErrNotFound
}

func (m *mockReviewGetter) ListReviews(_ context.Context, _ storage.ListFilter) (*storage.ListResult, error) {
	return &storage.ListResult{Reviews: []storage.ReviewRecord{}}, nil
}

func (m *mockReviewGetter) GetMetrics(_ context.Context) (*storage.Metrics, error) {
	return &storage.Metrics{}, nil
}

// newCookieJar creates a cookie jar for HTTP clients that follow redirects
// while preserving cookies across hops.
func newCookieJar(t *testing.T) *cookiejar.Jar {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("failed to create cookie jar: %v", err)
	}
	return jar
}

// newRedirectClient returns an *http.Client with a cookie jar that follows
// redirects and preserves Set-Cookie headers across hops. It skips TLS
// verification since the test server uses a self-signed certificate.
func newRedirectClient(t *testing.T) *http.Client {
	t.Helper()
	return &http.Client{
		Jar: newCookieJar(t),
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
}

// newNoFollowClient returns an *http.Client that does not follow redirects
// and skips TLS verification for the self-signed test server certificate.
func newNoFollowClient(t *testing.T) *http.Client {
	t.Helper()
	return &http.Client{
		Jar:       newCookieJar(t),
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// performOAuthLogin hits /auth/login with a no-follow client and extracts
// the OAuth state parameter from the redirect URL. The returned client
// carries the oauth_state cookie needed by the CSRF check in /auth/callback.
func performOAuthLogin(t *testing.T, srv *integrationServer) (state string, client *http.Client) {
	t.Helper()
	loginClient := newNoFollowClient(t)
	resp, err := loginClient.Get(srv.baseURL + "/auth/login")
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	resp.Body.Close()

	redirectURL, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("failed to parse redirect URL: %v", err)
	}
	return redirectURL.Query().Get("state"), loginClient
}

// authenticatedClient performs a full OAuth login+callback flow and returns
// an *http.Client with valid session cookies. The returned client can be used
// directly in tests that require an authenticated user.
func authenticatedClient(t *testing.T, srv *integrationServer) *http.Client {
	t.Helper()

	state, loginClient := performOAuthLogin(t, srv)

	callbackURL := srv.baseURL + "/auth/callback?code=fake-auth-code&state=" + state
	resp, err := loginClient.Get(callbackURL)
	if err != nil {
		t.Fatalf("oauth callback request failed: %v", err)
	}
	resp.Body.Close()

	// The callback sets session cookies and redirects to /.
	// Switch to a redirect-following client that reuses the same jar.
	return &http.Client{
		Jar:       loginClient.Jar,
		Transport: loginClient.Transport,
	}
}
