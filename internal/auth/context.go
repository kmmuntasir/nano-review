package auth

import (
	"context"
	"log/slog"
)

// userKeyType is the key type for storing User in context.
// Using an unexported key prevents collisions with other packages.
type userKeyType struct{}

var userKey userKeyType

// User represents an authenticated entity in the system.
// For webhook-based auth, this typically contains the webhook source identifier.
type User struct {
	// ID is the unique identifier for the user or webhook source.
	ID string

	// Source indicates where the authentication came from
	// (e.g., "webhook", "api_token", "jwt").
	Source string

	// Attributes holds optional additional information about the user.
	// For webhooks, this might include the originating repository or event type.
	Attributes map[string]string
}

// String returns a concise string representation of the User for logging.
func (u User) String() string {
	if u.ID == "" {
		return "user<none>"
	}
	return "user<" + u.Source + ":" + u.ID + ">"
}

// LogValue returns a structured log value for the User.
func (u User) LogValue() slog.Value {
	attrs := make([]slog.Attr, 0, len(u.Attributes)+2)
	attrs = append(attrs, slog.String("id", u.ID), slog.String("source", u.Source))
	for k, v := range u.Attributes {
		attrs = append(attrs, slog.String(k, v))
	}
	return slog.GroupValue(attrs...)
}

// UserFromContext extracts the User from the context.
// Returns a zero User if no user is present in the context.
func UserFromContext(ctx context.Context) User {
	if u, ok := ctx.Value(userKey).(User); ok {
		return u
	}
	return User{}
}

// ContextWithUser returns a new context with the User attached.
// This is typically used by authentication middleware before passing
// the context to downstream handlers.
func ContextWithUser(ctx context.Context, user User) context.Context {
	return context.WithValue(ctx, userKey, user)
}

// WithUser is a convenience function that creates a User and attaches it to the context.
func WithUser(ctx context.Context, id, source string, attrs map[string]string) context.Context {
	return ContextWithUser(ctx, User{
		ID:         id,
		Source:     source,
		Attributes: attrs,
	})
}
