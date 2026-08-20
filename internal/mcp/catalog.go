package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/elmissouri16/snow-core/internal/tools"
)

func (rt *serverRuntime) catalogFromLive(init *sdkmcp.InitializeResult, remoteTools []*sdkmcp.Tool) (cachedCatalog, error) {
	catalog := cachedCatalog{
		ServerID:                 rt.spec.ID,
		Scope:                    rt.decl.Scope,
		ProjectIdentityHash:      rt.cached.ProjectIdentityHash,
		ConfigurationFingerprint: rt.cached.ConfigurationFingerprint,
		CachedAt:                 rt.manager.now().UTC(),
		ProtocolVersion:          init.ProtocolVersion,
	}
	if init.ServerInfo != nil {
		catalog.ServerName, catalog.ServerVersion = init.ServerInfo.Name, init.ServerInfo.Version
	}
	if init.Capabilities != nil {
		if init.Capabilities.Tools != nil {
			catalog.Capabilities = append(catalog.Capabilities, "tools")
		}
		if init.Capabilities.Resources != nil {
			catalog.Capabilities = append(catalog.Capabilities, "resources")
			if init.Capabilities.Resources.Subscribe {
				catalog.Capabilities = append(catalog.Capabilities, "resources.subscribe")
			}
		}
		if init.Capabilities.Prompts != nil {
			catalog.Capabilities = append(catalog.Capabilities, "prompts")
		}
		if init.Capabilities.Logging != nil {
			catalog.Capabilities = append(catalog.Capabilities, "logging")
		}
		if init.Capabilities.Completions != nil {
			catalog.Capabilities = append(catalog.Capabilities, "completions")
		}
	}
	for _, remote := range remoteTools {
		if remote == nil || remote.Name == "" {
			continue
		}
		schema, err := marshalSchema(remote.InputSchema)
		if err != nil {
			return cachedCatalog{}, fmt.Errorf("mcp %s tool %s schema: %w", rt.spec.ID, remote.Name, err)
		}
		catalog.Tools = append(catalog.Tools, cachedTool{Name: remote.Name, Title: remote.Title, Description: remote.Description, InputSchema: schema})
	}
	if err := validateCachedCatalog(catalog); err != nil {
		return cachedCatalog{}, fmt.Errorf("mcp %s cache catalog: %w", rt.spec.ID, err)
	}
	return catalog, nil
}

func (rt *serverRuntime) commitLiveCatalog(session *sdkmcp.ClientSession, used map[string]string, catalog cachedCatalog) error {
	live := make(map[string]string, len(catalog.Tools))
	for _, tool := range catalog.Tools {
		live[tool.Name] = string(tool.InputSchema)
	}
	liveCapabilities := make(map[string]bool, len(catalog.Capabilities))
	for _, capability := range catalog.Capabilities {
		liveCapabilities[capability] = true
	}
	rt.mu.Lock()
	if rt.closed || rt.state == stateClosed || rt.session != session {
		rt.mu.Unlock()
		return errors.New("runtime closed during refresh")
	}
	rt.used = used
	rt.liveTools = live
	rt.liveCapabilities = liveCapabilities
	rt.cached = cloneCachedCatalog(catalog)
	rt.lazyEligible = rt.configuredLazy() && !catalog.requiresEagerFallback()
	if rt.lazyEligible && strings.HasPrefix(rt.warning, "lazy lifecycle uses eager fallback") {
		rt.warning = ""
	}
	rt.mu.Unlock()

	var cacheErr error
	if rt.configuredLazy() && rt.manager.cache != nil {
		cacheErr = rt.manager.cache.put(rt.cacheKey, catalog, rt.manager.now())
		rt.mu.Lock()
		if cacheErr != nil {
			rt.warning = "MCP cache write: " + boundString(cacheErr.Error(), 512)
		} else if strings.HasPrefix(rt.warning, "MCP cache ") {
			rt.warning = ""
		}
		rt.mu.Unlock()
	}
	rt.manager.updateRuntimeStatus(rt, "")
	if cacheErr != nil && rt.manager.opts.ForceRefresh {
		return fmt.Errorf("MCP cache write: %w", cacheErr)
	}
	return nil
}

func (rt *serverRuntime) installCached(catalog cachedCatalog) error {
	remote := make([]*sdkmcp.Tool, 0, len(catalog.Tools))
	for _, tool := range catalog.Tools {
		remote = append(remote, &sdkmcp.Tool{Name: tool.Name, Title: tool.Title, Description: tool.Description, InputSchema: append(json.RawMessage(nil), tool.InputSchema...)})
	}
	sort.SliceStable(remote, func(i, j int) bool { return remote[i].Name < remote[j].Name })
	used := make(map[string]string)
	descriptors := make([]tools.ToolDescriptor, 0, len(remote))
	for _, tool := range remote {
		descriptor, err := rt.toolDescriptor(tool, used)
		if err != nil {
			return err
		}
		descriptors = append(descriptors, descriptor)
	}
	descriptors = append(descriptors, rt.bridgeDescriptors(catalog.Capabilities, used)...)
	replacer, ok := rt.manager.registry.(tools.AtomicOwnerRegistry)
	if !ok {
		return errors.New("MCP registry does not support atomic owner replacement")
	}
	if err := replacer.ReplaceOwner(rt.owner, descriptors, nil); err != nil {
		return err
	}
	rt.mu.Lock()
	rt.cached = cloneCachedCatalog(catalog)
	rt.used = used
	rt.lazyEligible = rt.configuredLazy() && !catalog.requiresEagerFallback()
	rt.state = stateCached
	rt.mu.Unlock()
	rt.manager.updateRuntimeStatus(rt, "cached")
	return nil
}

func (rt *serverRuntime) finishRefresh() {
	rt.mu.Lock()
	rt.refreshing = false
	rt.armIdleLocked()
	rt.mu.Unlock()
}

func (rt *serverRuntime) disconnectLive(final bool) error {
	rt.mu.Lock()
	if rt.idleTimer != nil {
		rt.idleTimer.Stop()
		rt.idleTimer = nil
	}
	session := rt.session
	cancel := rt.refreshCancel
	stop := rt.refreshStop
	done := rt.refreshDone
	rt.client, rt.session = nil, nil
	rt.refreshCancel, rt.refreshReq, rt.refreshStop, rt.refreshDone = nil, nil, nil, nil
	rt.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if stop != nil {
		close(stop)
	}
	var errs []error
	if done != nil {
		select {
		case <-done:
		case <-time.After(defaultCloseTimeout):
			errs = append(errs, errors.New("refresh worker did not stop within timeout"))
		}
	}
	if session != nil {
		closed := make(chan error, 1)
		go func() { closed <- session.Close() }()
		select {
		case err := <-closed:
			if err != nil {
				errs = append(errs, err)
			}
		case <-time.After(defaultCloseTimeout):
			errs = append(errs, errors.New("session close did not finish within timeout"))
		}
	}
	if final {
		rt.manager.registry.UnregisterOwner(rt.owner)
	}
	return errors.Join(errs...)
}

// bootstrapDisconnect turns a successful lazy bootstrap into cached state.
func (rt *serverRuntime) bootstrapDisconnect(ctx context.Context) error {
	_ = ctx
	if err := rt.disconnectLive(false); err != nil {
		return err
	}
	rt.mu.Lock()
	if rt.state != stateClosed {
		rt.state = stateCached
	}
	rt.mu.Unlock()
	rt.manager.updateRuntimeStatus(rt, "")
	return nil
}
