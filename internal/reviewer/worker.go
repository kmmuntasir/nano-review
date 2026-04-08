package reviewer

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kmmuntasir/nano-review/internal/api"
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
	logger            Logger
	gitPath           string
	claudePath        string
	model             string
	mcpConfigPath     string
	maxReviewDuration time.Duration
	maxRetries        int
}

// NewWorker creates a new review Worker with the given dependencies.
func NewWorker(claude ClaudeRunner, logger Logger, gitPath, claudePath, model, mcpConfigPath string, maxReviewDuration time.Duration, maxRetries int) *Worker {
	if maxReviewDuration <= 0 {
		maxReviewDuration = DefaultMaxReviewDuration
	}
	if maxRetries < 0 {
		maxRetries = 0
	}
	return &Worker{
		claude:            claude,
		logger:            logger,
		gitPath:           gitPath,
		claudePath:        claudePath,
		model:             model,
		mcpConfigPath:     mcpConfigPath,
		maxReviewDuration: maxReviewDuration,
		maxRetries:        maxRetries,
	}
}

// StartReview validates the payload, generates a unique run ID, and launches
// the review asynchronously. It returns the run ID immediately without blocking.
func (w *Worker) StartReview(ctx context.Context, p api.ReviewPayload) (string, error) {
	runID := uuid.New().String()
	// Create a new context for the background goroutine, independent of the HTTP request context
	bgCtx := context.Background()
	go w.processReview(bgCtx, runID, p)
	return runID, nil
}

// processReview is the async goroutine that clones the repo, runs Claude Code
// CLI, and ensures cleanup regardless of success or failure.
func (w *Worker) processReview(ctx context.Context, runID string, p api.ReviewPayload) {
	logger := w.logger.With("run_id", runID, "pr_number", p.PRNumber, "repo", p.RepoURL)

	reviewCtx, cancel := context.WithTimeout(ctx, w.maxReviewDuration)
	defer cancel()

	logger.Info("review started")

	dir, err := os.MkdirTemp("", "nano-review-*")
	if err != nil {
		logger.Error("failed to create temp directory", "error", err)
		return
	}
	defer os.RemoveAll(dir)

	owner, repo := parseRepoURL(p.RepoURL)
	repoDir := filepath.Join(dir, repo)

	logger.Info("git clone started")
	if err := w.cloneRepo(reviewCtx, p, repoDir); err != nil {
		logger.Error("git clone failed", "error", err)
		return
	}
	logger.Info("git clone completed")

	logger.Info("claude execution started")

	prompt := fmt.Sprintf("/pr-review Review pull request #%d in %s/%s (base: %s, head: %s). The repo is cloned at ./%s/", p.PRNumber, owner, repo, p.BaseBranch, p.HeadBranch, repo)

	args := []string{w.claudePath, "-p", prompt, "--dangerously-skip-permissions"}
	if w.model != "" {
		args = append(args, "--model", w.model)
	}
	if w.mcpConfigPath != "" {
		args = append(args, "--mcp-config", w.mcpConfigPath, "--strict-mcp-config")
	}

	var output string
	var exitCode int
	var err error

	for attempt := range w.maxRetries + 1 {
		output, exitCode, err = w.claude.Run(reviewCtx, dir, args...)

		// Always persist output for debugging, even on retry
		if saveErr := saveReviewOutput(runID, p, output); saveErr != nil {
			logger.Error("failed to save review output", "error", saveErr)
		}

		if err == nil && exitCode == 0 {
			logger.Info("claude execution completed", "exit_code", exitCode)
			logger.Info("review completed")
			return
		}

		// Context cancelled or timed out — never retry
		if reviewCtx.Err() != nil {
			if errors.Is(reviewCtx.Err(), context.DeadlineExceeded) {
				logger.Error("review timed out", "duration", w.maxReviewDuration)
			} else {
				logger.Error("review cancelled", "error", reviewCtx.Err())
			}
			return
		}

		// Check if the failure is transient and retries remain
		if !isTransientError(err, exitCode, output) {
			logger.Error("claude execution failed", "exit_code", exitCode, "error", err)
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
			select {
			case <-time.After(backoff):
			case <-reviewCtx.Done():
				logger.Error("review cancelled during retry backoff")
				return
			}
			continue
		}

		logger.Error("claude execution failed after all retries",
			"attempts", w.maxRetries+1,
			"exit_code", exitCode,
			"error", err,
		)
		return
	}
}

// cloneRepo performs a shallow single-branch clone of the target repo.
func (w *Worker) cloneRepo(ctx context.Context, p api.ReviewPayload, dir string) error {
	cmd := exec.CommandContext(
		ctx,
		w.gitPath,
		"clone",
		"--branch", p.HeadBranch,
		"--single-branch",
		p.RepoURL,
		dir,
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone %s into %s: %w (output: %s)", p.RepoURL, dir, err, string(out))
	}

	return nil
}

// reviewOutputDir is the directory where Claude CLI output files are stored.
const reviewOutputDir = "/app/logs/reviews"

// saveReviewOutput persists the raw Claude CLI output to a timestamped file.
func saveReviewOutput(runID string, p api.ReviewPayload, output string) error {
	if err := os.MkdirAll(reviewOutputDir, 0755); err != nil {
		return fmt.Errorf("create review output directory: %w", err)
	}

	repoSlug := p.RepoURL
	if idx := strings.LastIndex(repoSlug, "/"); idx >= 0 {
		repoSlug = repoSlug[idx+1:]
	}
	repoSlug = strings.TrimSuffix(repoSlug, ".git")

	ts := time.Now().UTC().Format("20060102-150405")
	filename := fmt.Sprintf("%s_%s_pr%d_%s.txt", ts, repoSlug, p.PRNumber, runID[:8])
	path := filepath.Join(reviewOutputDir, filename)

	return os.WriteFile(path, []byte(output), 0644)
}

// parseRepoURL extracts the GitHub owner and repo name from a git URL.
// Supports formats: git@github.com:owner/repo.git, https://github.com/owner/repo.git
func parseRepoURL(raw string) (owner, repo string) {
	raw = strings.TrimSuffix(raw, ".git")

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
