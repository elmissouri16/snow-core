package session

import (
	"context"
	"errors"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// ReadMessageImage is the worker-owned counterpart of Catalog.Image. The
// caller holds session admission throughout. Only bounded built-in history
// stores qualify; no inactive catalog or unbounded fallback is consulted.
func ReadMessageImage(ctx context.Context, store Store, p protocol.RPCMessageImageParams) (protocol.RPCCatalogImage, error) {
	var result protocol.RPCCatalogImage
	if !validImageID(p.SessionID) || p.SessionID != store.ID() || p.Index < 0 || p.Index > 10000 || (p.MessageID == "") == (p.TurnID == "") || (p.MessageID != "" && !validImageID(p.MessageID)) || (p.TurnID != "" && !validImageID(p.TurnID)) {
		return result, errors.New("session: invalid image selector")
	}
	ctx, cancel := context.WithTimeout(ctx, catalogQueryTimeout)
	defer cancel()
	history, ok := store.(messageEditHistoryStore)
	if !ok {
		return result, errors.New("session: bounded image history unavailable")
	}
	entries, err := history.messageEditEntries(ctx)
	if err != nil {
		return result, err
	}
	if len(entries) == 0 || entries[0].Type != EntryMeta || entries[0].Key != "root" || entries[0].Value != p.SessionID || entries[0].ParentID != "" || entries[0].ID == "" {
		return result, ErrNotFound
	}
	selected := -1
	for i, entry := range entries {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if i > 0 && entry.ParentID != entries[i-1].ID {
			return result, ErrNotFound
		}
		if p.MessageID != "" && entry.ID == p.MessageID {
			selected = i
		}
	}
	if p.TurnID != "" {
		spans, err := verifiedInputSpans(ctx, entries)
		if err != nil {
			return result, err
		}
		for _, span := range spans {
			if span.root != span.marker || entries[span.root].ID != p.TurnID || !IsAgentTurnMarker(entries[span.root]) || entries[span.root].Value != "user" {
				continue
			}
			// Exactly one adjacent user in this root input span. Follow-up input spans
			// are deliberately not treated as aliases of the original sent prompt.
			count := 0
			for i := span.marker + 1; i < span.end; i++ {
				if entries[i].Message != nil && entries[i].Message.Role == protocol.RoleUser {
					count++
					selected = i
				}
			}
			if count != 1 || selected != span.marker+1 {
				return result, ErrNotFound
			}
		}
	}
	if selected < 0 {
		return result, ErrNotFound
	}
	entry := entries[selected]
	if entry.Type != EntryMessage || entry.Message == nil || entry.Message.Role != protocol.RoleUser || entry.Message.ID != entry.ID || p.Index >= len(entry.Message.Content) {
		return result, ErrNotFound
	}
	block := entry.Message.Content[p.Index]
	if block.Type != protocol.BlockImage {
		return result, ErrNotFound
	}
	if err := validateMessageImage(block.MIMEType, block.Data); err != nil {
		return result, err
	}
	if store.BranchTip() != entries[len(entries)-1].ID {
		return result, errors.New("session: image branch changed")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	return protocol.RPCCatalogImage{SessionID: p.SessionID, MessageID: entry.ID, Index: p.Index, MIMEType: block.MIMEType, Data: block.Data}, nil
}
