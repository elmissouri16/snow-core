package mcp

import (
	"context"
	"io"

	"github.com/elmissouri16/snow-core/internal/tools"
	publicmcp "github.com/elmissouri16/snow-core/pkg/mcp"
)

// NewManager creates an idle MCP manager.
func NewManager(registry tools.Registry, opts Options) *Manager {
	if opts.HostName == "" {
		opts.HostName = "snow"
	}
	if opts.HostVersion == "" {
		opts.HostVersion = "dev"
	}
	if opts.MaxOutputBytes <= 0 {
		opts.MaxOutputBytes = defaultMaxOutput
	}
	if opts.RefreshTimeout <= 0 {
		opts.RefreshTimeout = defaultRefreshTimeout
	}
	if opts.RefreshDebounce <= 0 {
		opts.RefreshDebounce = defaultRefreshDebounce
	}
	if opts.ServerStderr == nil {
		opts.ServerStderr = io.Discard
	}
	manager := &Manager{registry: registry, opts: opts, runtimes: make(map[string]*serverRuntime), statuses: make(map[string]publicmcp.Status), claimed: make(map[string]bool)}
	if opts.CacheRoot != "" {
		manager.cache = newCatalogCache(opts.CacheRoot)
	}
	return manager
}

// ConnectAll preserves the original eager-oriented internal API for focused
// callers and tests. Lifecycle fields are still honored.
func (m *Manager) ConnectAll(ctx context.Context, specs []publicmcp.ServerSpec) {
	declarations := make([]Declaration, 0, len(specs))
	for _, spec := range specs {
		declarations = append(declarations, Declaration{Spec: spec, Scope: "explicit", ProjectIdentity: m.opts.CWD})
	}
	m.Initialize(ctx, declarations)
}

// Initialize registers cached lazy descriptors, bootstraps uncached lazy
// servers, and connects eager servers. One unavailable server remains a status
// diagnostic rather than making the agent unusable.
func (m *Manager) Initialize(ctx context.Context, declarations []Declaration) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		for _, decl := range declarations {
			m.setStatus(publicmcp.Status{ID: decl.Spec.ID, Transport: decl.Spec.EffectiveTransport(), State: stateClosed.String(), Message: "MCP manager is closed"})
		}
		return
	}
	m.connectWG.Add(1)
	m.mu.Unlock()
	defer m.connectWG.Done()

	counts := make(map[string]int, len(declarations))
	for _, decl := range declarations {
		counts[decl.Spec.ID]++
	}
	for _, decl := range declarations {
		m.initializeOne(ctx, decl, counts[decl.Spec.ID])
	}
}

func (m *Manager) initializeOne(ctx context.Context, decl Declaration, count int) {
	spec := decl.Spec
	status := publicmcp.Status{ID: spec.ID, Transport: spec.EffectiveTransport(), State: stateConfigured.String()}
	if count > 1 {
		status.Message = "duplicate MCP server id"
		m.setStatus(status)
		return
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		status.State, status.Message = stateClosed.String(), "MCP manager is closed"
		m.setStatus(status)
		return
	}
	if m.claimed[spec.ID] {
		m.mu.Unlock()
		m.setRuntimeMessage(spec.ID, "duplicate MCP server id ignored")
		return
	}
	m.claimed[spec.ID] = true
	m.mu.Unlock()
	if spec.Disabled {
		status.Message = "disabled"
		m.setStatus(status)
		return
	}
	if err := spec.Validate(); err != nil {
		status.Message = err.Error()
		m.setStatus(status)
		return
	}
	if decl.Scope == "" {
		decl.Scope = "explicit"
	}
	runtimeCtx, runtimeCancel := context.WithCancel(context.Background())
	rt := &serverRuntime{
		manager: m, decl: decl, spec: spec, owner: "mcp:" + spec.ID,
		used: make(map[string]string), liveTools: make(map[string]string), liveCapabilities: make(map[string]bool), subscriptions: make(map[string]struct{}),
		state: stateConfigured, lazyEligible: spec.Lifecycle == publicmcp.LifecycleLazy || spec.Lifecycle == publicmcp.LifecycleLazyKeepAlive,
		runtimeCtx: runtimeCtx, runtimeCancel: runtimeCancel,
	}
	rt.cacheKey, rt.cached.ProjectIdentityHash, rt.cached.ConfigurationFingerprint = cacheIdentity(decl, m.opts.Roots)
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		runtimeCancel()
		status.State, status.Message = stateClosed.String(), "MCP manager is closed"
		m.setStatus(status)
		return
	}
	m.runtimes[spec.ID] = rt
	m.mu.Unlock()

	if rt.configuredLazy() && m.cache != nil && !m.opts.ForceRefresh {
		catalog, ok, err := m.cache.get(rt.cacheKey, m.now())
		if err != nil {
			rt.mu.Lock()
			rt.warning = "MCP cache read: " + boundString(err.Error(), 512)
			rt.mu.Unlock()
		}
		identityMatches := ok && catalog.ServerID == spec.ID && catalog.Scope == decl.Scope && catalog.ProjectIdentityHash == rt.cached.ProjectIdentityHash && catalog.ConfigurationFingerprint == rt.cached.ConfigurationFingerprint
		if identityMatches && (!catalog.requiresEagerFallback() || rt.strictNoBootstrap()) {
			if err := rt.installCached(catalog); err == nil {
				if catalog.requiresEagerFallback() {
					rt.mu.Lock()
					rt.warning = "strict no-bootstrap: cached catalog has no activation descriptor; run snow mcp cache refresh " + spec.ID + " to discover changes"
					rt.mu.Unlock()
					m.updateRuntimeStatus(rt, "")
				}
				return
			} else {
				m.setRuntimeMessage(spec.ID, "MCP cached catalog: "+boundString(err.Error(), 512))
			}
		}
	}
	if rt.strictNoBootstrap() && !m.opts.ForceRefresh {
		rt.mu.Lock()
		rt.state = stateConfigured
		rt.warning = "strict no-bootstrap: no valid MCP cache; run snow mcp cache refresh " + spec.ID
		rt.mu.Unlock()
		m.updateRuntimeStatus(rt, "")
		return
	}

	rt.mu.Lock()
	rt.state = stateConnecting
	rt.mu.Unlock()
	if err := rt.connectLive(ctx); err != nil {
		rt.mu.Lock()
		if rt.state != stateClosed {
			rt.state = stateFailed
		}
		rt.connectErr = err
		rt.mu.Unlock()
		m.updateRuntimeStatus(rt, err.Error())
		return
	}
	rt.mu.Lock()
	if rt.state != stateClosed {
		rt.state = stateConnected
	}
	catalog := cloneCachedCatalog(rt.cached)
	rt.mu.Unlock()
	m.updateRuntimeStatus(rt, "")

	if rt.configuredLazy() && !catalog.requiresEagerFallback() {
		if err := rt.bootstrapDisconnect(ctx); err != nil {
			m.setRuntimeMessage(spec.ID, rt.safeRuntimeError("bootstrap disconnect", err).Error())
		}
	} else if rt.configuredLazy() && catalog.requiresEagerFallback() {
		rt.mu.Lock()
		rt.warning = "lazy lifecycle uses eager fallback because the server exposes no activatable tools, resources, or prompts"
		rt.mu.Unlock()
		m.updateRuntimeStatus(rt, "")
	}
}
