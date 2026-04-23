package reviewer

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/kmmuntasir/nano-review/internal/api"
	"github.com/kmmuntasir/nano-review/internal/storage"
)

type pendingReview struct {
	runID   string
	payload api.ReviewPayload
	key     string
}

type Queue struct {
	worker        *Worker
	store         storage.ReviewStore
	pending       chan pendingReview
	sem           chan struct{}
	maxConcurrent int
	maxQueueSize  int
	active        atomic.Int32
	waiting       atomic.Int32
	wg            sync.WaitGroup
	quit          chan struct{}
	startedAt     time.Time
	mu            sync.Mutex
	cancelMap     map[string]context.CancelFunc
	pendingMap    map[string]string
	staleRunIDs   map[string]bool
}

func NewQueue(worker *Worker, store storage.ReviewStore, maxConcurrent, maxQueueSize int) *Queue {
	return &Queue{
		worker:        worker,
		store:         store,
		pending:       make(chan pendingReview, maxQueueSize),
		sem:           make(chan struct{}, maxConcurrent),
		maxConcurrent: maxConcurrent,
		maxQueueSize:  maxQueueSize,
		quit:          make(chan struct{}),
		cancelMap:     make(map[string]context.CancelFunc),
		pendingMap:    make(map[string]string),
		staleRunIDs:   make(map[string]bool),
	}
}

func dedupKey(p api.ReviewPayload) string {
	owner, repo := parseRepoURL(p.RepoURL)
	return fmt.Sprintf("%s/%s:%d", owner, repo, p.PRNumber)
}

func (q *Queue) Start() {
	q.startedAt = time.Now()
	q.wg.Add(1)
	go q.dispatch()
}

func (q *Queue) dispatch() {
	defer q.wg.Done()
	for entry := range q.pending {
		q.mu.Lock()
		if q.staleRunIDs[entry.runID] {
			delete(q.staleRunIDs, entry.runID)
			if q.pendingMap[entry.key] == entry.runID {
				delete(q.pendingMap, entry.key)
			}
			q.mu.Unlock()

			if q.store != nil {
				_ = q.store.UpdateReview(context.Background(), entry.runID,
					storage.StatusCancelled, storage.ConclusionCancelled, 0, 0, "")
			}
			q.worker.BroadcastReviewUpdate(context.Background(), entry.runID,
				storage.StatusCancelled, string(storage.ConclusionCancelled), 0)
			q.waiting.Add(-1)
			continue
		}

		delete(q.pendingMap, entry.key)

		ctx, cancel := context.WithCancel(context.Background())
		q.cancelMap[entry.key] = cancel
		q.mu.Unlock()

		select {
		case q.sem <- struct{}{}:
			q.waiting.Add(-1)
			q.active.Add(1)
			q.wg.Add(1)
			go func(e pendingReview, reviewCtx context.Context, reviewCancel context.CancelFunc) {
				defer q.wg.Done()
				defer func() { <-q.sem }()
				defer q.active.Add(-1)
				defer func() {
					q.mu.Lock()
					delete(q.cancelMap, e.key)
					q.mu.Unlock()
					reviewCancel()
				}()
				if q.store != nil {
					_ = q.store.UpdateReview(context.Background(), e.runID,
						storage.StatusPending, "", 0, 0, "")
				}
				q.worker.processReview(reviewCtx, e.runID, e.payload)
			}(entry, ctx, cancel)
		case <-q.quit:
			cancel()
			q.mu.Lock()
			delete(q.cancelMap, entry.key)
			q.mu.Unlock()
			return
		}
	}
}

func (q *Queue) Stop() {
	close(q.quit)
	close(q.pending)
	done := make(chan struct{})
	go func() {
		q.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
	}
}

func (q *Queue) StartReview(_ context.Context, p api.ReviewPayload) (*api.StartResult, error) {
	runID := uuid.New().String()
	key := dedupKey(p)

	var cancelledRunID string

	q.mu.Lock()
	if cancelFn, ok := q.cancelMap[key]; ok {
		cancelFn()
		delete(q.cancelMap, key)
	}
	if oldRunID, ok := q.pendingMap[key]; ok {
		q.staleRunIDs[oldRunID] = true
		cancelledRunID = oldRunID
	}
	q.pendingMap[key] = runID
	q.mu.Unlock()

	if q.store != nil {
		record := storage.ReviewRecord{
			RunID:      runID,
			Repo:       p.RepoURL,
			PRNumber:   p.PRNumber,
			BaseBranch: p.BaseBranch,
			HeadBranch: p.HeadBranch,
			Status:     storage.StatusQueued,
			CreatedAt:  time.Now(),
		}
		_ = q.store.CreateReview(context.Background(), record)
	}

	result := &api.StartResult{
		RunID:          runID,
		Status:         "accepted",
		CancelledRunID: cancelledRunID,
	}

	if int(q.active.Load()) >= q.maxConcurrent {
		result.Status = "queued"
		depth := q.waiting.Load()
		result.QueueDepth = int(depth)
		result.RetryAfter = (int(depth)+1)*30/q.maxConcurrent + 1
	}

	q.waiting.Add(1)
	select {
	case q.pending <- pendingReview{runID: runID, payload: p, key: key}:
		return result, nil
	default:
		q.waiting.Add(-1)
		q.mu.Lock()
		if q.pendingMap[key] == runID {
			delete(q.pendingMap, key)
		}
		delete(q.staleRunIDs, runID)
		q.mu.Unlock()
		return nil, api.ErrQueueFull
	}
}

func (q *Queue) Stats() api.HealthResponse {
	return api.HealthResponse{
		Status:        "ok",
		ActiveReviews: q.active.Load(),
		QueuedReviews: q.waiting.Load(),
		MaxConcurrent: q.maxConcurrent,
		MaxQueueSize:  q.maxQueueSize,
		UptimeSeconds: int64(time.Since(q.startedAt).Seconds()),
	}
}
