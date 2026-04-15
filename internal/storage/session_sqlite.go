package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

func (s *sqliteStore) CreateSession(ctx context.Context, rec SessionRecord) error {
	query := `INSERT INTO sessions (id, review_id, status, executor, name, context_data, created_at, updated_at)
	          VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, query,
		rec.ID, rec.ReviewID, string(rec.Status), string(rec.Executor),
		rec.Name, rec.ContextData,
		rec.CreatedAt.UTC().Format(time.RFC3339),
		rec.UpdatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("create session %s: %w", rec.ID, err)
	}
	return nil
}

func (s *sqliteStore) GetSession(ctx context.Context, id string) (*SessionRecord, error) {
	query := `SELECT id, review_id, status, executor, name, context_data,
	                 created_at, updated_at, completed_at
	          FROM sessions WHERE id = ?`
	row := s.db.QueryRowContext(ctx, query, id)

	var rec SessionRecord
	var status, executor, name, contextData, completedAt sql.NullString

	err := row.Scan(
		&rec.ID, &rec.ReviewID, &status, &executor, &name, &contextData,
		&rec.CreatedAt, &rec.UpdatedAt, &completedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("get session %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get session %s: %w", id, err)
	}

	rec.Status = SessionStatus(status.String)
	rec.Executor = SessionExecutor(executor.String)
	rec.Name = name.String
	rec.ContextData = contextData.String
	if completedAt.Valid {
		t, _ := time.Parse(time.RFC3339, completedAt.String)
		rec.CompletedAt = &t
	}
	return &rec, nil
}

func (s *sqliteStore) GetSessionByReviewID(ctx context.Context, reviewID string) (*SessionRecord, error) {
	query := `SELECT id, review_id, status, executor, name, context_data,
	                 created_at, updated_at, completed_at
	          FROM sessions WHERE review_id = ? ORDER BY created_at DESC LIMIT 1`
	row := s.db.QueryRowContext(ctx, query, reviewID)

	var rec SessionRecord
	var status, executor, name, contextData, completedAt sql.NullString

	err := row.Scan(
		&rec.ID, &rec.ReviewID, &status, &executor, &name, &contextData,
		&rec.CreatedAt, &rec.UpdatedAt, &completedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("get session by review %s: %w", reviewID, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get session by review %s: %w", reviewID, err)
	}

	rec.Status = SessionStatus(status.String)
	rec.Executor = SessionExecutor(executor.String)
	rec.Name = name.String
	rec.ContextData = contextData.String
	if completedAt.Valid {
		t, _ := time.Parse(time.RFC3339, completedAt.String)
		rec.CompletedAt = &t
	}
	return &rec, nil
}

func (s *sqliteStore) UpdateSession(ctx context.Context, id string, status SessionStatus, contextData string) error {
	query := `UPDATE sessions SET status = ?, context_data = ?, updated_at = ?, completed_at = ?
	          WHERE id = ?`
	var completedAt *string
	if status == SessionStatusCompleted || status == SessionStatusFailed ||
		status == SessionStatusTimedOut || status == SessionStatusCancelled {
		now := time.Now().UTC().Format(time.RFC3339)
		completedAt = &now
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx, query,
		string(status), contextData, now, completedAt, id,
	)
	if err != nil {
		return fmt.Errorf("update session %s: %w", id, err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update session %s: %w", id, err)
	}
	if rows == 0 {
		return fmt.Errorf("update session %s: %w", id, ErrNotFound)
	}
	return nil
}

func (s *sqliteStore) ListSessions(ctx context.Context, f SessionListFilter) ([]SessionRecord, error) {
	var conditions []string
	var args []any

	if f.ReviewID != "" {
		conditions = append(conditions, "review_id = ?")
		args = append(args, f.ReviewID)
	}
	if f.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, string(f.Status))
	}
	if f.Executor != "" {
		conditions = append(conditions, "executor = ?")
		args = append(args, string(f.Executor))
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

	// Exclude context_data from list queries for performance.
	query := fmt.Sprintf(
		`SELECT id, review_id, status, executor, name,
		        created_at, updated_at, completed_at
		 FROM sessions %s ORDER BY created_at DESC LIMIT %d OFFSET %d`,
		where, limit, offset,
	)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var records []SessionRecord
	for rows.Next() {
		var rec SessionRecord
		var status, executor, name, completedAt sql.NullString
		err := rows.Scan(
			&rec.ID, &rec.ReviewID, &status, &executor, &name,
			&rec.CreatedAt, &rec.UpdatedAt, &completedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan session row: %w", err)
		}
		rec.Status = SessionStatus(status.String)
		rec.Executor = SessionExecutor(executor.String)
		rec.Name = name.String
		if completedAt.Valid {
			t, _ := time.Parse(time.RFC3339, completedAt.String)
			rec.CompletedAt = &t
		}
		records = append(records, rec)
	}
	return records, rows.Err()
}

func (s *sqliteStore) DeleteSession(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete session %s: %w", id, err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete session %s: %w", id, err)
	}
	if rows == 0 {
		return fmt.Errorf("delete session %s: %w", id, ErrNotFound)
	}
	return nil
}

func (s *sqliteStore) DeleteExpiredSessions(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-olderThan).Format(time.RFC3339)
	query := `DELETE FROM sessions
	          WHERE status IN ('completed', 'failed', 'timed_out', 'cancelled')
	            AND updated_at < ?`
	res, err := s.db.ExecContext(ctx, query, cutoff)
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	return res.RowsAffected()
}
