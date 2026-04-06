package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type mockReviewStarter struct {
	runID string
	err   error
}

func (m *mockReviewStarter) StartReview(_ context.Context, _ ReviewPayload) (string, error) {
	return m.runID, m.err
}

func TestHandleReview(t *testing.T) {
	const secret = "test-secret"
	validBody := `{"repo_url":"git@github.com:owner/repo.git","pr_number":42,"base_branch":"main","head_branch":"feature/x"}`

	tests := []struct {
		name       string
		method     string
		secret     string
		body       string
		starter    ReviewStarter
		wantStatus int
		wantJSON   string
	}{
		{
			name:       "valid request returns 200 accepted",
			method:     http.MethodPost,
			secret:     secret,
			body:       validBody,
			starter:    &mockReviewStarter{runID: "abc-123"},
			wantStatus: http.StatusOK,
			wantJSON:   `"status":"accepted"`,
		},
		{
			name:       "wrong method returns 405",
			method:     http.MethodGet,
			secret:     secret,
			body:       validBody,
			starter:    &mockReviewStarter{},
			wantStatus: http.StatusMethodNotAllowed,
			wantJSON:   `"error":"method not allowed"`,
		},
		{
			name:       "missing secret returns 401",
			method:     http.MethodPost,
			secret:     "",
			body:       validBody,
			starter:    &mockReviewStarter{},
			wantStatus: http.StatusUnauthorized,
			wantJSON:   `"error":"invalid or missing webhook secret"`,
		},
		{
			name:       "wrong secret returns 401",
			method:     http.MethodPost,
			secret:     "wrong-secret",
			body:       validBody,
			starter:    &mockReviewStarter{},
			wantStatus: http.StatusUnauthorized,
			wantJSON:   `"error":"invalid or missing webhook secret"`,
		},
		{
			name:       "invalid JSON returns 400",
			method:     http.MethodPost,
			secret:     secret,
			body:       `{invalid json}`,
			starter:    &mockReviewStarter{},
			wantStatus: http.StatusBadRequest,
			wantJSON:   `"error":"invalid JSON body"`,
		},
		{
			name:       "missing fields returns 400",
			method:     http.MethodPost,
			secret:     secret,
			body:       `{"repo_url":"git@github.com:owner/repo.git"}`,
			starter:    &mockReviewStarter{},
			wantStatus: http.StatusBadRequest,
			wantJSON:   `"error":`,
		},
		{
			name:       "starter error returns 500",
			method:     http.MethodPost,
			secret:     secret,
			body:       validBody,
			starter:    &mockReviewStarter{err: errors.New("starter failed")},
			wantStatus: http.StatusInternalServerError,
			wantJSON:   `"error":"internal server error"`,
		},
		{
			name:       "empty body returns 400",
			method:     http.MethodPost,
			secret:     secret,
			body:       ``,
			starter:    &mockReviewStarter{},
			wantStatus: http.StatusBadRequest,
			wantJSON:   `"error":"invalid JSON body"`,
		},
		{
			name:       "valid request contains run_id in response",
			method:     http.MethodPost,
			secret:     secret,
			body:       validBody,
			starter:    &mockReviewStarter{runID: "unique-run-id-42"},
			wantStatus: http.StatusOK,
			wantJSON:   `"run_id":"unique-run-id-42"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/review", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			if tt.secret != "" {
				req.Header.Set("X-Webhook-Secret", tt.secret)
			}
			w := httptest.NewRecorder()

			handler := HandleReview(secret, tt.starter)
			handler(w, req)

			resp := w.Result()
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}

			contentType := resp.Header.Get("Content-Type")
			if !strings.Contains(contentType, "application/json") {
				t.Errorf("Content-Type = %q, want application/json", contentType)
			}

			var body map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("failed to decode response body: %v", err)
			}

			encoded, err := json.Marshal(body)
			if err != nil {
				t.Fatalf("failed to re-encode response body: %v", err)
			}

			if !strings.Contains(string(encoded), tt.wantJSON) {
				t.Errorf("response body = %s, want to contain %s", string(encoded), tt.wantJSON)
			}
		})
	}
}

func TestHandleReview_ResponseFields(t *testing.T) {
	const secret = "test-secret"
	validBody := `{"repo_url":"git@github.com:owner/repo.git","pr_number":42,"base_branch":"main","head_branch":"feature/x"}`

	req := httptest.NewRequest(http.MethodPost, "/review", strings.NewReader(validBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Secret", secret)
	w := httptest.NewRecorder()

	handler := HandleReview(secret, &mockReviewStarter{runID: "test-run-123"})
	handler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	var result AcceptResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result.Status != "accepted" {
		t.Errorf("status = %q, want %q", result.Status, "accepted")
	}
	if result.RunID != "test-run-123" {
		t.Errorf("run_id = %q, want %q", result.RunID, "test-run-123")
	}
}
