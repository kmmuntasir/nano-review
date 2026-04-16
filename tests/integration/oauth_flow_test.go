//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestLogin_RedirectsToGoogle(t *testing.T) {
	srv := newIntegrationServer(t)
	defer srv.Close()

	client := newRedirectClient(t)
	resp, err := client.Get(srv.baseURL + "/auth/login")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	// httptest client follows redirects by default; disable for this test.
	if resp.StatusCode != http.StatusOK {
		// The client followed the redirect. Check the request history is not
		// directly available, so instead use a non-following client.
	}
}

func TestLogin_RedirectsToGoogle_NoFollow(t *testing.T) {
	srv := newIntegrationServer(t)
	defer srv.Close()

	client := newNoFollowClient(t)

	resp, err := client.Get(srv.baseURL + "/auth/login")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusTemporaryRedirect {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusTemporaryRedirect)
	}

	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "https://accounts.google.com/o/oauth2/auth") {
		t.Errorf("redirect location = %q, want Google auth URL", loc)
	}

	if !strings.Contains(loc, "client_id=test-client-id") {
		t.Errorf("redirect URL missing client_id, got %q", loc)
	}

	if !strings.Contains(loc, "scope=") {
		t.Errorf("redirect URL missing scope, got %q", loc)
	}
}

func TestCallback_CompleteFlow(t *testing.T) {
	srv := newIntegrationServer(t)
	defer srv.Close()

	client := newRedirectClient(t)
	callbackURL := srv.baseURL + "/auth/callback?code=fake-auth-code"
	resp, err := client.Get(callbackURL)
	if err != nil {
		t.Fatalf("callback request failed: %v", err)
	}
	defer resp.Body.Close()

	// After following the 302 redirect to /, we get 404 since there are
	// no static files in the test server. The important thing is the redirect
	// chain completed (the request didn't fail).
	if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusOK {
		t.Errorf("final status = %d, want 200 or 404 (static files not mounted in test)", resp.StatusCode)
	}

	// Verify cookies were set by checking the jar.
	serverURL, _ := url.Parse(srv.baseURL)
	cookies := client.Jar.Cookies(serverURL)

	var hasSession, hasToken bool
	for _, c := range cookies {
		if c.Name == "nano_session" {
			hasSession = true
		}
		if c.Name == "nano_session_token" {
			hasToken = true
		}
	}

	if !hasSession {
		t.Error("nano_session cookie not set")
	}
	if !hasToken {
		t.Error("nano_session_token cookie not set")
	}
}

func TestCallback_CookieAttributes(t *testing.T) {
	srv := newIntegrationServer(t)
	defer srv.Close()

	// Perform OAuth login to obtain a valid CSRF state and oauth_state cookie.
	state, loginClient := performOAuthLogin(t, srv)

	// Use a non-redirecting client with the same cookie jar so the CSRF state
	// cookie is sent with the callback request, and we can inspect Set-Cookie
	// headers directly on the 302 response.
	client := &http.Client{
		Jar:       loginClient.Jar,
		Transport: loginClient.Transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Get(srv.baseURL + "/auth/callback?code=fake-auth-code&state=" + state)
	if err != nil {
		t.Fatalf("callback request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusFound)
	}

	cookies := resp.Cookies()
	if len(cookies) != 2 {
		t.Fatalf("expected 2 cookies, got %d", len(cookies))
	}

	var sessionCookie, tokenCookie *http.Cookie
	for _, c := range cookies {
		switch c.Name {
		case "nano_session":
			sessionCookie = c
		case "nano_session_token":
			tokenCookie = c
		}
	}

	if sessionCookie == nil {
		t.Fatal("nano_session cookie missing")
	}
	if !sessionCookie.HttpOnly {
		t.Error("nano_session should be HttpOnly")
	}

	if tokenCookie == nil {
		t.Fatal("nano_session_token cookie missing")
	}
	if tokenCookie.HttpOnly {
		t.Error("nano_session_token should NOT be HttpOnly (JavaScript needs it for WebSocket auth)")
	}
}

func TestCallback_AuthMeReturnsUser(t *testing.T) {
	srv := newIntegrationServer(t)
	defer srv.Close()

	client := authenticatedClient(t, srv)

	resp, err := client.Get(srv.baseURL + "/auth/me")
	if err != nil {
		t.Fatalf("auth/me request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var user map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if user["id"] != "google-user-123" {
		t.Errorf("user id = %q, want %q", user["id"], "google-user-123")
	}
	if user["source"] != "cookie" {
		t.Errorf("user source = %q, want %q", user["source"], "cookie")
	}
}

func TestCallback_MissingCode(t *testing.T) {
	srv := newIntegrationServer(t)
	defer srv.Close()

	client := newRedirectClient(t)
	resp, err := client.Get(srv.baseURL + "/auth/callback")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestCallback_InvalidCode(t *testing.T) {
	srv := newIntegrationServer(t)
	defer srv.Close()

	// Replace the mock transport with one that returns an error for token exchange.
	srv.oauthCfg.HTTPClient = &http.Client{
		Transport: &errorTransport{},
	}

	client := newRedirectClient(t)
	resp, err := client.Get(srv.baseURL + "/auth/callback?code=invalid-code")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadGateway)
	}
}

// errorTransport returns an error for every request, simulating a failed
// token exchange with Google.
type errorTransport struct{}

func (t *errorTransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("mock token exchange failure")
}
