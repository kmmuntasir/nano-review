package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	// cookieName is the name of the HttpOnly session cookie.
	cookieName = "nano_session"

	// tokenCookieName is the name of the non-HttpOnly session token cookie,
	// readable by JavaScript for WebSocket authentication.
	tokenCookieName = "nano_session_token"

	// signatureLength is the byte length of the HMAC-SHA256 signature.
	signatureLength = 32

	// randomBytesLength is the number of random bytes used in the token.
	randomBytesLength = 16
)

var (
	// ErrInvalidToken is returned when a token fails HMAC validation.
	ErrInvalidToken = errors.New("invalid session token")

	// ErrExpiredToken is returned when a token's timestamp is beyond maxAge.
	ErrExpiredToken = errors.New("expired session token")
)

// SessionManager handles session token creation, validation, and cookie management.
// Tokens are stateless: they carry the session ID, a creation timestamp, and an
// HMAC-SHA256 signature so the server can verify validity without storage lookups.
type SessionManager struct {
	hmacKey        []byte
	maxAge         time.Duration
	allowedDomains []string
	authEnabled    bool
	secure         bool
}

// NewSessionManager creates a SessionManager with the given HMAC key, max session
// age, and allowed cookie domains.
//
// hmacKey must be at least 32 bytes; panics otherwise.
// maxAgeHours must be positive; defaults to 24h if zero or negative.
// allowedDomains restricts the cookie Domain attribute; empty means no restriction.
func NewSessionManager(hmacKey []byte, maxAgeHours float64, allowedDomains []string) *SessionManager {
	if len(hmacKey) < 32 {
		panic("auth: hmacKey must be at least 32 bytes")
	}

	maxAge := time.Duration(maxAgeHours * float64(time.Hour))
	if maxAge <= 0 {
		maxAge = 24 * time.Hour
	}

	return &SessionManager{
		hmacKey:        hmacKey,
		maxAge:         maxAge,
		allowedDomains: allowedDomains,
		authEnabled:    parseAuthEnabled(),
		secure:         parseSecureCookies(),
	}
}

// parseAuthEnabled reads AUTH_ENABLED from the environment.
// Returns true unless the value is exactly "false" (case-insensitive).
func parseAuthEnabled() bool {
	v := os.Getenv("AUTH_ENABLED")
	if strings.EqualFold(v, "false") {
		slog.Info("authentication disabled (AUTH_ENABLED=false)")
		return false
	}
	return true
}

// parseSecureCookies reads SECURE_COOKIES from the environment.
// Returns true unless the value is exactly "false" (case-insensitive).
// Defaults to true so cookies are secure-by-default over HTTPS.
func parseSecureCookies() bool {
	v := os.Getenv("SECURE_COOKIES")
	if strings.EqualFold(v, "false") {
		slog.Info("secure cookies disabled (SECURE_COOKIES=false) — only use for local HTTP development")
		return false
	}
	return true
}

// CreateToken generates a signed, stateless session token containing the session ID
// and a creation timestamp.
//
// Token format: base64(sessionID) + "." + base64(timestamp_bytes) + "." + base64(random) + "." + base64(signature)
//
// The signature covers sessionID + timestamp + random bytes to prevent token forgery
// and replay attacks.
func (m *SessionManager) CreateToken(sessionID string) string {
	ts := make([]byte, 8)
	binary.BigEndian.PutUint64(ts, uint64(time.Now().UTC().Unix()))

	randBytes := make([]byte, randomBytesLength)
	if _, err := rand.Read(randBytes); err != nil {
		// crypto/rand.Read should never fail on supported platforms.
		panic(fmt.Sprintf("auth: failed to generate random bytes: %v", err))
	}

	sig := m.computeSignature(sessionID, ts, randBytes)

	return encodeSegment(sessionID) + "." +
		encodeSegment(string(ts)) +
		"." + encodeSegment(string(randBytes)) +
		"." + encodeSegment(string(sig))
}

// ValidateToken parses and validates a token, returning the embedded session ID.
// Returns ErrInvalidToken if the signature doesn't match, or ErrExpiredToken if
// the token's age exceeds maxAge.
func (m *SessionManager) ValidateToken(token string) (string, error) {
	parts := strings.SplitN(token, ".", 4)
	if len(parts) != 4 {
		return "", ErrInvalidToken
	}

	sessionIDBytes, err := decodeSegment(parts[0])
	if err != nil {
		return "", fmt.Errorf("%w: malformed session ID segment", ErrInvalidToken)
	}

	tsBytes, err := decodeSegment(parts[1])
	if err != nil || len(tsBytes) != 8 {
		return "", fmt.Errorf("%w: malformed timestamp segment", ErrInvalidToken)
	}

	randBytes, err := decodeSegment(parts[2])
	if err != nil {
		return "", fmt.Errorf("%w: malformed random segment", ErrInvalidToken)
	}

	sigBytes, err := decodeSegment(parts[3])
	if err != nil {
		return "", fmt.Errorf("%w: malformed signature segment", ErrInvalidToken)
	}

	sessionID := string(sessionIDBytes)

	// Verify HMAC.
	expectedSig := m.computeSignature(sessionID, tsBytes, randBytes)
	if !hmac.Equal(sigBytes, expectedSig) {
		return "", ErrInvalidToken
	}

	// Check expiration.
	createdAt := time.Unix(int64(binary.BigEndian.Uint64(tsBytes)), 0)
	if time.Since(createdAt) > m.maxAge {
		return "", ErrExpiredToken
	}

	return sessionID, nil
}

// SetCookie writes the session cookie to an http.ResponseWriter.
func (m *SessionManager) SetCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		Secure:   m.secure,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(m.maxAge.Seconds()),
		Domain:   m.cookieDomain(),
	})
}

// ClearCookie removes the session cookie by setting MaxAge to -1.
func (m *SessionManager) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		Secure:   m.secure,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Domain:   m.cookieDomain(),
	})
}

// SetTokenCookie writes a non-HttpOnly session token cookie so JavaScript can
// read the value for WebSocket authentication via query parameter.
func (m *SessionManager) SetTokenCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     tokenCookieName,
		Value:    token,
		Path:     "/",
		Secure:   m.secure,
		HttpOnly: false,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(m.maxAge.Seconds()),
		Domain:   m.cookieDomain(),
	})
}

// ClearTokenCookie removes the non-HttpOnly token cookie by setting MaxAge to -1.
func (m *SessionManager) ClearTokenCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     tokenCookieName,
		Value:    "",
		Path:     "/",
		Secure:   m.secure,
		HttpOnly: false,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Domain:   m.cookieDomain(),
	})
}

// CookieName returns the session cookie name for use in middleware.
func (m *SessionManager) CookieName() string {
	return cookieName
}

// MaxAge returns the configured maximum session age.
func (m *SessionManager) MaxAge() time.Duration {
	return m.maxAge
}

// AuthEnabled returns whether authentication is enabled for this SessionManager.
func (m *SessionManager) AuthEnabled() bool {
	return m.authEnabled
}

// Secure returns whether the Secure flag is set on cookies.
func (m *SessionManager) Secure() bool {
	return m.secure
}

// RequireAuth returns an HTTP middleware that validates the nano_session cookie.
// If authentication is disabled (AUTH_ENABLED=false), it passes requests through
// without checking. Otherwise it reads the cookie (or the ?token= query parameter
// for WebSocket upgrades), validates the token, and attaches the User to the
// request context. Returns 401 JSON on failure.
func (m *SessionManager) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !m.authEnabled {
			next.ServeHTTP(w, r)
			return
		}

		tokenValue := ""
		source := "cookie"

		// Try cookie first.
		cookie, err := r.Cookie(cookieName)
		if err == nil {
			tokenValue = cookie.Value
		}

		// Fall back to ?token= query parameter (used by WebSocket connections).
		if tokenValue == "" {
			tokenValue = r.URL.Query().Get("token")
			source = "query"
		}

		if tokenValue == "" {
			writeUnauthorized(w, "missing session cookie")
			return
		}

		sessionID, err := m.ValidateToken(tokenValue)
		if err != nil {
			writeUnauthorized(w, "invalid or expired session token")
			return
		}

		user := User{
			ID:     sessionID,
			Source: source,
		}

		ctx := ContextWithUser(r.Context(), user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// writeUnauthorized writes a 401 JSON error response.
func writeUnauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte(`{"error":"` + msg + `"}`))
}

// computeSignature generates an HMAC-SHA256 signature over the session ID,
// timestamp, and random bytes.
func (m *SessionManager) computeSignature(sessionID string, ts, randBytes []byte) []byte {
	mac := hmac.New(sha256.New, m.hmacKey)
	mac.Write([]byte(sessionID))
	mac.Write(ts)
	mac.Write(randBytes)
	return mac.Sum(nil)
}

// cookieDomain returns the first allowed domain if configured, or empty string.
func (m *SessionManager) cookieDomain() string {
	if len(m.allowedDomains) > 0 {
		return m.allowedDomains[0]
	}
	return ""
}

// encodeSegment encodes raw bytes as URL-safe base64 (no padding).
func encodeSegment(raw string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// decodeSegment decodes a URL-safe base64 segment back to raw bytes.
func decodeSegment(seg string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(seg)
}

// MaxAgeFromCookie parses the Max-Age from a Set-Cookie header value.
// This is a convenience function for tests.
func MaxAgeFromCookie(setCookieHeader string) (int, error) {
	for _, part := range strings.Split(setCookieHeader, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "Max-Age=") {
			return strconv.Atoi(strings.TrimPrefix(part, "Max-Age="))
		}
	}
	return 0, errors.New("max-age not found in set-cookie header")
}
