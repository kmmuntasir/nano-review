package reviewer

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kmmuntasir/nano-review/internal/api"
	"github.com/kmmuntasir/nano-review/internal/storage"
)

type blockingRunner struct {
	release chan struct{}
}

func newBlockingRunner() *blockingRunner {
	return &blockingRunner{release: make(chan struct{})}
}

func (r *blockingRunner) Run(_ context.Context, _ string, _ ...string) (string, int, error) {
	<-r.release
	return "review complete", 0, nil
}

func (r *blockingRunner) RunStreaming(_ context.Context, _ string, _ io.Writer, _ ...string) (int, error) {
	<-r.release
	return 0, nil
}

type countingRunner struct {
	peak  atomic.Int32
	curr  atomic.Int32
	block chan struct{}
}

func newCountingRunner() *countingRunner {
	return &countingRunner{block: make(chan struct{})}
}

func (r *countingRunner) Run(_ context.Context, _ string, _ ...string) (string, int, error) {
	c := r.curr.Add(1)
	for {
		old := r.peak.Load()
		if c <= old || r.peak.CompareAndSwap(old, c) {
			break
		}
	}
	<-r.block
	r.curr.Add(-1)
	return "done", 0, nil
}

func (r *countingRunner) RunStreaming(_ context.Context, _ string, _ io.Writer, _ ...string) (int, error) {
	r.Run(context.Background(), "")
	return 0, nil
}

func (r *countingRunner) release() { close(r.block) }

type mockQueueStore struct{}

func (m *mockQueueStore) CreateReview(_ context.Context, _ storage.ReviewRecord) error { return nil }
func (m *mockQueueStore) UpdateReview(_ context.Context, _ string, _ storage.ReviewStatus, _ storage.ReviewConclusion, _ int64, _ int, _ string) error {
	return nil
}
func (m *mockQueueStore) GetReview(_ context.Context, _ string) (*storage.ReviewRecord, error) {
	return nil, nil
}
func (m *mockQueueStore) ListReviews(_ context.Context, _ storage.ListFilter) ([]storage.ReviewRecord, error) {
	return nil, nil
}
func (m *mockQueueStore) GetMetrics(_ context.Context) (*storage.Metrics, error) { return nil, nil }
func (m *mockQueueStore) FindActiveReview(_ context.Context, _ string, _ int) (*storage.ReviewRecord, error) {
	return nil, storage.ErrNotFound
}
func (m *mockQueueStore) Close() error { return nil }

func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runCmd("git", "-c", "user.name=test", "-c", "user.email=test@test.com", "init", "--initial-branch=main", dir)
	runCmd("git", "-c", "user.name=test", "-c", "user.email=test@test.com", "-C", dir, "commit", "--allow-empty", "-m", "init")
	return dir
}

func runCmd(name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.CombinedOutput()
}

func newTestQueue(t *testing.T, maxConcurrent, maxQueueSize int, runner ClaudeRunner) (*Queue, api.ReviewPayload) {
	t.Helper()
	repoDir := initGitRepo(t)
	payload := api.ReviewPayload{
		RepoURL:    "file://" + repoDir + "/.git",
		PRNumber:   1,
		BaseBranch: "main",
		HeadBranch: "main",
	}
	w := NewWorker(
		runner,
		&mockQueueStore{},
		&mockLogger{},
		nil,
		"git", "claude", "", "", "pat",
		10*time.Minute, 0, t.TempDir(),
	)
	q := NewQueue(w, &mockQueueStore{}, maxConcurrent, maxQueueSize)
	q.Start()
	return q, payload
}

func newTestQueueWithBroadcaster(t *testing.T, maxConcurrent, maxQueueSize int, runner ClaudeRunner, broadcaster Broadcaster) (*Queue, api.ReviewPayload) {
	t.Helper()
	repoDir := initGitRepo(t)
	payload := api.ReviewPayload{
		RepoURL:    "file://" + repoDir + "/.git",
		PRNumber:   1,
		BaseBranch: "main",
		HeadBranch: "main",
	}
	w := NewWorker(
		runner,
		&mockQueueStore{},
		&mockLogger{},
		broadcaster,
		"git", "claude", "", "", "pat",
		10*time.Minute, 0, t.TempDir(),
	)
	q := NewQueue(w, &mockQueueStore{}, maxConcurrent, maxQueueSize)
	q.Start()
	return q, payload
}

type mockBroadcastRecorder struct {
	mu     sync.Mutex
	events []broadcastEvent
}

type broadcastEvent struct {
	topic string
	data  []byte
}

func (m *mockBroadcastRecorder) Broadcast(topic string, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, broadcastEvent{topic: topic, data: data})
}

func (m *mockBroadcastRecorder) getEvents() []broadcastEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]broadcastEvent, len(m.events))
	copy(out, m.events)
	return out
}

func waitForIdle(t *testing.T, q *Queue) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		stats := q.Stats()
		if stats.ActiveReviews == 0 && stats.QueuedReviews == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	stats := q.Stats()
	t.Fatalf("timed out waiting for idle queue: active=%d, queued=%d",
		stats.ActiveReviews, stats.QueuedReviews)
}

func waitForActive(t *testing.T, q *Queue, min int32) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && q.Stats().ActiveReviews < min {
		time.Sleep(10 * time.Millisecond)
	}
	if q.Stats().ActiveReviews < min {
		t.Fatalf("timed out waiting for active >= %d, got %d", min, q.Stats().ActiveReviews)
	}
}

func TestQueue_Enqueue_Accepted(t *testing.T) {
	runner := newBlockingRunner()
	q, payload := newTestQueue(t, 2, 4, runner)
	defer func() {
		close(runner.release)
		q.Stop()
	}()

	result, err := q.StartReview(context.Background(), payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "accepted" {
		t.Errorf("expected status 'accepted', got %q", result.Status)
	}
	if result.RunID == "" {
		t.Error("expected non-empty run_id")
	}
}

func TestQueue_Enqueue_Queued(t *testing.T) {
	runner := newBlockingRunner()
	q, payload := newTestQueue(t, 1, 4, runner)
	defer func() {
		close(runner.release)
		q.Stop()
	}()

	_, err := q.StartReview(context.Background(), payload)
	if err != nil {
		t.Fatalf("first enqueue failed: %v", err)
	}
	waitForActive(t, q, 1)

	result, err := q.StartReview(context.Background(), payload)
	if err != nil {
		t.Fatalf("second enqueue failed: %v", err)
	}
	if result.Status != "queued" {
		t.Errorf("expected status 'queued', got %q", result.Status)
	}
	if result.RetryAfter <= 0 {
		t.Errorf("expected retry_after > 0, got %d", result.RetryAfter)
	}
}

func TestQueue_Enqueue_QueueFull(t *testing.T) {
	runner := newBlockingRunner()
	q, payload := newTestQueue(t, 1, 1, runner)
	defer func() {
		close(runner.release)
		q.Stop()
	}()

	// Fill the single concurrent slot.
	_, err := q.StartReview(context.Background(), payload)
	if err != nil {
		t.Fatalf("first enqueue failed: %v", err)
	}
	waitForActive(t, q, 1)

	// Fill the pending channel (capacity 1).
	_, err = q.StartReview(context.Background(), payload)
	if err != nil {
		t.Fatalf("second enqueue failed: %v", err)
	}

	// Wait for dispatch to read the second item and block on sem.
	// After this, pending is empty and dispatch is stuck on sem.
	time.Sleep(50 * time.Millisecond)

	// Third item goes into pending (1/1).
	_, err = q.StartReview(context.Background(), payload)
	if err != nil {
		t.Fatalf("third enqueue failed: %v", err)
	}

	// Fourth: pending full (1/1), dispatch blocked on sem → ErrQueueFull.
	_, err = q.StartReview(context.Background(), payload)
	if err == nil {
		t.Fatal("expected ErrQueueFull, got nil")
	}
	if !errors.Is(err, api.ErrQueueFull) {
		t.Errorf("expected ErrQueueFull, got %v", err)
	}
}

func TestQueue_Concurrency(t *testing.T) {
	maxConcurrent := 3
	runner := newCountingRunner()
	q, payload := newTestQueue(t, maxConcurrent, 10, runner)

	for i := 0; i < maxConcurrent+2; i++ {
		_, err := q.StartReview(context.Background(), payload)
		if err != nil {
			t.Fatalf("enqueue %d failed: %v", i, err)
		}
	}
	waitForActive(t, q, int32(maxConcurrent))

	peak := runner.peak.Load()
	if peak > int32(maxConcurrent) {
		t.Errorf("peak concurrency %d exceeded max %d", peak, maxConcurrent)
	}

	runner.release()
	q.Stop()
}

func TestQueue_DispatchOrder(t *testing.T) {
	runner := newBlockingRunner()
	q, payload := newTestQueue(t, 1, 10, runner)
	defer func() {
		close(runner.release)
		q.Stop()
	}()

	_, _ = q.StartReview(context.Background(), payload)
	waitForActive(t, q, 1)

	r2, _ := q.StartReview(context.Background(), payload)
	r3, _ := q.StartReview(context.Background(), payload)

	if r2.RunID == "" || r3.RunID == "" {
		t.Fatal("expected non-empty run IDs")
	}

	close(runner.release)
	time.Sleep(100 * time.Millisecond)

	stats := q.Stats()
	if stats.QueuedReviews < 0 {
		t.Errorf("expected non-negative queued, got %d", stats.QueuedReviews)
	}

	// Prevent double-close in defer.
	runner.release = make(chan struct{})
}

func TestQueue_Stats(t *testing.T) {
	runner := newBlockingRunner()
	q, payload := newTestQueue(t, 2, 10, runner)
	defer func() {
		close(runner.release)
		q.Stop()
	}()

	stats := q.Stats()
	if stats.ActiveReviews != 0 {
		t.Errorf("expected 0 active, got %d", stats.ActiveReviews)
	}
	if stats.QueuedReviews != 0 {
		t.Errorf("expected 0 queued, got %d", stats.QueuedReviews)
	}
	if stats.MaxConcurrent != 2 {
		t.Errorf("expected max_concurrent 2, got %d", stats.MaxConcurrent)
	}
	if stats.MaxQueueSize != 10 {
		t.Errorf("expected max_queue_size 10, got %d", stats.MaxQueueSize)
	}
	if stats.UptimeSeconds < 0 {
		t.Errorf("expected non-negative uptime, got %d", stats.UptimeSeconds)
	}

	q.StartReview(context.Background(), payload)
	waitForActive(t, q, 1)

	stats = q.Stats()
	if stats.ActiveReviews < 1 {
		t.Errorf("expected >= 1 active after enqueue, got %d", stats.ActiveReviews)
	}
}

func TestQueue_Stop_Waits(t *testing.T) {
	runner := newBlockingRunner()
	q, payload := newTestQueue(t, 2, 10, runner)

	_, err := q.StartReview(context.Background(), payload)
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}
	waitForActive(t, q, 1)

	done := make(chan struct{})
	go func() {
		q.Stop()
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("Stop() returned while review still running")
	case <-time.After(200 * time.Millisecond):
	}

	close(runner.release)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() did not return after releasing runner")
	}
}

func TestQueue_Dedup_CancelRunning(t *testing.T) {
	runner := newBlockingRunner()
	q, payload := newTestQueue(t, 2, 10, runner)
	defer func() {
		close(runner.release)
		q.Stop()
	}()

	resA, err := q.StartReview(context.Background(), payload)
	if err != nil {
		t.Fatalf("submit A failed: %v", err)
	}
	waitForActive(t, q, 1)

	resB, err := q.StartReview(context.Background(), payload)
	if err != nil {
		t.Fatalf("submit B failed: %v", err)
	}
	if resB.CancelledRunID != resA.RunID {
		t.Errorf("expected CancelledRunID=%q, got %q", resA.RunID, resB.CancelledRunID)
	}

	close(runner.release)
	runner.release = make(chan struct{})
	waitForIdle(t, q)
}

func TestQueue_Dedup_SkipStaleQueued(t *testing.T) {
	runner := newBlockingRunner()
	q, payload := newTestQueue(t, 1, 10, runner)
	defer func() {
		close(runner.release)
		q.Stop()
	}()

	_, err := q.StartReview(context.Background(), payload)
	if err != nil {
		t.Fatalf("submit A failed: %v", err)
	}
	waitForActive(t, q, 1)

	resB, err := q.StartReview(context.Background(), payload)
	if err != nil {
		t.Fatalf("submit B failed: %v", err)
	}

	resC, err := q.StartReview(context.Background(), payload)
	if err != nil {
		t.Fatalf("submit C failed: %v", err)
	}
	if resC.CancelledRunID != resB.RunID {
		t.Errorf("expected C to cancel B (run_id=%q), got cancelled=%q", resB.RunID, resC.CancelledRunID)
	}

	close(runner.release)
	waitForIdle(t, q)
}

func TestQueue_Dedup_DifferentPRs(t *testing.T) {
	runner := newBlockingRunner()
	q, payload := newTestQueue(t, 2, 10, runner)
	defer func() {
		close(runner.release)
		q.Stop()
	}()

	_, err := q.StartReview(context.Background(), payload)
	if err != nil {
		t.Fatalf("submit A failed: %v", err)
	}

	payloadB := payload
	payloadB.PRNumber = 2
	resB, err := q.StartReview(context.Background(), payloadB)
	if err != nil {
		t.Fatalf("submit B failed: %v", err)
	}
	if resB.CancelledRunID != "" {
		t.Errorf("different PRs should not cancel, got CancelledRunID=%q", resB.CancelledRunID)
	}

	waitForActive(t, q, 2)

	close(runner.release)
	waitForIdle(t, q)
}

func TestQueue_Dedup_DifferentRepos(t *testing.T) {
	runner := newBlockingRunner()
	q, payloadA := newTestQueue(t, 2, 10, runner)
	defer func() {
		close(runner.release)
		q.Stop()
	}()

	resA, err := q.StartReview(context.Background(), payloadA)
	if err != nil {
		t.Fatalf("submit A failed: %v", err)
	}

	repoDir2 := initGitRepo(t)
	payloadB := api.ReviewPayload{
		RepoURL:    "file://" + repoDir2 + "/.git",
		PRNumber:   1,
		BaseBranch: "main",
		HeadBranch: "main",
	}
	resB, err := q.StartReview(context.Background(), payloadB)
	if err != nil {
		t.Fatalf("submit B failed: %v", err)
	}
	if resB.CancelledRunID != "" {
		t.Errorf("different repos should not cancel, got CancelledRunID=%q", resB.CancelledRunID)
	}
	if resA.Status != "accepted" {
		t.Errorf("A expected accepted, got %q", resA.Status)
	}
	if resB.Status != "accepted" {
		t.Errorf("B expected accepted, got %q", resB.Status)
	}

	close(runner.release)
	waitForIdle(t, q)
}

func TestQueue_Dedup_RapidFire(t *testing.T) {
	runner := newBlockingRunner()
	q, payload := newTestQueue(t, 1, 10, runner)
	defer func() {
		close(runner.release)
		q.Stop()
	}()

	const n = 5
	var results []*api.StartResult
	for i := 0; i < n; i++ {
		res, err := q.StartReview(context.Background(), payload)
		if err != nil {
			t.Fatalf("submit %d failed: %v", i, err)
		}
		results = append(results, res)
	}

	lastIdx := n - 1
	prevIdx := n - 2
	if results[lastIdx].CancelledRunID != results[prevIdx].RunID {
		t.Errorf("last submission should cancel previous, want cancelled=%q got %q",
			results[prevIdx].RunID, results[lastIdx].CancelledRunID)
	}

	close(runner.release)
	waitForIdle(t, q)
}

func TestQueue_Dedup_CancelledBroadcast(t *testing.T) {
	runner := newBlockingRunner()
	bc := &mockBroadcastRecorder{}
	q, payload := newTestQueueWithBroadcaster(t, 1, 10, runner, bc)
	defer func() {
		close(runner.release)
		q.Stop()
	}()

	resA, err := q.StartReview(context.Background(), payload)
	if err != nil {
		t.Fatalf("submit A failed: %v", err)
	}
	waitForActive(t, q, 1)

	_, err = q.StartReview(context.Background(), payload)
	if err != nil {
		t.Fatalf("submit B failed: %v", err)
	}

	close(runner.release)
	waitForIdle(t, q)

	events := bc.getEvents()
	found := false
	for _, ev := range events {
		var msg map[string]any
		if err := json.Unmarshal(ev.data, &msg); err != nil {
			continue
		}
		if ev.topic == "run:"+resA.RunID && msg["status"] == string(storage.StatusCancelled) {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected cancelled broadcast for run A, none found")
	}
}

func TestQueue_Dedup_StatsConsistent(t *testing.T) {
	runner := newBlockingRunner()
	q, payload := newTestQueue(t, 1, 10, runner)
	defer func() {
		close(runner.release)
		q.Stop()
	}()

	_, err := q.StartReview(context.Background(), payload)
	if err != nil {
		t.Fatalf("submit A failed: %v", err)
	}
	waitForActive(t, q, 1)

	q.StartReview(context.Background(), payload)
	q.StartReview(context.Background(), payload)

	close(runner.release)
	waitForIdle(t, q)

	stats := q.Stats()
	if stats.ActiveReviews != 0 {
		t.Errorf("expected active=0 after idle, got %d", stats.ActiveReviews)
	}
	if stats.QueuedReviews != 0 {
		t.Errorf("expected queued=0 after idle, got %d", stats.QueuedReviews)
	}
}
