package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/elmissouri16/snow-core/internal/agent"
	internalplugin "github.com/elmissouri16/snow-core/internal/plugin"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/subagent"
	"github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// PluginUIHandler is owned by an interactive surface. It must marshal work to
// that surface's event loop; app/core never import terminal implementation types.
type PluginUIHandler func(context.Context, protocol.PluginUIEvent) (json.RawMessage, error)
type pluginTransition struct {
	branch string
	fork   *protocol.BranchForkOptions
}
type extensionServices struct {
	sessionMu   sync.RWMutex
	ctx         context.Context
	cancel      context.CancelFunc
	ready       sync.Once
	wg          sync.WaitGroup
	mu          sync.Mutex
	ui          PluginUIHandler
	generation  atomic.Uint64
	store       *internalplugin.StateStore
	views       map[string]protocol.PluginView
	commands    map[string]context.CancelFunc
	children    map[string][]string
	transitions map[string]pluginTransition
}
type appExtensionHost struct {
	app        *App
	services   *extensionServices
	info       protocol.PluginInfo
	agent      *agent.Agent
	store      session.Store
	generation uint64
	child      bool
}

func (a *App) bindExtensions(globalDir string) {
	if a.PluginManager == nil || !a.PluginManager.HasJavaScript() {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	statePath := filepath.Join(globalDir, "plugin-state.db")
	if a.Session.Path() == "" {
		statePath = ""
	}
	s := &extensionServices{ctx: ctx, cancel: cancel, store: internalplugin.NewStateStore(statePath), views: map[string]protocol.PluginView{}, commands: map[string]context.CancelFunc{}, children: map[string][]string{}, transitions: map[string]pluginTransition{}}
	s.generation.Store(1)
	a.extensions = s
	a.PluginManager.BindExtensions(func(info protocol.PluginInfo) plugin.ExtensionHost {
		for _, view := range info.Views {
			s.views[view.ID] = view
		}
		return &appExtensionHost{app: a, services: s, info: info, agent: a.Agent, store: a.Session, generation: s.generation.Load()}
	})
}

// StartPluginExtensions is idempotent and invoked after a surface installs its
// interaction brokers. Validation/check never starts readiness callbacks.
func (a *App) StartPluginExtensions() {
	if a.extensions == nil {
		return
	}
	s := a.extensions
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return
	}
	s.ready.Do(func() { s.wg.Go(func() { a.PluginManager.ReadyExtensions(s.ctx) }) })
}
func (a *App) AttachPluginUI(handler PluginUIHandler) {
	if a.extensions != nil {
		a.extensions.mu.Lock()
		a.extensions.ui = handler
		a.extensions.mu.Unlock()
	}
	a.StartPluginExtensions()
}
func (a *App) PluginCommands() []protocol.PluginCommand {
	if a.PluginManager == nil {
		return nil
	}
	return a.PluginManager.Commands()
}
func (a *App) PluginInfos() []protocol.PluginInfo {
	if a.PluginManager == nil {
		return nil
	}
	return a.PluginManager.ExtensionInfos()
}
func (a *App) PluginViews() []protocol.PluginView {
	if a.extensions == nil {
		return nil
	}
	s := a.extensions
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []protocol.PluginView
	for _, view := range s.views {
		out = append(out, view)
	}
	raw, _ := jsonv2.Marshal(out)
	_ = jsonv2.Unmarshal(raw, &out)
	slices.SortFunc(out, func(a, b protocol.PluginView) int { return strings.Compare(a.ID, b.ID) })
	return out
}
func (a *App) RunPluginCommand(ctx context.Context, id, input string) (plugin.ToolResult, error) {
	if a.extensions == nil {
		return plugin.ToolResult{}, plugin.ErrUnavailable
	}
	a.StartPluginExtensions()
	s := a.extensions
	for _, command := range a.PluginCommands() {
		if command.Alias == id {
			id = command.ID
			break
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.ctx, cancel)
	defer func() { stop(); cancel() }()
	s.mu.Lock()
	if _, exists := s.commands[id]; exists {
		s.mu.Unlock()
		return plugin.ToolResult{}, errors.New("plugin command already running")
	}
	if len(s.commands) >= 32 {
		s.mu.Unlock()
		return plugin.ToolResult{}, errors.New("too many active plugin commands")
	}
	if s.ctx.Err() != nil {
		s.mu.Unlock()
		return plugin.ToolResult{}, errors.New("plugin host closed")
	}
	s.wg.Add(1)
	defer s.wg.Done()
	s.commands[id] = cancel
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.commands, id)
		delete(s.transitions, id)
		delete(s.children, id)
		s.mu.Unlock()
	}()
	result, err := a.PluginManager.RunCommand(ctx, id, input)
	if err == nil {
		err = ctx.Err()
	}
	s.mu.Lock()
	branch := s.transitions[id]
	s.mu.Unlock()
	if err == nil && !result.IsError {
		if branch.fork != nil {
			_, err = a.ForkBranchWithOptions(*branch.fork)
		} else if branch.branch != "" {
			err = a.SelectBranch(branch.branch)
		}
	}
	if err != nil || result.IsError {
		err = errors.Join(err, a.cleanupPluginChildren(id))
	}
	return result, err
}
func (a *App) CancelPluginCommand(id string) bool {
	if a.extensions == nil {
		return false
	}
	for _, command := range a.PluginCommands() {
		if command.Alias == id {
			id = command.ID
			break
		}
	}
	s := a.extensions
	s.mu.Lock()
	defer s.mu.Unlock()
	if cancel := s.commands[id]; cancel != nil {
		cancel()
		return true
	}
	return false
}
func (h *appExtensionHost) Environment() plugin.Environment {
	h.services.sessionMu.RLock()
	defer h.services.sessionMu.RUnlock()
	h.services.mu.Lock()
	ui := h.services.ui != nil
	h.services.mu.Unlock()
	kind := "root"
	if h.child {
		kind = "child"
		ui = false
	}
	return plugin.Environment{SessionID: h.store.ID(), CWD: h.app.CWD(), Kind: kind, UI: ui, Generation: h.generation}
}
func (h *appExtensionHost) Call(ctx context.Context, inv plugin.Invocation, operation string, raw json.RawMessage) (json.RawMessage, error) {
	h.services.sessionMu.RLock()
	defer h.services.sessionMu.RUnlock()
	if inv.Generation != h.services.generation.Load() {
		return nil, errors.New("plugin session changed")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if h.services.ctx.Err() != nil {
		return nil, errors.New("plugin host closed")
	}
	if inv.PluginID != h.info.ID || !plugin.AllowsOperation(inv.Uses, operation) {
		return nil, errors.New("plugin operation not authorized")
	}
	if inv.Kind == "hook" || inv.Kind == "renderer" {
		return nil, errors.New("host calls unavailable in hooks/renderers")
	}
	capability := plugin.CapabilityForOperation(operation)
	if capability != "read" && !slices.Contains(h.info.Capabilities, capability) && !(capability == "tools" && len(h.info.HostTools) > 0) {
		return nil, errors.New("manifest does not grant host capability")
	}
	if h.child && operation != "tools.call" && capability != "storage" && operation != "sleep" {
		return nil, plugin.ErrUnavailable
	}
	if inv.Kind == "tool" && (capability == "agent" || capability == "session" || capability == "goals") {
		return nil, errors.New("agent and session controls require a command")
	}
	if len(raw) > h.app.Cfg.ToolOutputLimit() {
		return nil, errors.New("plugin host input exceeds limit")
	}
	if strings.HasPrefix(operation, "storage.") {
		var arg struct {
			Scope string `json:"scope"`
		}
		if err := jsonv2.Unmarshal(raw, &arg); err != nil {
			return nil, err
		}
		scope := ""
		switch arg.Scope {
		case "", "project":
			scope = fmt.Sprintf("project:%x", sha256.Sum256([]byte(h.app.CWD())))
		case "global":
			scope = "global"
		case "session":
			scope = "session:" + h.store.ID()
		default:
			return nil, errors.New("unknown state scope")
		}
		return h.services.store.Call(ctx, inv.PluginID, scope, operation, raw)
	}
	if strings.HasPrefix(operation, "ui.") {
		return h.callUI(ctx, inv, operation, raw)
	}
	if operation == "sleep" {
		var ms int64
		if err := jsonv2.Unmarshal(raw, &ms); err != nil {
			return nil, err
		}
		if ms < 0 || ms > 60000 {
			return nil, errors.New("sleep expects 0..60000 milliseconds")
		}
		timer := time.NewTimer(time.Duration(ms) * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
			return []byte("null"), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	value, err := h.callControl(ctx, inv, operation, raw)
	if err != nil {
		return nil, err
	}
	return jsonv2.Marshal(value)
}

// lockPluginSession prevents replacement while an accepted host operation uses
// the old store. The runtime itself is not locked while awaiting host work.
func (a *App) lockPluginSession() (func(), error) {
	if a.extensions == nil {
		return func() {}, nil
	}
	if !a.extensions.sessionMu.TryLock() {
		return nil, errors.New("plugin host operation active; finish or cancel it before switching session")
	}
	return a.extensions.sessionMu.Unlock, nil
}
func (a *App) pluginSessionChanged() {
	if a.extensions == nil {
		return
	}
	s := a.extensions
	s.generation.Add(1)
	s.mu.Lock()
	for _, cancel := range s.commands {
		cancel()
	}
	for id, view := range s.views {
		view.Content = nil
		s.views[id] = view
	}
	s.mu.Unlock()
	a.PluginManager.BindExtensions(func(info protocol.PluginInfo) plugin.ExtensionHost {
		return &appExtensionHost{app: a, services: s, info: info, agent: a.Agent, store: a.Session, generation: s.generation.Load()}
	})
}

func (a *App) cleanupPluginChildren(id string) error {
	s := a.extensions
	s.mu.Lock()
	children := slices.Clone(s.children[id])
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, target := range children {
		_, _ = a.InterruptSubagent(ctx, target)
		for {
			_, err := a.CloseSubagent(ctx, target)
			if err == nil || errors.Is(err, subagent.ErrClosed) {
				break
			}
			timer := time.NewTimer(10 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return fmt.Errorf("plugin child cleanup: %w", ctx.Err())
			case <-timer.C:
			}
		}
	}
	return nil
}

func (a *App) PluginGeneration() uint64 {
	if a.extensions == nil {
		return 0
	}
	return a.extensions.generation.Load()
}
