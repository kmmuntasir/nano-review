package storage

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func testDB(t *testing.T) *sqliteStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestCreateAndGetReview(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()

	now := time.Now().UTC()
	record := ReviewRecord{
		RunID:      "test-run-001",
		Repo:       "git@github.com:owner/repo.git",
		PRNumber:   42,
		BaseBranch: "main",
		HeadBranch: "feature/x",
		CreatedAt:  now,
	}

	if err := store.CreateReview(ctx, record); err != nil {
		t.Fatalf("CreateReview failed: %v", err)
	}

	got, err := store.GetReview(ctx, "test-run-001")
	if err != nil {
		t.Fatalf("GetReview failed: %v", err)
	}

	if got.RunID != "test-run-001" {
		t.Errorf("RunID = %q, want %q", got.RunID, "test-run-001")
	}
	if got.Repo != "git@github.com:owner/repo.git" {
		t.Errorf("Repo = %q, want %q", got.Repo, "git@github.com:owner/repo.git")
	}
	if got.PRNumber != 42 {
		t.Errorf("PRNumber = %d, want %d", got.PRNumber, 42)
	}
	if got.Status != StatusPending {
		t.Errorf("Status = %q, want %q", got.Status, StatusPending)
	}
	if !got.CreatedAt.UTC().Equal(now.UTC()) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, now)
	}
}

func TestGetReview_NotFound(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()

	_, err := store.GetReview(ctx, "nonexistent")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestUpdateReview(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()

	create := ReviewRecord{
		RunID:      "test-run-002",
		Repo:       "git@github.com:owner/repo.git",
		PRNumber:   10,
		BaseBranch: "main",
		HeadBranch: "fix/bug",
		CreatedAt:  time.Now().UTC(),
	}
	if err := store.CreateReview(ctx, create); err != nil {
		t.Fatalf("CreateReview failed: %v", err)
	}

	err := store.UpdateReview(ctx, "test-run-002",
		StatusCompleted, ConclusionSuccess,
		5000, 1, "review output here")
	if err != nil {
		t.Fatalf("UpdateReview failed: %v", err)
	}

	got, err := store.GetReview(ctx, "test-run-002")
	if err != nil {
		t.Fatalf("GetReview failed: %v", err)
	}
	if got.Status != StatusCompleted {
		t.Errorf("Status = %q, want %q", got.Status, StatusCompleted)
	}
	if got.Conclusion != ConclusionSuccess {
		t.Errorf("Conclusion = %q, want %q", got.Conclusion, ConclusionSuccess)
	}
	if got.DurationMs != 5000 {
		t.Errorf("DurationMs = %d, want %d", got.DurationMs, 5000)
	}
	if got.Attempts != 1 {
		t.Errorf("Attempts = %d, want %d", got.Attempts, 1)
	}
	if got.ClaudeOutput != "review output here" {
		t.Errorf("ClaudeOutput = %q, want %q", got.ClaudeOutput, "review output here")
	}
	if got.CompletedAt == nil {
		t.Error("CompletedAt is nil, want non-nil")
	}
}

func TestListReviews_NoFilter(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()
	now := time.Now().UTC()

	for i := 0; i < 3; i++ {
		if err := store.CreateReview(ctx, ReviewRecord{
			RunID:      "list-test-" + string(rune('A'+i)),
			Repo:       "git@github.com:owner/repo.git",
			PRNumber:   i + 1,
			BaseBranch: "main",
			HeadBranch: "feature/x",
			CreatedAt:  now.Add(time.Duration(i) * time.Minute),
		}); err != nil {
			t.Fatalf("CreateReview %d failed: %v", i, err)
		}
	}

	records, err := store.ListReviews(ctx, ListFilter{})
	if err != nil {
		t.Fatalf("ListReviews failed: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("len(records) = %d, want 3", len(records))
	}

	// Verify DESC order by created_at (most recent first)
	if records[0].RunID != "list-test-C" {
		t.Errorf("first record RunID = %q, want list-test-C", records[0].RunID)
	}
}

func TestListReviews_FilterByRepo(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()
	now := time.Now().UTC()

	store.CreateReview(ctx, ReviewRecord{RunID: "r1", Repo: "owner/a.git", PRNumber: 1, BaseBranch: "main", HeadBranch: "f", CreatedAt: now})
	store.CreateReview(ctx, ReviewRecord{RunID: "r2", Repo: "owner/b.git", PRNumber: 2, BaseBranch: "main", HeadBranch: "f", CreatedAt: now})
	store.CreateReview(ctx, ReviewRecord{RunID: "r3", Repo: "owner/a.git", PRNumber: 3, BaseBranch: "main", HeadBranch: "f", CreatedAt: now})

	records, err := store.ListReviews(ctx, ListFilter{Repo: "owner/a.git"})
	if err != nil {
		t.Fatalf("ListReviews failed: %v", err)
	}
	if len(records) != 2 {
		t.Errorf("len(records) = %d, want 2", len(records))
	}
}

func TestListReviews_FilterByStatus(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()
	now := time.Now().UTC()

	store.CreateReview(ctx, ReviewRecord{RunID: "s1", Repo: "a", PRNumber: 1, BaseBranch: "m", HeadBranch: "f", CreatedAt: now})
	store.CreateReview(ctx, ReviewRecord{RunID: "s2", Repo: "a", PRNumber: 2, BaseBranch: "m", HeadBranch: "f", CreatedAt: now})
	store.UpdateReview(ctx, "s2", StatusCompleted, ConclusionSuccess, 100, 1, "")
	store.UpdateReview(ctx, "s1", StatusFailed, ConclusionFailure, 200, 1, "err")

	records, err := store.ListReviews(ctx, ListFilter{Status: StatusFailed})
	if err != nil {
		t.Fatalf("ListReviews failed: %v", err)
	}
	if len(records) != 1 {
		t.Errorf("len(records) = %d, want 1", len(records))
	}
	if records[0].RunID != "s1" {
		t.Errorf("RunID = %q, want s1", records[0].RunID)
	}
}

func TestListReviews_Pagination(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()
	now := time.Now().UTC()

	for i := 0; i < 10; i++ {
		store.CreateReview(ctx, ReviewRecord{
			RunID:      "pg-" + string(rune('A'+i)),
			Repo:       "owner/repo.git",
			PRNumber:   i + 1,
			BaseBranch: "main",
			HeadBranch: "f",
			CreatedAt:  now.Add(time.Duration(i) * time.Minute),
		})
	}

	// Page 1: limit=5, offset=0
	page1, err := store.ListReviews(ctx, ListFilter{Limit: 5, Offset: 0})
	if err != nil {
		t.Fatalf("ListReviews page 1 failed: %v", err)
	}
	if len(page1) != 5 {
		t.Errorf("page1 len = %d, want 5", len(page1))
	}

	// Page 2: limit=5, offset=5
	page2, err := store.ListReviews(ctx, ListFilter{Limit: 5, Offset: 5})
	if err != nil {
		t.Fatalf("ListReviews page 2 failed: %v", err)
	}
	if len(page2) != 5 {
		t.Errorf("page2 len = %d, want 5", len(page2))
	}

	// Verify no overlap
	if page1[0].RunID == page2[0].RunID {
		t.Error("pages overlap: same first record")
	}
}

func TestListReviews_DefaultLimit(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()
	now := time.Now().UTC()

	for i := 0; i < 60; i++ {
		store.CreateReview(ctx, ReviewRecord{
			RunID:      "def-" + string(rune('A'+i%26)) + string(rune('A'+i/26)),
			Repo:       "owner/repo.git",
			PRNumber:   i + 1,
			BaseBranch: "main",
			HeadBranch: "f",
			CreatedAt:  now.Add(time.Duration(i) * time.Minute),
		})
	}

	records, err := store.ListReviews(ctx, ListFilter{})
	if err != nil {
		t.Fatalf("ListReviews failed: %v", err)
	}
	if len(records) != 50 {
		t.Errorf("default limit: len(records) = %d, want 50", len(records))
	}
}

func TestGetMetrics(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// Insert reviews with different conclusions
	store.CreateReview(ctx, ReviewRecord{RunID: "m1", Repo: "a", PRNumber: 1, BaseBranch: "m", HeadBranch: "f", CreatedAt: now})
	store.UpdateReview(ctx, "m1", StatusCompleted, ConclusionSuccess, 5000, 1, "")

	store.CreateReview(ctx, ReviewRecord{RunID: "m2", Repo: "a", PRNumber: 2, BaseBranch: "m", HeadBranch: "f", CreatedAt: now})
	store.UpdateReview(ctx, "m2", StatusFailed, ConclusionFailure, 3000, 2, "err")

	store.CreateReview(ctx, ReviewRecord{RunID: "m3", Repo: "a", PRNumber: 3, BaseBranch: "m", HeadBranch: "f", CreatedAt: now})
	store.UpdateReview(ctx, "m3", StatusTimedOut, ConclusionTimedOut, 10000, 1, "")

	store.CreateReview(ctx, ReviewRecord{RunID: "m4", Repo: "a", PRNumber: 4, BaseBranch: "m", HeadBranch: "f", CreatedAt: now})
	store.UpdateReview(ctx, "m4", StatusCancelled, ConclusionCancelled, 1000, 1, "")

	// One pending review should not count in conclusions
	store.CreateReview(ctx, ReviewRecord{RunID: "m5", Repo: "a", PRNumber: 5, BaseBranch: "m", HeadBranch: "f", CreatedAt: now})

	metrics, err := store.GetMetrics(ctx)
	if err != nil {
		t.Fatalf("GetMetrics failed: %v", err)
	}

	if metrics.TotalReviews != 5 {
		t.Errorf("TotalReviews = %d, want 5", metrics.TotalReviews)
	}
	if metrics.SuccessCount != 1 {
		t.Errorf("SuccessCount = %d, want 1", metrics.SuccessCount)
	}
	if metrics.FailureCount != 1 {
		t.Errorf("FailureCount = %d, want 1", metrics.FailureCount)
	}
	if metrics.TimedOutCount != 1 {
		t.Errorf("TimedOutCount = %d, want 1", metrics.TimedOutCount)
	}
	if metrics.CancelledCount != 1 {
		t.Errorf("CancelledCount = %d, want 1", metrics.CancelledCount)
	}
	// Avg of completed reviews: (5000+3000+10000+1000)/4 = 4750
	if metrics.AvgDurationMs != 4750.0 {
		t.Errorf("AvgDurationMs = %f, want 4750.0", metrics.AvgDurationMs)
	}
	if metrics.ReviewsToday < 5 {
		t.Errorf("ReviewsToday = %d, want >= 5", metrics.ReviewsToday)
	}
}

func TestGetMetrics_EmptyDatabase(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()

	metrics, err := store.GetMetrics(ctx)
	if err != nil {
		t.Fatalf("GetMetrics failed: %v", err)
	}

	if metrics.TotalReviews != 0 {
		t.Errorf("TotalReviews = %d, want 0", metrics.TotalReviews)
	}
	if metrics.AvgDurationMs != 0 {
		t.Errorf("AvgDurationMs = %f, want 0.0", metrics.AvgDurationMs)
	}
}

func TestConcurrentWrites(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			runID := "concurrent-" + string(rune('A'+idx))
			record := ReviewRecord{
				RunID:      runID,
				Repo:       "owner/repo.git",
				PRNumber:   idx + 1,
				BaseBranch: "main",
				HeadBranch: "f",
				CreatedAt:  time.Now().UTC(),
			}
			if err := store.CreateReview(ctx, record); err != nil {
				t.Errorf("CreateReview %d failed: %v", idx, err)
				return
			}
			if err := store.UpdateReview(ctx, runID,
				StatusCompleted, ConclusionSuccess, int64(idx+1)*100, 1, "output"); err != nil {
				t.Errorf("UpdateReview %d failed: %v", idx, err)
			}
		}(i)
	}
	wg.Wait()

	records, err := store.ListReviews(ctx, ListFilter{Limit: 20})
	if err != nil {
		t.Fatalf("ListReviews failed: %v", err)
	}
	if len(records) != 10 {
		t.Errorf("len(records) = %d, want 10", len(records))
	}
}

func TestOpen_CreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "subdir", "nested", "test.db")

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	store.Close()
}

func TestOpen_DefaultPath(t *testing.T) {
	// Pass empty string — should use DefaultDatabasePath.
	// We can't actually write to /app/data in tests, so we just verify
	// that it fails with the expected path-related error, not a parse error.
	store, err := Open("")
	if store != nil {
		store.Close()
	}
	// Expected: either permission error (can't mkdir /app/data) or success
	// Either way, no panic and no nil-pointer dereference.
	if err != nil {
		// Verify it's a directory creation error (expected in CI without /app/data)
		if !isPermissionOrPathError(err) {
			t.Fatalf("unexpected error type: %v", err)
		}
	}
}

func isPermissionOrPathError(err error) bool {
	s := err.Error()
	return contains(s, "permission") || contains(s, "mkdir") || contains(s, "no such file") || contains(s, "access denied")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
