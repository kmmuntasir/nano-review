package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/kmmuntasir/nano-review/internal/storage"
)

// ReviewStarter initiates an asynchronous PR review.
type ReviewStarter interface {
	StartReview(ctx context.Context, p ReviewPayload) (*StartResult, error)
}

// QueueStater provides queue-aware health metrics.
type QueueStater interface {
	Stats() HealthResponse
}

// HandleReview returns an http.HandlerFunc that validates the webhook request
// and initiates an asynchronous PR review.
func HandleReview(secret string, starter ReviewStarter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		if r.Header.Get("X-Webhook-Secret") != secret {
			writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "invalid or missing webhook secret"})
			return
		}

		var payload ReviewPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid JSON body"})
			return
		}

		if err := ValidatePayload(payload); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
			return
		}

		result, err := starter.StartReview(r.Context(), payload)
		if err != nil {
			if errors.Is(err, ErrQueueFull) {
				w.Header().Set("Retry-After", "30")
				writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "review queue full"})
				return
			}
			slog.Error("failed to start review", "error", err)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
			return
		}

		slog.Info("review accepted",
			"run_id", result.RunID,
			"status", result.Status,
			"pr_number", payload.PRNumber,
			"repo", payload.RepoURL,
		)

		if result.Status == "queued" {
			if result.RetryAfter > 0 {
				w.Header().Set("Retry-After", strconv.Itoa(result.RetryAfter))
			}
			writeJSON(w, http.StatusAccepted, result)
			return
		}

		writeJSON(w, http.StatusOK, result)
	}
}

// writeJSON marshals v as JSON and writes it to w with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("failed to write JSON response", "error", err)
	}
}

// ReviewGetter provides read access to review records and metrics.
type ReviewGetter interface {
	GetReview(ctx context.Context, runID string) (*storage.ReviewRecord, error)
	ListReviews(ctx context.Context, f storage.ListFilter) ([]storage.ReviewRecord, error)
	GetMetrics(ctx context.Context) (*storage.Metrics, error)
}

// HandleListReviews returns an http.HandlerFunc that lists reviews with optional filters.
// Query params: repo, status, limit, offset
func HandleListReviews(getter ReviewGetter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f := storage.ListFilter{
			Repo:   r.URL.Query().Get("repo"),
			Status: storage.ReviewStatus(r.URL.Query().Get("status")),
		}
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				f.Limit = n
			}
		}
		if v := r.URL.Query().Get("offset"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				f.Offset = n
			}
		}

		reviews, err := getter.ListReviews(r.Context(), f)
		if err != nil {
			slog.Error("failed to list reviews", "error", err)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
			return
		}

		writeJSON(w, http.StatusOK, ListReviewsResponse{Reviews: reviews, Count: len(reviews)})
	}
}

// HandleGetReview returns an http.HandlerFunc that retrieves a single review by run_id.
func HandleGetReview(getter ReviewGetter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		runID := r.PathValue("run_id")
		if runID == "" {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "run_id is required"})
			return
		}

		review, err := getter.GetReview(r.Context(), runID)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "review not found"})
				return
			}
			slog.Error("failed to get review", "run_id", runID, "error", err)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
			return
		}

		writeJSON(w, http.StatusOK, review)
	}
}

// HandleHealthz returns a simple health check for Docker/orchestrator liveness probes.
// Returns 200 with {"status":"ok"} unconditionally.
func HandleHealthz() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// HandleHealth returns queue-aware health metrics. Reports "degraded" status
// when queued reviews exceed 80% of max queue capacity.
func HandleHealth(qs QueueStater) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stats := qs.Stats()
		if stats.QueuedReviews > int32(float64(stats.MaxQueueSize)*0.8) {
			stats.Status = "degraded"
		}
		writeJSON(w, http.StatusOK, stats)
	}
}

// HandleGetMetrics returns an http.HandlerFunc that returns aggregate review metrics.
func HandleGetMetrics(getter ReviewGetter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		metrics, err := getter.GetMetrics(r.Context())
		if err != nil {
			slog.Error("failed to get metrics", "error", err)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
			return
		}

		writeJSON(w, http.StatusOK, metrics)
	}
}
