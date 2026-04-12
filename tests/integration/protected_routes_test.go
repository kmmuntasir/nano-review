//go:build integration

package integration

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"testing"
)

// insecureClient is an HTTP client that skips TLS verification for the
// self-signed test server certificate.
var insecureClient = &http.Client{
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	},
}

func TestProtectedRoutes_401WithoutCookie(t *testing.T) {
	srv := newIntegrationServer(t)
	defer srv.Close()

	paths := []string{
		"/auth/me",
		"/reviews",
		"/metrics",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			resp, err := insecureClient.Get(srv.baseURL + path)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
			}

			var body map[string]string
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			if body["error"] != "missing session cookie" {
				t.Errorf("error = %q, want %q", body["error"], "missing session cookie")
			}
		})
	}
}

func TestProtectedRoutes_401InvalidToken(t *testing.T) {
	srv := newIntegrationServer(t)
	defer srv.Close()

	paths := []string{
		"/auth/me",
		"/reviews",
		"/metrics",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, srv.baseURL+path, nil)
			req.AddCookie(&http.Cookie{
				Name:  "nano_session",
				Value: "invalid-token-garbage",
			})

			resp, err := insecureClient.Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
			}

			var body map[string]string
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			if body["error"] != "invalid or expired session token" {
				t.Errorf("error = %q, want %q", body["error"], "invalid or expired session token")
			}
		})
	}
}

func TestProtectedRoutes_200WithValidSession(t *testing.T) {
	srv := newIntegrationServer(t)
	defer srv.Close()

	client := authenticatedClient(t, srv)

	tests := []struct {
		path string
	}{
		{"/auth/me"},
		{"/reviews"},
		{"/metrics"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			resp, err := client.Get(srv.baseURL + tt.path)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Errorf("status = %d, want 200 for %s", resp.StatusCode, tt.path)
			}
		})
	}
}
