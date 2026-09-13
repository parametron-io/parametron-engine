package api

import (
	"context"
	"errors"
	"strings"
	"sync"

	"parametron/internal/engine/handoff"
)

var (
	errQueuedPackageRequired = errors.New("queued package is required")
	errQueuedJobIDRequired   = errors.New("queued package job id is required")
)

type JobQueue interface {
	Enqueue(pkg *handoff.Package) error
	Dequeue() (*handoff.Package, bool)
	Peek() (*handoff.Package, bool)
	Len() int
	Contains(jobID string) bool
}

type blockingJobQueue interface {
	JobQueue
	WaitDequeue(ctx context.Context) (*handoff.Package, bool)
}

type inMemoryJobQueue struct {
	mu       sync.Mutex
	order    []string
	packages map[string]handoff.Package
	notify   chan struct{}
}

func newInMemoryJobQueue() JobQueue {
	return &inMemoryJobQueue{
		order:    make([]string, 0),
		packages: make(map[string]handoff.Package),
		notify:   make(chan struct{}, 1),
	}
}

func (q *inMemoryJobQueue) Enqueue(pkg *handoff.Package) error {
	if pkg == nil {
		return errQueuedPackageRequired
	}
	jobID := strings.TrimSpace(pkg.JobID)
	if jobID == "" {
		return errQueuedJobIDRequired
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	if _, exists := q.packages[jobID]; exists {
		return nil
	}

	cloned := clonePackage(pkg)
	cloned.JobID = jobID
	q.packages[jobID] = *cloned
	q.order = append(q.order, jobID)

	select {
	case q.notify <- struct{}{}:
	default:
	}
	return nil
}

func (q *inMemoryJobQueue) Dequeue() (*handoff.Package, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.order) == 0 {
		return nil, false
	}

	jobID := q.order[0]
	q.order = q.order[1:]
	pkg := q.packages[jobID]
	delete(q.packages, jobID)
	return clonePackage(&pkg), true
}

func (q *inMemoryJobQueue) Peek() (*handoff.Package, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.order) == 0 {
		return nil, false
	}

	pkg := q.packages[q.order[0]]
	return clonePackage(&pkg), true
}

func (q *inMemoryJobQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()

	return len(q.order)
}

func (q *inMemoryJobQueue) Contains(jobID string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	_, ok := q.packages[strings.TrimSpace(jobID)]
	return ok
}

func (q *inMemoryJobQueue) WaitDequeue(ctx context.Context) (*handoff.Package, bool) {
	for {
		if pkg, ok := q.Dequeue(); ok {
			return pkg, true
		}

		select {
		case <-ctx.Done():
			return nil, false
		case <-q.notify:
		}
	}
}
