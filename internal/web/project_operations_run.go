//go:build darwin || linux

package web

import (
	"context"
	"path/filepath"
	"time"
)

// change serializes durable transitions against HTTP revision-CAS controls.
func (m *ProjectOperations) change(id string, apply func(*ProjectOperation) error) (ProjectOperation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(m.ctx), registryTimeout)
	defer cancel()
	op, err := m.registry.operationGet(ctx, id)
	if err != nil {
		return op, err
	}
	previous := op.Revision
	if err := apply(&op); err != nil {
		return op, err
	}
	op.Revision++
	op.UpdatedAt = max(op.UpdatedAt, m.now().UnixMilli())
	return op, m.registry.operationUpdate(ctx, previous, op)
}
func (m *ProjectOperations) run(ctx context.Context, op ProjectOperation) {
	// An admitted row exists before even starting the runtime-free worker.
	worker, err := m.backend.Open(ctx, op)
	if err != nil {
		m.finish(op.ID, ProjectOperationTerminal{State: "failed", Outcome: "not_observed"}, "worker_unavailable")
		return
	}
	closed := false
	closeWorker := func() error {
		if closed {
			return nil
		}
		closed = true
		return worker.Close()
	}
	defer closeWorker()
	op, err = m.change(op.ID, func(current *ProjectOperation) error {
		if current.State != "admitted" {
			return ErrOperationConflict
		}
		current.State = "creating"
		return nil
	})
	if err != nil {
		closeErr := closeWorker()
		if op.State == "cancel_requested" && closeErr == nil {
			m.finish(op.ID, ProjectOperationTerminal{State: "canceled", Outcome: "not_observed"}, "canceled")
		} else {
			m.finish(op.ID, ProjectOperationTerminal{State: "needs_review", Outcome: "unknown"}, "storage_failed")
		}
		return
	}
	if ctx.Err() != nil {
		_ = closeWorker()
		m.finish(op.ID, ProjectOperationTerminal{State: "canceled", Outcome: "not_observed"}, "canceled")
		return
	}
	child, err := worker.Prepare(ctx, op)
	if err != nil {
		_ = closeWorker()
		m.finish(op.ID, ProjectOperationTerminal{State: "needs_review", Outcome: "unknown"}, "prepare_failed")
		return
	}
	// Validate exact parent/leaf correspondence as well as the opened identity.
	if child.Path != filepath.Join(op.Parent.Path, op.Name) || !matchesDirectoryIdentity(child) || !matchesDirectoryIdentity(op.Parent) {
		_ = closeWorker()
		m.finish(op.ID, ProjectOperationTerminal{State: "needs_review", Outcome: "unknown"}, "identity_changed")
		return
	}
	op, err = m.change(op.ID, func(current *ProjectOperation) error {
		if current.State != "creating" && current.State != "cancel_requested" {
			return ErrOperationConflict
		}
		current.Child = child
		current.Outcome = "observed"
		if current.State != "cancel_requested" {
			current.State = "running"
		}
		return nil
	})
	if err != nil {
		_ = closeWorker()
		m.finish(op.ID, ProjectOperationTerminal{State: "needs_review", Outcome: "unknown"}, "storage_failed")
		return
	}
	if op.State == "cancel_requested" || ctx.Err() != nil {
		m.cancelAndFinish(worker, closeWorker, op.ID)
		return
	}
	// This is the durable-manager ACK. A successful clone start is admission,
	// not completion; network work has its own future correlated terminal event.
	if err := worker.Start(ctx, op); err != nil {
		m.cancelAndFinish(worker, closeWorker, op.ID)
		return
	}
	terminal, err := worker.Wait(ctx)
	if err != nil {
		m.cancelAndFinish(worker, closeWorker, op.ID)
		return
	}
	if closeWorker() != nil {
		terminal = ProjectOperationTerminal{State: "needs_review", Outcome: "unknown"}
	}
	m.finish(op.ID, terminal, "")
}
func (m *ProjectOperations) cancelAndFinish(worker ProjectOperationWorker, closeWorker func() error, id string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(m.ctx), 5*time.Second)
	defer cancel()
	_ = worker.Cancel(ctx)
	terminal, err := worker.Wait(ctx)
	closeErr := closeWorker()
	if err != nil || closeErr != nil {
		terminal = ProjectOperationTerminal{State: "needs_review", Outcome: "unknown"}
	}
	m.finish(id, terminal, "")
}
func (m *ProjectOperations) finish(id string, terminal ProjectOperationTerminal, code string) {
	_, err := m.change(id, func(current *ProjectOperation) error {
		if !operationActive(current.State) {
			return ErrOperationConflict
		}
		switch terminal.State {
		case "succeeded", "failed", "canceled", "needs_review":
		default:
			terminal.State = "needs_review"
			terminal.Outcome = "unknown"
		}
		switch terminal.Outcome {
		case "observed", "not_observed", "unknown":
		default:
			terminal.Outcome = "unknown"
			terminal.State = "needs_review"
		}
		if terminal.State == "succeeded" && (current.Child == (DirectoryIdentity{}) || terminal.Outcome != "observed") {
			terminal.State = "needs_review"
			terminal.Outcome = "unknown"
		}
		current.State = terminal.State
		current.Outcome = terminal.Outcome
		current.Error = code
		switch current.State {
		case "succeeded":
			current.State = "awaiting_registration"
			current.Error = ""
		case "needs_review":
			if code == "" {
				current.Error = "worker_lost"
			}
		case "canceled":
			current.Error = "canceled"
		case "failed":
			if code == "" {
				current.Error = "clone_failed"
			}
		}
		return nil
	})
	if err != nil {
		// A failed terminal commit must never cause registration or dispatch.
		// Best-effort durable uncertainty is safe; if storage is still failing,
		// startup will interrupt the old nonterminal row without replaying it.
		_, _ = m.change(id, func(current *ProjectOperation) error {
			if !operationActive(current.State) {
				return ErrOperationConflict
			}
			current.State, current.Outcome, current.Error = "needs_review", "unknown", "storage_failed"
			return nil
		})
		return
	}
	// Successful filesystem work settles at awaiting_registration. Neither this
	// callback nor startup/read/reconcile paths may register a project; that
	// authority belongs only to a separate explicitly reviewed Register request.

}

func (m *ProjectOperations) Get(ctx context.Context, id string) (ProjectOperation, error) {
	op, err := m.registry.operationGet(ctx, id)
	return publicOperation(op), err
}

type ProjectOperationPage struct {
	Operations []ProjectOperation `json:"operations"`
	NextOffset int                `json:"next_offset"`
	HasMore    bool               `json:"has_more"`
}

func (m *ProjectOperations) List(ctx context.Context, offset int) (ProjectOperationPage, error) {
	page := ProjectOperationPage{Operations: []ProjectOperation{}, NextOffset: offset}
	if offset < 0 || offset > 128 {
		return page, ErrOperationInvalid
	}
	ops, err := m.registry.operationList(ctx, offset, 33)
	if err != nil {
		return page, err
	}
	// Also enforce the byte ceiling: paths can be long even on a 32-row page.
	for _, op := range ops {
		if len(page.Operations) == 32 {
			page.HasMore = true
			break
		}
		candidate := publicOperation(op)
		size := operationPublicSize(candidate)
		total := 256
		for _, existing := range page.Operations {
			total += operationPublicSize(existing) + 1
		}
		if total+size > 63<<10 {
			page.HasMore = true
			break
		}
		page.Operations = append(page.Operations, candidate)
		page.NextOffset++
	}
	return page, nil
}
func (m *ProjectOperations) Cancel(ctx context.Context, id string, revision int64) (ProjectOperation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	op, err := m.registry.operationGet(ctx, id)
	if err != nil {
		return ProjectOperation{}, err
	}
	if op.Revision != revision || !operationActive(op.State) {
		return ProjectOperation{}, ErrOperationConflict
	}
	if op.State == "cancel_requested" {
		return publicOperation(op), nil
	}
	previous := op.Revision
	op.Revision++
	op.State = "cancel_requested"
	op.UpdatedAt = max(op.UpdatedAt, m.now().UnixMilli())
	if err := m.registry.operationUpdate(ctx, previous, op); err != nil {
		return ProjectOperation{}, err
	}
	if run := m.active[id]; run != nil {
		run.cancel()
	}
	return publicOperation(op), nil
}
func (m *ProjectOperations) Reconcile(ctx context.Context, id string, revision int64) (ProjectOperation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	op, err := m.registry.operationGet(ctx, id)
	if err != nil {
		return ProjectOperation{}, err
	}
	if op.Revision != revision || operationActive(op.State) {
		return ProjectOperation{}, ErrOperationConflict
	}
	// Reconciliation observes only. No mkdir, clone, registration, or PID action.
	previous := op.Revision
	op.Revision++
	op.UpdatedAt = max(op.UpdatedAt, m.now().UnixMilli())
	if op.Child == (DirectoryIdentity{}) || !matchesDirectoryIdentity(op.Parent) || !matchesDirectoryIdentity(op.Child) {
		op.State = "needs_review"
		op.Outcome = "unknown"
		op.Error = "identity_changed"
	}
	if err := m.registry.operationUpdate(ctx, previous, op); err != nil {
		return ProjectOperation{}, err
	}
	return publicOperation(op), nil
}
func (m *ProjectOperations) Register(ctx context.Context, id string, revision int64, review bool) (ProjectOperation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	op, err := m.registry.registerOperation(ctx, id, revision, review)
	if err == nil {
		return publicOperation(op), nil
	}
	// Registration was explicitly requested but did not commit. Preserve the
	// destination and outcome, recording only a fixed error when its reviewed
	// revision is still current. Stale CAS attempts must not mutate metadata.
	current, readErr := m.registry.operationGet(ctx, id)
	if readErr == nil && current.State == "awaiting_registration" && current.Revision == revision {
		current.Revision++
		current.UpdatedAt = max(current.UpdatedAt, m.now().UnixMilli())
		current.Error = "registration_failed"
		_ = m.registry.operationUpdate(ctx, revision, current)
	}
	return ProjectOperation{}, err
}
func (m *ProjectOperations) Dismiss(ctx context.Context, id string, revision int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.active[id]; ok {
		return ErrOperationBusy
	}
	return m.registry.operationDismiss(ctx, id, revision)
}
