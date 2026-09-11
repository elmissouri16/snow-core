package plugin

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/elmissouri16/snow-core/internal/tools"
	public "github.com/elmissouri16/snow-core/pkg/plugin"
)

// JavaScriptReplacement is a detached, initialized registration bundle. It owns
// no live subscriptions or host authority until CommitJavaScript succeeds.
// Close must be called by the preparer; after commit it is a no-op.
type JavaScriptReplacement struct {
	mu            sync.Mutex
	manager       *Manager
	old           *managedPlugin
	replacement   *managedPlugin
	descriptors   []tools.ToolDescriptor
	subscriptions []*replacementSubscription
	committed     bool
	closed        bool
}
type replacementSubscription struct {
	typ     public.EventType
	fn      public.EventHandler
	enabled bool
}
type replacementRegistrar struct {
	scoped        *scopedRegistrar
	subscriptions []*replacementSubscription
}

func (r *replacementRegistrar) RegisterTool(def public.ToolDefinition) error {
	return r.scoped.RegisterTool(def)
}
func (r *replacementRegistrar) Subscribe(typ public.EventType, fn public.EventHandler) func() {
	s := &replacementSubscription{typ: typ, fn: fn, enabled: fn != nil}
	r.subscriptions = append(r.subscriptions, s)
	return func() { s.enabled = false }
}

// PrepareJavaScript executes registration against a private registry. It never
// binds a host, runs readiness, or installs candidate observers in the manager.
func (m *Manager) PrepareJavaScript(ctx context.Context, id string, p public.Plugin, fingerprint string) (*JavaScriptReplacement, error) {
	if p == nil || p.Manifest().ID != id {
		return nil, errors.New("plugin reload: candidate identity mismatch")
	}
	if err := public.ValidateManifest(p.Manifest()); err != nil {
		return nil, err
	}
	m.mu.Lock()
	var old *managedPlugin
	var scope string
	if m.ready && !m.closed {
		for _, item := range m.plugins {
			if item.id == id && item.source == tools.SourceJSPlugin {
				old = item
				scope = item.scope
				break
			}
		}
	}
	m.mu.Unlock()
	if old == nil {
		return nil, errors.New("plugin reload: requires an initialized, loaded JavaScript plugin")
	}
	reg := tools.NewRegistry()
	staging := NewManager(reg, m.options)
	replacement := &managedPlugin{id: id, owner: old.owner, goPlugin: p, source: tools.SourceJSPlugin, fingerprint: fingerprint, scope: scope}
	registrar := &replacementRegistrar{scoped: &scopedRegistrar{manager: staging, owner: old.owner, pluginID: id, source: tools.SourceJSPlugin, fingerprint: fingerprint, cleanups: &replacement.cleanups}}
	candidate := &JavaScriptReplacement{manager: m, old: old, replacement: replacement}
	if err := p.Register(nonNilContext(ctx), registrar); err != nil {
		_ = candidate.Close(context.Background())
		return nil, fmt.Errorf("plugin reload registration: %w", err)
	}
	candidate.descriptors = reg.Descriptors()
	candidate.subscriptions = registrar.subscriptions
	// Managed tool execution must use the stable live manager, never the staging
	// manager. Subscription cleanup closures are built only at the commit point.
	for _, desc := range candidate.descriptors {
		if tool, ok := desc.Tool.(*goManagedTool); ok {
			tool.manager = m
		}
	}
	m.mu.Lock()
	err := m.validateReplacementLocked(candidate)
	m.mu.Unlock()
	if err != nil {
		_ = candidate.Close(context.Background())
		return nil, err
	}
	return candidate, nil
}

func (m *Manager) validateReplacementLocked(c *JavaScriptReplacement) error {
	if m.closed || !m.ready {
		return errors.New("plugin reload: manager is not available")
	}
	validation := NewManager(m.registry, m.options)
	found := false
	for _, p := range m.plugins {
		if p == c.old {
			validation.plugins = append(validation.plugins, c.replacement)
			found = true
		} else {
			validation.plugins = append(validation.plugins, p)
		}
	}
	if !found {
		return errors.New("plugin reload: loaded generation changed")
	}
	return validation.ValidateCommands()
}

// CommitJavaScript replaces precisely one owner. The caller must exclude root
// and child execution and host operations. prepare must not reenter manager or
// registry; it atomically prepares the deferred router against the full catalog.
// The returned cleanup runs AFTER commit and must never unregister the owner.
func (m *Manager) CommitJavaScript(ctx context.Context, c *JavaScriptReplacement, prepare func([]tools.ToolDescriptor) error) (func(context.Context) error, error) {
	if c == nil || c.manager != m {
		return nil, errors.New("plugin reload: foreign candidate")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.committed {
		return nil, errors.New("plugin reload: candidate already consumed")
	}
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := nonNilContext(ctx).Err(); err != nil {
		return nil, err
	}
	if err := m.validateReplacementLocked(c); err != nil {
		return nil, err
	}
	registry, ok := m.registry.(tools.AtomicOwnerRegistry)
	if !ok {
		return nil, errors.New("plugin reload: registry cannot atomically replace owners")
	}
	if err := registry.ReplaceOwner(c.old.owner, c.descriptors, prepare); err != nil {
		return nil, err
	}
	for typ, set := range m.subs {
		for id := range set {
			if m.subOwners[id] == c.old.owner {
				delete(m.subs[typ], id)
				delete(m.subOwners, id)
			}
		}
	}
	for _, sub := range c.subscriptions {
		if !sub.enabled {
			continue
		}
		c.replacement.cleanups = append(c.replacement.cleanups, m.subscribeLocked(sub.typ, sub.fn, c.replacement.owner))
	}
	for i, p := range m.plugins {
		if p == c.old {
			m.plugins[i] = c.replacement
			break
		}
	}
	c.committed = true
	if retire, ok := c.old.goPlugin.(interface{ RetireForReload() }); ok {
		retire.RetireForReload()
	}
	old := c.old
	return func(ctx context.Context) error {
		for _, cleanup := range slices.Backward(old.cleanups) {
			cleanup()
		}
		return old.goPlugin.Close(nonNilContext(ctx))
	}, nil
}

func (c *JavaScriptReplacement) Close(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.committed || c.closed {
		return nil
	}
	c.closed = true
	return c.replacement.goPlugin.Close(nonNilContext(ctx))
}

// ReadyJavaScript starts only the selected replacement. Surviving plugins do
// not repeat readiness side effects on a neighbor's reload.
func (m *Manager) ReadyJavaScript(ctx context.Context, id string) error {
	for _, e := range m.extensionList() {
		if e.ExtensionInfo().ID == id {
			return e.Ready(ctx)
		}
	}
	return nil // API 1 adapters may have no readiness extension.
}

// FreezeJavaScriptDelivery closes manager event delivery and all live JS
// admission. Its nonblocking check includes callbacks already snapshotted by
// Emit, so no late observer can obtain a replacement host generation. Release
// only after every host and catalog projection has been rebound.
func (m *Manager) FreezeJavaScriptDelivery() (func(), error) {
	if !m.deliveryMu.TryLock() {
		return nil, errors.New("plugin reload: event delivery active")
	}
	m.mu.Lock()
	var releases []func()
	release := sync.OnceFunc(func() {
		for _, fn := range slices.Backward(releases) {
			fn()
		}
		m.deliveryMu.Unlock()
	})
	if m.closed || !m.ready {
		m.mu.Unlock()
		release()
		return nil, errors.New("plugin reload: manager unavailable")
	}
	for _, p := range m.plugins {
		if p.source != tools.SourceJSPlugin {
			continue
		}
		runtime, ok := p.goPlugin.(interface{ FreezeForReload() (func(), error) })
		if !ok {
			m.mu.Unlock()
			release()
			return nil, errors.New("plugin reload: JavaScript adapter cannot quiesce")
		}
		resume, err := runtime.FreezeForReload()
		if err != nil {
			m.mu.Unlock()
			release()
			return nil, fmt.Errorf("plugin %s: %w", p.id, err)
		}
		releases = append(releases, resume)
	}
	m.mu.Unlock()
	return release, nil
}
