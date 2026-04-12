//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestLogout_ClearsCookiesAndRedirects(t *testing.T) {
	srv := newIntegrationServer(t)
	defer srv.Close()

	// Authenticate first.
	client := authenticatedClient(t, srv)

	// Logout — use a non-redirecting client to inspect Set-Cookie headers.
	noFollow := newNoFollowClient(t)
	noFollow.Jar = client.Jar

	resp, err := noFollow.Get(srv.baseURL + "/auth/logout")
	if err != nil {
		t.Fatalf("logout request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusFound)
	}

	loc := resp.Header.Get("Location")
	if loc != "/" {
		t.Errorf("redirect location = %q, want %q", loc, "/")
	}

	cookies := resp.Cookies()
	if len(cookies) != 2 {
		t.Fatalf("expected 2 cookies, got %d", len(cookies))
	}

	for _, c := range cookies {
		if c.MaxAge != -1 {
			t.Errorf("cookie %q MaxAge = %d, want -1 (cleared)", c.Name, c.MaxAge)
		}
	}
}

func TestLogout_SubsequentRequestReturns401(t *testing.T) {
	srv := newIntegrationServer(t)
	defer srv.Close()

	// Authenticate first.
	client := authenticatedClient(t, srv)

	// Logout.
	noFollow := newNoFollowClient(t)
	noFollow.Jar = client.Jar

	resp, err := noFollow.Get(srv.baseURL + "/auth/logout")
	if err != nil {
		t.Fatalf("logout request failed: %v", err)
	}
	resp.Body.Close()

	// Access a protected route with the same client (cookies now cleared).
	followClient := newRedirectClient(t)
	followClient.Jar = client.Jar

	meResp, err := followClient.Get(srv.baseURL + "/auth/me")
	if err != nil {
		t.Fatalf("auth/me request after logout failed: %v", err)
	}
	defer meResp.Body.Close()

	if meResp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", meResp.StatusCode, http.StatusUnauthorized)
	}

	var body map[string]string
	if err := json.NewDecoder(meResp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body["error"] != "missing session cookie" {
		t.Errorf("error = %q, want %q", body["error"], "missing session cookie")
	}
}
