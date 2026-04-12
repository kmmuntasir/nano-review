package storage

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Helper to create a review record so sessions can reference it via FK.
func createTestReview(t *testing.T, ctx context.Context, store *sqliteStore, runID string) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	if err := store.CreateReview(ctx, ReviewRecord{
		RunID:      runID,
		Repo:       "git@github.com:owner/repo.git",
		PRNumber:   1,
		BaseBranch: "main",
		HeadBranch: "feature/x",
		CreatedAt:  now,
	}); err != nil {
		t.Fatalf("create test review %s: %v", runID, err)
	}
}

func seedSession(t *testing.T, ctx context.Context, store *sqliteStore, rec SessionRecord) {
	t.Helper()
	if err := store.CreateSession(ctx, rec); err != nil {
		t.Fatalf("CreateSession %s: %v", rec.ID, err)
	}
}

func TestCreateSession(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	tests := []struct {
		name string
		rec  SessionRecord
	}{
		{
			name: "full record with all fields",
			rec: SessionRecord{
				ID: "s-full", ReviewID: "r-full", Status: SessionStatusPending,
				Executor: ExecutorClaudeCode, Name: "initial review",
				ContextData: `{"key":"val"}`, CreatedAt: now, UpdatedAt: now,
			},
		},
		{
			name: "minimal record with only required fields",
			rec: SessionRecord{
				ID: "s-min", ReviewID: "r-min", Status: SessionStatusRunning,
				CreatedAt: now, UpdatedAt: now,
			},
		},
		{
			name: "session with AMP executor",
			rec: SessionRecord{
				ID: "s-amp", ReviewID: "r-amp", Status: SessionStatusPending,
				Executor: ExecutorAMP, CreatedAt: now, UpdatedAt: now,
			},
		},
		{
			name: "session with Gemini executor",
			rec: SessionRecord{
				ID: "s-gem", ReviewID: "r-gem", Status: SessionStatusRunning,
				Executor: ExecutorGemini, Name: "gemini session",
				CreatedAt: now, UpdatedAt: now,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			createTestReview(t, ctx, store, tt.rec.ReviewID)

			if err := store.CreateSession(ctx, tt.rec); err != nil {
				t.Fatalf("CreateSession failed: %v", err)
			}

			got, err := store.GetSession(ctx, tt.rec.ID)
			if err != nil {
				t.Fatalf("GetSession failed: %v", err)
			}

			if got.ID != tt.rec.ID {
				t.Errorf("ID = %q, want %q", got.ID, tt.rec.ID)
			}
			if got.ReviewID != tt.rec.ReviewID {
				t.Errorf("ReviewID = %q, want %q", got.ReviewID, tt.rec.ReviewID)
			}
			if got.Status != tt.rec.Status {
				t.Errorf("Status = %q, want %q", got.Status, tt.rec.Status)
			}
			if got.Executor != tt.rec.Executor {
				t.Errorf("Executor = %q, want %q", got.Executor, tt.rec.Executor)
			}
			if got.Name != tt.rec.Name {
				t.Errorf("Name = %q, want %q", got.Name, tt.rec.Name)
			}
			if got.ContextData != tt.rec.ContextData {
				t.Errorf("ContextData = %q, want %q", got.ContextData, tt.rec.ContextData)
			}
			if got.CompletedAt != nil {
				t.Errorf("CompletedAt should be nil, got %v", got.CompletedAt)
			}
		})
	}
}

func TestGetSession(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	createTestReview(t, ctx, store, "r-get1")
	seedSession(t, ctx, store, SessionRecord{
		ID: "s-get1", ReviewID: "r-get1", Status: SessionStatusPending,
		Executor: ExecutorClaudeCode, Name: "findable",
		ContextData: `{}`, CreatedAt: now, UpdatedAt: now,
	})

	tests := []struct {
		name      string
		id        string
		wantErr   error
		wantID    string
		wantName  string
	}{
		{
			name:     "existing session",
			id:       "s-get1",
			wantID:   "s-get1",
			wantName: "findable",
		},
		{
			name:    "nonexistent session",
			id:      "nonexistent",
			wantErr: ErrNotFound,
		},
		{
			name:    "empty ID",
			id:      "",
			wantErr: ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := store.GetSession(ctx, tt.id)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected %v, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("GetSession failed: %v", err)
			}
			if got.ID != tt.wantID {
				t.Errorf("ID = %q, want %q", got.ID, tt.wantID)
			}
			if got.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", got.Name, tt.wantName)
			}
		})
	}
}

func TestGetSessionByReviewID(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	// Seed: two sessions for same review, one for a different review.
	createTestReview(t, ctx, store, "r-mr")
	seedSession(t, ctx, store, SessionRecord{
		ID: "s-mr-old", ReviewID: "r-mr", Status: SessionStatusPending,
		Executor: ExecutorClaudeCode, CreatedAt: now.Add(-5 * time.Minute), UpdatedAt: now.Add(-5 * time.Minute),
	})
	seedSession(t, ctx, store, SessionRecord{
		ID: "s-mr-new", ReviewID: "r-mr", Status: SessionStatusRunning,
		Executor: ExecutorGemini, CreatedAt: now, UpdatedAt: now,
	})

	tests := []struct {
		name     string
		reviewID string
		wantErr  error
		wantID   string
	}{
		{
			name:     "returns most recent session for review",
			reviewID: "r-mr",
			wantID:   "s-mr-new",
		},
		{
			name:     "nonexistent review",
			reviewID: "nonexistent-review",
			wantErr:  ErrNotFound,
		},
		{
			name:     "review with single session",
			reviewID: "r-mr",
			wantID:   "s-mr-new",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := store.GetSessionByReviewID(ctx, tt.reviewID)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("expected %v, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("GetSessionByReviewID failed: %v", err)
			}
			if got.ID != tt.wantID {
				t.Errorf("ID = %q, want %q", got.ID, tt.wantID)
			}
			if got.Executor != ExecutorGemini {
				t.Errorf("Executor = %q, want %q", got.Executor, ExecutorGemini)
			}
		})
	}
}

func TestUpdateSession(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	tests := []struct {
		name           string
		status         SessionStatus
		contextData    string
		wantCompleted  bool
	}{
		{name: "completed sets completed_at", status: SessionStatusCompleted, contextData: `{"output":"done"}`, wantCompleted: true},
		{name: "failed sets completed_at", status: SessionStatusFailed, contextData: "error details", wantCompleted: true},
		{name: "timed_out sets completed_at", status: SessionStatusTimedOut, contextData: "", wantCompleted: true},
		{name: "cancelled sets completed_at", status: SessionStatusCancelled, contextData: "", wantCompleted: true},
		{name: "running does not set completed_at", status: SessionStatusRunning, contextData: "in progress", wantCompleted: false},
		{name: "pending does not set completed_at", status: SessionStatusPending, contextData: "", wantCompleted: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reviewID := "r-upd-" + tt.name
			sessionID := "s-upd-" + tt.name
			createTestReview(t, ctx, store, reviewID)
			seedSession(t, ctx, store, SessionRecord{
				ID: sessionID, ReviewID: reviewID, Status: SessionStatusPending,
				Executor: ExecutorClaudeCode, CreatedAt: now, UpdatedAt: now,
			})

			if err := store.UpdateSession(ctx, sessionID, tt.status, tt.contextData); err != nil {
				t.Fatalf("UpdateSession failed: %v", err)
			}

			got, err := store.GetSession(ctx, sessionID)
			if err != nil {
				t.Fatalf("GetSession failed: %v", err)
			}
			if got.Status != tt.status {
				t.Errorf("Status = %q, want %q", got.Status, tt.status)
			}
			if got.ContextData != tt.contextData {
				t.Errorf("ContextData = %q, want %q", got.ContextData, tt.contextData)
			}
			if tt.wantCompleted && got.CompletedAt == nil {
				t.Error("CompletedAt is nil, want non-nil")
			}
			if !tt.wantCompleted && got.CompletedAt != nil {
				t.Errorf("CompletedAt should be nil, got %v", got.CompletedAt)
			}
		})
	}
}

func TestUpdateSession_NotFound(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()

	tests := []struct {
		name   string
		id     string
		status SessionStatus
	}{
		{name: "nonexistent ID", id: "nonexistent", status: SessionStatusFailed},
		{name: "empty ID", id: "", status: SessionStatusCompleted},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.UpdateSession(ctx, tt.id, tt.status, "data")
			if !errors.Is(err, ErrNotFound) {
				t.Errorf("expected ErrNotFound, got %v", err)
			}
		})
	}
}

func TestListSessions(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	// Seed: 3 sessions across 2 reviews with mixed statuses/executors.
	createTestReview(t, ctx, store, "r-ls-a")
	createTestReview(t, ctx, store, "r-ls-b")
	seedSession(t, ctx, store, SessionRecord{ID: "ls1", ReviewID: "r-ls-a", Status: SessionStatusPending, Executor: ExecutorClaudeCode, CreatedAt: now, UpdatedAt: now})
	seedSession(t, ctx, store, SessionRecord{ID: "ls2", ReviewID: "r-ls-b", Status: SessionStatusRunning, Executor: ExecutorAMP, CreatedAt: now.Add(time.Minute), UpdatedAt: now.Add(time.Minute)})
	seedSession(t, ctx, store, SessionRecord{ID: "ls3", ReviewID: "r-ls-a", Status: SessionStatusCompleted, Executor: ExecutorClaudeCode, CreatedAt: now.Add(2 * time.Minute), UpdatedAt: now.Add(2 * time.Minute)})

	tests := []struct {
		name       string
		filter     SessionListFilter
		wantCount  int
		wantFirst  string // Expected first ID (DESC order)
		wantNoData bool   // Expect ContextData to be empty (excluded from list)
	}{
		{
			name:      "no filter returns all",
			filter:    SessionListFilter{},
			wantCount: 3,
			wantFirst: "ls3",
		},
		{
			name:      "filter by review ID",
			filter:    SessionListFilter{ReviewID: "r-ls-a"},
			wantCount: 2,
			wantFirst: "ls3",
		},
		{
			name:      "filter by status",
			filter:    SessionListFilter{Status: SessionStatusPending},
			wantCount: 1,
			wantFirst: "ls1",
		},
		{
			name:      "filter by executor",
			filter:    SessionListFilter{Executor: ExecutorAMP},
			wantCount: 1,
			wantFirst: "ls2",
		},
		{
			name:      "filter by review and status combined",
			filter:    SessionListFilter{ReviewID: "r-ls-a", Status: SessionStatusCompleted},
			wantCount: 1,
			wantFirst: "ls3",
		},
		{
			name:      "filter with no matches",
			filter:    SessionListFilter{ReviewID: "nonexistent"},
			wantCount: 0,
		},
		{
			name:       "context_data excluded from list queries",
			filter:     SessionListFilter{},
			wantCount:  3,
			wantFirst:  "ls3",
			wantNoData: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			records, err := store.ListSessions(ctx, tt.filter)
			if err != nil {
				t.Fatalf("ListSessions failed: %v", err)
			}
			if len(records) != tt.wantCount {
				t.Fatalf("len(records) = %d, want %d", len(records), tt.wantCount)
			}
			if tt.wantCount > 0 && records[0].ID != tt.wantFirst {
				t.Errorf("first record ID = %q, want %q", records[0].ID, tt.wantFirst)
			}
			if tt.wantNoData && tt.wantCount > 0 {
				for i, rec := range records {
					if rec.ContextData != "" {
						t.Errorf("records[%d].ContextData should be empty, got %q", i, rec.ContextData)
					}
				}
			}
		})
	}
}

func TestListSessions_Pagination(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	// Seed 10 sessions.
	for i := 0; i < 10; i++ {
		reviewID := "r-pg-" + string(rune('A'+i))
		createTestReview(t, ctx, store, reviewID)
		seedSession(t, ctx, store, SessionRecord{
			ID: "s-pg-" + string(rune('A'+i)), ReviewID: reviewID,
			Status: SessionStatusPending, Executor: ExecutorClaudeCode,
			CreatedAt: now.Add(time.Duration(i) * time.Minute),
			UpdatedAt: now.Add(time.Duration(i) * time.Minute),
		})
	}

	tests := []struct {
		name      string
		filter    SessionListFilter
		wantCount int
	}{
		{name: "page 1: limit 5 offset 0", filter: SessionListFilter{Limit: 5, Offset: 0}, wantCount: 5},
		{name: "page 2: limit 5 offset 5", filter: SessionListFilter{Limit: 5, Offset: 5}, wantCount: 5},
		{name: "limit 3 offset 7", filter: SessionListFilter{Limit: 3, Offset: 7}, wantCount: 3},
		{name: "offset beyond data", filter: SessionListFilter{Limit: 5, Offset: 15}, wantCount: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			records, err := store.ListSessions(ctx, tt.filter)
			if err != nil {
				t.Fatalf("ListSessions failed: %v", err)
			}
			if len(records) != tt.wantCount {
				t.Errorf("len(records) = %d, want %d", len(records), tt.wantCount)
			}
		})
	}
}

func TestListSessions_DefaultLimit(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	// Seed 60 sessions.
	for i := 0; i < 60; i++ {
		reviewID := "r-dl-" + string(rune('A'+i%26)) + string(rune('A'+i/26))
		createTestReview(t, ctx, store, reviewID)
		seedSession(t, ctx, store, SessionRecord{
			ID: "s-dl-" + string(rune('A'+i%26)) + string(rune('A'+i/26)), ReviewID: reviewID,
			Status: SessionStatusPending, Executor: ExecutorClaudeCode,
			CreatedAt: now.Add(time.Duration(i) * time.Minute),
			UpdatedAt: now.Add(time.Duration(i) * time.Minute),
		})
	}

	records, err := store.ListSessions(ctx, SessionListFilter{})
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if len(records) != 50 {
		t.Errorf("default limit: len(records) = %d, want 50", len(records))
	}
}

func TestDeleteSession(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	createTestReview(t, ctx, store, "r-del")
	seedSession(t, ctx, store, SessionRecord{
		ID: "s-del", ReviewID: "r-del", Status: SessionStatusPending,
		Executor: ExecutorClaudeCode, CreatedAt: now, UpdatedAt: now,
	})

	tests := []struct {
		name    string
		id      string
		wantErr error
	}{
		{
			name: "existing session is deleted",
			id:   "s-del",
		},
		{
			name:    "nonexistent session",
			id:      "nonexistent",
			wantErr: ErrNotFound,
		},
		{
			name:    "empty ID",
			id:      "",
			wantErr: ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.DeleteSession(ctx, tt.id)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("expected %v, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("DeleteSession failed: %v", err)
			}
			// Verify deletion.
			_, err = store.GetSession(ctx, tt.id)
			if !errors.Is(err, ErrNotFound) {
				t.Errorf("expected ErrNotFound after delete, got %v", err)
			}
		})
	}
}
