package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"unicode/utf8"

	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/tools"
	publicplugin "github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// Diagnostic describes a plugin that was skipped, failed, or closed.
type Diagnostic struct {
	PluginID string
	Status   string
	Message  string
}

const (
	defaultOutputBytes   = 256 * 1024
	defaultProgressBytes = 16 * 1024
)

// ManagerOptions configures the context and output limits exposed to Go plugins.
type ManagerOptions struct {
	CWD              string
	SessionID        string
	MaxProgressBytes int
	MaxOutputBytes   int
}

type managedPlugin struct {
	id          string
	owner       string
	goPlugin    publicplugin.Plugin
	cleanups    []func()
	source      tools.Source
	fingerprint string
	scope       string
}

// Manager owns plugin lifecycles, registrations, and observation delivery.
type Manager struct {
	lifecycleMu   sync.Mutex
	mu            sync.Mutex
	registry      tools.DescriptorRegistry
	options       ManagerOptions
	plugins       []*managedPlugin
	ids           map[string]bool
	subs          map[publicplugin.EventType]map[int]publicplugin.EventHandler
	nextSub       int
	initialized   bool
	initializeErr error
	ready         bool
	closed        bool
	diagnostics   []Diagnostic
}

// NewManager creates an empty deterministic plugin manager. Options are
// optional for embedders that only need static registration.
func NewManager(reg tools.DescriptorRegistry, options ...ManagerOptions) *Manager {
	var opts ManagerOptions
	if len(options) > 0 {
		opts = options[0]
	}
	if opts.MaxProgressBytes <= 0 {
		opts.MaxProgressBytes = defaultProgressBytes
	}
	if opts.MaxOutputBytes <= 0 {
		opts.MaxOutputBytes = defaultOutputBytes
	}
	return &Manager{registry: reg, options: opts, ids: make(map[string]bool), subs: make(map[publicplugin.EventType]map[int]publicplugin.EventHandler)}
}

// LoadGo queues a statically linked Go plugin. It does not execute Register
// until Initialize, so all loading has a deterministic startup boundary.
func (m *Manager) LoadGo(p publicplugin.Plugin) error {
	return m.load(p, tools.SourceGoPlugin, "")
}

// LoadJavaScript loads a host-created JavaScript adapter with its pinned scope.
func (m *Manager) LoadJavaScript(p publicplugin.Plugin, fingerprint string) error {
	return m.load(p, tools.SourceJSPlugin, fingerprint)
}

func (m *Manager) load(p publicplugin.Plugin, source tools.Source, fingerprint string) error {
	if p == nil {
		return errors.New("plugin manager: nil Go plugin")
	}
	manifest := p.Manifest()
	if manifest.ProtocolVersion == 0 {
		manifest.ProtocolVersion = publicplugin.ProtocolVersion
	}
	if err := publicplugin.ValidateManifest(manifest); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.initialized {
		return errors.New("plugin manager: cannot load after initialize")
	}
	if m.ids[manifest.ID] {
		return fmt.Errorf("plugin %q already loaded", manifest.ID)
	}
	m.ids[manifest.ID] = true
	m.plugins = append(m.plugins, &managedPlugin{id: manifest.ID, owner: ownerFor(manifest.ID), goPlugin: p, source: source, fingerprint: fingerprint})
	return nil
}

// Initialize registers all queued plugins in load order. A failed plugin is
// rolled back to its owner scope; later plugins are not allowed to observe a
// partially initialized registry.
func (m *Manager) Initialize(ctx context.Context) error {
	ctx = nonNilContext(ctx)
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return errors.New("plugin manager: closed")
	}
	if m.initialized {
		err := m.initializeErr
		m.mu.Unlock()
		return err
	}
	plugins := slices.Clone(m.plugins)
	m.initialized = true
	m.mu.Unlock()
	for _, p := range plugins {
		p.cleanups = nil
		r := &scopedRegistrar{manager: m, owner: p.owner, pluginID: p.id, cleanups: &p.cleanups, source: p.source, fingerprint: p.fingerprint}
		err := p.goPlugin.Register(ctx, r)
		if err != nil {
			message := err.Error()
			rollbackErr := m.rollback(p)
			if rollbackErr != nil {
				message += "; cleanup: " + rollbackErr.Error()
			}
			m.addDiagnostic(p.id, "failed", message)
			initializeErr := errors.Join(fmt.Errorf("plugin %s: initialize: %w", p.id, err), rollbackErr)
			m.mu.Lock()
			m.initializeErr = initializeErr
			m.mu.Unlock()
			return initializeErr
		}
	}
	if err := m.ValidateCommands(); err != nil {
		m.mu.Lock()
		m.initializeErr = err
		m.mu.Unlock()
		return err
	}
	m.mu.Lock()
	m.ready = true
	m.mu.Unlock()
	return nil
}

// Emit forwards a sanitized, observation-only event to interested plugins.
func (m *Manager) Emit(ev protocol.AgentEvent) {
	eventType := publicplugin.EventType(ev.Type)
	m.mu.Lock()
	if !m.ready || m.closed {
		m.mu.Unlock()
		return
	}
	fns := make([]publicplugin.EventHandler, 0, len(m.subs[eventType]))
	ids := make([]int, 0, len(m.subs[eventType]))
	for id := range m.subs[eventType] {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		fns = append(fns, m.subs[eventType][id])
	}
	m.mu.Unlock()
	if len(fns) == 0 {
		return
	}
	ev = sanitizeEvent(ev.Clone())
	e := publicplugin.Event{Version: publicplugin.ProtocolVersion, Type: eventType, Payload: ev}
	for _, fn := range fns {
		observerEvent := e
		observerEvent.Payload = e.Payload.Clone()
		func() { defer func() { _ = recover() }(); fn(observerEvent) }()
	}
}

// Subscribe registers an observation handler. Unknown event types are still
// accepted so future protocol events can be passed through.
func (m *Manager) Subscribe(t publicplugin.EventType, fn publicplugin.EventHandler) func() {
	if fn == nil {
		return func() {}
	}
	m.mu.Lock()
	id := m.nextSub
	m.nextSub++
	if m.subs[t] == nil {
		m.subs[t] = make(map[int]publicplugin.EventHandler)
	}
	m.subs[t][id] = fn
	m.mu.Unlock()
	return func() {
		m.mu.Lock()
		if set := m.subs[t]; set != nil {
			delete(set, id)
		}
		m.mu.Unlock()
	}
}

// Diagnostics returns a stable snapshot of lifecycle diagnostics.
func (m *Manager) Diagnostics() []Diagnostic {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.diagnostics)
}

// Close unregisters capabilities and closes resources in reverse load order.
func (m *Manager) Close(ctx context.Context) error {
	ctx = nonNilContext(ctx)
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	m.ready = false
	plugins := slices.Clone(m.plugins)
	m.mu.Unlock()
	var errs []error
	for i := len(plugins) - 1; i >= 0; i-- {
		p := plugins[i]
		m.registry.UnregisterOwner(p.owner)
		for j := len(p.cleanups) - 1; j >= 0; j-- {
			p.cleanups[j]()
		}
		if p.goPlugin != nil {
			if err := p.goPlugin.Close(ctx); err != nil {
				errs = append(errs, fmt.Errorf("plugin %s: %w", p.id, err))
			}
		}
	}
	return errors.Join(errs...)
}

func (m *Manager) rollback(p *managedPlugin) error {
	m.registry.UnregisterOwner(p.owner)
	for i := len(p.cleanups) - 1; i >= 0; i-- {
		p.cleanups[i]()
	}
	var err error
	if p.goPlugin != nil {
		err = p.goPlugin.Close(context.Background())
		p.goPlugin = nil
	}
	return err
}

func (m *Manager) addDiagnostic(id, status, message string) {
	m.mu.Lock()
	m.diagnostics = append(m.diagnostics, Diagnostic{PluginID: id, Status: status, Message: message})
	m.mu.Unlock()
}
func nonNilContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func ownerFor(id string) string { return "plugin:" + id }

type scopedRegistrar struct {
	manager         *Manager
	owner, pluginID string
	cleanups        *[]func()
	source          tools.Source
	fingerprint     string
}

func (r *scopedRegistrar) RegisterTool(def publicplugin.ToolDefinition) error {
	if err := validateDefinition(def); err != nil {
		return err
	}
	name, err := publicplugin.Namespace("plugin", r.pluginID, def.Name)
	if err != nil {
		return err
	}
	risk, err := declaredRisk(def.Risk)
	if err != nil {
		return err
	}
	tool := &goManagedTool{schema: protocol.ToolSchema{Name: name, Description: def.Description, Parameters: append(json.RawMessage(nil), def.Parameters...), Discovery: cloneDiscovery(def.Discovery)}, definition: def, manager: r.manager, pluginID: r.pluginID, source: r.source, fingerprint: r.fingerprint}
	return r.manager.registry.RegisterDescriptor(tools.ToolDescriptor{Schema: tool.schema, Tool: tool, Source: r.source, Owner: r.owner, PluginID: r.pluginID, OriginalName: def.Name, Risk: risk, Capabilities: slices.Clone(def.Capabilities)})
}
func (r *scopedRegistrar) Subscribe(t publicplugin.EventType, fn publicplugin.EventHandler) func() {
	u := r.manager.Subscribe(t, fn)
	*r.cleanups = append(*r.cleanups, u)
	return u
}

func validateDefinition(def publicplugin.ToolDefinition) error {
	if err := publicplugin.ValidateIdentifier("tool name", def.Name); err != nil {
		return err
	}
	if def.Executor == nil {
		return errors.New("plugin: tool executor is required")
	}
	if !validSchema(def.Parameters) {
		return fmt.Errorf("plugin tool %q has invalid parameters schema", def.Name)
	}
	return nil
}
func validSchema(raw json.RawMessage) bool {
	var schema map[string]any
	return len(raw) > 0 && json.Unmarshal(raw, &schema) == nil && schema != nil
}

func declaredRisk(s string) (permission.Risk, error) {
	if s == "" {
		return permission.RiskExec, nil
	}
	r := permission.Risk(s)
	switch r {
	case permission.RiskRead, permission.RiskWrite, permission.RiskExec, permission.RiskNet:
		return r, nil
	default:
		return "", fmt.Errorf("plugin: invalid risk %q", s)
	}
}

type goManagedTool struct {
	schema      protocol.ToolSchema
	definition  publicplugin.ToolDefinition
	manager     *Manager
	pluginID    string
	source      tools.Source
	fingerprint string
}

func (t *goManagedTool) Schema() tools.ToolSchema { return t.schema }
func (t *goManagedTool) Run(ctx context.Context, args json.RawMessage, host tools.ToolHost) (tools.ToolResult, error) {
	ctx = nonNilContext(ctx)
	callID := ""
	if p, ok := host.(interface{ ToolCallID() string }); ok {
		callID = p.ToolCallID()
	}
	progress := func(u publicplugin.ProgressUpdate) error {
		msg := boundUTF8(u.Message, t.manager.options.MaxProgressBytes)
		if host != nil {
			host.EmitProgress(tools.ToolProgressEvent{ToolCallID: callID, Name: t.schema.Name, Message: msg, Done: u.Done, IsError: u.IsError})
		}
		return nil
	}
	cwd := t.manager.options.CWD
	if host != nil && host.CWD() != "" {
		cwd = host.CWD()
	}
	var invoke func(context.Context, string, json.RawMessage) (publicplugin.ToolResult, error)
	if t.source == tools.SourceJSPlugin {
		if bridge, ok := host.(tools.BuiltinInvoker); ok {
			invoke = func(ctx context.Context, name string, args json.RawMessage) (publicplugin.ToolResult, error) {
				result, err := bridge.InvokeBuiltin(ctx, name, args)
				return publicplugin.ToolResult{Content: result.Content, IsError: result.IsError}, err
			}
		}
	}
	result, err := t.definition.Executor(ctx, publicplugin.ToolContext{Context: ctx, SessionID: t.manager.options.SessionID, CWD: cwd, ToolCallID: callID, Progress: progress, CallTool: invoke}, args)
	if err != nil {
		return tools.ErrorResult(err), nil
	}
	if encoded, _ := json.Marshal(result.Content); len(encoded) > t.manager.options.MaxOutputBytes {
		return tools.ToolResult{Content: []protocol.ContentBlock{protocol.NewTextBlock("Error: plugin output exceeded limit")}, IsError: true}, nil
	}
	return tools.ToolResult{Content: result.Content, Details: result.Details, IsError: result.IsError}, nil
}

func sanitizeEvent(ev protocol.AgentEvent) protocol.AgentEvent {
	ev.Text = boundUTF8(ev.Text, 16*1024)
	ev.Message = boundUTF8(ev.Message, 4*1024)
	ev.ToolOutput = boundUTF8(ev.ToolOutput, 8*1024)
	if ev.ToolProgress != nil {
		p := *ev.ToolProgress
		p.Message = boundUTF8(p.Message, 16*1024)
		ev.ToolProgress = &p
	}
	if ev.ToolRouting != nil {
		routing := *ev.ToolRouting
		routing.ToolIDs = slices.Clone(routing.ToolIDs)
		ev.ToolRouting = &routing
	}
	if ev.Mode != nil {
		mode := *ev.Mode
		ev.Mode = &mode
	}
	if ev.Plan != nil {
		plan := *ev.Plan
		plan.Text = boundUTF8(plan.Text, 64*1024)
		ev.Plan = &plan
	}
	if ev.PlanUpdate != nil {
		ev.PlanUpdate = ev.PlanUpdate.Clone()
	}
	if ev.ThreadGoal != nil {
		ev.ThreadGoal = ev.ThreadGoal.Clone()
	}
	return ev
}
func boundUTF8(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	suffix := []byte("…")
	if max <= len(suffix) {
		return ""
	}
	limit := max - len(suffix)
	if limit < 0 {
		limit = 0
	}
	b := []byte(s)
	if len(b) > limit {
		b = b[:limit]
	}
	for len(b) > 0 && !utf8.Valid(b) {
		b = b[:len(b)-1]
	}
	return string(b) + string(suffix)
}

func cloneDiscovery(in *protocol.ToolDiscovery) *protocol.ToolDiscovery {
	if in == nil {
		return nil
	}
	out := *in
	out.Keywords = slices.Clone(in.Keywords)
	return &out
}

func (t *goManagedTool) PluginInvocation(callID string) (*protocol.PluginOrigin, string) {
	if t.source != tools.SourceJSPlugin {
		return nil, ""
	}
	return &protocol.PluginOrigin{PluginID: t.pluginID, ToolName: t.schema.Name, ParentToolCallID: callID}, t.fingerprint
}

// RecordDiagnostic records bounded runtime messages without publishing events.
func (m *Manager) RecordDiagnostic(id, status, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, d := range m.diagnostics {
		if d.PluginID == id {
			count++
		}
	}
	if count >= 260 {
		return
	}
	m.diagnostics = append(m.diagnostics, Diagnostic{PluginID: id, Status: status, Message: boundUTF8(message, 2048)})
}

// HasJavaScript indicates whether this manager owns any JavaScript runtimes.
func (m *Manager) HasJavaScript() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.plugins {
		if p.source == tools.SourceJSPlugin {
			return true
		}
	}
	return false
}
