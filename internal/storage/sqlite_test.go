package storage

import (
	"context"
	"os"
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
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestCreateAndGetReview(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
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

	result, err := store.ListReviews(ctx, ListFilter{})
	if err != nil {
		t.Fatalf("ListReviews failed: %v", err)
	}
	if len(result.Reviews) != 3 {
		t.Fatalf("len(result.Reviews) = %d, want 3", len(result.Reviews))
	}
	if result.Total != 3 {
		t.Errorf("total = %d, want 3", result.Total)
	}

	// Verify DESC order by created_at (most recent first)
	if result.Reviews[0].RunID != "list-test-C" {
		t.Errorf("first record RunID = %q, want list-test-C", result.Reviews[0].RunID)
	}
}

func TestListReviews_FilterByRepo(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()
	now := time.Now().UTC()

	_ = store.CreateReview(ctx, ReviewRecord{RunID: "r1", Repo: "owner/a.git", PRNumber: 1, BaseBranch: "main", HeadBranch: "f", CreatedAt: now})
	_ = store.CreateReview(ctx, ReviewRecord{RunID: "r2", Repo: "owner/b.git", PRNumber: 2, BaseBranch: "main", HeadBranch: "f", CreatedAt: now})
	_ = store.CreateReview(ctx, ReviewRecord{RunID: "r3", Repo: "owner/a.git", PRNumber: 3, BaseBranch: "main", HeadBranch: "f", CreatedAt: now})

	result, err := store.ListReviews(ctx, ListFilter{Repo: "owner/a.git"})
	if err != nil {
		t.Fatalf("ListReviews failed: %v", err)
	}
	if len(result.Reviews) != 2 {
		t.Fatalf("len(result.Reviews) = %d, want 2", len(result.Reviews))
	}
	if result.Total != 2 {
		t.Errorf("total = %d, want 2", result.Total)
	}
}

func TestListReviews_FilterByStatus(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()
	now := time.Now().UTC()

	_ = store.CreateReview(ctx, ReviewRecord{RunID: "s1", Repo: "a", PRNumber: 1, BaseBranch: "m", HeadBranch: "f", CreatedAt: now})
	_ = store.CreateReview(ctx, ReviewRecord{RunID: "s2", Repo: "a", PRNumber: 2, BaseBranch: "m", HeadBranch: "f", CreatedAt: now})
	_ = store.UpdateReview(ctx, "s2", StatusCompleted, ConclusionSuccess, 100, 1, "")
	_ = store.UpdateReview(ctx, "s1", StatusFailed, ConclusionFailure, 200, 1, "err")

	result, err := store.ListReviews(ctx, ListFilter{Status: StatusFailed})
	if err != nil {
		t.Fatalf("ListReviews failed: %v", err)
	}
	if len(result.Reviews) != 1 {
		t.Fatalf("len(result.Reviews) = %d, want 1", len(result.Reviews))
	}
	if result.Reviews[0].RunID != "s1" {
		t.Errorf("RunID = %q, want s1", result.Reviews[0].RunID)
	}
}

func TestListReviews_Pagination(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()
	now := time.Now().UTC()

	for i := 0; i < 10; i++ {
		_ = store.CreateReview(ctx, ReviewRecord{
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
	if len(page1.Reviews) != 5 {
		t.Fatalf("page1 len = %d, want 5", len(page1.Reviews))
	}
	if page1.Total != 10 {
		t.Errorf("page1 total = %d, want 10", page1.Total)
	}

	// Page 2: limit=5, offset=5
	page2, err := store.ListReviews(ctx, ListFilter{Limit: 5, Offset: 5})
	if err != nil {
		t.Fatalf("ListReviews page 2 failed: %v", err)
	}
	if len(page2.Reviews) != 5 {
		t.Fatalf("page2 len = %d, want 5", len(page2.Reviews))
	}

	// Verify no overlap
	if page1.Reviews[0].RunID == page2.Reviews[0].RunID {
		t.Error("pages overlap: same first record")
	}
}

func TestListReviews_DefaultLimit(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()
	now := time.Now().UTC()

	for i := 0; i < 60; i++ {
		_ = store.CreateReview(ctx, ReviewRecord{
			RunID:      "def-" + string(rune('A'+i%26)) + string(rune('A'+i/26)),
			Repo:       "owner/repo.git",
			PRNumber:   i + 1,
			BaseBranch: "main",
			HeadBranch: "f",
			CreatedAt:  now.Add(time.Duration(i) * time.Minute),
		})
	}

	result, err := store.ListReviews(ctx, ListFilter{})
	if err != nil {
		t.Fatalf("ListReviews failed: %v", err)
	}
	if len(result.Reviews) != 50 {
		t.Fatalf("default limit: len(result.Reviews) = %d, want 50", len(result.Reviews))
	}
	if result.Total != 60 {
		t.Errorf("default limit: total = %d, want 60", result.Total)
	}
}

func TestListReviews_PageParams(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()
	now := time.Now().UTC()

	for i := 0; i < 12; i++ {
		_ = store.CreateReview(ctx, ReviewRecord{
			RunID:      "pg2-" + string(rune('A'+i)),
			Repo:       "owner/repo.git",
			PRNumber:   i + 1,
			BaseBranch: "main",
			HeadBranch: "f",
			CreatedAt:  now.Add(time.Duration(i) * time.Minute),
		})
	}

	result, err := store.ListReviews(ctx, ListFilter{Page: 2, PageSize: 5})
	if err != nil {
		t.Fatalf("ListReviews failed: %v", err)
	}
	if len(result.Reviews) != 5 {
		t.Fatalf("len(result.Reviews) = %d, want 5", len(result.Reviews))
	}
	if result.Total != 12 {
		t.Errorf("total = %d, want 12", result.Total)
	}
	// Page 2 with PageSize=5 should skip first 5 (most recent by DESC), return next 5
}

func TestListReviews_TotalCountWithFilters(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()
	now := time.Now().UTC()

	_ = store.CreateReview(ctx, ReviewRecord{RunID: "t1", Repo: "owner/a.git", PRNumber: 1, BaseBranch: "m", HeadBranch: "f", CreatedAt: now})
	_ = store.CreateReview(ctx, ReviewRecord{RunID: "t2", Repo: "owner/a.git", PRNumber: 2, BaseBranch: "m", HeadBranch: "f", CreatedAt: now})
	_ = store.UpdateReview(ctx, "t2", StatusCompleted, ConclusionSuccess, 100, 1, "")
	_ = store.CreateReview(ctx, ReviewRecord{RunID: "t3", Repo: "owner/b.git", PRNumber: 3, BaseBranch: "m", HeadBranch: "f", CreatedAt: now})
	_ = store.CreateReview(ctx, ReviewRecord{RunID: "t4", Repo: "owner/a.git", PRNumber: 4, BaseBranch: "m", HeadBranch: "f", CreatedAt: now})
	_ = store.UpdateReview(ctx, "t4", StatusFailed, ConclusionFailure, 200, 1, "err")
	_ = store.CreateReview(ctx, ReviewRecord{RunID: "t5", Repo: "owner/b.git", PRNumber: 5, BaseBranch: "m", HeadBranch: "f", CreatedAt: now})

	// Filter by status=failed
	result, err := store.ListReviews(ctx, ListFilter{Status: StatusFailed})
	if err != nil {
		t.Fatalf("ListReviews failed: %v", err)
	}
	if len(result.Reviews) != 1 {
		t.Fatalf("len(result.Reviews) = %d, want 1", len(result.Reviews))
	}
	if result.Total != 1 {
		t.Errorf("total = %d, want 1 (only failed reviews)", result.Total)
	}
	if result.Reviews[0].RunID != "t4" {
		t.Errorf("RunID = %q, want t4", result.Reviews[0].RunID)
	}

	// Filter by repo with mixed statuses — total should count all matching repo
	repoResult, err := store.ListReviews(ctx, ListFilter{Repo: "owner/a.git"})
	if err != nil {
		t.Fatalf("ListReviews repo filter failed: %v", err)
	}
	if repoResult.Total != 3 {
		t.Errorf("total = %d, want 3 (all owner/a.git reviews)", repoResult.Total)
	}

	// Combine repo + status filter
	combinedResult, err := store.ListReviews(ctx, ListFilter{Repo: "owner/a.git", Status: StatusCompleted})
	if err != nil {
		t.Fatalf("ListReviews combined filter failed: %v", err)
	}
	if combinedResult.Total != 1 {
		t.Errorf("total = %d, want 1 (owner/a.git + completed)", combinedResult.Total)
	}
}

func TestGetMetrics(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// Insert reviews with different conclusions
	_ = store.CreateReview(ctx, ReviewRecord{RunID: "m1", Repo: "a", PRNumber: 1, BaseBranch: "m", HeadBranch: "f", CreatedAt: now})
	_ = store.UpdateReview(ctx, "m1", StatusCompleted, ConclusionSuccess, 5000, 1, "")

	_ = store.CreateReview(ctx, ReviewRecord{RunID: "m2", Repo: "a", PRNumber: 2, BaseBranch: "m", HeadBranch: "f", CreatedAt: now})
	_ = store.UpdateReview(ctx, "m2", StatusFailed, ConclusionFailure, 3000, 2, "err")

	_ = store.CreateReview(ctx, ReviewRecord{RunID: "m3", Repo: "a", PRNumber: 3, BaseBranch: "m", HeadBranch: "f", CreatedAt: now})
	_ = store.UpdateReview(ctx, "m3", StatusTimedOut, ConclusionTimedOut, 10000, 1, "")

	_ = store.CreateReview(ctx, ReviewRecord{RunID: "m4", Repo: "a", PRNumber: 4, BaseBranch: "m", HeadBranch: "f", CreatedAt: now})
	_ = store.UpdateReview(ctx, "m4", StatusCancelled, ConclusionCancelled, 1000, 1, "")

	// One pending review should not count in conclusions
	_ = store.CreateReview(ctx, ReviewRecord{RunID: "m5", Repo: "a", PRNumber: 5, BaseBranch: "m", HeadBranch: "f", CreatedAt: now})

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

	result, err := store.ListReviews(ctx, ListFilter{Limit: 20})
	if err != nil {
		t.Fatalf("ListReviews failed: %v", err)
	}
	if len(result.Reviews) != 10 {
		t.Fatalf("len(result.Reviews) = %d, want 10", len(result.Reviews))
	}
}

func TestOpen_CreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "subdir", "nested", "test.db")

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	_ = store.Close()
}

func TestOpen_DefaultPath(t *testing.T) {
	// Pass empty string — should use defaultDatabasePath().
	// We can't actually write to /app/data in tests, so we just verify
	// that it fails with the expected path-related error, not a parse error.
	store, err := Open("")
	if store != nil {
		_ = store.Close()
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

func TestOpen_NanoDataDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NANO_DATA_DIR", dir)

	store, err := Open("")
	if err != nil {
		t.Fatalf("Open with NANO_DATA_DIR failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Verify the database file was created in the custom directory.
	dbFile := filepath.Join(dir, "reviews.db")
	if _, err := os.Stat(dbFile); err != nil {
		t.Errorf("expected database file at %s: %v", dbFile, err)
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
