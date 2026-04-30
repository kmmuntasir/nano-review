package reviewer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/kmmuntasir/nano-review/internal/api"
	"github.com/kmmuntasir/nano-review/internal/storage"
)

// DefaultMaxRetries is the maximum number of retry attempts for transient failures.
const DefaultMaxRetries = 2

// transientExitCodes are Claude Code CLI exit codes that indicate transient failures
// worth retrying (rate limits, overloaded API, etc.).
var transientExitCodes = map[int]bool{
	// Claude Code CLI uses exit code 2 for rate-limit / overload errors.
	2: true,
}

// ClaudeRunner abstracts execution of the Claude Code CLI for testability.
type ClaudeRunner interface {
	Run(ctx context.Context, dir string, args ...string) (output string, exitCode int, err error)
	RunStreaming(ctx context.Context, dir string, streamWriter io.Writer, args ...string) (exitCode int, err error)
}

// Logger provides structured logging capabilities.
type Logger interface {
	Info(msg string, keysAndValues ...any)
	Error(msg string, keysAndValues ...any)
	With(keysAndValues ...any) Logger
}

// DefaultMaxReviewDuration is the maximum time allowed for a single review.
const DefaultMaxReviewDuration = 10 * time.Minute

// Worker handles the lifecycle of a single PR review: clone, run Claude, cleanup.
type Worker struct {
	claude            ClaudeRunner
	store             storage.ReviewStore
	logger            Logger
	broadcaster       Broadcaster
	gitPath           string
	claudePath        string
	model             string
	mcpConfigPath     string
	githubPat         string
	maxReviewDuration time.Duration
	maxRetries        int
	skillsDir         string
	streamPaths       sync.Map // runID (string) -> .stream.json absolute path (string)
	reviewOutputDir   string
}

// NewWorker creates a new review Worker with the given dependencies.
// If store is nil, review records are not persisted (useful for testing).
// If broadcaster is nil, no WebSocket events are sent.
func NewWorker(claude ClaudeRunner, store storage.ReviewStore, logger Logger, broadcaster Broadcaster, gitPath, claudePath, model, mcpConfigPath string, githubPat string, maxReviewDuration time.Duration, maxRetries int, skillsDir string, reviewOutputDir string) *Worker {
	if maxReviewDuration <= 0 {
		maxReviewDuration = DefaultMaxReviewDuration
	}
	if maxRetries < 0 {
		maxRetries = 0
	}
	if reviewOutputDir == "" {
		reviewOutputDir = "/app/logs/reviews"
	}
	return &Worker{
		claude:            claude,
		store:             store,
		logger:            logger,
		broadcaster:       broadcaster,
		gitPath:           gitPath,
		claudePath:        claudePath,
		model:             model,
		mcpConfigPath:     mcpConfigPath,
		githubPat:         githubPat,
		maxReviewDuration: maxReviewDuration,
		maxRetries:        maxRetries,
		skillsDir:         skillsDir,
		reviewOutputDir:   reviewOutputDir,
	}
}

// GetStreamPath returns the path to the .stream.json file for a review run.
func (w *Worker) GetStreamPath(runID string) (string, bool) {
	v, ok := w.streamPaths.Load(runID)
	if !ok {
		return "", false
	}
	return v.(string), true
}

// GetReviewStatus returns the current status of a review run.
func (w *Worker) GetReviewStatus(ctx context.Context, runID string) (storage.ReviewStatus, bool) {
	if w.store == nil {
		return "", false
	}
	r, err := w.store.GetReview(ctx, runID)
	if err != nil {
		return "", false
	}
	return r.Status, true
}

// StartReview validates the payload, generates a unique run ID, and launches
// the review asynchronously. It returns the run ID immediately without blocking.
func (w *Worker) StartReview(_ context.Context, p api.ReviewPayload) (*api.StartResult, error) {
	runID := uuid.New().String()
	go w.processReview(context.Background(), runID, p)
	return &api.StartResult{RunID: runID, Status: "accepted"}, nil
}

// processReview is the async goroutine that clones the repo, runs Claude Code
// CLI, and ensures cleanup regardless of success or failure.
func (w *Worker) processReview(ctx context.Context, runID string, p api.ReviewPayload) {
	logger := w.logger.With("run_id", runID, "pr_number", p.PRNumber, "repo", p.RepoURL)

	reviewCtx, cancel := context.WithTimeout(ctx, w.maxReviewDuration)
	defer cancel()

	logger.Info("review started")
	startTime := time.Now()

	// Record review as pending in storage
	if w.store != nil {
		if err := w.store.CreateReview(ctx, storage.ReviewRecord{
			RunID:      runID,
			Repo:       p.RepoURL,
			PRNumber:   p.PRNumber,
			BaseBranch: p.BaseBranch,
			HeadBranch: p.HeadBranch,
			CreatedAt:  startTime,
		}); err != nil {
			logger.Error("failed to create review record", "error", err)
		}
	}

	dir, err := os.MkdirTemp("", "nano-review-*")
	if err != nil {
		logger.Error("failed to create temp directory", "error", err)
		w.recordResult(ctx, runID, startTime, storage.StatusFailed, storage.ConclusionFailure, 0, 0, "")
		return
	}
	defer func() { _ = os.RemoveAll(dir) }()

	owner, repo := parseRepoURL(p.RepoURL)
	repoDir := filepath.Join(dir, repo)

	logger.Info("git clone started")
	err = w.cloneRepo(reviewCtx, p, repoDir)
	if err != nil {
		logger.Error("git clone failed", "error", err)
		w.recordResult(ctx, runID, startTime, storage.StatusFailed, storage.ConclusionFailure, 0, 0, "")
		return
	}
	logger.Info("git clone completed")

	// Install skills into the temp working directory so Claude Code discovers them.
	// Claude discovers skills from <cwd>/.claude/skills/, not from ~/.claude/skills/.
	if w.skillsDir != "" {
		if err := w.installSkills(dir); err != nil {
			logger.Error("failed to install skills", "error", err)
		} else {
			logger.Info("skills installed", "source", w.skillsDir, "dest", filepath.Join(dir, ".claude", "skills"))
		}
	} else {
		logger.Info("no skills directory configured, skill commands will not be available")
	}

	// Update to running
	w.recordResult(ctx, runID, startTime, storage.StatusRunning, "", 0, 0, "")

	logger.Info("claude execution started")

	prompt := fmt.Sprintf("/pr-review Review pull request #%d in %s/%s (base: %s, head: %s). The repo is cloned at %s/", p.PRNumber, owner, repo, p.BaseBranch, p.HeadBranch, repoDir)

	args := []string{w.claudePath, "-p", prompt, "--dangerously-skip-permissions",
		"--output-format", "stream-json", "--verbose", "--include-partial-messages"}
	if w.model != "" {
		args = append(args, "--model", w.model)
	}
	if w.mcpConfigPath != "" {
		args = append(args, "--mcp-config", w.mcpConfigPath, "--strict-mcp-config")
	}

	// Set up streaming output file so the WebSocket hub can push to subscribers.
	streamPath := streamFilePath(runID, p, w.reviewOutputDir)
	w.streamPaths.Store(runID, streamPath)

	var output string
	var exitCode int

	for attempt := range w.maxRetries + 1 {
		accum, accumErr := newStreamAccumulator(streamPath)
		if accumErr != nil {
			logger.Error("failed to create stream accumulator, using non-streaming mode", "error", accumErr)
			output, exitCode, err = w.claude.Run(reviewCtx, dir, args...)
		} else {
			defer func() { _ = accum.Close() }()
			writer := io.Writer(accum)
			if w.broadcaster != nil {
				ws := newWSStreamWriter(accum, w.broadcaster, runID)
				defer func() { _ = ws.Close() }()
				writer = ws
			}
			exitCode, err = w.claude.RunStreaming(reviewCtx, dir, writer, args...)
			output = accum.Text()
		}

		// Always persist output for debugging, even on retry
		if saveErr := w.saveReviewOutput(runID, p, output); saveErr != nil {
			logger.Error("failed to save review output", "error", saveErr)
		}

		if err == nil && exitCode == 0 {
			logger.Info("claude execution completed", "exit_code", exitCode)
			logger.Info("review completed")
			w.recordResult(ctx, runID, startTime, storage.StatusCompleted, storage.ConclusionSuccess,
				time.Since(startTime).Milliseconds(), attempt+1, output)
			return
		}

		// Context cancelled or timed out — never retry
		if reviewCtx.Err() != nil {
			if errors.Is(reviewCtx.Err(), context.DeadlineExceeded) {
				logger.Error("review timed out", "duration", w.maxReviewDuration)
				w.recordResult(ctx, runID, startTime, storage.StatusTimedOut, storage.ConclusionTimedOut,
					time.Since(startTime).Milliseconds(), attempt+1, output)
			} else {
				logger.Error("review cancelled", "error", reviewCtx.Err())
				w.recordResult(ctx, runID, startTime, storage.StatusCancelled, storage.ConclusionCancelled,
					time.Since(startTime).Milliseconds(), attempt+1, output)
			}
			return
		}

		// Check if the failure is transient and retries remain
		if !isTransientError(err, exitCode, output) {
			logger.Error("claude execution failed", "exit_code", exitCode, "error", err)
			w.recordResult(ctx, runID, startTime, storage.StatusFailed, storage.ConclusionFailure,
				time.Since(startTime).Milliseconds(), attempt+1, output)
			return
		}

		// Transient failure — retry if attempts remain
		if attempt < w.maxRetries {
			backoff := time.Duration(1<<uint(attempt)) * time.Second
			logger.Info("transient failure, retrying",
				"attempt", attempt+1,
				"max_retries", w.maxRetries,
				"backoff", backoff,
				"exit_code", exitCode,
				"error", err,
			)
			timer := time.NewTimer(backoff)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-reviewCtx.Done():
				logger.Error("review cancelled during retry backoff")
				w.recordResult(ctx, runID, startTime, storage.StatusCancelled, storage.ConclusionCancelled,
					time.Since(startTime).Milliseconds(), attempt+1, output)
				return
			}
			continue
		}

		logger.Error("claude execution failed after all retries",
			"attempts", w.maxRetries+1,
			"exit_code", exitCode,
			"error", err,
		)
		w.recordResult(ctx, runID, startTime, storage.StatusFailed, storage.ConclusionFailure,
			time.Since(startTime).Milliseconds(), w.maxRetries+1, output)
		return
	}
}

// recordResult updates the review record in storage. No-op if store is nil.
// Storage errors are logged but never abort the review.
func (w *Worker) recordResult(ctx context.Context, runID string, startTime time.Time, status storage.ReviewStatus, conclusion storage.ReviewConclusion, durationMs int64, attempts int, output string) {
	if w.store != nil {
		if err := w.store.UpdateReview(ctx, runID, status, conclusion, durationMs, attempts, output); err != nil {
			w.logger.Error("failed to update review record",
				"run_id", runID,
				"status", status,
				"error", err,
			)
		}
	}
	w.broadcastReviewUpdate(ctx, runID, status, string(conclusion), durationMs)
}

// broadcastReviewUpdate sends a review_update event to all WebSocket subscribers.
// When the store is available, the full review record is included in the message
// so that subscribers on the "all" topic can update their UI without an extra API call.
func (w *Worker) broadcastReviewUpdate(ctx context.Context, runID string, status storage.ReviewStatus, conclusion string, durationMs int64) {
	if w.broadcaster == nil {
		return
	}

	msgData := map[string]any{
		"type":        "review_update",
		"run_id":      runID,
		"status":      string(status),
		"conclusion":  conclusion,
		"duration_ms": durationMs,
	}

	// Include full review record so the frontend doesn't need a separate fetch.
	if w.store != nil {
		if review, err := w.store.GetReview(ctx, runID); err == nil && review != nil {
			msgData["review"] = map[string]any{
				"run_id":       review.RunID,
				"repo":         review.Repo,
				"pr_number":    review.PRNumber,
				"base_branch":  review.BaseBranch,
				"head_branch":  review.HeadBranch,
				"status":       string(review.Status),
				"conclusion":   string(review.Conclusion),
				"duration_ms":  review.DurationMs,
				"attempts":     review.Attempts,
				"created_at":   review.CreatedAt.Format(time.RFC3339),
				"completed_at": formatCompletedAt(review.CompletedAt),
			}
		}
	}

	msg, err := json.Marshal(msgData)
	if err != nil {
		return
	}
	w.broadcaster.Broadcast("run:"+runID, msg)
	w.broadcaster.Broadcast("all", msg)

	// Broadcast stream_done for terminal statuses.
	if isTerminal(status) {
		doneMsg, _ := json.Marshal(map[string]string{
			"type":   "stream_done",
			"run_id": runID,
		})
		w.broadcaster.Broadcast("run:"+runID, doneMsg)
	}

	// For terminal statuses, broadcast updated metrics.
	if isTerminal(status) && w.store != nil {
		metrics := w.calculateMetrics(ctx)
		if metrics != nil {
			metricsMsg, err := json.Marshal(map[string]any{
				"type":    "metrics_update",
				"metrics": metrics,
			})
			if err == nil {
				w.broadcaster.Broadcast("all", metricsMsg)
			}
		}
	}
}

// BroadcastReviewUpdate sends a review status event to WebSocket subscribers.
// Exported wrapper for broadcastReviewUpdate, used by Queue to broadcast
// cancellation events for deduplication.
func (w *Worker) BroadcastReviewUpdate(ctx context.Context, runID string, status storage.ReviewStatus, conclusion string, durationMs int64) {
	w.broadcastReviewUpdate(ctx, runID, status, conclusion, durationMs)
}

func formatCompletedAt(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(time.RFC3339)
	return &s
}

func isTerminal(s storage.ReviewStatus) bool {
	switch s {
	case storage.StatusCompleted, storage.StatusFailed, storage.StatusTimedOut, storage.StatusCancelled:
		return true
	}
	return false
}

// cloneRepo performs a shallow single-branch clone of the target repo.
// The GitHub PAT is injected into the clone URL at runtime (never written to disk or logged).
func (w *Worker) cloneRepo(ctx context.Context, p api.ReviewPayload, dir string) error {
	cloneURL := w.buildCloneURL(p.RepoURL)

	cmd := exec.CommandContext(
		ctx,
		w.gitPath,
		"clone",
		"--branch", p.HeadBranch,
		"--single-branch",
		cloneURL,
		dir,
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone %s into %s: %w (output: %s)", sanitizeURL(cloneURL), dir, err, string(out))
	}

	return nil
}

// installSkills copies the skill definitions from the configured skills directory
// into the temp working directory so Claude Code discovers them via <cwd>/.claude/skills/.
func (w *Worker) installSkills(workDir string) error {
	entries, err := os.ReadDir(w.skillsDir)
	if err != nil {
		return fmt.Errorf("read skills dir %s: %w", w.skillsDir, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		src := filepath.Join(w.skillsDir, entry.Name())
		dst := filepath.Join(workDir, ".claude", "skills", entry.Name())
		if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
			return fmt.Errorf("copy skill %s: %w", entry.Name(), err)
		}
	}
	return nil
}

// buildCloneURL converts any git URL format to HTTPS with PAT authentication.
// Supports SSH (git@github.com:owner/repo.git), HTTPS (https://github.com/owner/repo.git),
// and PAT-embedded (https://x-access-token:<PAT>@github.com/owner/repo.git) formats.
// Non-GitHub URLs (file://, etc.) are returned as-is.
func (w *Worker) buildCloneURL(raw string) string {
	// Already a PAT URL — return as-is
	if strings.HasPrefix(raw, "https://x-access-token:") {
		return raw
	}
	// SSH format: git@github.com:owner/repo.git → HTTPS PAT format
	if strings.HasPrefix(raw, "git@") {
		raw = strings.TrimPrefix(raw, "git@")
		raw = strings.Replace(raw, ":", "/", 1)
		return "https://x-access-token:" + w.githubPat + "@" + raw
	}
	// HTTPS format: https://github.com/owner/repo.git → inject PAT
	if strings.HasPrefix(raw, "https://") {
		return strings.Replace(raw, "https://", "https://x-access-token:"+w.githubPat+"@", 1)
	}
	// file:// or other — return as-is
	return raw
}

// sanitizeURL masks the PAT token in a URL for safe logging.
func sanitizeURL(raw string) string {
	if idx := strings.Index(raw, "x-access-token:"); idx >= 0 {
		end := strings.Index(raw[idx:], "@")
		if end >= 0 {
			return raw[:idx] + "x-access-token:***" + raw[idx+end:]
		}
	}
	return raw
}

// saveReviewOutput persists the raw Claude CLI output to a timestamped file.
func (w *Worker) saveReviewOutput(runID string, p api.ReviewPayload, output string) error {
	if err := os.MkdirAll(w.reviewOutputDir, 0755); err != nil {
		return fmt.Errorf("create review output directory: %w", err)
	}

	repoSlug := p.RepoURL
	if idx := strings.LastIndex(repoSlug, "/"); idx >= 0 {
		repoSlug = repoSlug[idx+1:]
	}
	repoSlug = strings.TrimSuffix(repoSlug, ".git")

	ts := time.Now().UTC().Format("20060102-150405")
	filename := fmt.Sprintf("%s_%s_pr%d_%s.txt", ts, repoSlug, p.PRNumber, runID[:8])
	path := filepath.Join(w.reviewOutputDir, filename)

	return os.WriteFile(path, []byte(output), 0644)
}

// parseRepoURL extracts the GitHub owner and repo name from a git URL.
// Supports formats: git@github.com:owner/repo.git, https://github.com/owner/repo.git,
// file:///path/to/repo
func parseRepoURL(raw string) (owner, repo string) {
	raw = strings.TrimSuffix(raw, ".git")

	// Strip file:// scheme prefix if present
	if strings.HasPrefix(raw, "file://") {
		raw = strings.TrimPrefix(raw, "file://")
		// Extract the last path component as the repo name
		if idx := strings.LastIndex(raw, "/"); idx >= 0 {
			return raw[:idx], raw[idx+1:]
		}
		return "", raw
	}

	var slug string
	if idx := strings.LastIndex(raw, ":"); idx >= 0 {
		slug = raw[idx+1:]
	} else if idx := strings.LastIndex(raw, "/"); idx >= 0 {
		// Take last two segments for HTTPS URLs
		parts := strings.Split(raw, "/")
		if len(parts) >= 2 {
			slug = parts[len(parts)-2] + "/" + parts[len(parts)-1]
		} else {
			slug = raw
		}
	}

	parts := strings.SplitN(slug, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return slug, ""
}

// calculateMetrics computes aggregate review statistics from the store.
// Returns nil if the store is unavailable or a database error occurs.
func (w *Worker) calculateMetrics(ctx context.Context) map[string]any {
	if w.store == nil {
		return nil
	}

	reviews, err := w.store.ListReviews(ctx, storage.ListFilter{Limit: 10000})
	if err != nil {
		return nil
	}

	var total, success, failed, timedOut, cancelled, todayCount int
	var totalDuration int64
	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	for _, r := range reviews {
		total++
		totalDuration += r.DurationMs

		switch r.Status {
		case storage.StatusCompleted:
			success++
		case storage.StatusFailed:
			failed++
		case storage.StatusTimedOut:
			timedOut++
		case storage.StatusCancelled:
			cancelled++
		}

		if r.CreatedAt.After(todayStart) && r.CreatedAt.Before(now.Add(24*time.Hour)) {
			todayCount++
		}
	}

	avgDuration := int64(0)
	if total > 0 {
		avgDuration = totalDuration / int64(total)
	}

	return map[string]any{
		"total_reviews":   total,
		"success_count":   success,
		"failed_count":    failed,
		"timed_out_count": timedOut,
		"cancelled_count": cancelled,
		"avg_duration_ms": avgDuration,
		"reviews_today":   todayCount,
	}
}

// isTransientError returns true if the Claude Code CLI failure should be retried.
// This covers: network timeouts, rate limits (HTTP 429), server errors (500/502/503),
// and known transient CLI exit codes.
func isTransientError(err error, exitCode int, output string) bool {
	if err == nil && exitCode == 0 {
		return false
	}

	// Network-level errors: DNS failure, connection refused, timeout
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	// os/exec wraps context.DeadlineExceeded — the caller already handles
	// context cancellation, so we only match pure network timeouts here.
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	// Connection reset / broken pipe etc.
	if errors.Is(err, net.ErrClosed) {
		return true
	}

	// Known transient exit codes (e.g. Claude Code CLI exit code 2 = rate limit)
	if transientExitCodes[exitCode] {
		return true
	}

	// Heuristic: check CLI output for transient error patterns
	lower := strings.ToLower(output)
	for _, pattern := range []string{
		"rate limit",
		"too many requests",
		"overloaded",
		"502 bad gateway",
		"503 service unavailable",
		"500 internal server error",
		"temporary failure",
		"connection reset",
		"econnreset",
		"etimedout",
	} {
		if strings.Contains(lower, pattern) {
			return true
		}
	}

	return false
}
