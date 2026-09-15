package web

import (
	"context"
	"encoding/json/v2"
	"strings"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const runtimeVersionsCapability = "branch_versions"
const runtimeVersionWireBytes = 2 << 20

type runtimeVersionState struct {
	preparation                                 *protocol.RPCBranchRestorePrepared
	instanceID                                  string
	activityPrompt                              uint64
	expiresAt                                   time.Time
	provider, model, permission, thinking, mode string
}

// Parent runtime publication wires this helper alongside other capability
// refreshes. Merely implementing the optional Go interface grants no authority.
func (r *liveRuntime) refreshVersionsLocked() {
	r.snapshot.VersionsEnabled = r.worker != nil && r.supports(runtimeVersionsCapability)
	r.snapshot.HistoryControlEnabled = r.snapshot.VersionsEnabled && r.supports("history_control")
	r.snapshot.ReasoningEnabled = r.worker != nil && r.supports(protocol.RPCSessionReasoningCapability)
	r.snapshot.CompactionEnabled = r.worker != nil && r.supports("compaction_run") && r.supports("goal_run") && r.supports("messages_public_history")
}

func validVersionIdentity(id string, empty bool) bool {
	return (empty || id != "") && runtimeOption(id)
}
func validVersionCursor(cursor string) bool {
	return len(cursor) <= 2048 && runtimeText(cursor, 2048) == cursor
}

func (m *RuntimeManager) versionRuntime(ctx context.Context, projectID, instanceID, sessionID string, restore bool) (*liveRuntime, error) {
	if !runtimeIdentifier(sessionID) {
		return nil, ErrRuntimeInvalid
	}
	r, err := m.controlRuntime(ctx, projectID, instanceID)
	if err != nil {
		return nil, err
	}
	if !r.supports(runtimeVersionsCapability) {
		r.control.Unlock()
		return nil, ErrRuntimeInvalid
	}
	r.mu.Lock()
	valid := r.snapshot.SessionID == sessionID && r.instanceID == instanceID
	if valid && restore {
		if r.goalBlocksHistoryLocked() {
			err = ErrRuntimeBusy
		}
		if r.busy || r.transitioning || r.snapshot.Status != "idle" || r.snapshot.CancelRequested || r.snapshot.Permission != nil || r.snapshot.Input != nil {
			err = ErrRuntimeBusy
		}
		if r.snapshot.Queue != nil && len(r.snapshot.Queue.Items) != 0 || r.queue.control != nil && (len(r.queue.control.Items) != 0 || len(r.queue.control.ReviewItems) != 0) {
			err = ErrRuntimeQueueReview
		}
	}
	r.mu.Unlock()
	if !valid {
		err = ErrRuntimeInvalid
	}
	if err != nil {
		r.control.Unlock()
		return nil, err
	}
	project := r.project
	project.checkIdentity()
	if !project.Available {
		r.control.Unlock()
		return nil, ErrProjectInvalid
	}
	return r, nil
}

// A read result belongs to the same exact worker and session after its bounded
// RPC finishes. It never replaces a snapshot, changes branch, or starts work.
func (m *RuntimeManager) versionReadScope(r *liveRuntime, projectID, instanceID, sessionID string) (uint64, error) {
	if err := m.revalidate(r, projectID, instanceID); err != nil {
		return 0, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ctx.Err() != nil || r.snapshot.Status == "failed" || r.snapshot.Status == "closing" || r.snapshot.SessionID != sessionID || r.instanceID != instanceID {
		return 0, ErrRuntimeUnavailable
	}
	return r.snapshot.Revision, nil
}

func (m *RuntimeManager) ListVersions(ctx context.Context, projectID, instanceID, sessionID, cursor string) (RuntimeVersionsPage, error) {
	if !validVersionCursor(cursor) {
		return RuntimeVersionsPage{}, ErrRuntimeInvalid
	}
	r, err := m.versionRuntime(ctx, projectID, instanceID, sessionID, false)
	if err != nil {
		return RuntimeVersionsPage{}, err
	}
	defer r.control.Unlock()
	var page protocol.RPCBranchesPage
	if err := r.call(protocol.RPCRequest{Type: "branches_page"}, protocol.RPCBranchesPageParams{SessionID: sessionID, Limit: protocol.RPCBranchesPageMaxItems, Cursor: cursor}, &page); err != nil {
		return RuntimeVersionsPage{}, err
	}
	if !validVersionsPage(page, sessionID, cursor) {
		r.fail()
		return RuntimeVersionsPage{}, ErrRuntimeUnavailable
	}
	revision, err := m.versionReadScope(r, projectID, instanceID, sessionID)
	if err != nil {
		return RuntimeVersionsPage{}, err
	}
	out := RuntimeVersionsPage{ProjectID: projectID, InstanceID: instanceID, SessionID: sessionID, Revision: revision, CurrentBranchID: page.ActiveBranchID, CurrentTipID: page.ActiveTipID, NextCursor: page.NextCursor, HasMore: page.NextCursor != "", Versions: make([]RuntimeVersion, 0, len(page.Branches))}
	for _, branch := range page.Branches {
		out.Versions = append(out.Versions, RuntimeVersion{BranchID: strings.Clone(branch.ID), TipID: strings.Clone(branch.TipID), Name: runtimeText(branch.Name, 256), Current: branch.Active})
	}
	return out, nil
}

func validVersionsPage(page protocol.RPCBranchesPage, session, cursor string) bool {
	if page.SessionID != session || !validVersionIdentity(page.ActiveBranchID, false) || !validVersionIdentity(page.ActiveTipID, true) || len(page.Branches) > protocol.RPCBranchesPageMaxItems || !validVersionCursor(page.NextCursor) || page.NextCursor != "" && (page.NextCursor == cursor || len(page.Branches) == 0) {
		return false
	}
	seen := make(map[string]bool, len(page.Branches))
	for _, branch := range page.Branches {
		if !validVersionIdentity(branch.ID, false) || !validVersionIdentity(branch.TipID, true) || !validVersionIdentity(branch.ParentID, true) || !validVersionIdentity(branch.ForkedFromID, true) || seen[branch.ID] || len(branch.Name) > 256 || branch.Active != (branch.ID == page.ActiveBranchID) || branch.Active && branch.TipID != page.ActiveTipID {
			return false
		}
		seen[branch.ID] = true
	}
	return true
}

func (m *RuntimeManager) PreviewVersion(ctx context.Context, projectID, instanceID, sessionID, branchID, tipID, cursor string) (RuntimeVersionPreview, error) {
	if !validVersionIdentity(branchID, false) || !validVersionIdentity(tipID, true) || !validVersionCursor(cursor) {
		return RuntimeVersionPreview{}, ErrRuntimeInvalid
	}
	r, err := m.versionRuntime(ctx, projectID, instanceID, sessionID, false)
	if err != nil {
		return RuntimeVersionPreview{}, err
	}
	defer r.control.Unlock()
	var page protocol.RPCBranchMessagesPage
	params := protocol.RPCBranchMessagesPageParams{SessionID: sessionID, BranchID: branchID, TipID: tipID, Cursor: cursor, Limit: protocol.RPCBranchMessagesPageMaxItems}
	if err := r.call(protocol.RPCRequest{Type: "branch_messages_page"}, params, &page); err != nil {
		return RuntimeVersionPreview{}, err
	}
	if !validVersionHistory(page, sessionID, branchID, tipID, cursor) {
		r.fail()
		return RuntimeVersionPreview{}, ErrRuntimeUnavailable
	}
	revision, err := m.versionReadScope(r, projectID, instanceID, sessionID)
	if err != nil {
		return RuntimeVersionPreview{}, err
	}
	projected := projectVersionHistory(page)
	for i := range projected.Messages {
		projected.Messages[i].CanEdit = false
		projected.Messages[i].CanRegenerate = false
	}
	projected = displaySnapshot(projected)
	return RuntimeVersionPreview{ProjectID: projectID, InstanceID: instanceID, SessionID: sessionID, Revision: revision, BranchID: branchID, TipID: tipID, Messages: projected.Messages, NextCursor: page.NextCursor, HasMore: page.NextCursor != "", HistoryTruncated: projected.HistoryTruncated, HistoryToolsTruncated: projected.HistoryToolsTruncated}, nil
}

func validVersionHistory(page protocol.RPCBranchMessagesPage, session, branch, tip, cursor string) bool {
	if page.SessionID != session || page.BranchID != branch || page.TipID != tip || page.Start < 0 || page.Total < 0 || page.Start > page.Total || len(page.Messages) > protocol.RPCBranchMessagesPageMaxItems || len(page.Messages) > page.Total-page.Start || !validVersionCursor(page.NextCursor) || page.NextCursor != "" && (page.NextCursor == cursor || len(page.Messages) == 0) {
		return false
	}
	encoded, err := json.Marshal(page)
	if err != nil || len(encoded) > runtimeVersionWireBytes {
		return false
	}
	seen := make(map[string]bool, len(page.Messages))
	for _, message := range page.Messages {
		if !validVersionIdentity(message.ID, false) || seen[message.ID] {
			return false
		}
		seen[message.ID] = true
	}
	return validVersionHistoryTools(page)
}

func projectVersionHistory(page protocol.RPCBranchMessagesPage) RuntimeSnapshot {
	projected := liveRuntime{assistant: -1, plan: -1, snapshot: RuntimeSnapshot{Messages: []RuntimeMessage{}}}
	tools := page.HistoryTools
	if tools == nil {
		// The bounded branch API owns tool provenance. An omitted empty map never
		// authorizes reconstruction from a partial page or raw message payload.
		tools = make(map[string][]protocol.RPCHistoryTool)
	}
	projected.projectHistory(protocol.RPCMessagesPage{Messages: page.Messages, Start: page.Start, Total: page.Total, HasMore: page.NextCursor != "", HistoryTools: tools, HistoryToolsTruncated: page.HistoryToolsTruncated})
	return projected.snapshot
}

// Public cross-page result ownership is supplied by core. The result entry may
// intentionally lie outside this page; the assistant owner must be on it.
func validVersionHistoryTools(page protocol.RPCBranchMessagesPage) bool {
	if len(page.HistoryTools) > protocol.RPCHistoryMaxTools {
		return false
	}
	owners := make(map[string]bool)
	for _, message := range page.Messages {
		if message.Role == protocol.RoleAssistant {
			owners[message.ID] = true
		}
	}
	count, total := 0, 0
	ids := make(map[string]bool)
	for owner, tools := range page.HistoryTools {
		if !owners[owner] || len(tools) > protocol.RPCHistoryMaxTools-count {
			return false
		}
		count += len(tools)
		for _, tool := range tools {
			if tool.OwnerID != owner || tool.ID == "" || len(tool.ID) > protocol.RPCHistoryMaxIDBytes || ids[tool.ID] || len(tool.ResultID) > protocol.RPCHistoryMaxIDBytes || len(tool.Tool) > protocol.RPCHistoryMaxNameBytes || len(tool.Output) > protocol.RPCHistoryMaxOutputBytes {
				return false
			}
			ids[tool.ID] = true
			total += len(tool.Output)
			if total > protocol.RPCHistoryMaxTotalOutputBytes || !tool.OutputAvailable && tool.Output != "" {
				return false
			}
			switch tool.Status {
			case "completed", "failed":
				if tool.ResultID == "" {
					return false
				}
			case "unresolved":
				if tool.ResultID != "" || tool.OutputAvailable {
					return false
				}
			default:
				return false
			}
		}
	}
	return true
}
