package storage

import (
	"context"
	"testing"
	"time"
)

func TestCleanupStaleReviews(t *testing.T) {
	store := testDB(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)

	insertReview := func(runID string, status string, completedAt *string) {
		var err error
		if completedAt != nil {
			_, err = store.db.ExecContext(ctx,
				`INSERT INTO reviews (run_id, repo, pr_number, base_branch, head_branch, status, created_at, completed_at)
				 VALUES (?, 'git@github.com:owner/repo.git', 1, 'main', 'feat', ?, ?, ?)`,
				runID, status, now.Format(time.RFC3339), *completedAt)
		} else {
			_, err = store.db.ExecContext(ctx,
				`INSERT INTO reviews (run_id, repo, pr_number, base_branch, head_branch, status, created_at)
				 VALUES (?, 'git@github.com:owner/repo.git', 1, 'main', 'feat', ?, ?)`,
				runID, status, now.Format(time.RFC3339))
		}
		if err != nil {
			t.Fatalf("insert review %s: %v", runID, err)
		}
	}

	completedTime := now.Add(5 * time.Minute).Format(time.RFC3339)

	insertReview("run-running", "running", nil)
	insertReview("run-pending", "pending", nil)
	insertReview("run-queued", "queued", nil)
	insertReview("run-completed", "completed", &completedTime)
	insertReview("run-failed", "failed", &completedTime)

	affected, err := store.CleanupStaleReviews(ctx)
	if err != nil {
		t.Fatalf("CleanupStaleReviews: %v", err)
	}
	if affected != 3 {
		t.Errorf("expected 3 rows affected, got %d", affected)
	}

	assertStatus := func(runID, wantStatus string) {
		var status string
		err := store.db.QueryRowContext(ctx, `SELECT status FROM reviews WHERE run_id = ?`, runID).Scan(&status)
		if err != nil {
			t.Fatalf("query status for %s: %v", runID, err)
		}
		if status != wantStatus {
			t.Errorf("run %s: status = %q, want %q", runID, status, wantStatus)
		}
	}

	assertStatus("run-running", "cancelled")
	assertStatus("run-pending", "cancelled")
	assertStatus("run-queued", "cancelled")
	assertStatus("run-completed", "completed")
	assertStatus("run-failed", "failed")

	affected2, err := store.CleanupStaleReviews(ctx)
	if err != nil {
		t.Fatalf("second CleanupStaleReviews: %v", err)
	}
	if affected2 != 0 {
		t.Errorf("expected 0 rows on idempotent call, got %d", affected2)
	}
}
