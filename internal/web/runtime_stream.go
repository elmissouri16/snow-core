package web

const runtimeSubscriberLimit = 32

// runtimeSubscription retains only a wakeup, never a second transcript or an
// unbounded token queue. All fields except the immutable binding use runtime.mu.
type runtimeSubscription struct {
	runtime  *liveRuntime
	instance string
	changes  chan struct{}
	closed   bool
}

// Subscribe registers and snapshots under the same runtime lock. Holding the
// manager lock also makes admission atomic with worker removal/replacement.
func (m *RuntimeManager) Subscribe(projectID, instanceID string) (RuntimeSnapshot, RuntimeSubscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.workers[projectID]
	if m.closed || r == nil {
		return RuntimeSnapshot{}, nil, ErrRuntimeClosed
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if instanceID == "" || r.instanceID != instanceID || r.snapshot.Status == "closing" {
		return RuntimeSnapshot{}, nil, ErrRuntimeClosed
	}
	if len(r.subscribers) >= runtimeSubscriberLimit {
		return RuntimeSnapshot{}, nil, ErrRuntimeBusy
	}
	if r.subscribers == nil {
		r.subscribers = make(map[*runtimeSubscription]struct{})
	}
	sub := &runtimeSubscription{runtime: r, instance: instanceID, changes: make(chan struct{}, 1)}
	r.subscribers[sub] = struct{}{}
	return r.snapshot.clone(), sub, nil
}

func (s *runtimeSubscription) Changes() <-chan struct{} { return s.changes }

func (s *runtimeSubscription) Snapshot() (RuntimeSnapshot, bool) {
	r := s.runtime
	r.mu.Lock()
	defer r.mu.Unlock()
	if s.closed || r.instanceID != s.instance || r.snapshot.Status == "closing" {
		return RuntimeSnapshot{}, false
	}
	return r.snapshot.clone(), true
}

func (s *runtimeSubscription) Close() {
	r := s.runtime
	r.mu.Lock()
	defer r.mu.Unlock()
	if !s.closed {
		s.closed = true
		delete(r.subscribers, s)
		close(s.changes)
	}
}

// publishLocked is the sole revision commit point. Callers hold r.mu; neither
// Markdown rendering nor a network write can block the RPC event drain here.
func (r *liveRuntime) publishLocked() {
	r.refreshQueueLocked()
	r.refreshSteerLocked()
	r.refreshVersionsLocked()
	r.snapshot.Revision++
	for sub := range r.subscribers {
		select {
		case sub.changes <- struct{}{}:
		default:
		}
	}
}

var _ RuntimeSubscriber = (*RuntimeManager)(nil)
var _ RuntimeSubscription = (*runtimeSubscription)(nil)
