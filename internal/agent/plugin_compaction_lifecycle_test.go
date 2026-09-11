package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/permission"
	internalplugin "github.com/elmissouri16/snow-core/internal/plugin"
	"github.com/elmissouri16/snow-core/internal/plugin/javascript"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestPluginCompactionGateFailsClosedWithoutSummaryOrFallback(t *testing.T) {
	for _, trigger := range []compactionTrigger{compactionManual, compactionPressure, compactionToolHistory, compactionOverflow} {
		for _, behavior := range []string{`({block:"unfinished"})`, `(()=>{throw new Error("gate failed")})()`, `({text:"rewrite"})`} {
			t.Run(string(trigger)+behavior, func(t *testing.T) {
				provider := &scriptedProvider{}
				a, store := setup(t, provider, nil, permission.ModeDeny)
				a.opts.Compaction.Fallback = "local"
				manager := internalplugin.NewManager(tools.NewRegistry())
				defer manager.Close(t.Context())
				pkg := &javascript.Package{Manifest: javascript.Manifest{ID: "guard", Name: "guard", Version: "1", APIVersion: 2, Entry: "main.js", Capabilities: []string{"hooks"}}, Config: json.RawMessage(`{}`), Script: []byte(`snow.registerHook("before_compaction",r=>` + behavior + `);`)}
				if err := manager.LoadJavaScript(javascript.New(pkg, javascript.Options{}), "test"); err != nil {
					t.Fatal(err)
				}
				if err := manager.Initialize(t.Context()); err != nil {
					t.Fatal(err)
				}
				a.opts.PluginHooks = manager
				for i := range 6 {
					message := protocol.NewUserMessage(fmt.Sprint(i), "", strings.Repeat("message ", 100))
					if err := store.Append(session.Entry{Type: session.EntryMessage, ID: message.ID, Message: &message}); err != nil {
						t.Fatal(err)
					}
				}
				tip := store.BranchTip()
				done := make(chan protocol.AgentEvent, 1)
				unsubscribe := a.Subscribe(func(ev protocol.AgentEvent) {
					if ev.Type == protocol.EvCompactionDone {
						done <- ev
					}
				})
				defer unsubscribe()
				result, err := a.compactActiveContext(t.Context(), trigger)
				if err == nil || result.UsedFallback || len(provider.requests) != 0 || store.BranchTip() != tip {
					t.Fatalf("gate bypass: result=%+v err=%v provider=%d", result, err, len(provider.requests))
				}
				if err := a.DrainEvents(t.Context()); err != nil {
					t.Fatal(err)
				}
				select {
				case event := <-done:
					if !event.IsError || event.Compaction == nil {
						t.Fatalf("terminal event %+v", event)
					}
				default:
					t.Fatal("missing terminal compaction event")
				}
			})
		}
	}
}

type compactionLifecycleHooks struct{ calls []plugin.HookRequest }

func (h *compactionLifecycleHooks) HasHook(phase string, _ bool) bool {
	return phase == "before_compaction"
}
func (h *compactionLifecycleHooks) RunHooks(_ context.Context, request plugin.HookRequest) (plugin.HookRequest, []protocol.PluginTransform, error) {
	h.calls = append(h.calls, request)
	return request, nil, nil
}

func TestPluginCompactionPlanAndRootOnlyNoOp(t *testing.T) {
	provider := &scriptedProvider{scripts: [][]protocol.StreamEvent{{{Type: protocol.EvStreamTextDelta, Text: "checkpoint"}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}}}
	a, store := setup(t, provider, nil, permission.ModeDeny)
	hooks := &compactionLifecycleHooks{}
	a.opts.PluginHooks = hooks
	if _, err := a.Compact(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(hooks.calls) != 0 {
		t.Fatal("no-op compaction invoked gate")
	}
	for i := range 6 {
		message := protocol.NewUserMessage(fmt.Sprint(i), "", "message")
		if err := store.Append(session.Entry{Type: session.EntryMessage, ID: message.ID, Message: &message}); err != nil {
			t.Fatal(err)
		}
	}
	result, err := a.Compact(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(hooks.calls) != 1 {
		t.Fatalf("gates=%d", len(hooks.calls))
	}
	request := hooks.calls[0]
	if request.Compaction == nil || request.Compaction.Trigger != "manual" || request.Compaction.BoundaryID == "" || request.Compaction.SummarizedMessages != result.SummarizedMessages || request.Compaction.RetainedMessages != result.RetainedMessages || len(request.Content) != 0 || len(request.Context) != 0 {
		t.Fatalf("unsafe/incorrect plan %+v", request)
	}
	// Child compaction does not invoke the new root-only gate, even if an
	// alternate hook implementation claims to support child execution.
	childProvider := &scriptedProvider{scripts: [][]protocol.StreamEvent{{{Type: protocol.EvStreamTextDelta, Text: "checkpoint"}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}}}
	child, childStore := setup(t, childProvider, nil, permission.ModeDeny)
	child.opts.PluginHooks = hooks
	child.opts.Identity = &protocol.AgentRef{ThreadID: "child", Path: "/root/child"}
	for i := range 6 {
		message := protocol.NewUserMessage(fmt.Sprint(i), "", "message")
		if err := childStore.Append(session.Entry{Type: session.EntryMessage, ID: message.ID, Message: &message}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := child.compactActiveContext(t.Context(), compactionManual); err != nil {
		t.Fatal(err)
	}
	if len(hooks.calls) != 1 {
		t.Fatal("child invoked root-only gate")
	}
}
