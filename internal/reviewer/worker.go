package reviewer

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/google/uuid"
	"github.com/kmmuntasir/nano-review/internal/api"
)

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

// Worker handles the lifecycle of a single PR review: clone, run Claude, cleanup.
type Worker struct {
	claude     ClaudeRunner
	logger     Logger
	gitPath    string
	claudePath string
}

// NewWorker creates a new review Worker with the given dependencies.
func NewWorker(claude ClaudeRunner, logger Logger, gitPath, claudePath string) *Worker {
	return &Worker{
		claude:     claude,
		logger:     logger,
		gitPath:    gitPath,
		claudePath: claudePath,
	}
}

// StartReview validates the payload, generates a unique run ID, and launches
// the review asynchronously. It returns the run ID immediately without blocking.
func (w *Worker) StartReview(ctx context.Context, p api.ReviewPayload) (string, error) {
	runID := uuid.New().String()
	go w.processReview(ctx, runID, p)
	return runID, nil
}

// processReview is the async goroutine that clones the repo, runs Claude Code
// CLI, and ensures cleanup regardless of success or failure.
func (w *Worker) processReview(ctx context.Context, runID string, p api.ReviewPayload) {
	logger := w.logger.With("run_id", runID, "pr_number", p.PRNumber, "repo", p.RepoURL)

	logger.Info("review started")

	dir, err := os.MkdirTemp("", "nano-review-*")
	if err != nil {
		logger.Error("failed to create temp directory", "error", err)
		return
	}
	defer os.RemoveAll(dir)

	logger.Info("git clone started")
	if err := w.cloneRepo(ctx, p, dir); err != nil {
		logger.Error("git clone failed", "error", err)
		return
	}
	logger.Info("git clone completed")

	logger.Info("claude execution started")
	output, exitCode, err := w.claude.Run(ctx, dir, w.claudePath, "-p", "/pr-review", "--dangerously-skip-permissions")
	if err != nil {
		logger.Error("claude execution failed", "exit_code", exitCode, "error", err, "output", output)
		return
	}
	logger.Info("claude execution completed", "exit_code", exitCode)
	logger.Info("review completed")
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
