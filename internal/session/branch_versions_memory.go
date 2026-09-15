package session

import (
	"context"
	"errors"
	"slices"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func (s *MemoryStore) BranchVersions(ctx context.Context, after string, limit int) (protocol.RPCBranchesPage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := protocol.RPCBranchesPage{SessionID: s.id, ActiveBranchID: s.activeBranch, ActiveTipID: s.tip, Branches: []protocol.RPCBranchVersion{}}
	if s.closed || limit < 1 || limit > protocol.RPCBranchesPageMaxItems || len(s.branches) > messageEditMaxEntries {
		return result, errors.New("session: bounded branch listing unavailable")
	}
	ids := make([]string, 0, limit+1)
	for id := range s.branches {
		if !ValidVersionIdentity(id, false) {
			return result, ErrBranchVersionStale
		}
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if id <= after {
			continue
		}
		index, _ := slices.BinarySearch(ids, id)
		ids = slices.Insert(ids, index, id)
		if len(ids) > limit+1 {
			ids = ids[:limit+1]
		}
	}
	for _, id := range ids {
		branch, err := boundedVersion(s.branches[id])
		if err != nil {
			return result, err
		}
		result.Branches = append(result.Branches, branch)
	}
	if len(result.Branches) > limit {
		result.Branches = result.Branches[:limit]
		result.NextCursor = result.Branches[limit-1].ID
	}
	return result, ctx.Err()
}
func (s *MemoryStore) BranchVersion(ctx context.Context, branchID string) (BranchVersionSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result BranchVersionSnapshot
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if s.closed {
		return result, errors.New("session: store closed")
	}
	branch, ok := s.branches[branchID]
	if !ok {
		return result, ErrNotFound
	}
	var err error
	result.Branch, err = boundedVersion(branch)
	if err != nil {
		return result, err
	}
	mode := s.threadModes[branchID]
	if mode == "" {
		mode = protocol.ModeDefault
	}
	result.Mode, err = protocol.ParseCollaborationMode(string(mode))
	if err != nil {
		return result, err
	}
	result.Active = BranchVersionIdentity{s.id, s.activeBranch, s.tip}
	for _, id := range []string{s.activeBranch, branchID} {
		if g := s.threadGoals[id]; g != nil && !g.Status.Terminal() {
			result.NonterminalGoal = true
		}
	}
	result.Entries, err = s.boundedVersionEntriesLocked(ctx, branch.TipID)
	return result, err
}
func (s *MemoryStore) RestoreBranchVersion(ctx context.Context, p protocol.RPCBranchRestorePrepareParams, expectedMode protocol.CollaborationMode) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed {
		return errors.New("session: store closed")
	}
	if err := ValidateBranchRestoreBinding(p); err != nil {
		return err
	}
	target, ok := s.branches[p.TargetBranchID]
	source, sourceOK := s.branches[p.SourceBranchID]
	if !ok || !sourceOK || s.id != p.SessionID || s.activeBranch != p.SourceBranchID || s.tip != p.SourceTipID || source.TipID != p.SourceTipID || target.TipID != p.TargetTipID {
		return ErrBranchVersionStale
	}
	mode := s.threadModes[p.TargetBranchID]
	if mode == "" {
		mode = protocol.ModeDefault
	}
	if mode != expectedMode {
		return ErrBranchVersionStale
	}
	for _, id := range []string{p.SourceBranchID, p.TargetBranchID} {
		if g := s.threadGoals[id]; g != nil && !g.Status.Terminal() {
			return errors.New("session: branch restore rejects nonterminal goals")
		}
	}
	source.Active = false
	target.Active = true
	s.branches[source.ID] = source
	s.branches[target.ID] = target
	s.activeBranch = target.ID
	s.tip = target.TipID
	return nil
}
func (s *MemoryStore) ProbeBranchVersion(ctx context.Context) (BranchVersionIdentity, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return BranchVersionIdentity{}, err
	}
	if s.closed {
		return BranchVersionIdentity{}, errors.New("session: store closed")
	}
	return BranchVersionIdentity{s.id, s.activeBranch, s.tip}, nil
}

func (s *MemoryStore) AdoptBranchVersion(ctx context.Context, expected BranchVersionIdentity) error {
	actual, err := s.ProbeBranchVersion(ctx)
	if err != nil {
		return err
	}
	if actual != expected {
		return ErrBranchVersionStale
	}
	return nil
}
