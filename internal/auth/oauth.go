package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// googleScopes are the OAuth scopes requested during Google login.
// openid and email are sufficient for identifying users.
var googleScopes = []string{
	"https://www.googleapis.com/auth/userinfo.email",
	"https://www.googleapis.com/auth/userinfo.profile",
	"openid",
}

// OAuthConfig holds the dependencies needed by OAuth handlers.
type OAuthConfig struct {
	// ClientID is the Google OAuth2 client ID.
	ClientID string

	// ClientSecret is the Google OAuth2 client secret.
	ClientSecret string

	// RedirectURL is the callback URL registered with Google.
	RedirectURL string

	// SessionManager handles cookie-based session tokens.
	SessionManager *SessionManager

	// HTTPClient is an optional custom HTTP client used for token exchange
	// and userinfo calls. When nil, http.DefaultClient is used.
	HTTPClient *http.Client
}

// OAuthEndpoint returns the *oauth2.Config for Google OAuth2 flows.
// Returns nil if ClientID or ClientSecret is empty (auth disabled).
func (c *OAuthConfig) OAuthEndpoint() *oauth2.Config {
	if c.ClientID == "" || c.ClientSecret == "" {
		return nil
	}
	return &oauth2.Config{
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret,
		RedirectURL:  c.RedirectURL,
		Scopes:       googleScopes,
		Endpoint:     google.Endpoint,
	}
}

// Validate checks that the OAuth configuration has the required fields.
// Returns an error listing missing GOOGLE_CLIENT_ID and/or GOOGLE_CLIENT_SECRET.
func (c *OAuthConfig) Validate() error {
	var missing []string
	if c.ClientID == "" {
		missing = append(missing, "GOOGLE_CLIENT_ID")
	}
	if c.ClientSecret == "" {
		missing = append(missing, "GOOGLE_CLIENT_SECRET")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required OAuth config: %s", strings.Join(missing, ", "))
	}
	return nil
}

// HandleGoogleLogin returns an http.HandlerFunc that redirects the user to
// Google's OAuth consent screen.
//
// If OAuth is not configured (missing ClientID/ClientSecret), it returns 501.
func HandleGoogleLogin(cfg *OAuthConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}

		endpoint := cfg.OAuthEndpoint()
		if endpoint == nil {
			http.Error(w, `{"error":"OAuth is not configured"}`, http.StatusNotImplemented)
			return
		}

		// Optional state parameter for CSRF protection — read from query if
		// provided by the frontend, otherwise generate a random one.
		state := r.URL.Query().Get("state")

		url := endpoint.AuthCodeURL(state, oauth2.AccessTypeOffline)
		slog.Info("redirecting to Google OAuth", "redirect_url", url)

		http.Redirect(w, r, url, http.StatusTemporaryRedirect)
	}
}

// HandleOAuthCallback processes the OAuth callback from Google. It exchanges
// the authorization code for tokens, fetches the user's profile from the
// userinfo endpoint, creates a session token, and sets it as a cookie.
func HandleOAuthCallback(cfg *OAuthConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}

		endpoint := cfg.OAuthEndpoint()
		if endpoint == nil {
			http.Error(w, `{"error":"OAuth is not configured"}`, http.StatusNotImplemented)
			return
		}

		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, `{"error":"missing authorization code"}`, http.StatusBadRequest)
			return
		}

		// Inject a custom HTTP client into the context if configured,
		// so token exchange and userinfo calls can be intercepted in tests.
		ctx := r.Context()
		if cfg.HTTPClient != nil {
			ctx = context.WithValue(ctx, oauth2.HTTPClient, cfg.HTTPClient)
		}

		token, err := endpoint.Exchange(ctx, code)
		if err != nil {
			slog.Error("OAuth token exchange failed", "error", err)
			http.Error(w, `{"error":"token exchange failed"}`, http.StatusBadGateway)
			return
		}

		// Use the same context so the oauth2.Transport Base also picks up
		// the injected HTTP client for the userinfo GET.
		client := endpoint.Client(ctx, token)
		resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
		if err != nil {
			slog.Error("failed to fetch user info", "error", err)
			http.Error(w, `{"error":"failed to fetch user info"}`, http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		var info struct {
			ID      string `json:"id"`
			Email   string `json:"email"`
			Name    string `json:"name"`
			Picture string `json:"picture"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
			slog.Error("failed to decode user info", "error", err)
			http.Error(w, `{"error":"failed to decode user info"}`, http.StatusBadGateway)
			return
		}

		slog.Info("user authenticated", "google_id", info.ID, "email", info.Email)

		sessionToken := cfg.SessionManager.CreateToken(info.ID)
		cfg.SessionManager.SetCookie(w, sessionToken)
		cfg.SessionManager.SetTokenCookie(w, sessionToken)

		http.Redirect(w, r, "/", http.StatusFound)
	}
}

// HandleLogout clears the session cookie and redirects to the home page.
func HandleLogout(sm *SessionManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sm.ClearCookie(w)
		sm.ClearTokenCookie(w)
		http.Redirect(w, r, "/", http.StatusFound)
	}
}

// HandleSessionInfo returns the authenticated user's session info as JSON.
// Requires a valid session cookie.
func HandleSessionInfo(sm *SessionManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}

		user := UserFromContext(r.Context())
		if user.ID == "" {
			http.Error(w, `{"error":"not authenticated"}`, http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"id":     user.ID,
			"source": user.Source,
		})
	}
}
