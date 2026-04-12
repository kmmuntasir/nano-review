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

func TestCreateAndGetSession(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()

	createTestReview(t, ctx, store, "review-001")

	now := time.Now().UTC().Truncate(time.Second)
	rec := SessionRecord{
		ID:          "session-001",
		ReviewID:    "review-001",
		Status:      SessionStatusPending,
		Executor:    ExecutorClaudeCode,
		Name:        "initial review",
		ContextData: "{}",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := store.CreateSession(ctx, rec); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	got, err := store.GetSession(ctx, "session-001")
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}

	if got.ID != "session-001" {
		t.Errorf("ID = %q, want %q", got.ID, "session-001")
	}
	if got.ReviewID != "review-001" {
		t.Errorf("ReviewID = %q, want %q", got.ReviewID, "review-001")
	}
	if got.Status != SessionStatusPending {
		t.Errorf("Status = %q, want %q", got.Status, SessionStatusPending)
	}
	if got.Executor != ExecutorClaudeCode {
		t.Errorf("Executor = %q, want %q", got.Executor, ExecutorClaudeCode)
	}
	if got.Name != "initial review" {
		t.Errorf("Name = %q, want %q", got.Name, "initial review")
	}
	if got.ContextData != "{}" {
		t.Errorf("ContextData = %q, want %q", got.ContextData, "{}")
	}
	if got.CompletedAt != nil {
		t.Errorf("CompletedAt should be nil, got %v", got.CompletedAt)
	}
}

func TestGetSession_NotFound(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()

	_, err := store.GetSession(ctx, "nonexistent")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestGetSessionByReviewID(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()

	createTestReview(t, ctx, store, "review-002")

	now := time.Now().UTC().Truncate(time.Second)
	rec := SessionRecord{
		ID:        "session-002",
		ReviewID:  "review-002",
		Status:    SessionStatusRunning,
		Executor:  ExecutorAMP,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := store.CreateSession(ctx, rec); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	got, err := store.GetSessionByReviewID(ctx, "review-002")
	if err != nil {
		t.Fatalf("GetSessionByReviewID failed: %v", err)
	}
	if got.ID != "session-002" {
		t.Errorf("ID = %q, want %q", got.ID, "session-002")
	}
	if got.Executor != ExecutorAMP {
		t.Errorf("Executor = %q, want %q", got.Executor, ExecutorAMP)
	}
}

func TestGetSessionByReviewID_MostRecent(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()

	createTestReview(t, ctx, store, "review-003")
	now := time.Now().UTC().Truncate(time.Second)

	store.CreateSession(ctx, SessionRecord{
		ID: "session-003a", ReviewID: "review-003", Status: SessionStatusPending,
		Executor: ExecutorClaudeCode, CreatedAt: now.Add(-2 * time.Minute), UpdatedAt: now.Add(-2 * time.Minute),
	})
	store.CreateSession(ctx, SessionRecord{
		ID: "session-003b", ReviewID: "review-003", Status: SessionStatusRunning,
		Executor: ExecutorGemini, CreatedAt: now, UpdatedAt: now,
	})

	got, err := store.GetSessionByReviewID(ctx, "review-003")
	if err != nil {
		t.Fatalf("GetSessionByReviewID failed: %v", err)
	}
	if got.ID != "session-003b" {
		t.Errorf("ID = %q, want session-003b (most recent)", got.ID)
	}
}

func TestGetSessionByReviewID_NotFound(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()

	_, err := store.GetSessionByReviewID(ctx, "nonexistent-review")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestUpdateSession(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()

	createTestReview(t, ctx, store, "review-004")

	now := time.Now().UTC().Truncate(time.Second)
	store.CreateSession(ctx, SessionRecord{
		ID: "session-004", ReviewID: "review-004", Status: SessionStatusPending,
		Executor: ExecutorClaudeCode, CreatedAt: now, UpdatedAt: now,
	})

	err := store.UpdateSession(ctx, "session-004", SessionStatusCompleted, `{"output": "done"}`)
	if err != nil {
		t.Fatalf("UpdateSession failed: %v", err)
	}

	got, err := store.GetSession(ctx, "session-004")
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if got.Status != SessionStatusCompleted {
		t.Errorf("Status = %q, want %q", got.Status, SessionStatusCompleted)
	}
	if got.ContextData != `{"output": "done"}` {
		t.Errorf("ContextData = %q, want %q", got.ContextData, `{"output": "done"}`)
	}
	if got.CompletedAt == nil {
		t.Error("CompletedAt is nil, want non-nil for completed status")
	}
}

func TestUpdateSession_NotFound(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()

	err := store.UpdateSession(ctx, "nonexistent", SessionStatusFailed, "error data")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestUpdateSession_SetsCompletedAtForTerminalStatuses(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	terminalStatuses := []SessionStatus{
		SessionStatusCompleted,
		SessionStatusFailed,
		SessionStatusTimedOut,
		SessionStatusCancelled,
	}

	for _, status := range terminalStatuses {
		reviewID := "review-terminal-" + string(status)
		sessionID := "session-terminal-" + string(status)
		createTestReview(t, ctx, store, reviewID)
		store.CreateSession(ctx, SessionRecord{
			ID: sessionID, ReviewID: reviewID, Status: SessionStatusPending,
			Executor: ExecutorClaudeCode, CreatedAt: now, UpdatedAt: now,
		})

		if err := store.UpdateSession(ctx, sessionID, status, ""); err != nil {
			t.Fatalf("UpdateSession %s failed: %v", status, err)
		}

		got, err := store.GetSession(ctx, sessionID)
		if err != nil {
			t.Fatalf("GetSession %s failed: %v", status, err)
		}
		if got.CompletedAt == nil {
			t.Errorf("CompletedAt is nil for status %q, want non-nil", status)
		}
	}

	// Non-terminal status should NOT set completed_at.
	createTestReview(t, ctx, store, "review-running")
	store.CreateSession(ctx, SessionRecord{
		ID: "session-running", ReviewID: "review-running", Status: SessionStatusPending,
		Executor: ExecutorClaudeCode, CreatedAt: now, UpdatedAt: now,
	})

	store.UpdateSession(ctx, "session-running", SessionStatusRunning, "in progress")
	got, err := store.GetSession(ctx, "session-running")
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if got.CompletedAt != nil {
		t.Errorf("CompletedAt should be nil for running status, got %v", got.CompletedAt)
	}
}

func TestListSessions_NoFilter(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	for i := 0; i < 3; i++ {
		reviewID := "review-list-" + string(rune('A'+i))
		createTestReview(t, ctx, store, reviewID)
		if err := store.CreateSession(ctx, SessionRecord{
			ID: "session-list-" + string(rune('A'+i)),
			ReviewID: reviewID,
			Status:   SessionStatusPending,
			Executor: ExecutorClaudeCode,
			CreatedAt: now.Add(time.Duration(i) * time.Minute),
			UpdatedAt: now.Add(time.Duration(i) * time.Minute),
		}); err != nil {
			t.Fatalf("CreateSession %d failed: %v", i, err)
		}
	}

	records, err := store.ListSessions(ctx, SessionListFilter{})
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("len(records) = %d, want 3", len(records))
	}

	// Verify DESC order by created_at (most recent first)
	if records[0].ID != "session-list-C" {
		t.Errorf("first record ID = %q, want session-list-C", records[0].ID)
	}
	// context_data should be excluded from list queries
	if records[0].ContextData != "" {
		t.Errorf("ContextData should be empty in list, got %q", records[0].ContextData)
	}
}

func TestListSessions_FilterByReviewID(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	createTestReview(t, ctx, store, "review-filter-a")
	createTestReview(t, ctx, store, "review-filter-b")

	store.CreateSession(ctx, SessionRecord{ID: "s1", ReviewID: "review-filter-a", Status: SessionStatusPending, Executor: ExecutorClaudeCode, CreatedAt: now, UpdatedAt: now})
	store.CreateSession(ctx, SessionRecord{ID: "s2", ReviewID: "review-filter-b", Status: SessionStatusRunning, Executor: ExecutorAMP, CreatedAt: now, UpdatedAt: now})
	store.CreateSession(ctx, SessionRecord{ID: "s3", ReviewID: "review-filter-a", Status: SessionStatusCompleted, Executor: ExecutorClaudeCode, CreatedAt: now.Add(time.Minute), UpdatedAt: now.Add(time.Minute)})

	records, err := store.ListSessions(ctx, SessionListFilter{ReviewID: "review-filter-a"})
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if len(records) != 2 {
		t.Errorf("len(records) = %d, want 2", len(records))
	}
}

func TestListSessions_FilterByStatus(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	createTestReview(t, ctx, store, "review-sf-1")
	createTestReview(t, ctx, store, "review-sf-2")

	store.CreateSession(ctx, SessionRecord{ID: "sf1", ReviewID: "review-sf-1", Status: SessionStatusPending, Executor: ExecutorClaudeCode, CreatedAt: now, UpdatedAt: now})
	store.CreateSession(ctx, SessionRecord{ID: "sf2", ReviewID: "review-sf-2", Status: SessionStatusRunning, Executor: ExecutorClaudeCode, CreatedAt: now, UpdatedAt: now})
	store.UpdateSession(ctx, "sf2", SessionStatusCompleted, "")

	records, err := store.ListSessions(ctx, SessionListFilter{Status: SessionStatusCompleted})
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if len(records) != 1 {
		t.Errorf("len(records) = %d, want 1", len(records))
	}
	if records[0].ID != "sf2" {
		t.Errorf("ID = %q, want sf2", records[0].ID)
	}
}

func TestListSessions_FilterByExecutor(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	createTestReview(t, ctx, store, "review-ef-1")
	createTestReview(t, ctx, store, "review-ef-2")

	store.CreateSession(ctx, SessionRecord{ID: "ef1", ReviewID: "review-ef-1", Status: SessionStatusPending, Executor: ExecutorClaudeCode, CreatedAt: now, UpdatedAt: now})
	store.CreateSession(ctx, SessionRecord{ID: "ef2", ReviewID: "review-ef-2", Status: SessionStatusPending, Executor: ExecutorGemini, CreatedAt: now, UpdatedAt: now})

	records, err := store.ListSessions(ctx, SessionListFilter{Executor: ExecutorGemini})
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if len(records) != 1 {
		t.Errorf("len(records) = %d, want 1", len(records))
	}
	if records[0].ID != "ef2" {
		t.Errorf("ID = %q, want ef2", records[0].ID)
	}
}

func TestListSessions_Pagination(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	for i := 0; i < 10; i++ {
		reviewID := "review-pg-" + string(rune('A'+i))
		createTestReview(t, ctx, store, reviewID)
		store.CreateSession(ctx, SessionRecord{
			ID: "session-pg-" + string(rune('A'+i)),
			ReviewID: reviewID,
			Status:   SessionStatusPending,
			Executor: ExecutorClaudeCode,
			CreatedAt: now.Add(time.Duration(i) * time.Minute),
			UpdatedAt: now.Add(time.Duration(i) * time.Minute),
		})
	}

	page1, err := store.ListSessions(ctx, SessionListFilter{Limit: 5, Offset: 0})
	if err != nil {
		t.Fatalf("ListSessions page 1 failed: %v", err)
	}
	if len(page1) != 5 {
		t.Errorf("page1 len = %d, want 5", len(page1))
	}

	page2, err := store.ListSessions(ctx, SessionListFilter{Limit: 5, Offset: 5})
	if err != nil {
		t.Fatalf("ListSessions page 2 failed: %v", err)
	}
	if len(page2) != 5 {
		t.Errorf("page2 len = %d, want 5", len(page2))
	}

	if page1[0].ID == page2[0].ID {
		t.Error("pages overlap: same first record")
	}
}

func TestListSessions_DefaultLimit(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	for i := 0; i < 60; i++ {
		reviewID := "review-dl-" + string(rune('A'+i%26)) + string(rune('A'+i/26))
		createTestReview(t, ctx, store, reviewID)
		store.CreateSession(ctx, SessionRecord{
			ID: "session-dl-" + string(rune('A'+i%26)) + string(rune('A'+i/26)),
			ReviewID: reviewID,
			Status:   SessionStatusPending,
			Executor: ExecutorClaudeCode,
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

func TestListSessions_MultipleFilters(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	createTestReview(t, ctx, store, "review-mf-1")
	createTestReview(t, ctx, store, "review-mf-2")

	store.CreateSession(ctx, SessionRecord{ID: "mf1", ReviewID: "review-mf-1", Status: SessionStatusPending, Executor: ExecutorClaudeCode, CreatedAt: now, UpdatedAt: now})
	store.CreateSession(ctx, SessionRecord{ID: "mf2", ReviewID: "review-mf-1", Status: SessionStatusCompleted, Executor: ExecutorClaudeCode, CreatedAt: now.Add(time.Minute), UpdatedAt: now.Add(time.Minute)})
	store.CreateSession(ctx, SessionRecord{ID: "mf3", ReviewID: "review-mf-2", Status: SessionStatusCompleted, Executor: ExecutorGemini, CreatedAt: now.Add(2 * time.Minute), UpdatedAt: now.Add(2 * time.Minute)})

	// Filter by both review_id and status
	records, err := store.ListSessions(ctx, SessionListFilter{
		ReviewID: "review-mf-1",
		Status:   SessionStatusCompleted,
	})
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if len(records) != 1 {
		t.Errorf("len(records) = %d, want 1", len(records))
	}
	if records[0].ID != "mf2" {
		t.Errorf("ID = %q, want mf2", records[0].ID)
	}
}

func TestDeleteSession(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	createTestReview(t, ctx, store, "review-del-1")
	store.CreateSession(ctx, SessionRecord{
		ID: "session-del-1", ReviewID: "review-del-1", Status: SessionStatusPending,
		Executor: ExecutorClaudeCode, CreatedAt: now, UpdatedAt: now,
	})

	if err := store.DeleteSession(ctx, "session-del-1"); err != nil {
		t.Fatalf("DeleteSession failed: %v", err)
	}

	// Verify it's gone
	_, err := store.GetSession(ctx, "session-del-1")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestDeleteSession_NotFound(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()

	err := store.DeleteSession(ctx, "nonexistent")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}
