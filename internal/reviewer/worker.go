package reviewer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

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
	claude        ClaudeRunner
	logger        Logger
	gitPath       string
	claudePath    string
	model         string
	mcpConfigPath string
}

// NewWorker creates a new review Worker with the given dependencies.
func NewWorker(claude ClaudeRunner, logger Logger, gitPath, claudePath, model, mcpConfigPath string) *Worker {
	return &Worker{
		claude:        claude,
		logger:        logger,
		gitPath:       gitPath,
		claudePath:    claudePath,
		model:         model,
		mcpConfigPath: mcpConfigPath,
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
	if err := w.cloneRepo(ctx, p, repoDir); err != nil {
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

	output, exitCode, err := w.claude.Run(ctx, dir, args...)

	// Always persist Claude output for debugging, regardless of exit code
	if saveErr := saveReviewOutput(runID, p, output); saveErr != nil {
		logger.Error("failed to save review output", "error", saveErr)
	}

	if err != nil {
		logger.Error("claude execution failed", "exit_code", exitCode, "error", err)
		return
	}
	if exitCode != 0 {
		logger.Error("claude execution failed with non-zero exit code", "exit_code", exitCode)
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
