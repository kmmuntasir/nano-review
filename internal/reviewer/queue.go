package reviewer

import (
	"context"
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
	}
}

func (q *Queue) Start() {
	q.startedAt = time.Now()
	q.wg.Add(1)
	go q.dispatch()
}

func (q *Queue) dispatch() {
	defer q.wg.Done()
	for entry := range q.pending {
		select {
		case q.sem <- struct{}{}:
			q.waiting.Add(-1)
			q.active.Add(1)
			q.wg.Add(1)
			go func(e pendingReview) {
				defer q.wg.Done()
				defer func() { <-q.sem }()
				defer q.active.Add(-1)
				q.worker.processReview(context.Background(), e.runID, e.payload)
			}(entry)
		case <-q.quit:
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
		RunID:  runID,
		Status: "accepted",
	}

	if int(q.active.Load()) >= q.maxConcurrent {
		result.Status = "queued"
		depth := q.waiting.Load()
		result.QueueDepth = int(depth)
		result.RetryAfter = (int(depth)+1)*30/q.maxConcurrent + 1
	}

	q.waiting.Add(1)
	select {
	case q.pending <- pendingReview{runID: runID, payload: p}:
		return result, nil
	default:
		q.waiting.Add(-1)
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
