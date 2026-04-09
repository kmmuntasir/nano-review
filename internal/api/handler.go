package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/kmmuntasir/nano-review/internal/storage"
)

// ReviewStarter initiates an asynchronous PR review.
type ReviewStarter interface {
	StartReview(ctx context.Context, p ReviewPayload) (string, error)
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

		runID, err := starter.StartReview(r.Context(), payload)
		if err != nil {
			slog.Error("failed to start review", "error", err)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
			return
		}

		slog.Info("review accepted",
			"run_id", runID,
			"pr_number", payload.PRNumber,
			"repo", payload.RepoURL,
		)

		writeJSON(w, http.StatusOK, AcceptResponse{Status: "accepted", RunID: runID})
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

// StreamPathGetter provides the path to a review's streaming output file
// and the ability to check review status.
type StreamPathGetter interface {
	GetStreamPath(runID string) (string, bool)
	GetReviewStatus(ctx context.Context, runID string) (storage.ReviewStatus, bool)
}

// isTerminalStatus returns true if the review is in a final state.
func isTerminalStatus(s storage.ReviewStatus) bool {
	switch s {
	case storage.StatusCompleted, storage.StatusFailed, storage.StatusTimedOut, storage.StatusCancelled:
		return true
	}
	return false
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
// It optionally includes streaming output when ?include_stream=true is set.
func HandleGetReview(getter ReviewGetter, pathGetter StreamPathGetter) http.HandlerFunc {
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

		if r.URL.Query().Get("include_stream") == "true" && pathGetter != nil {
			if streamPath, ok := pathGetter.GetStreamPath(runID); ok {
				if data, readErr := os.ReadFile(streamPath); readErr == nil {
					type reviewWithStream struct {
						*storage.ReviewRecord
						StreamingOutput string `json:"streaming_output,omitempty"`
					}
					writeJSON(w, http.StatusOK, reviewWithStream{
						ReviewRecord:    review,
						StreamingOutput: string(data),
					})
					return
				}
			}
		}

		writeJSON(w, http.StatusOK, review)
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

// HandleStreamReview returns an http.HandlerFunc that streams Claude Code output
// via Server-Sent Events (SSE). For in-progress reviews it polls the .stream.json
// file and sends new lines. For completed reviews it sends the full file and closes.
func HandleStreamReview(getter ReviewGetter, pathGetter StreamPathGetter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		runID := r.PathValue("run_id")
		if runID == "" {
			http.Error(w, `{"error":"run_id is required"}`, http.StatusBadRequest)
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")

		ctx := r.Context()

		// For completed reviews with no stream file, send stored output and close.
		status, hasStatus := pathGetter.GetReviewStatus(ctx, runID)
		if !hasStatus {
			writeSSEEvent(w, flusher, "error", `{"error":"review not found"}`)
			return
		}
		if isTerminalStatus(status) {
			streamPath, hasPath := pathGetter.GetStreamPath(runID)
			if hasPath {
				if data, err := os.ReadFile(streamPath); err == nil && len(data) > 0 {
					writeSSEEvent(w, flusher, "output", string(data))
				}
			} else {
				// Old review without stream file — send stored output.
				if review, err := getter.GetReview(ctx, runID); err == nil && review.ClaudeOutput != "" {
					writeSSEEvent(w, flusher, "output", review.ClaudeOutput)
				}
			}
			writeSSEEvent(w, flusher, "done", "")
			return
		}

		// In-progress review: poll the stream file.
		ticker := time.NewTicker(500 * time.Millisecond)
		heartbeat := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		defer heartbeat.Stop()

		var lastOffset int64

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				streamPath, hasPath := pathGetter.GetStreamPath(runID)
				if !hasPath {
					continue
				}

				info, err := os.Stat(streamPath)
				if err != nil {
					continue
				}

				if info.Size() > lastOffset {
					f, err := os.Open(streamPath)
					if err != nil {
						continue
					}
					f.Seek(lastOffset, io.SeekStart)
					newBytes, err := io.ReadAll(f)
					f.Close()
					if err != nil {
						continue
					}
					if len(newBytes) > 0 {
						writeSSEEvent(w, flusher, "output", string(newBytes))
						lastOffset += int64(len(newBytes))
					}
				}

				// Check if the review has finished.
				if currentStatus, ok := pathGetter.GetReviewStatus(ctx, runID); ok && isTerminalStatus(currentStatus) {
					// Small delay to let the final bytes flush.
					time.Sleep(200 * time.Millisecond)
					streamPath, hasPath := pathGetter.GetStreamPath(runID)
					if hasPath {
						f, err := os.Open(streamPath)
						if err == nil {
							f.Seek(lastOffset, io.SeekStart)
							finalBytes, _ := io.ReadAll(f)
							f.Close()
							if len(finalBytes) > 0 {
								writeSSEEvent(w, flusher, "output", string(finalBytes))
							}
						}
					}
					writeSSEEvent(w, flusher, "done", "")
					return
				}

			case <-heartbeat.C:
				writeSSEComment(w, flusher, "keepalive")
			}
		}
	}
}

// writeSSEEvent writes a single SSE event to the writer and flushes.
func writeSSEEvent(w io.Writer, flusher http.Flusher, event, data string) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
	flusher.Flush()
}

// writeSSEComment writes an SSE comment (heartbeat) to the writer and flushes.
func writeSSEComment(w io.Writer, flusher http.Flusher, comment string) {
	fmt.Fprintf(w, ": %s\n\n", comment)
	flusher.Flush()
}
