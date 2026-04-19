package storage

import (
	"context"
	"time"
)

// ReviewStatus represents the current state of a review.
type ReviewStatus string

const (
	StatusPending   ReviewStatus = "pending"
	StatusRunning   ReviewStatus = "running"
	StatusCompleted ReviewStatus = "completed"
	StatusFailed    ReviewStatus = "failed"
	StatusTimedOut  ReviewStatus = "timed_out"
	StatusCancelled ReviewStatus = "cancelled"
)

// ReviewConclusion describes the final outcome of a completed review.
type ReviewConclusion string

const (
	ConclusionSuccess   ReviewConclusion = "success"
	ConclusionFailure   ReviewConclusion = "failure"
	ConclusionTimedOut  ReviewConclusion = "timed_out"
	ConclusionCancelled ReviewConclusion = "cancelled"
)

// ReviewRecord represents a persisted review result.
type ReviewRecord struct {
	RunID        string           `json:"run_id"`
	Repo         string           `json:"repo"`
	PRNumber     int              `json:"pr_number"`
	BaseBranch   string           `json:"base_branch,omitempty"`
	HeadBranch   string           `json:"head_branch,omitempty"`
	Status       ReviewStatus     `json:"status"`
	Conclusion   ReviewConclusion `json:"conclusion,omitempty"`
	DurationMs   int64            `json:"duration_ms"`
	Attempts     int              `json:"attempts"`
	ClaudeOutput string           `json:"claude_output,omitempty"`
	CreatedAt    time.Time        `json:"created_at"`
	CompletedAt  *time.Time       `json:"completed_at,omitempty"`
}

// ListFilter specifies optional filters for listing reviews.
type ListFilter struct {
	Repo     string
	Status   ReviewStatus
	Limit    int
	Offset   int
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}

// ListResult holds a page of reviews with the total count.
type ListResult struct {
	Reviews []ReviewRecord `json:"reviews"`
	Total   int            `json:"total"`
}

// Metrics holds aggregate review statistics.
type Metrics struct {
	TotalReviews   int     `json:"total_reviews"`
	SuccessCount   int     `json:"success_count"`
	FailureCount   int     `json:"failure_count"`
	TimedOutCount  int     `json:"timed_out_count"`
	CancelledCount int     `json:"cancelled_count"`
	AvgDurationMs  float64 `json:"avg_duration_ms"`
	ReviewsToday   int     `json:"reviews_today"`
}

// ReviewStore persists review records and provides query access.
type ReviewStore interface {
	CreateReview(ctx context.Context, r ReviewRecord) error
	UpdateReview(ctx context.Context, runID string, status ReviewStatus, conclusion ReviewConclusion, durationMs int64, attempts int, output string) error
	GetReview(ctx context.Context, runID string) (*ReviewRecord, error)
	ListReviews(ctx context.Context, f ListFilter) (*ListResult, error)
	GetMetrics(ctx context.Context) (*Metrics, error)
	Close() error
}
