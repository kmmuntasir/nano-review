package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
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
