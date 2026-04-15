package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// oauthStateCookieName is the name of the short-lived CSRF state cookie
// used during the OAuth flow.
const oauthStateCookieName = "nano_oauth_state"

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

	// AllowedEmailDomains restricts which email domains can authenticate.
	// Empty means all domains are allowed.
	AllowedEmailDomains []string
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

		// TODO(#2): Add rate limiting to prevent OAuth abuse.

		// Generate CSRF token (32 random bytes, base64url-encoded).
		csrfBytes := make([]byte, 32)
		if _, err := rand.Read(csrfBytes); err != nil {
			slog.Error("failed to generate CSRF token", "error", err)
			http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
			return
		}
		csrfToken := base64.RawURLEncoding.EncodeToString(csrfBytes)

		// Append frontend redirect path from query parameter.
		redirectPath := r.URL.Query().Get("state")
		stateValue := csrfToken
		if redirectPath != "" {
			stateValue = csrfToken + ":" + redirectPath
		}

		// Store state in a short-lived cookie for CSRF verification.
		http.SetCookie(w, &http.Cookie{
			Name:     oauthStateCookieName,
			Value:    stateValue,
			Path:     "/",
			Secure:   cfg.SessionManager.Secure(),
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   300,
		})

		url := endpoint.AuthCodeURL(stateValue, oauth2.AccessTypeOffline)
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

		// Verify CSRF state parameter.
		stateParam := r.URL.Query().Get("state")
		if stateParam == "" {
			http.Error(w, `{"error":"missing state parameter"}`, http.StatusBadRequest)
			return
		}

		stateCookie, err := r.Cookie(oauthStateCookieName)
		if err != nil {
			http.Error(w, `{"error":"missing state cookie"}`, http.StatusBadRequest)
			return
		}

		// Split both on ":" — first segment is CSRF token, must match.
		csrfParam := strings.SplitN(stateParam, ":", 2)[0]
		csrfCookie := strings.SplitN(stateCookie.Value, ":", 2)[0]
		if !hmac.Equal([]byte(csrfParam), []byte(csrfCookie)) {
			http.Error(w, `{"error":"invalid state parameter"}`, http.StatusBadRequest)
			return
		}

		// Clear state cookie (single use).
		http.SetCookie(w, &http.Cookie{
			Name:     oauthStateCookieName,
			Value:    "",
			Path:     "/",
			Secure:   cfg.SessionManager.Secure(),
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   -1,
		})

		// Extract redirect path from state (after the first ":").
		redirectPath := "/"
		if parts := strings.SplitN(stateParam, ":", 2); len(parts) == 2 && parts[1] != "" {
			redirectPath = parts[1]
		}
		// Ensure redirectPath starts with "/" for absolute redirect (not relative to /auth/callback)
		if !strings.HasPrefix(redirectPath, "/") {
			redirectPath = "/" + redirectPath
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

		if !isEmailAllowed(info.Email, cfg.AllowedEmailDomains) {
			slog.Warn("email domain not allowed", "email", info.Email, "allowed_domains", cfg.AllowedEmailDomains)
			http.Error(w, `{"error":"email domain not allowed"}`, http.StatusForbidden)
			return
		}

		sessionToken := cfg.SessionManager.CreateToken(info.ID, TokenUserInfo{
			Email:   info.Email,
			Name:    info.Name,
			Picture: info.Picture,
		})
		cfg.SessionManager.SetCookie(w, sessionToken)
		cfg.SessionManager.SetTokenCookie(w, sessionToken)

		http.Redirect(w, r, redirectPath, http.StatusFound)
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

// HandleSessionInfo returns session info as JSON.
// This is a public endpoint that handles three cases:
//   - Auth disabled: returns {"auth_enabled":false}
//   - No valid token: returns {}
//   - Valid token: returns {id, email, name, picture}
func HandleSessionInfo(sm *SessionManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		if !sm.AuthEnabled() {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"auth_enabled": false})
			return
		}

		tokenValue := ""
		if cookie, err := r.Cookie(cookieName); err == nil {
			tokenValue = cookie.Value
		}
		if tokenValue == "" {
			tokenValue = r.URL.Query().Get("token")
		}
		if tokenValue == "" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{})
			return
		}

		session, err := sm.ValidateToken(tokenValue)
		if err != nil {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{})
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]string{
			"id":      session.SessionID,
			"email":   session.UserInfo.Email,
			"name":    session.UserInfo.Name,
			"picture": session.UserInfo.Picture,
		})
	}
}

// isEmailAllowed returns true if the email's domain is in the allowed list,
// or if the allowed list is empty (all domains permitted).
func isEmailAllowed(email string, allowedDomains []string) bool {
	if len(allowedDomains) == 0 {
		return true
	}

	at := strings.LastIndex(email, "@")
	if at < 0 {
		return false
	}

	domain := strings.ToLower(email[at+1:])
	for _, allowed := range allowedDomains {
		if strings.EqualFold(domain, strings.TrimPrefix(allowed, "@")) {
			return true
		}
	}
	return false
}
