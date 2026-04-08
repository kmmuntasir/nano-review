package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

func (s *sqliteStore) CreateReview(ctx context.Context, r ReviewRecord) error {
	query := `INSERT INTO reviews (run_id, repo, pr_number, base_branch, head_branch, status, created_at)
              VALUES (?, ?, ?, ?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, query,
		r.RunID, r.Repo, r.PRNumber, r.BaseBranch, r.HeadBranch,
		string(StatusPending), r.CreatedAt.UTC().Format(time.RFC3339),
	)
	return err
}

func (s *sqliteStore) UpdateReview(ctx context.Context, runID string, status ReviewStatus, conclusion ReviewConclusion, durationMs int64, attempts int, output string) error {
	query := `UPDATE reviews SET status = ?, conclusion = ?, duration_ms = ?, attempts = ?, claude_output = ?, completed_at = ?
              WHERE run_id = ?`
	var completedAt *string
	if status == StatusCompleted || status == StatusFailed || status == StatusTimedOut || status == StatusCancelled {
		now := time.Now().UTC().Format(time.RFC3339)
		completedAt = &now
	}
	_, err := s.db.ExecContext(ctx, query,
		string(status), string(conclusion), durationMs, attempts, output,
		completedAt, runID,
	)
	return err
}

func (s *sqliteStore) GetReview(ctx context.Context, runID string) (*ReviewRecord, error) {
	query := `SELECT run_id, repo, pr_number, base_branch, head_branch, status, conclusion,
                     duration_ms, attempts, claude_output, created_at, completed_at
              FROM reviews WHERE run_id = ?`
	row := s.db.QueryRowContext(ctx, query, runID)

	var r ReviewRecord
	var status, conclusion, claudeOutput, completedAt sql.NullString

	err := row.Scan(
		&r.RunID, &r.Repo, &r.PRNumber, &r.BaseBranch, &r.HeadBranch,
		&status, &conclusion, &r.DurationMs, &r.Attempts,
		&claudeOutput, &r.CreatedAt, &completedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get review %s: %w", runID, err)
	}

	r.Status = ReviewStatus(status.String)
	r.Conclusion = ReviewConclusion(conclusion.String)
	r.ClaudeOutput = claudeOutput.String
	if completedAt.Valid {
		t, _ := time.Parse(time.RFC3339, completedAt.String)
		r.CompletedAt = &t
	}
	return &r, nil
}

func (s *sqliteStore) ListReviews(ctx context.Context, f ListFilter) ([]ReviewRecord, error) {
	var conditions []string
	var args []any

	if f.Repo != "" {
		conditions = append(conditions, "repo = ?")
		args = append(args, f.Repo)
	}
	if f.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, string(f.Status))
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	limit := 50
	if f.Limit > 0 && f.Limit <= 200 {
		limit = f.Limit
	}

	offset := 0
	if f.Offset > 0 {
		offset = f.Offset
	}

	// Exclude claude_output from list queries for performance.
	query := fmt.Sprintf(
		`SELECT run_id, repo, pr_number, base_branch, head_branch, status, conclusion,
		        duration_ms, attempts, created_at, completed_at
		 FROM reviews %s ORDER BY created_at DESC LIMIT %d OFFSET %d`,
		where, limit, offset,
	)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list reviews: %w", err)
	}
	defer rows.Close()

	var records []ReviewRecord
	for rows.Next() {
		var r ReviewRecord
		var status, conclusion, completedAt sql.NullString
		err := rows.Scan(
			&r.RunID, &r.Repo, &r.PRNumber, &r.BaseBranch, &r.HeadBranch,
			&status, &conclusion, &r.DurationMs, &r.Attempts,
			&r.CreatedAt, &completedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan review row: %w", err)
		}
		r.Status = ReviewStatus(status.String)
		r.Conclusion = ReviewConclusion(conclusion.String)
		if completedAt.Valid {
			t, _ := time.Parse(time.RFC3339, completedAt.String)
			r.CompletedAt = &t
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

func (s *sqliteStore) GetMetrics(ctx context.Context) (*Metrics, error) {
	query := `
        SELECT
            COUNT(*) as total,
            COALESCE(SUM(CASE WHEN conclusion = 'success' THEN 1 ELSE 0 END), 0) as successes,
            COALESCE(SUM(CASE WHEN conclusion = 'failure' THEN 1 ELSE 0 END), 0) as failures,
            COALESCE(SUM(CASE WHEN conclusion = 'timed_out' THEN 1 ELSE 0 END), 0) as timeouts,
            COALESCE(SUM(CASE WHEN conclusion = 'cancelled' THEN 1 ELSE 0 END), 0) as cancelled,
            COALESCE(AVG(CASE WHEN conclusion IN ('success','failure','timed_out','cancelled') THEN duration_ms END), 0) as avg_duration,
            COALESCE(SUM(CASE WHEN date(created_at) = date('now') THEN 1 ELSE 0 END), 0) as today
        FROM reviews
    `

	var m Metrics
	err := s.db.QueryRowContext(ctx, query).Scan(
		&m.TotalReviews, &m.SuccessCount, &m.FailureCount,
		&m.TimedOutCount, &m.CancelledCount, &m.AvgDurationMs, &m.ReviewsToday,
	)
	if err != nil {
		return nil, fmt.Errorf("get metrics: %w", err)
	}
	return &m, nil
}
