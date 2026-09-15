package session

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

var ErrBranchVersionStale = errors.New("session: branch version identity is stale")
var ErrBranchRestoreUnknown = errors.New("session: branch restore outcome requires authoritative refresh")

// BranchVersionStore is deliberately separate from legacy unbounded branch
// listing and selection. A successful selection compares both branches and tips
// under the same writer reservation; an error never proves absence of mutation.
type BranchVersionStore interface {
	BranchVersions(context.Context, string, int) (protocol.RPCBranchesPage, error)
	BranchVersion(context.Context, string) (BranchVersionSnapshot, error)
	RestoreBranchVersion(context.Context, protocol.RPCBranchRestorePrepareParams, protocol.CollaborationMode) error
	ProbeBranchVersion(context.Context) (BranchVersionIdentity, error)
	AdoptBranchVersion(context.Context, BranchVersionIdentity) error
}
type BranchVersionIdentity struct{ SessionID, BranchID, TipID string }
type BranchVersionSnapshot struct {
	Mode            protocol.CollaborationMode
	Active          BranchVersionIdentity
	Branch          protocol.RPCBranchVersion
	Entries         []Entry
	NonterminalGoal bool
}

func (s BranchVersionSnapshot) Messages() []protocol.Message {
	out := make([]protocol.Message, 0)
	for _, entry := range s.Entries {
		if entry.Type == EntryMessage && entry.Message != nil {
			out = append(out, *entry.Message)
		}
	}
	return out
}
func ValidVersionIdentity(s string, empty bool) bool {
	return (empty || s != "") && len(s) <= 256 && utf8.ValidString(s) && !strings.ContainsFunc(s, unicode.IsControl)
}
func ValidateBranchRestoreBinding(p protocol.RPCBranchRestorePrepareParams) error {
	for _, id := range []string{p.SessionID, p.SourceBranchID, p.TargetBranchID} {
		if !ValidVersionIdentity(id, false) {
			return ErrBranchVersionStale
		}
	}
	if !ValidVersionIdentity(p.SourceTipID, true) || !ValidVersionIdentity(p.TargetTipID, true) || p.SourceBranchID == p.TargetBranchID {
		return ErrBranchVersionStale
	}
	return nil
}
func (s BranchVersionSnapshot) Matches(p protocol.RPCBranchRestorePrepareParams) bool {
	return s.Active == (BranchVersionIdentity{p.SessionID, p.SourceBranchID, p.SourceTipID}) && s.Branch.ID == p.TargetBranchID && s.Branch.TipID == p.TargetTipID
}
func boundedVersion(branch protocol.SessionBranch) (protocol.RPCBranchVersion, error) {
	for _, id := range []string{branch.ID, branch.ParentID, branch.ForkedFromID, branch.TipID} {
		if !ValidVersionIdentity(id, true) {
			return protocol.RPCBranchVersion{}, ErrBranchVersionStale
		}
	}
	if branch.ID == "" || len(branch.Name) > 256 || !utf8.ValidString(branch.Name) || strings.ContainsRune(branch.Name, 0) {
		return protocol.RPCBranchVersion{}, ErrBranchVersionStale
	}
	return protocol.RPCBranchVersion{ID: branch.ID, Name: branch.Name, ParentID: branch.ParentID, ForkedFromID: branch.ForkedFromID, TipID: branch.TipID, CreatedAt: branch.CreatedAt, UpdatedAt: branch.UpdatedAt, Active: branch.Active}, nil
}
