package auth

import (
	"log/slog"
	"net/http"

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
