package session

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// ErrHistoryControlUnknown means commit/publication was attempted but its
// outcome could not be established. Callers must refresh, never retry blindly.
var ErrHistoryControlUnknown = errors.New("history control outcome requires authoritative refresh")

// HistoryControlStore linearizes all binding checks and mutations (or immutable
// source capture) under one store lock / SQLite writer reservation. Errors other
// than ErrHistoryControlUnknown establish that no durable mutation committed.
type HistoryControlStore interface {
	ForkHistoryBranch(context.Context, protocol.RPCManagedBranchForkParams, protocol.CollaborationMode) (protocol.SessionBranch, error)
	RenameHistoryBranch(context.Context, protocol.RPCManagedBranchRenameParams) (protocol.SessionBranch, error)
	CaptureHistoryFork(context.Context, protocol.RPCManagedSessionForkParams, protocol.CollaborationMode) (*HistoryForkSnapshot, error)
}

// HistoryForkSnapshot contains a private, bounded, immutable source chain. It
// must never be serialized as public history. Capture is the source CAS
// linearization point; publication does not reread a mutable source cursor.
type HistoryForkSnapshot struct{ snapshot forkSnapshot }

// CreateHistoryFork publishes only the immutable capture, after the source
// writer reservation has been released. Destination allocation and provenance
// persistence use the existing atomic session index publication mechanism.
func (f *FileIndex) CreateHistoryFork(ctx context.Context, cwd string, capture *HistoryForkSnapshot) (Store, protocol.SessionForkResult, error) {
	if capture == nil {
		return nil, protocol.SessionForkResult{}, errors.New("session: missing history fork capture")
	}
	if err := ctx.Err(); err != nil {
		return nil, protocol.SessionForkResult{}, err
	}
	child, result, err := f.createForkSnapshot(cwd, capture.snapshot, protocol.SessionForkOptions{Name: capture.snapshot.name})
	if err != nil {
		return child, result, errors.Join(ErrHistoryControlUnknown, err)
	}
	return child, result, nil
}

func validateHistoryBinding(p protocol.RPCHistoryControlBinding) error {
	for _, id := range []string{p.SessionID, p.SourceBranchID, p.TargetBranchID} {
		if !ValidVersionIdentity(id, false) {
			return ErrBranchVersionStale
		}
	}
	if !ValidVersionIdentity(p.SourceTipID, true) || !ValidVersionIdentity(p.TargetTipID, true) {
		return ErrBranchVersionStale
	}
	return nil
}
func validateHistoryName(name string, maxRunes int) error {
	if name == "" || name != strings.TrimSpace(name) || len(name) > protocol.RPCHistoryControlMaxNameBytes || !utf8.ValidString(name) || utf8.RuneCountInString(name) > maxRunes || strings.ContainsFunc(name, unicode.IsControl) {
		return errors.New("session: invalid bounded history display name")
	}
	return nil
}
func historySessionBranch(b protocol.RPCBranchVersion) protocol.SessionBranch {
	return protocol.SessionBranch{ID: b.ID, Name: b.Name, ParentID: b.ParentID, ForkedFromID: b.ForkedFromID, TipID: b.TipID, CreatedAt: b.CreatedAt, UpdatedAt: b.UpdatedAt, Active: b.Active}
}
func capturedHistoryFork(p protocol.RPCManagedSessionForkParams, snapshot BranchVersionSnapshot) *HistoryForkSnapshot {
	return &HistoryForkSnapshot{snapshot: forkSnapshot{entries: snapshot.Entries, sourceSessionID: p.SessionID, sourceBranchID: p.TargetBranchID, sourceEntryID: p.TargetTipID, name: p.Name, mode: snapshot.Mode, copyMode: true}}
}
