package api

import (
	"fmt"

	"github.com/kmmuntasir/nano-review/internal/storage"
)

// ReviewPayload represents the request body for initiating a PR review.
type ReviewPayload struct {
	RepoURL    string `json:"repo_url"`
	PRNumber   int    `json:"pr_number"`
	BaseBranch string `json:"base_branch"`
	HeadBranch string `json:"head_branch"`
}

// AcceptResponse is returned when a review request is accepted for processing.
type AcceptResponse struct {
	Status string `json:"status"`
	RunID  string `json:"run_id"`
}

// ErrorResponse is returned for error conditions.
type ErrorResponse struct {
	Error string `json:"error"`
}

// ValidatePayload checks that all required fields are present and non-zero.
// It returns a wrapped ErrInvalidPayload with context about the first missing field.
func ValidatePayload(p ReviewPayload) error {
	if p.RepoURL == "" {
		return fmt.Errorf("%w: repo_url is required", ErrInvalidPayload)
	}
	if p.PRNumber == 0 {
		return fmt.Errorf("%w: pr_number is required", ErrInvalidPayload)
	}
	if p.BaseBranch == "" {
		return fmt.Errorf("%w: base_branch is required", ErrInvalidPayload)
	}
	if p.HeadBranch == "" {
		return fmt.Errorf("%w: head_branch is required", ErrInvalidPayload)
	}
	return nil
}

// ListReviewsResponse wraps a page of review records.
type ListReviewsResponse struct {
	Reviews []storage.ReviewRecord `json:"reviews"`
	Count   int                    `json:"count"`
}

// StartResult replaces AcceptResponse as the review start response.
// RetryAfter and QueueDepth are set when the review is queued.
type StartResult struct {
	RunID      string `json:"run_id"`
	Status     string `json:"status"`
	RetryAfter int    `json:"retry_after,omitempty"`
	QueueDepth int    `json:"queue_depth,omitempty"`
}

// HealthResponse is returned by GET /health with queue-aware metrics.
type HealthResponse struct {
	Status        string `json:"status"`
	ActiveReviews int32  `json:"active_reviews"`
	QueuedReviews int32  `json:"queued_reviews"`
	MaxConcurrent int    `json:"max_concurrent"`
	MaxQueueSize  int    `json:"max_queue_size"`
	UptimeSeconds int64  `json:"uptime_seconds"`
}
