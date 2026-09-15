package app

import (
	"context"
	"encoding/base64"
	json "encoding/json/v2"
	"errors"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type branchVersionCursor struct {
	Kind, Session, Branch, Tip, After string
	Next                              int
}

// VersionMessageCursor is an opaque continuation, never restore authority.
func VersionMessageCursor(sessionID, branch, tip string, next int) string {
	return encodeVersionCursor(branchVersionCursor{Kind: "messages-v1", Session: sessionID, Branch: branch, Tip: tip, Next: next})
}
func encodeVersionCursor(c branchVersionCursor) string {
	wire, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(wire)
}
func decodeVersionCursor(value string) (branchVersionCursor, error) {
	var c branchVersionCursor
	if len(value) > 2048 {
		return c, session.ErrBranchVersionStale
	}
	wire, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return c, session.ErrBranchVersionStale
	}
	if err := json.Unmarshal(wire, &c, json.RejectUnknownMembers(true)); err != nil {
		return c, session.ErrBranchVersionStale
	}
	return c, nil
}
func (a *App) versionStoreAdmitted(sessionID string) (session.BranchVersionStore, error) {
	if !session.ValidVersionIdentity(sessionID, false) || a.Session == nil || a.Session.ID() != sessionID {
		return nil, session.ErrBranchVersionStale
	}
	store, ok := a.Session.(session.BranchVersionStore)
	if !ok {
		return nil, errors.New("app: bounded branch versions unsupported by store")
	}
	return store, nil
}
func (a *App) BranchesPage(ctx context.Context, p protocol.RPCBranchesPageParams) (protocol.RPCBranchesPage, error) {
	var result protocol.RPCBranchesPage
	if p.Limit == 0 {
		p.Limit = 32
	}
	if p.Limit < 1 || p.Limit > protocol.RPCBranchesPageMaxItems {
		return result, errors.New("app: branch page limit out of bounds")
	}
	unlock, err := a.Agent.LockAdmissionContext(ctx)
	if err != nil {
		return result, err
	}
	defer unlock()
	store, err := a.versionStoreAdmitted(p.SessionID)
	if err != nil {
		return result, err
	}
	var c branchVersionCursor
	if p.Cursor != "" {
		c, err = decodeVersionCursor(p.Cursor)
		if err != nil || c.Kind != "branches-v1" || c.Session != p.SessionID || c.Next != 0 || !session.ValidVersionIdentity(c.After, false) {
			return result, session.ErrBranchVersionStale
		}
	}
	result, err = store.BranchVersions(ctx, c.After, p.Limit)
	if err != nil {
		return result, err
	}
	if result.SessionID != p.SessionID || p.Cursor != "" && (c.Branch != result.ActiveBranchID || c.Tip != result.ActiveTipID) {
		return protocol.RPCBranchesPage{}, session.ErrBranchVersionStale
	}
	if result.NextCursor != "" {
		result.NextCursor = encodeVersionCursor(branchVersionCursor{Kind: "branches-v1", Session: p.SessionID, Branch: result.ActiveBranchID, Tip: result.ActiveTipID, After: result.NextCursor})
	}
	return result, nil
}

// BranchMessagesPage returns internal history. RPC must project its explicit
// public allowlist and apply the conservative encoded frame bound before output.
func (a *App) BranchMessagesPage(ctx context.Context, p protocol.RPCBranchMessagesPageParams) (protocol.RPCBranchMessagesPage, error) {
	var result protocol.RPCBranchMessagesPage
	if p.Limit == 0 {
		p.Limit = 32
	}
	if p.Limit < 1 || p.Limit > protocol.RPCBranchMessagesPageMaxItems || !session.ValidVersionIdentity(p.BranchID, false) || !session.ValidVersionIdentity(p.TipID, true) {
		return result, session.ErrBranchVersionStale
	}
	unlock, err := a.Agent.LockAdmissionContext(ctx)
	if err != nil {
		return result, err
	}
	defer unlock()
	store, err := a.versionStoreAdmitted(p.SessionID)
	if err != nil {
		return result, err
	}
	snapshot, err := store.BranchVersion(ctx, p.BranchID)
	if err != nil {
		return result, err
	}
	if snapshot.Active.SessionID != p.SessionID || snapshot.Branch.TipID != p.TipID {
		return result, session.ErrBranchVersionStale
	}
	start := 0
	if p.Cursor != "" {
		c, err := decodeVersionCursor(p.Cursor)
		if err != nil || c.Kind != "messages-v1" || c.Session != p.SessionID || c.Branch != p.BranchID || c.Tip != p.TipID || c.After != "" {
			return result, session.ErrBranchVersionStale
		}
		start = c.Next
	}
	messages := snapshot.Messages()
	if start < 0 || start > len(messages) {
		return result, session.ErrBranchVersionStale
	}
	end := min(start+p.Limit, len(messages))
	result = protocol.RPCBranchMessagesPage{SessionID: p.SessionID, BranchID: p.BranchID, TipID: p.TipID, Start: start, Total: len(messages), Messages: messages[start:end]}
	allTools, truncated := protocol.ProjectHistoryTools(messages)
	result.HistoryTools = make(map[string][]protocol.RPCHistoryTool)
	for _, message := range result.Messages {
		if tools, ok := allTools[message.ID]; ok {
			result.HistoryTools[message.ID] = tools
		}
	}
	result.HistoryToolsTruncated = truncated
	if end < len(messages) {
		result.NextCursor = VersionMessageCursor(p.SessionID, p.BranchID, p.TipID, end)
	}
	return result, ctx.Err()
}
