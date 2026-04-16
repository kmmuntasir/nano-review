package storage

import (
	"context"
	"time"
)

// SessionStatus represents the current state of an orchestrator session.
type SessionStatus string

const (
	SessionStatusPending   SessionStatus = "pending"
	SessionStatusRunning   SessionStatus = "running"
	SessionStatusCompleted SessionStatus = "completed"
	SessionStatusFailed    SessionStatus = "failed"
	SessionStatusTimedOut  SessionStatus = "timed_out"
	SessionStatusCancelled SessionStatus = "cancelled"
)

// SessionExecutor identifies which coding agent is running the session.
type SessionExecutor string

const (
	ExecutorClaudeCode  SessionExecutor = "claude_code"
	ExecutorAMP         SessionExecutor = "amp"
	ExecutorGemini      SessionExecutor = "gemini"
	ExecutorCodex       SessionExecutor = "codex"
	ExecutorOpenCode    SessionExecutor = "opencode"
	ExecutorCursorAgent SessionExecutor = "cursor_agent"
	ExecutorQwenCode    SessionExecutor = "qwen_code"
	ExecutorCopilot     SessionExecutor = "copilot"
	ExecutorDroid       SessionExecutor = "droid"
)

// SessionRecord represents a persisted orchestrator session for rehydration.
type SessionRecord struct {
	ID          string          `json:"id"`
	ReviewID    string          `json:"review_id"`
	Status      SessionStatus   `json:"status"`
	Executor    SessionExecutor `json:"executor,omitempty"`
	Name        string          `json:"name,omitempty"`
	ContextData string          `json:"context_data,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	CompletedAt *time.Time      `json:"completed_at,omitempty"`
}

// SessionListFilter specifies optional filters for listing sessions.
type SessionListFilter struct {
	ReviewID string
	Status   SessionStatus
	Executor SessionExecutor
	Limit    int
	Offset   int
}

// SessionStore persists orchestrator sessions and provides query access.
type SessionStore interface {
	// CreateSession stores a new session record.
	CreateSession(ctx context.Context, s SessionRecord) error

	// GetSession retrieves a session by ID.
	GetSession(ctx context.Context, id string) (*SessionRecord, error)

	// GetSessionByReviewID retrieves the active session for a review.
	GetSessionByReviewID(ctx context.Context, reviewID string) (*SessionRecord, error)

	// UpdateSession updates session status, context data, and completion timestamp.
	UpdateSession(ctx context.Context, id string, status SessionStatus, contextData string) error

	// ListSessions returns sessions matching the given filters.
	ListSessions(ctx context.Context, f SessionListFilter) ([]SessionRecord, error)

	// DeleteSession removes a session record.
	DeleteSession(ctx context.Context, id string) error

	// DeleteExpiredSessions removes completed/failed/timed_out/cancelled sessions
	// older than the given threshold.
	DeleteExpiredSessions(ctx context.Context, olderThan time.Duration) (int64, error)
}
