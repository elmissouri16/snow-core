package web

import (
	"context"
	"sync"
)

// sidebarReadAdmission keeps background reads out of the mutation mutex. An
// explicit owner change preempts same-project reads instead of queuing behind
// their normal completion. Only their canceled I/O teardown precedes activation.
type sidebarReadAdmission struct {
	mu      sync.Mutex
	reads   map[*sidebarRead]string
	blocked map[string]bool
}

type sidebarRead struct {
	owner  *sidebarReadAdmission
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

func (a *sidebarReadAdmission) begin(ctx context.Context, projectID string) (context.Context, *sidebarRead, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.blocked[projectID] || len(a.reads) >= 4 || ctx.Err() != nil {
		return nil, nil, ErrRuntimeBusy
	}
	if a.reads == nil {
		a.reads = make(map[*sidebarRead]string)
	}
	ctx, cancel := context.WithCancel(ctx)
	read := &sidebarRead{owner: a, cancel: cancel, done: make(chan struct{})}
	a.reads[read] = projectID
	return ctx, read, nil
}

func (r *sidebarRead) finish() {
	r.once.Do(func() {
		r.owner.mu.Lock()
		delete(r.owner.reads, r)
		close(r.done)
		r.owner.mu.Unlock()
	})
}

// preempt is called after explicit control authorization or authenticated
// foreground catalog navigation. No read is retried. Catalog implementations
// must return after canceled worker teardown;
// a backend that cannot stop within the control context fails closed.
func (a *sidebarReadAdmission) preempt(ctx context.Context, projectID string) (func(), error) {
	a.mu.Lock()
	if a.blocked[projectID] {
		a.mu.Unlock()
		return nil, ErrRuntimeBusy
	}
	if a.blocked == nil {
		a.blocked = make(map[string]bool)
	}
	a.blocked[projectID] = true
	var reads []*sidebarRead
	for read, id := range a.reads {
		if id == projectID {
			read.cancel()
			reads = append(reads, read)
		}
	}
	a.mu.Unlock()
	release := func() { a.mu.Lock(); delete(a.blocked, projectID); a.mu.Unlock() }
	for _, read := range reads {
		select {
		case <-read.done:
		case <-ctx.Done():
			release()
			return nil, ErrRuntimeBusy
		}
	}
	if ctx.Err() != nil {
		release()
		return nil, ErrRuntimeBusy
	}
	return release, nil
}
