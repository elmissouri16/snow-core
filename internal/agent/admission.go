package agent

import (
	"context"
	"sync"
)

// admissionMutex preserves atomic control transactions while allowing canceled
// tools to leave the admission queue. Controls may join a canceled turn while
// holding admission, so its tools must never wait unconditionally for this lock.
// The zero value is ready for use, including agents constructed by package tests.
type admissionMutex struct {
	once  sync.Once
	token chan struct{}
}

func (m *admissionMutex) Lock()   { _ = m.LockContext(context.Background()) }
func (m *admissionMutex) Unlock() { <-m.token }
func (m *admissionMutex) LockContext(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	m.once.Do(func() { m.token = make(chan struct{}, 1) })
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case m.token <- struct{}{}:
		// Cancellation wins even if admission became available at the same time.
		if err := ctx.Err(); err != nil {
			m.Unlock()
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// LockAdmissionContext is the cancelable form used by tools and prompt callers.
// On failure no lock is held and the returned release function is nil.
func (a *Agent) LockAdmissionContext(ctx context.Context) (func(), error) {
	if err := a.admissionMu.LockContext(ctx); err != nil {
		return nil, err
	}
	return a.admissionMu.Unlock, nil
}
