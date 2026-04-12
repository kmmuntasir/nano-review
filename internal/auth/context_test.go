package auth

import (
	"context"
	"testing"
)

func TestUserFromContext(t *testing.T) {
	tests := []struct {
		name     string
		ctx      context.Context
		wantID   string
		wantSrc  string
		wantAttr map[string]string
	}{
		{
			name:     "no user in context",
			ctx:      context.Background(),
			wantID:   "",
			wantSrc:  "",
			wantAttr: nil,
		},
		{
			name: "user in context",
			ctx: ContextWithUser(context.Background(), User{
				ID:     "webhook-123",
				Source: "webhook",
				Attributes: map[string]string{
					"repo":      "owner/repo",
					"event":     "pull_request",
					"pr_number": "42",
				},
			}),
			wantID:  "webhook-123",
			wantSrc: "webhook",
			wantAttr: map[string]string{
				"repo":      "owner/repo",
				"event":     "pull_request",
				"pr_number": "42",
			},
		},
		{
			name: "user with nil attributes",
			ctx: ContextWithUser(context.Background(), User{
				ID:     "api-token-abc",
				Source: "api_token",
			}),
			wantID:   "api-token-abc",
			wantSrc:  "api_token",
			wantAttr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UserFromContext(tt.ctx)

			if got.ID != tt.wantID {
				t.Errorf("UserFromContext() ID = %q, want %q", got.ID, tt.wantID)
			}
			if got.Source != tt.wantSrc {
				t.Errorf("UserFromContext() Source = %q, want %q", got.Source, tt.wantSrc)
			}

			if tt.wantAttr == nil {
				if got.Attributes != nil {
					t.Errorf("UserFromContext() Attributes = %v, want nil", got.Attributes)
				}
			} else {
				for k, wantV := range tt.wantAttr {
					gotV, ok := got.Attributes[k]
					if !ok {
						t.Errorf("UserFromContext() Attributes missing key %q", k)
					}
					if gotV != wantV {
						t.Errorf("UserFromContext() Attributes[%q] = %q, want %q", k, gotV, wantV)
					}
				}
			}
		})
	}
}

func TestContextWithUser(t *testing.T) {
	u := User{
		ID:     "test-user",
		Source: "test",
		Attributes: map[string]string{
			"key": "value",
		},
	}

	ctx := ContextWithUser(context.Background(), u)

	got := UserFromContext(ctx)
	if got.ID != u.ID {
		t.Errorf("ContextWithUser() got ID %q, want %q", got.ID, u.ID)
	}
	if got.Source != u.Source {
		t.Errorf("ContextWithUser() got Source %q, want %q", got.Source, u.Source)
	}
	if got.Attributes["key"] != "value" {
		t.Errorf("ContextWithUser() got Attributes[key] %q, want %q", got.Attributes["key"], "value")
	}
}

func TestWithUser(t *testing.T) {
	attrs := map[string]string{"foo": "bar"}
	ctx := WithUser(context.Background(), "user-1", "webhook", attrs)

	u := UserFromContext(ctx)
	if u.ID != "user-1" {
		t.Errorf("WithUser() got ID %q, want %q", u.ID, "user-1")
	}
	if u.Source != "webhook" {
		t.Errorf("WithUser() got Source %q, want %q", u.Source, "webhook")
	}
	if u.Attributes["foo"] != "bar" {
		t.Errorf("WithUser() got Attributes[foo] %q, want %q", u.Attributes["foo"], "bar")
	}
}

func TestUserString(t *testing.T) {
	tests := []struct {
		name string
		user User
		want string
	}{
		{
			name: "zero user",
			user: User{},
			want: "user<none>",
		},
		{
			name: "webhook user",
			user: User{ID: "webhook-123", Source: "webhook"},
			want: "user<webhook:webhook-123>",
		},
		{
			name: "api token user",
			user: User{ID: "token-abc", Source: "api_token"},
			want: "user<api_token:token-abc>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.user.String(); got != tt.want {
				t.Errorf("User.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUserLogValue(t *testing.T) {
	u := User{
		ID:     "webhook-123",
		Source: "webhook",
		Attributes: map[string]string{
			"repo": "owner/repo",
		},
	}

	val := u.LogValue()
	// LogValue returns a slog.Value; we can't inspect it directly,
	// but we can verify it doesn't panic and returns a non-zero value.
	if val.Kind() == 0 {
		t.Error("User.LogValue() returned zero value")
	}
}

func TestContextIsolation(t *testing.T) {
	// Verify that modifying the returned context doesn't affect the original.
	u1 := User{ID: "user1", Source: "source1"}
	ctx1 := ContextWithUser(context.Background(), u1)

	u2 := User{ID: "user2", Source: "source2"}
	ctx2 := ContextWithUser(ctx1, u2)

	got1 := UserFromContext(ctx1)
	got2 := UserFromContext(ctx2)

	if got1.ID != "user1" {
		t.Errorf("Original context got ID %q, want %q", got1.ID, "user1")
	}
	if got2.ID != "user2" {
		t.Errorf("Derived context got ID %q, want %q", got2.ID, "user2")
	}
}
