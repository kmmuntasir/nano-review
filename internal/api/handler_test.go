package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kmmuntasir/nano-review/internal/storage"
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

// ---------------------------------------------------------------------------
// Mocks for review history handlers
// ---------------------------------------------------------------------------

type mockReviewGetter struct {
	review  *storage.ReviewRecord
	reviews []storage.ReviewRecord
	metrics *storage.Metrics
	err     error
}

func (m *mockReviewGetter) GetReview(_ context.Context, runID string) (*storage.ReviewRecord, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.review != nil && m.review.RunID == runID {
		return m.review, nil
	}
	return nil, storage.ErrNotFound
}

func (m *mockReviewGetter) ListReviews(_ context.Context, _ storage.ListFilter) ([]storage.ReviewRecord, error) {
	return m.reviews, m.err
}

func (m *mockReviewGetter) GetMetrics(_ context.Context) (*storage.Metrics, error) {
	return m.metrics, m.err
}

// ---------------------------------------------------------------------------
// Mock StreamPathGetter
// ---------------------------------------------------------------------------

type mockStreamPathGetter struct {
	streamPath string
	status     storage.ReviewStatus
	hasPath    bool
	hasStatus  bool
}

func (m *mockStreamPathGetter) GetStreamPath(_ string) (string, bool) {
	return m.streamPath, m.hasPath
}

func (m *mockStreamPathGetter) GetReviewStatus(_ context.Context, _ string) (storage.ReviewStatus, bool) {
	return m.status, m.hasStatus
}

// ---------------------------------------------------------------------------
// HandleListReviews tests
// ---------------------------------------------------------------------------

func TestHandleListReviews_Success(t *testing.T) {
	getter := &mockReviewGetter{
		reviews: []storage.ReviewRecord{
			{RunID: "r1", Repo: "owner/repo.git", PRNumber: 1, Status: storage.StatusCompleted, Conclusion: storage.ConclusionSuccess, CreatedAt: time.Now()},
			{RunID: "r2", Repo: "owner/repo.git", PRNumber: 2, Status: storage.StatusPending, CreatedAt: time.Now()},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/reviews", nil)
	w := httptest.NewRecorder()

	HandleListReviews(getter)(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var result ListReviewsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if result.Count != 2 {
		t.Errorf("count = %d, want 2", result.Count)
	}
}

func TestHandleListReviews_StorageError(t *testing.T) {
	getter := &mockReviewGetter{
		reviews: nil,
		err:     errors.New("db error"),
	}

	req := httptest.NewRequest(http.MethodGet, "/reviews", nil)
	w := httptest.NewRecorder()

	HandleListReviews(getter)(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
	}
}

// ---------------------------------------------------------------------------
// HandleGetReview tests
// ---------------------------------------------------------------------------

func TestHandleGetReview_Success(t *testing.T) {
	getter := &mockReviewGetter{
		review: &storage.ReviewRecord{
			RunID:      "abc-123",
			Repo:       "owner/repo.git",
			PRNumber:   42,
			Status:     storage.StatusCompleted,
			Conclusion: storage.ConclusionSuccess,
			DurationMs: 5000,
			CreatedAt:  time.Now(),
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /reviews/{run_id}", HandleGetReview(getter, nil))

	req := httptest.NewRequest(http.MethodGet, "/reviews/abc-123", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var result storage.ReviewRecord
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if result.RunID != "abc-123" {
		t.Errorf("run_id = %q, want %q", result.RunID, "abc-123")
	}
}

func TestHandleGetReview_NotFound(t *testing.T) {
	getter := &mockReviewGetter{}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /reviews/{run_id}", HandleGetReview(getter, nil))

	req := httptest.NewRequest(http.MethodGet, "/reviews/nonexistent", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestHandleGetReview_StorageError(t *testing.T) {
	getter := &mockReviewGetter{
		err: errors.New("db error"),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /reviews/{run_id}", HandleGetReview(getter, nil))

	req := httptest.NewRequest(http.MethodGet, "/reviews/abc-123", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
	}
}

// ---------------------------------------------------------------------------
// HandleGetMetrics tests
// ---------------------------------------------------------------------------

func TestHandleGetMetrics_Success(t *testing.T) {
	getter := &mockReviewGetter{
		metrics: &storage.Metrics{
			TotalReviews:   10,
			SuccessCount:   8,
			FailureCount:   1,
			TimedOutCount:  1,
			CancelledCount: 0,
			AvgDurationMs:  4500.0,
			ReviewsToday:   3,
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()

	HandleGetMetrics(getter)(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var result storage.Metrics
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if result.TotalReviews != 10 {
		t.Errorf("total_reviews = %d, want 10", result.TotalReviews)
	}
	if result.SuccessCount != 8 {
		t.Errorf("success_count = %d, want 8", result.SuccessCount)
	}
}

func TestHandleGetMetrics_StorageError(t *testing.T) {
	getter := &mockReviewGetter{
		metrics: nil,
		err:     errors.New("db error"),
	}

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()

	HandleGetMetrics(getter)(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
	}
}

// ---------------------------------------------------------------------------
// HandleStreamReview tests
// ---------------------------------------------------------------------------

func TestHandleStreamReview_CompletedReviewWithStreamFile(t *testing.T) {
	streamFile := filepath.Join(t.TempDir(), "test.stream.json")
	if err := os.WriteFile(streamFile, []byte(`{"type":"message","content":"hello"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	getter := &mockReviewGetter{
		review: &storage.ReviewRecord{
			RunID:  "abc-123",
			Status: storage.StatusCompleted,
		},
	}
	pathGetter := &mockStreamPathGetter{
		streamPath: streamFile,
		status:     storage.StatusCompleted,
		hasPath:    true,
		hasStatus:  true,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /reviews/{run_id}/stream", HandleStreamReview(getter, pathGetter))

	server := httptest.NewServer(mux)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/reviews/abc-123/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	bodyStr := string(body)

	if !strings.Contains(bodyStr, "event: output") {
		t.Errorf("expected 'output' event, got: %s", bodyStr)
	}
	if !strings.Contains(bodyStr, "event: done") {
		t.Errorf("expected 'done' event, got: %s", bodyStr)
	}
	if !strings.Contains(bodyStr, `{"type":"message","content":"hello"}`) {
		t.Errorf("expected stream file content, got: %s", bodyStr)
	}
}

func TestHandleStreamReview_CompletedReviewNoStreamFile(t *testing.T) {
	getter := &mockReviewGetter{
		review: &storage.ReviewRecord{
			RunID:        "abc-123",
			Status:       storage.StatusCompleted,
			ClaudeOutput: "stored output text",
		},
	}
	pathGetter := &mockStreamPathGetter{
		status:    storage.StatusCompleted,
		hasPath:   false,
		hasStatus: true,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /reviews/{run_id}/stream", HandleStreamReview(getter, pathGetter))

	server := httptest.NewServer(mux)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/reviews/abc-123/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	if !strings.Contains(bodyStr, "stored output text") {
		t.Errorf("expected stored output for review without stream file, got: %s", bodyStr)
	}
	if !strings.Contains(bodyStr, "event: done") {
		t.Errorf("expected 'done' event, got: %s", bodyStr)
	}
}

func TestHandleStreamReview_ReviewNotFound(t *testing.T) {
	getter := &mockReviewGetter{}
	pathGetter := &mockStreamPathGetter{
		hasPath:   false,
		hasStatus: false,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /reviews/{run_id}/stream", HandleStreamReview(getter, pathGetter))

	server := httptest.NewServer(mux)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/reviews/nonexistent/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "event: error") {
		t.Errorf("expected 'error' event for unknown review, got: %s", string(body))
	}
}

func TestHandleGetReview_IncludeStream(t *testing.T) {
	streamFile := filepath.Join(t.TempDir(), "test.stream.json")
	if err := os.WriteFile(streamFile, []byte(`{"type":"result"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	getter := &mockReviewGetter{
		review: &storage.ReviewRecord{
			RunID:      "abc-123",
			Repo:       "owner/repo.git",
			PRNumber:   42,
			Status:     storage.StatusCompleted,
			CreatedAt:  time.Now(),
		},
	}
	pathGetter := &mockStreamPathGetter{
		streamPath: streamFile,
		hasPath:    true,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /reviews/{run_id}", HandleGetReview(getter, pathGetter))

	req := httptest.NewRequest(http.MethodGet, "/reviews/abc-123?include_stream=true", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result["streaming_output"] == nil {
		t.Error("expected streaming_output field to be present")
	}
}
