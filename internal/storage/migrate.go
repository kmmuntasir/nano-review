package storage

import "database/sql"

const schema = `
CREATE TABLE IF NOT EXISTS reviews (
    run_id         TEXT PRIMARY KEY,
    repo           TEXT NOT NULL,
    pr_number      INTEGER NOT NULL,
    base_branch    TEXT NOT NULL DEFAULT '',
    head_branch    TEXT NOT NULL DEFAULT '',
    status         TEXT NOT NULL DEFAULT 'pending',
    conclusion     TEXT NOT NULL DEFAULT '',
    duration_ms    INTEGER NOT NULL DEFAULT 0,
    attempts       INTEGER NOT NULL DEFAULT 0,
    claude_output  TEXT NOT NULL DEFAULT '',
    created_at     DATETIME NOT NULL DEFAULT (datetime('now')),
    completed_at   DATETIME
);

CREATE INDEX IF NOT EXISTS idx_reviews_repo ON reviews(repo);
CREATE INDEX IF NOT EXISTS idx_reviews_status ON reviews(status);
CREATE INDEX IF NOT EXISTS idx_reviews_created_at ON reviews(created_at DESC);

CREATE TABLE IF NOT EXISTS sessions (
    id            TEXT PRIMARY KEY,
    review_id     TEXT NOT NULL REFERENCES reviews(run_id) ON DELETE CASCADE,
    status        TEXT NOT NULL DEFAULT 'pending',
    executor      TEXT NOT NULL DEFAULT '',
    name          TEXT NOT NULL DEFAULT '',
    context_data  TEXT NOT NULL DEFAULT '',
    created_at    DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at    DATETIME NOT NULL DEFAULT (datetime('now')),
    completed_at  DATETIME
);

CREATE INDEX IF NOT EXISTS idx_sessions_review_id ON sessions(review_id);
CREATE INDEX IF NOT EXISTS idx_sessions_status ON sessions(status);
CREATE INDEX IF NOT EXISTS idx_sessions_executor ON sessions(executor);
CREATE INDEX IF NOT EXISTS idx_sessions_created_at ON sessions(created_at DESC);
`

func migrate(db *sql.DB) error {
	_, err := db.Exec(schema)
	return err
}
