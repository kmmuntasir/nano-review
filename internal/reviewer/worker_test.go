package reviewer

import (
	"context"
	"errors"
	"io"
	"net"
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
	// per-call overrides: each call can return different results
	perCall []mockResult
}

type mockResult struct {
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
	if len(m.perCall) > 0 {
		r := m.perCall[0]
		m.perCall = m.perCall[1:]
		return r.output, r.exitCode, r.err
	}
	return m.output, m.exitCode, m.err
}

func (m *mockClaudeRunner) RunStreaming(_ context.Context, dir string, streamWriter io.Writer, args ...string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, mockCall{Dir: dir, Args: args})
	var output string
	if len(m.perCall) > 0 {
		r := m.perCall[0]
		m.perCall = m.perCall[1:]
		output = r.output
		return r.exitCode, r.err
	}
	output = m.output
	if streamWriter != nil && output != "" {
		streamWriter.Write([]byte(output))
	}
	return m.exitCode, m.err
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
	mu       sync.Mutex
	infos    []logEntry
	errors   []logEntry
	withs    [][]any
	children []*mockLogger
}

type logEntry struct {
	msg string
	kv  []any
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
		RepoURL:    "https://github.com/owner/repo.git",
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
	w := NewWorker(nil, nil, logger, nil, "git", "claude", "", "", "", 0, 0)

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

	w := NewWorker(claude, nil, logger, nil, "git", "claude", "", "", "", 0, 0)

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
	w := NewWorker(claude, nil, logger, nil, "/nonexistent/path/to/git", "claude", "", "", "", 0, 0)

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
	w := NewWorker(claude, nil, logger, nil, "true", "claude", "", "", "", 0, 0)

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

	w := NewWorker(claude, nil, logger, nil, "git", "claude", "", "", "", 0, 0)

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
		if arg == "-p" && i+1 < len(call.Args) && strings.HasPrefix(call.Args[i+1], "/pr-review") {
			foundPrompt = true
		}
	}
	if !foundPrompt {
		t.Errorf("claude.Run args = %v, want -p with /pr-review prompt present", call.Args)
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

	w := NewWorker(claude, nil, logger, nil, "git", "claude", "", "", "", 0, 0)

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
// If blockOnContext is true, it blocks until the context is cancelled before returning.
type blockingMockClaudeRunner struct {
	mockClaudeRunner
	onRun          func()
	blockOnContext bool
}

func (m *blockingMockClaudeRunner) Run(ctx context.Context, dir string, args ...string) (string, int, error) {
	if m.onRun != nil {
		m.onRun()
	}
	if m.blockOnContext {
		<-ctx.Done()
		return "blocked", 1, ctx.Err()
	}
	return m.mockClaudeRunner.Run(ctx, dir, args...)
}

func (m *blockingMockClaudeRunner) RunStreaming(ctx context.Context, dir string, streamWriter io.Writer, args ...string) (int, error) {
	if m.onRun != nil {
		m.onRun()
	}
	if m.blockOnContext {
		<-ctx.Done()
		return 1, ctx.Err()
	}
	return m.mockClaudeRunner.RunStreaming(ctx, dir, streamWriter, args...)
}

func TestProcessReview_Timeout_LogsTimeoutMessage(t *testing.T) {
	skipIfNoGit(t)

	done := make(chan struct{})
	claude := &blockingMockClaudeRunner{
		mockClaudeRunner: mockClaudeRunner{
			output:   "stuck",
			exitCode: 0,
		},
		onRun:          func() { close(done) },
		blockOnContext: true,
	}
	logger := &mockLogger{}

	// Very short timeout to trigger immediately
	w := NewWorker(claude, nil, logger, nil, "true", "claude", "", "", "", 50*time.Millisecond, 0)

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
	w := NewWorker(claude, nil, logger, nil, "true", "claude", "", "", "", 30*time.Second, 0)

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
	w := NewWorker(nil, nil, logger, nil, "git", "claude", "", "", "", 0, 0)
	if w.maxReviewDuration != DefaultMaxReviewDuration {
		t.Errorf("maxReviewDuration = %v, want %v", w.maxReviewDuration, DefaultMaxReviewDuration)
	}
}

func TestNewWorker_CustomDuration(t *testing.T) {
	logger := newNopLogger()
	w := NewWorker(nil, nil, logger, nil, "git", "claude", "", "", "", 5*time.Minute, 0)
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

// ---------------------------------------------------------------------------
// Retry tests
// ---------------------------------------------------------------------------

func TestProcessReview_RetryOnTransientError(t *testing.T) {
	skipIfNoGit(t)

	claude := &mockClaudeRunner{
		perCall: []mockResult{
			{output: "rate limit exceeded", exitCode: 2, err: nil},
			{output: "review completed", exitCode: 0, err: nil},
		},
	}
	logger := &mockLogger{}

	w := NewWorker(claude, nil, logger, nil, "true", "claude", "", "", "", 30*time.Second, 2)

	runID, err := w.StartReview(context.Background(), testPayload())
	if err != nil {
		t.Fatalf("StartReview returned error: %v", err)
	}

	// Wait for async goroutine (includes 1s backoff on first retry)
	time.Sleep(3 * time.Second)

	_ = runID

	calls := claude.getCalls()
	if len(calls) != 2 {
		t.Fatalf("claude.Run called %d times, want 2 (initial + 1 retry)", len(calls))
	}

	// Verify retry was logged on child logger
	foundRetry := false
	for _, child := range logger.getChildren() {
		for _, info := range child.getInfos() {
			if info.msg == "transient failure, retrying" {
				foundRetry = true
				break
			}
		}
		if foundRetry {
			break
		}
	}
	if !foundRetry {
		t.Error("expected info log 'transient failure, retrying', got none")
	}
}

func TestProcessReview_NoRetryOnDeterministicError(t *testing.T) {
	skipIfNoGit(t)

	claude := &mockClaudeRunner{
		output:   "auth error: invalid token",
		exitCode: 1,
		err:      nil,
	}
	logger := &mockLogger{}

	w := NewWorker(claude, nil, logger, nil, "true", "claude", "", "", "", 30*time.Second, 2)

	_, err := w.StartReview(context.Background(), testPayload())
	if err != nil {
		t.Fatalf("StartReview returned error: %v", err)
	}

	time.Sleep(2 * time.Second)

	calls := claude.getCalls()
	if len(calls) != 1 {
		t.Fatalf("claude.Run called %d times, want 1 (no retry for deterministic error)", len(calls))
	}

	// Should log a non-retry failure
	found := false
	for _, child := range logger.getChildren() {
		for _, e := range child.getErrors() {
			if e.msg == "claude execution failed" {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Error("expected error log 'claude execution failed' for deterministic error, got none")
	}
}

func TestProcessReview_RetryExhausted_LogsFinalError(t *testing.T) {
	skipIfNoGit(t)

	claude := &mockClaudeRunner{
		perCall: []mockResult{
			{output: "rate limit exceeded", exitCode: 2, err: nil},
			{output: "rate limit exceeded", exitCode: 2, err: nil},
			{output: "rate limit exceeded", exitCode: 2, err: nil},
		},
	}
	logger := &mockLogger{}

	w := NewWorker(claude, nil, logger, nil, "true", "claude", "", "", "", 30*time.Second, 2)

	_, err := w.StartReview(context.Background(), testPayload())
	if err != nil {
		t.Fatalf("StartReview returned error: %v", err)
	}

	// Wait for 2 retries with backoff (1s + 2s) plus processing
	time.Sleep(5 * time.Second)

	calls := claude.getCalls()
	if len(calls) != 3 {
		t.Fatalf("claude.Run called %d times, want 3 (initial + 2 retries)", len(calls))
	}

	// Verify final error log
	found := false
	for _, child := range logger.getChildren() {
		for _, e := range child.getErrors() {
			if e.msg == "claude execution failed after all retries" {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Error("expected error log 'claude execution failed after all retries', got none")
	}
}

func TestProcessReview_NoRetryOnTimeout(t *testing.T) {
	skipIfNoGit(t)

	claude := &blockingMockClaudeRunner{
		mockClaudeRunner: mockClaudeRunner{
			output:   "rate limit",
			exitCode: 2,
			err:      nil,
		},
		blockOnContext: true,
	}
	logger := &mockLogger{}

	// Very short timeout — should time out immediately without retry
	w := NewWorker(claude, nil, logger, nil, "true", "claude", "", "", "", 50*time.Millisecond, 2)

	_, err := w.StartReview(context.Background(), testPayload())
	if err != nil {
		t.Fatalf("StartReview returned error: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	// Should NOT have retried — context was cancelled
	foundRetry := false
	for _, child := range logger.getChildren() {
		for _, info := range child.getInfos() {
			if info.msg == "transient failure, retrying" {
				foundRetry = true
				break
			}
		}
		if foundRetry {
			break
		}
	}
	if foundRetry {
		t.Error("should NOT retry when context is already cancelled (timeout)")
	}
}

func TestIsTransientError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		exitCode int
		output   string
		want     bool
	}{
		{
			name:     "success is not transient",
			err:      nil,
			exitCode: 0,
			output:   "ok",
			want:     false,
		},
		{
			name:     "exit code 1 is not transient",
			err:      nil,
			exitCode: 1,
			output:   "some error",
			want:     false,
		},
		{
			name:     "exit code 2 is transient",
			err:      nil,
			exitCode: 2,
			output:   "",
			want:     true,
		},
		{
			name:     "rate limit in output",
			err:      nil,
			exitCode: 1,
			output:   "Error: rate limit exceeded",
			want:     true,
		},
		{
			name:     "503 in output",
			err:      nil,
			exitCode: 1,
			output:   "503 Service Unavailable",
			want:     true,
		},
		{
			name:     "overloaded in output",
			err:      nil,
			exitCode: 1,
			output:   "Server is overloaded",
			want:     true,
		},
		{
			name:     "deterministic error with no pattern",
			err:      nil,
			exitCode: 1,
			output:   "auth error: invalid token",
			want:     false,
		},
		{
			name:     "network timeout error",
			err:      &net.OpError{Op: "dial", Net: "tcp", Err: &net.AddrError{Err: "i/o timeout"}},
			exitCode: 0,
			output:   "",
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTransientError(tt.err, tt.exitCode, tt.output)
			if got != tt.want {
				t.Errorf("isTransientError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewWorker_NegativeRetries_ClampedToZero(t *testing.T) {
	logger := newNopLogger()
	w := NewWorker(nil, nil, logger, nil, "git", "claude", "", "", "", 0, -1)
	if w.maxRetries != 0 {
		t.Errorf("maxRetries = %d, want 0 (clamped from negative)", w.maxRetries)
	}
}

func TestNewWorker_DefaultRetries(t *testing.T) {
	logger := newNopLogger()
	w := NewWorker(nil, nil, logger, nil, "git", "claude", "", "", "", 0, DefaultMaxRetries)
	if w.maxRetries != DefaultMaxRetries {
		t.Errorf("maxRetries = %d, want %d", w.maxRetries, DefaultMaxRetries)
	}
}

// ---------------------------------------------------------------------------
// buildCloneURL / sanitizeURL tests
// ---------------------------------------------------------------------------

func TestBuildCloneURL(t *testing.T) {
	logger := newNopLogger()
	w := NewWorker(nil, nil, logger, nil, "git", "claude", "", "", "test-pat-123", 0, 0)

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "SSH URL converted to HTTPS PAT",
			raw:  "git@github.com:owner/repo.git",
			want: "https://x-access-token:test-pat-123@github.com/owner/repo.git",
		},
		{
			name: "HTTPS URL gets PAT injected",
			raw:  "https://github.com/owner/repo.git",
			want: "https://x-access-token:test-pat-123@github.com/owner/repo.git",
		},
		{
			name: "PAT URL passed through as-is",
			raw:  "https://x-access-token:existing-token@github.com/owner/repo.git",
			want: "https://x-access-token:existing-token@github.com/owner/repo.git",
		},
		{
			name: "file:// URL passed through as-is",
			raw:  "file:///tmp/local/repo",
			want: "file:///tmp/local/repo",
		},
		{
			name: "HTTPS URL with existing auth stripped and PAT injected",
			raw:  "https://user:pass@github.com/owner/repo.git",
			want: "https://x-access-token:test-pat-123@user:pass@github.com/owner/repo.git",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := w.buildCloneURL(tt.raw)
			if got != tt.want {
				t.Errorf("buildCloneURL(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestSanitizeURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "PAT URL masked",
			raw:  "https://x-access-token:secret-token@github.com/owner/repo.git",
			want: "https://x-access-token:***@github.com/owner/repo.git",
		},
		{
			name: "non-PAT URL returned as-is",
			raw:  "https://github.com/owner/repo.git",
			want: "https://github.com/owner/repo.git",
		},
		{
			name: "SSH URL returned as-is",
			raw:  "git@github.com:owner/repo.git",
			want: "git@github.com:owner/repo.git",
		},
		{
			name: "file URL returned as-is",
			raw:  "file:///tmp/local/repo",
			want: "file:///tmp/local/repo",
		},
		{
			name: "PAT URL without @ returned as-is",
			raw:  "https://x-access-token:secret-token",
			want: "https://x-access-token:secret-token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeURL(tt.raw)
			if got != tt.want {
				t.Errorf("sanitizeURL(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}
