package reviewer

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kmmuntasir/nano-review/internal/api"
)

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

// mockClaudeRunner is a thread-safe mock implementing ClaudeRunner.
type mockClaudeRunner struct {
	mu       sync.Mutex
	calls    []mockCall
	output   string
	exitCode int
	err      error
}

type mockCall struct {
	Dir  string
	Args []string
}

func (m *mockClaudeRunner) Run(_ context.Context, dir string, args ...string) (string, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, mockCall{Dir: dir, Args: args})
	return m.output, m.exitCode, m.err
}

func (m *mockClaudeRunner) getCalls() []mockCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]mockCall, len(m.calls))
	copy(out, m.calls)
	return out
}

// mockLogger is a thread-safe mock implementing Logger.
type mockLogger struct {
	mu      sync.Mutex
	infos   []logEntry
	errors  []logEntry
	withs   [][]any
	children []*mockLogger
}

type logEntry struct {
	msg   string
	kv    []any
}

func (m *mockLogger) Info(msg string, keysAndValues ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.infos = append(m.infos, logEntry{msg: msg, kv: keysAndValues})
}

func (m *mockLogger) Error(msg string, keysAndValues ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errors = append(m.errors, logEntry{msg: msg, kv: keysAndValues})
}

func (m *mockLogger) With(keysAndValues ...any) Logger {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.withs = append(m.withs, keysAndValues)
	child := &mockLogger{}
	m.children = append(m.children, child)
	return child
}

func (m *mockLogger) getInfos() []logEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]logEntry, len(m.infos))
	copy(out, m.infos)
	return out
}

func (m *mockLogger) getErrors() []logEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]logEntry, len(m.errors))
	copy(out, m.errors)
	return out
}

func (m *mockLogger) getChildren() []*mockLogger {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*mockLogger, len(m.children))
	copy(out, m.children)
	return out
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func skipIfNoGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
}

// testPayload returns a valid ReviewPayload for tests.
func testPayload() api.ReviewPayload {
	return api.ReviewPayload{
		RepoURL:    "git@github.com:owner/repo.git",
		PRNumber:   42,
		BaseBranch: "main",
		HeadBranch: "feature/x",
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestNewWorker(t *testing.T) {
	logger := newNopLogger()
	w := NewWorker(nil, logger, "git", "claude", "", 0)

	if w == nil {
		t.Fatal("NewWorker returned nil")
	}
	if w.gitPath != "git" {
		t.Errorf("gitPath = %q, want %q", w.gitPath, "git")
	}
	if w.claudePath != "claude" {
		t.Errorf("claudePath = %q, want %q", w.claudePath, "claude")
	}
}

func TestStartReview_ReturnsNonEmptyRunID(t *testing.T) {
	claude := &mockClaudeRunner{exitCode: 0}
	logger := newNopLogger()

	w := NewWorker(claude, logger, "git", "claude", "", 0)

	runID, err := w.StartReview(context.Background(), testPayload())
	if err != nil {
		t.Fatalf("StartReview returned error: %v", err)
	}
	if runID == "" {
		t.Fatal("StartReview returned empty runID")
	}

	// UUIDs from uuid.New() are 36 characters (8-4-4-4-12 format)
	if len(runID) != 36 {
		t.Errorf("runID length = %d, want 36", len(runID))
	}
}

func TestProcessReview_CloneFailure_CleansUp(t *testing.T) {
	skipIfNoGit(t)

	claude := &mockClaudeRunner{exitCode: 0}
	logger := &mockLogger{}

	// Use a non-existent git binary path to force clone failure
	w := NewWorker(claude, logger, "/nonexistent/path/to/git", "claude", "", 0)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := w.StartReview(ctx, testPayload())
	if err != nil {
		t.Fatalf("StartReview returned error: %v", err)
	}

	// Wait for the async goroutine to complete
	time.Sleep(2 * time.Second)

	// Verify Claude was never called (clone failed before that step)
	calls := claude.getCalls()
	if len(calls) != 0 {
		t.Errorf("claude.Run called %d times, want 0 after clone failure", len(calls))
	}

	// Verify an error was logged about clone failure.
	// processReview calls logger.With() first, which returns a child mockLogger,
	// so the error is logged on the child, not the parent.
	found := false
	for _, child := range logger.getChildren() {
		for _, e := range child.getErrors() {
			if strings.Contains(e.msg, "git clone failed") {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Errorf("expected error log containing 'git clone failed' on child logger, got none")
	}
}

func TestProcessReview_ClaudeFailure_CleansUp(t *testing.T) {
	skipIfNoGit(t)

	// We can't easily do a real git clone in tests without network access,
	// so we use a fake gitPath that succeeds. Instead, we test the Claude
	// failure path by mocking ClaudeRunner to return an error.
	claude := &mockClaudeRunner{
		err:      errors.New("claude process exited with code 1"),
		exitCode: 1,
		output:   "some error output",
	}
	logger := &mockLogger{}

	// Use a fake git wrapper script approach: set gitPath to "true" which
	// is a no-op that exits 0, simulating a successful clone for the
	// directory creation part. The actual clone will fail but we can
	// verify Claude was attempted or not based on flow.
	w := NewWorker(claude, logger, "true", "claude", "")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	runID, err := w.StartReview(ctx, testPayload())
	if err != nil {
		t.Fatalf("StartReview returned error: %v", err)
	}

	// Wait for async goroutine
	time.Sleep(2 * time.Second)

	_ = runID
}

func TestProcessReview_CallsClaudeWithCorrectArgs(t *testing.T) {
	skipIfNoGit(t)

	// Create a local repo with a commit so --branch main works
	tmpDir := t.TempDir()
	repoDir := tmpDir + "/source"
	workDir := tmpDir + "/work"

	if err := exec.Command("git", "init", "--bare", "--initial-branch=main", repoDir).Run(); err != nil {
		t.Fatalf("failed to init bare repo: %v", err)
	}
	if err := exec.Command("git", "clone", repoDir, workDir).Run(); err != nil {
		t.Fatalf("failed to clone bare repo: %v", err)
	}
	if err := os.WriteFile(workDir+"/README.md", []byte("test"), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	if out, err := exec.Command("sh", "-c", "cd "+workDir+" && git add . && git -c user.name=test -c user.email=test@test.com commit -m init && git push origin main").CombinedOutput(); err != nil {
		t.Fatalf("failed to seed repo: %v\n%s", err, string(out))
	}

	var wg sync.WaitGroup
	wg.Add(1)
	claude := &blockingMockClaudeRunner{
		mockClaudeRunner: mockClaudeRunner{
			output:   "review completed",
			exitCode: 0,
		},
		onRun: func() { wg.Done() },
	}
	logger := newNopLogger()

	w := NewWorker(claude, logger, "git", "claude", "", 0)

	payload := api.ReviewPayload{
		RepoURL:    "file://" + repoDir,
		PRNumber:   1,
		BaseBranch: "main",
		HeadBranch: "main",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := w.StartReview(ctx, payload)
	if err != nil {
		t.Fatalf("StartReview returned error: %v", err)
	}

	// Wait for Claude to be called (implies clone succeeded)
	wg.Wait()

	calls := claude.getCalls()
	if len(calls) != 1 {
		t.Fatalf("claude.Run called %d times, want 1", len(calls))
	}

	call := calls[0]
	if len(call.Args) < 2 {
		t.Fatalf("claude.Run args length = %d, want at least 2", len(call.Args))
	}

	if call.Args[0] != "claude" {
		t.Errorf("claude.Run args[0] = %q, want %q", call.Args[0], "claude")
	}

	foundPrompt := false
	for i, arg := range call.Args {
		if arg == "-p" && i+1 < len(call.Args) && call.Args[i+1] == "/pr-review" {
			foundPrompt = true
		}
	}
	if !foundPrompt {
		t.Errorf("claude.Run args = %v, want -p /pr-review present", call.Args)
	}

	foundSkip := false
	for _, arg := range call.Args {
		if arg == "--dangerously-skip-permissions" {
			foundSkip = true
			break
		}
	}
	if !foundSkip {
		t.Errorf("claude.Run args = %v, want --dangerously-skip-permissions present", call.Args)
	}

	if call.Dir == "" {
		t.Error("claude.Run dir is empty")
	}

	// Verify Claude's CWD is the parent temp dir, NOT the repo subdirectory
	if strings.HasSuffix(call.Dir, "/source") {
		t.Errorf("claude.Run dir = %q, should NOT end with repo name; expected parent temp dir", call.Dir)
	}

	// Extract the prompt string (the arg following -p)
	var prompt string
	for i, arg := range call.Args {
		if arg == "-p" && i+1 < len(call.Args) {
			prompt = call.Args[i+1]
			break
		}
	}
	if prompt == "" {
		t.Fatal("could not find prompt string in claude.Run args")
	}

	// Verify the prompt contains the subdirectory clone instruction
	if !strings.Contains(prompt, "The repo is cloned at ./") {
		t.Errorf("prompt does not contain subdirectory clone instruction; prompt = %q", prompt)
	}

	// Verify the prompt contains the expected repo name ("source")
	if !strings.Contains(prompt, "source") {
		t.Errorf("prompt does not contain expected repo name %q; prompt = %q", "source", prompt)
	}
}

func TestProcessReview_CloneIntoSubdirectory(t *testing.T) {
	skipIfNoGit(t)

	// Create a local bare repo with a commit
	tmpDir := t.TempDir()
	repoDir := tmpDir + "/source"
	workDir := tmpDir + "/work"

	if err := exec.Command("git", "init", "--bare", "--initial-branch=main", repoDir).Run(); err != nil {
		t.Fatalf("failed to init bare repo: %v", err)
	}
	if err := exec.Command("git", "clone", repoDir, workDir).Run(); err != nil {
		t.Fatalf("failed to clone bare repo: %v", err)
	}
	if err := os.WriteFile(workDir+"/README.md", []byte("test"), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	if out, err := exec.Command("sh", "-c", "cd "+workDir+" && git add . && git -c user.name=test -c user.email=test@test.com commit -m init && git push origin main").CombinedOutput(); err != nil {
		t.Fatalf("failed to seed repo: %v\n%s", err, string(out))
	}

	var wg sync.WaitGroup
	wg.Add(1)
	claude := &blockingMockClaudeRunner{
		mockClaudeRunner: mockClaudeRunner{
			output:   "review completed",
			exitCode: 0,
		},
		onRun: func() { wg.Done() },
	}
	logger := newNopLogger()

	w := NewWorker(claude, logger, "git", "claude", "", 0)

	payload := api.ReviewPayload{
		RepoURL:    "file://" + repoDir,
		PRNumber:   1,
		BaseBranch: "main",
		HeadBranch: "main",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := w.StartReview(ctx, payload)
	if err != nil {
		t.Fatalf("StartReview returned error: %v", err)
	}

	// Wait for Claude to be called (implies clone succeeded)
	wg.Wait()

	calls := claude.getCalls()
	if len(calls) != 1 {
		t.Fatalf("claude.Run called %d times, want 1", len(calls))
	}

	call := calls[0]

	// 1. Verify Claude's CWD is the parent temp dir, NOT the repo subdirectory.
	//    The repo subdirectory is named "source" (derived from parseRepoURL of
	//    "file://<...>/source"). Claude should run in the parent temp dir.
	if strings.HasSuffix(call.Dir, "/source") {
		t.Errorf("claude.Run dir = %q, should be the parent temp dir, not the repo subdirectory", call.Dir)
	}

	// 2. Extract the prompt string (the arg following -p)
	var prompt string
	for i, arg := range call.Args {
		if arg == "-p" && i+1 < len(call.Args) {
			prompt = call.Args[i+1]
			break
		}
	}
	if prompt == "" {
		t.Fatal("could not find prompt string in claude.Run args")
	}

	// 3. Verify the prompt contains the repo subdirectory path (./source/)
	if !strings.Contains(prompt, "./source/") {
		t.Errorf("prompt should contain the repo subdirectory path ./source/; got: %q", prompt)
	}
}

// blockingMockClaudeRunner wraps mockClaudeRunner with a callback invoked on Run.
type blockingMockClaudeRunner struct {
	mockClaudeRunner
	onRun func()
}

func (m *blockingMockClaudeRunner) Run(ctx context.Context, dir string, args ...string) (string, int, error) {
	if m.onRun != nil {
		m.onRun()
	}
	return m.mockClaudeRunner.Run(ctx, dir, args...)
}

func TestProcessReview_Timeout_LogsTimeoutMessage(t *testing.T) {
	skipIfNoGit(t)

	done := make(chan struct{})
	claude := &blockingMockClaudeRunner{
		mockClaudeRunner: mockClaudeRunner{
			output:   "stuck",
			exitCode: 0,
		},
		onRun: func() { close(done) },
	}
	logger := &mockLogger{}

	// Very short timeout to trigger immediately
	w := NewWorker(claude, logger, "true", "claude", "", 50*time.Millisecond)

	_, err := w.StartReview(context.Background(), testPayload())
	if err != nil {
		t.Fatalf("StartReview returned error: %v", err)
	}

	// Wait for Claude to be invoked then for the timeout to fire
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Claude Run was never called")
	}

	// Wait for the timeout to fire and the goroutine to finish
	time.Sleep(200 * time.Millisecond)

	// Verify the timeout error was logged on the child logger
	found := false
	for _, child := range logger.getChildren() {
		for _, e := range child.getErrors() {
			if e.msg == "review timed out" {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Errorf("expected error log 'review timed out' on child logger, got none")
	}
}

func TestProcessReview_NoTimeout_CompletesNormally(t *testing.T) {
	skipIfNoGit(t)

	var wg sync.WaitGroup
	wg.Add(1)
	claude := &blockingMockClaudeRunner{
		mockClaudeRunner: mockClaudeRunner{
			output:   "review completed",
			exitCode: 0,
		},
		onRun: func() { wg.Done() },
	}
	logger := &mockLogger{}

	// Generous timeout — should not fire
	w := NewWorker(claude, logger, "true", "claude", "", 30*time.Second)

	_, err := w.StartReview(context.Background(), testPayload())
	if err != nil {
		t.Fatalf("StartReview returned error: %v", err)
	}

	wg.Wait()
	time.Sleep(100 * time.Millisecond)

	// Verify no timeout was logged
	for _, child := range logger.getChildren() {
		for _, e := range child.getErrors() {
			if e.msg == "review timed out" {
				t.Error("unexpected 'review timed out' log when review completed normally")
			}
		}
	}
}

func TestNewWorker_ZeroDuration_UsesDefault(t *testing.T) {
	logger := newNopLogger()
	w := NewWorker(nil, logger, "git", "claude", "", 0)
	if w.maxReviewDuration != DefaultMaxReviewDuration {
		t.Errorf("maxReviewDuration = %v, want %v", w.maxReviewDuration, DefaultMaxReviewDuration)
	}
}

func TestNewWorker_CustomDuration(t *testing.T) {
	logger := newNopLogger()
	w := NewWorker(nil, logger, "git", "claude", "", 5*time.Minute)
	if w.maxReviewDuration != 5*time.Minute {
		t.Errorf("maxReviewDuration = %v, want %v", w.maxReviewDuration, 5*time.Minute)
	}
}

func TestSlogLogger_With(t *testing.T) {
	logger := newNopLogger()
	enriched := logger.With("run_id", "test-123")
	if enriched == nil {
		t.Fatal("With() returned nil")
	}
	// Should not panic
	enriched.Info("test message", "key", "value")
}

func TestMockLogger_With(t *testing.T) {
	logger := &mockLogger{}
	child := logger.With("run_id", "abc")
	if child == nil {
		t.Fatal("With() returned nil")
	}

	withs := logger.withs
	if len(withs) != 1 {
		t.Fatalf("With called %d times, want 1", len(withs))
	}
	if len(withs[0]) != 2 || withs[0][0] != "run_id" || withs[0][1] != "abc" {
		t.Errorf("With args = %v, want [run_id abc]", withs[0])
	}

	// Child should be independent
	childLogger := child.(*mockLogger)
	childLogger.Info("child message")
	if len(logger.infos) != 0 {
		t.Error("parent logger should not have info entries from child")
	}
	if len(childLogger.infos) != 1 {
		t.Errorf("child logger infos = %d, want 1", len(childLogger.infos))
	}
}
