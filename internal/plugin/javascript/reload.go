package javascript

import (
	"errors"
	"sync"
)

// SetDiagnostic binds diagnostics after a detached runtime has committed. The
// callback must return promptly and must not reenter the runtime.
func (r *Runtime) SetDiagnostic(fn func(string, string)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.diagnosticFn = fn
}

// FreezeForReload closes all invocation and observation admission only when the
// VM, its async host work, and its observation queue are idle. It never waits
// for user work or cancels it. The caller resumes after rebinding every host.
func (r *Runtime) FreezeForReload() (func(), error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closing || r.retired || r.frozen || r.active != 0 || len(r.queue) != 0 || r.extension.pending.Load() != 0 {
		return nil, errors.New("plugin reload: JavaScript invocation or observation pending")
	}
	r.frozen = true
	return sync.OnceFunc(func() { r.mu.Lock(); r.frozen = false; r.mu.Unlock() }), nil
}

// RetireForReload prevents snapshotted old adapters from regaining admission.
// Close remains available so onClose still runs once after the committed swap.
func (r *Runtime) RetireForReload() {
	r.mu.Lock()
	r.retired = true
	r.mu.Unlock()
}
func (r *Runtime) beginReloadActivity() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.frozen || r.retired {
		return errors.New("JavaScript runtime reload admission closed")
	}
	r.active++
	return nil
}
func (r *Runtime) endReloadActivity() { r.mu.Lock(); r.active--; r.mu.Unlock() }
