package session

import (
	"context"
	"errors"
	"slices"
	"time"
	"uuid"
)

var _ WorkflowStateStore = (*MemoryStore)(nil)

// WorkflowState replays one plugin's full active ancestry, including metadata
// before compaction boundaries. No provider-facing projection is consulted.
func (s *MemoryStore) WorkflowState(ctx context.Context, pluginID string) (WorkflowState, error) {
	if err := validateWorkflowOwner(pluginID); err != nil {
		return WorkflowState{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return WorkflowState{}, errors.New("session: store closed")
	}
	records, err := s.workflowRecordsLocked(ctx, 0)
	if err != nil {
		return WorkflowState{}, err
	}
	return projectWorkflow(ctx, pluginID, s.activeBranch, s.tip, records)
}

// ApplyWorkflowState appends one validated batch and moves the active tip under
// the same store lock. Errors leave entries and both branch cursors unchanged.
func (s *MemoryStore) ApplyWorkflowState(ctx context.Context, pluginID, expectedBranchID, expectedTipID string, update WorkflowUpdate) (WorkflowState, error) {
	if err := ctx.Err(); err != nil {
		return WorkflowState{}, err
	}
	raw, detached, err := encodeWorkflowUpdate(pluginID, update)
	if err != nil {
		return WorkflowState{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return WorkflowState{}, errors.New("session: store closed")
	}
	if s.activeBranch != expectedBranchID || s.tip != expectedTipID {
		return WorkflowState{}, ErrConflict
	}
	records, err := s.workflowRecordsLocked(ctx, len(raw))
	if err != nil {
		return WorkflowState{}, err
	}
	state, err := projectWorkflow(ctx, pluginID, s.activeBranch, s.tip, records)
	if err != nil {
		return WorkflowState{}, err
	}
	if err := applyWorkflowUpdate(&state, detached); err != nil {
		return WorkflowState{}, err
	}
	if err := ctx.Err(); err != nil {
		return WorkflowState{}, err
	}
	entry := Entry{Type: EntryMeta, ID: uuid.New().String(), ParentID: s.tip, Key: MetaPluginWorkflow, Value: raw}
	s.entries = append(s.entries, entry)
	s.byID[entry.ID] = len(s.entries) - 1
	s.tip = entry.ID
	branch := s.branches[s.activeBranch]
	branch.TipID, branch.UpdatedAt = entry.ID, time.Now().UnixMilli()
	s.branches[s.activeBranch] = branch
	state.TipID = entry.ID
	return state, nil
}

func (s *MemoryStore) workflowRecordsLocked(ctx context.Context, addedBytes int) ([]string, error) {
	count, size := 0, 0
	for _, entry := range s.entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.Type == EntryMeta && entry.Key == MetaPluginWorkflow {
			count++
			size += len(entry.Value)
			if err := checkWorkflowHistory(count, size, addedBytes); err != nil {
				return nil, err
			}
		}
	}
	if err := checkWorkflowHistory(count, size, addedBytes); err != nil {
		return nil, err
	}
	var records []string
	id := s.tip
	for steps := 0; id != ""; steps++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		index, ok := s.byID[id]
		if !ok || steps >= len(s.entries) {
			return nil, errors.New("session: malformed workflow ancestry")
		}
		entry := s.entries[index]
		if entry.Type == EntryMeta && entry.Key == MetaPluginWorkflow {
			records = append(records, entry.Value)
		}
		id = entry.ParentID
	}
	slices.Reverse(records)
	return records, nil
}
