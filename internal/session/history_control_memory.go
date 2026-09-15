package session

import (
	"context"
	"errors"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func (s *MemoryStore) historyControlLocked(ctx context.Context, p protocol.RPCHistoryControlBinding, history bool) (BranchVersionSnapshot, error) {
	var result BranchVersionSnapshot
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if s.closed {
		return result, errors.New("session: store closed")
	}
	if err := validateHistoryBinding(p); err != nil {
		return result, err
	}
	source, sourceOK := s.branches[p.SourceBranchID]
	target, targetOK := s.branches[p.TargetBranchID]
	if !sourceOK || !targetOK || s.id != p.SessionID || s.activeBranch != p.SourceBranchID || s.tip != p.SourceTipID || source.TipID != p.SourceTipID || target.TipID != p.TargetTipID {
		return result, ErrBranchVersionStale
	}
	for _, id := range []string{p.SourceBranchID, p.TargetBranchID} {
		if g := s.threadGoals[id]; g != nil && !g.Status.Terminal() {
			return result, errors.New("session: history control rejects nonterminal goals")
		}
	}
	var err error
	result.Branch, err = boundedVersion(target)
	if err != nil {
		return result, err
	}
	result.Mode = s.threadModes[p.TargetBranchID]
	if result.Mode == "" {
		result.Mode = protocol.ModeDefault
	}
	result.Mode, err = protocol.ParseCollaborationMode(string(result.Mode))
	if err != nil {
		return result, err
	}
	if history {
		result.Entries, err = s.boundedVersionEntriesLocked(ctx, p.TargetTipID)
		if err == nil {
			err = ValidateForkBoundary(result.Entries)
		}
	}
	return result, err
}

func (s *MemoryStore) ForkHistoryBranch(ctx context.Context, p protocol.RPCManagedBranchForkParams, expectedMode protocol.CollaborationMode) (protocol.SessionBranch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var empty protocol.SessionBranch
	if err := validateHistoryName(p.Name, 64); err != nil {
		return empty, err
	}
	snapshot, err := s.historyControlLocked(ctx, p.RPCHistoryControlBinding, true)
	if err != nil {
		return empty, err
	}
	if snapshot.Mode != expectedMode {
		return empty, ErrBranchVersionStale
	}
	if branchNameExists(s.branches, p.Name, "") {
		return empty, errors.New("session: branch name already exists")
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	now := time.Now().UnixMilli()
	branch := protocol.SessionBranch{ID: "branch-" + randomSuffix(), Name: p.Name, ParentID: p.TargetBranchID, ForkedFromID: p.TargetTipID, TipID: p.TargetTipID, CreatedAt: now, UpdatedAt: now, Active: true}
	for id, current := range s.branches {
		current.Active = false
		s.branches[id] = current
	}
	s.branches[branch.ID] = branch
	s.threadModes[branch.ID] = snapshot.Mode
	if goal := s.threadGoals[p.TargetBranchID]; goal != nil {
		copy := goal.Clone()
		copy.BranchID = branch.ID
		copy.GoalID = newID()
		copy.UpdatedAt = now
		s.threadGoals[branch.ID] = copy
	}
	s.goalDeferred[branch.ID] = s.goalDeferred[p.TargetBranchID]
	s.activeBranch, s.tip = branch.ID, p.TargetTipID
	branch.Messages, branch.Preview = branchStats(snapshot.Entries)
	return branch, nil
}

func (s *MemoryStore) RenameHistoryBranch(ctx context.Context, p protocol.RPCManagedBranchRenameParams) (protocol.SessionBranch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var empty protocol.SessionBranch
	if err := validateHistoryName(p.Name, 64); err != nil {
		return empty, err
	}
	if err := validateHistoryName(p.OldName, 64); err != nil {
		return empty, err
	}
	snapshot, err := s.historyControlLocked(ctx, p.RPCHistoryControlBinding, false)
	if err != nil {
		return empty, err
	}
	if snapshot.Branch.Name != p.OldName {
		return empty, ErrBranchVersionStale
	}
	if branchNameExists(s.branches, p.Name, p.TargetBranchID) {
		return empty, errors.New("session: branch name already exists")
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	branch := s.branches[p.TargetBranchID]
	branch.Name, branch.UpdatedAt = p.Name, time.Now().UnixMilli()
	s.branches[branch.ID] = branch
	return branch, nil
}

func (s *MemoryStore) CaptureHistoryFork(ctx context.Context, p protocol.RPCManagedSessionForkParams, expectedMode protocol.CollaborationMode) (*HistoryForkSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateHistoryName(p.Name, 72); err != nil {
		return nil, err
	}
	snapshot, err := s.historyControlLocked(ctx, p.RPCHistoryControlBinding, true)
	if err != nil {
		return nil, err
	}
	if snapshot.Mode != expectedMode {
		return nil, ErrBranchVersionStale
	}
	return capturedHistoryFork(p, snapshot), nil
}

var _ HistoryControlStore = (*MemoryStore)(nil)
