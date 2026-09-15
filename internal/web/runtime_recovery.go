package web

import (
	"context"
	"errors"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// RecoveryState records only observed evidence, not whether a prompt or its
// effects were durably saved. RPC admission precedes agent.Prompt persistence.
type RecoveryState string

const (
	RecoveryBound            RecoveryState = "bound"
	RecoveryAdmissionUnknown RecoveryState = "admission_unknown"
	RecoveryAdmitted         RecoveryState = "admitted"
	RecoveryCompleted        RecoveryState = "completed"
	RecoveryFailed           RecoveryState = "failed"
	RecoveryCanceled         RecoveryState = "canceled"
	RecoveryRejected         RecoveryState = "rejected"
)

var ErrRecoveryStorage = errors.New("web: recovery metadata could not be saved; no prompt was sent")

// RecoveryHint is navigation evidence only: never a prompt, replay token,
// permission, process identity, or old instance authority. One hint per project
// replaces its predecessor; exact history remains exclusively in session storage.
type RecoveryHint struct {
	SessionID string        `json:"session_id"`
	State     RecoveryState `json:"state"`
	UpdatedAt time.Time     `json:"updated_at"`
}

// Message returns fixed public copy; neither storage nor worker errors escape.
func (h RecoveryHint) Message() string {
	switch h.State {
	case RecoveryAdmissionUnknown:
		return "Prompt admission is unknown. Review saved history before explicitly continuing; nothing will be replayed."
	case RecoveryAdmitted:
		return "Prompt was admitted, but completion was not observed. Admission does not prove it was saved. Review saved history before explicitly continuing."
	case RecoveryCompleted:
		return "Prompt completion was observed. Review saved history before explicitly continuing."
	case RecoveryFailed:
		return "Prompt failure was observed. Review saved history before explicitly retrying."
	case RecoveryCanceled:
		return "Prompt cancellation was observed. Review saved history before explicitly continuing."
	case RecoveryRejected:
		return "Prompt was not accepted. No retry was queued."
	case RecoveryBound:
		return "A saved conversation was previously opened. Opening it again requires explicit activation."
	default:
		return ""
	}
}

func (h RecoveryHint) valid() bool {
	if !runtimeIdentifier(h.SessionID) || h.UpdatedAt.IsZero() || h.UpdatedAt.UnixMilli() <= 0 {
		return false
	}
	switch h.State {
	case RecoveryBound, RecoveryAdmissionUnknown, RecoveryAdmitted, RecoveryCompleted, RecoveryFailed, RecoveryCanceled, RecoveryRejected:
		return true
	default:
		return false
	}
}

// RecoveryStore is optional for embedders and tests. Implementations must honor
// context deadlines; production uses the private, exclusively leased Registry.
type RecoveryStore interface {
	LoadRecovery(context.Context, string) (RecoveryHint, bool, error)
	SaveRecovery(context.Context, string, RecoveryHint) error
}

// Recovery is a metadata-only read. It never restores workers or session authority.
func (m *RuntimeManager) Recovery(ctx context.Context, projectID string) (RecoveryHint, bool, error) {
	if m.recovery == nil {
		return RecoveryHint{}, false, nil
	}
	ctx, cancel := context.WithTimeout(ctx, registryTimeout)
	defer cancel()
	return m.recovery.LoadRecovery(ctx, projectID)
}

// bindRecovery is called after authoritative session verification, outside r.mu
// and under the control gate. Reopening the same session retains its evidence.
func (r *liveRuntime) bindRecovery(sessionID string) error {
	r.recoveryMu.Lock()
	defer r.recoveryMu.Unlock()
	hint := RecoveryHint{SessionID: sessionID, State: RecoveryBound, UpdatedAt: time.Now().UTC()}
	if r.recoveryStore != nil {
		ctx, cancel := context.WithTimeout(r.ctx, registryTimeout)
		defer cancel()
		old, found, err := r.recoveryStore.LoadRecovery(ctx, r.project.ID)
		if err != nil {
			return ErrRuntimeUnavailable
		}
		if found && old.SessionID == sessionID {
			hint = old
		}
		if err := r.recoveryStore.SaveRecovery(ctx, r.project.ID, hint); err != nil {
			return ErrRuntimeUnavailable
		}
	}
	r.mu.Lock()
	r.snapshot.Recovery = hint
	r.mu.Unlock()
	return nil
}

// savePromptIntent must succeed before dispatch. It does not claim RPC admission
// or durable session persistence. The caller holds the nonqueued control gate.
func (r *liveRuntime) savePromptIntent() error {
	r.recoveryMu.Lock()
	defer r.recoveryMu.Unlock()
	r.mu.Lock()
	hint := RecoveryHint{SessionID: r.snapshot.SessionID, State: RecoveryAdmissionUnknown, UpdatedAt: time.Now().UTC()}
	r.mu.Unlock()
	if r.recoveryStore != nil {
		ctx, cancel := context.WithTimeout(r.ctx, registryTimeout)
		defer cancel()
		if err := r.recoveryStore.SaveRecovery(ctx, r.project.ID, hint); err != nil {
			return ErrRecoveryStorage
		}
	}
	r.mu.Lock()
	r.snapshot.Recovery = hint
	r.mu.Unlock()
	return nil
}

// completeRecoveryLocked is only called for an accepted, correlated completion.
// Call persistRecovery after unlocking. An ack arriving later cannot downgrade it.
func (r *liveRuntime) completeRecoveryLocked(status protocol.RPCPromptStatus) {
	state := RecoveryCompleted
	switch status {
	case protocol.RPCPromptFailedStatus:
		state = RecoveryFailed
	case protocol.RPCPromptCanceledStatus:
		state = RecoveryCanceled
	case protocol.RPCPromptCompletedStatus:
	default:
		return
	}
	r.snapshot.Recovery.State = state
	r.snapshot.Recovery.UpdatedAt = time.Now().UTC()
}

// persistRecovery serializes writes and samples current evidence after taking
// that gate, so delayed acks cannot overwrite a newer completion. Cleanup uses a
// bounded independent context because failure cancels the worker context. A
// failed best-effort outcome write leaves the pre-dispatch intent conservative.
func (r *liveRuntime) persistRecovery() {
	if r.recoveryStore == nil {
		return
	}
	r.recoveryMu.Lock()
	defer r.recoveryMu.Unlock()
	r.mu.Lock()
	hint := r.snapshot.Recovery
	r.mu.Unlock()
	if !hint.valid() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), registryTimeout)
	defer cancel()
	_ = r.recoveryStore.SaveRecovery(ctx, r.project.ID, hint)
}

// Transport loss cannot prove tool cancellation or reversal of process effects.
func (r *liveRuntime) unknownActivitiesLocked() {
	r.activityCanceled = true
	for i := range r.snapshot.Activities {
		if r.snapshot.Activities[i].Status == "running" {
			r.snapshot.Activities[i].Status = "unknown"
		}
	}
}
