package agent

import (
	"context"
	jsonv2 "encoding/json/v2"
	"sync"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type pluginAuditKey struct{}
type pluginAudit struct {
	mu      sync.Mutex
	changes []protocol.PluginTransform
}

func (a *pluginAudit) add(changes []protocol.PluginTransform) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.changes = append(a.changes, changes...)
}
func (a *Agent) persistPluginAudit(changes []protocol.PluginTransform) error {
	if len(changes) == 0 {
		return nil
	}
	raw, err := jsonv2.Marshal(changes)
	if err != nil {
		return err
	}
	return a.opts.Session.Append(session.Entry{Type: session.EntryMeta, ID: newID(), ParentID: a.opts.Session.BranchTip(), Key: "plugin_transforms", Value: string(raw)})
}
func pluginAuditContext(ctx context.Context) (context.Context, *pluginAudit, bool) {
	if audit, ok := ctx.Value(pluginAuditKey{}).(*pluginAudit); ok {
		return ctx, audit, false
	}
	audit := &pluginAudit{}
	return context.WithValue(ctx, pluginAuditKey{}, audit), audit, true
}
