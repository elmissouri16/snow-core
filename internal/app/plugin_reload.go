package app

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/elmissouri16/snow-core/internal/plugin/javascript"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// ReloadPlugin reloads one already-loaded, enabled JavaScript registration.
// Package preparation has no live host. Before commit all errors preserve the
// old plugin; after commit errors become bounded diagnostics in an applied receipt.
func (a *App) ReloadPlugin(ctx context.Context, id string) (protocol.PluginReloadResult, error) {
	result := protocol.PluginReloadResult{PluginID: id}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := plugin.ValidateIdentifier("plugin id", id); err != nil {
		return result, err
	}
	if a.extensions == nil || a.PluginManager == nil {
		return result, plugin.ErrUnavailable
	}
	if !a.pluginMutationMu.TryLock() {
		return result, errors.New("plugin reload: plugin management active")
	}
	defer a.pluginMutationMu.Unlock()
	declaration, err := a.reloadDeclaration(id)
	if err != nil {
		return result, err
	}
	pkg, err := javascript.ReadPackage(ctx, declaration.Path, declaration.Root, declaration.Spec.Config)
	if err != nil {
		return result, err
	}
	if pkg.Manifest.ID != id {
		return result, errors.New("plugin reload: manifest identity changed")
	}
	runtime := javascript.New(pkg, javascript.Options{MaxOutputBytes: a.Cfg.ToolOutputLimit(), MaxProgressBytes: a.Cfg.ToolOutputLimit()})
	candidate, err := a.PluginManager.PrepareJavaScript(ctx, id, runtime, pkg.Fingerprint)
	if err != nil {
		_ = runtime.Close(context.Background())
		return result, err
	}
	defer candidate.Close(context.Background())
	s := a.extensions
	if !s.sessionMu.TryLock() {
		return result, errors.New("plugin reload: host operation or session transition active")
	}
	sessionHeld := true
	defer func() {
		if sessionHeld {
			s.sessionMu.Unlock()
		}
	}()
	s.mu.Lock()
	if s.ctx.Err() != nil || s.reloading || s.readyRunning || len(s.commands) > 0 {
		s.mu.Unlock()
		return result, errors.New("plugin reload: host closed, command, readiness, or reload active")
	}
	readyStarted := s.readyStarted
	s.reloading = true
	s.wg.Add(1)
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.reloading = false; s.mu.Unlock(); s.wg.Done() }()
	unlock, err := a.Agent.LockPluginReloadAdmission()
	if err != nil {
		return result, err
	}
	admitted := true
	defer func() {
		if admitted {
			unlock()
		}
	}()
	if a.Subagents != nil {
		if err := a.Subagents.CheckPluginReloadAdmitted(id); err != nil {
			return result, err
		}
	}
	resume, err := a.PluginManager.FreezeJavaScriptDelivery()
	if err != nil {
		return result, err
	}
	defer resume()
	resumeCandidate, err := runtime.FreezeForReload()
	if err != nil {
		return result, err
	}
	defer resumeCandidate()
	latest, err := a.reloadDeclaration(id)
	if err != nil {
		return result, err
	}
	if !reflect.DeepEqual(declaration, latest) {
		return result, errors.New("plugin reload: registration changed during preparation")
	}
	// Detect edits during preparation; execution still uses the original immutable
	// snapshot. Never reexecute a newly read script without candidate validation.
	current, err := javascript.ReadPackage(ctx, latest.Path, latest.Root, latest.Spec.Config)
	if err != nil {
		return result, err
	}
	if current.Fingerprint != pkg.Fingerprint {
		return result, errors.New("plugin reload: package changed during preparation")
	}
	cleanup, err := a.PluginManager.CommitJavaScript(ctx, candidate, a.preparePluginReloadCatalog)
	if err != nil {
		return result, err
	}
	result.Applied, result.Fingerprint = true, pkg.Fingerprint
	result.Generation = s.generation.Add(1)
	a.PluginManager.SetJavaScriptScope(id, latest.Scope)
	s.mu.Lock()
	for key, view := range s.views {
		if view.PluginID == id {
			delete(s.views, key)
		}
	}
	s.mu.Unlock()
	a.PluginManager.BindExtensions(func(info protocol.PluginInfo) plugin.ExtensionHost {
		if info.ID == id {
			s.mu.Lock()
			for _, view := range info.Views {
				s.views[view.ID] = view
			}
			s.mu.Unlock()
		}
		return &appExtensionHost{app: a, services: s, info: info, agent: a.Agent, store: a.Session, generation: result.Generation}
	})
	a.refreshPluginToolPolicy()
	// Candidate diagnostics are connected only after commit, so failed
	// preparation cannot pollute live plugin status or observer output.
	runtime.SetDiagnostic(func(status, message string) { a.PluginManager.RecordDiagnostic(id, status, message) })
	resumeCandidate()
	resume()
	unlock()
	admitted = false
	s.sessionMu.Unlock()
	sessionHeld = false
	diagnose := func(phase string, err error) {
		if err == nil {
			return
		}
		message := boundedReloadDiagnostic(err.Error())
		result.Diagnostics = append(result.Diagnostics, protocol.PluginReloadDiagnostic{Phase: phase, Message: message})
		a.PluginManager.RecordDiagnostic(id, phase, message)
	}
	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	diagnose("cleanup", cleanup(closeCtx))
	cancel()
	// Use host lifetime after commit. Caller cancellation cannot undo the swap,
	// but it must still bound a readiness callback that may await UI or host work.
	readyCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.ctx, cancel)
	if readyStarted {
		diagnose("ready", a.PluginManager.ReadyJavaScript(readyCtx, id))
	}
	stop()
	cancel()
	a.Agent.Publish(protocol.AgentEvent{Type: protocol.EvSessionUpdated, Message: "plugin reloaded: " + id})
	return result, nil
}

func (a *App) reloadDeclaration(id string) (javascript.Declaration, error) {
	declarations, err := a.currentPluginDeclarations()
	if err != nil {
		return javascript.Declaration{}, err
	}
	for _, d := range declarations {
		if d.ID != id {
			continue
		}
		if d.Disabled {
			return d, errors.New("plugin reload: disabled registration requires restart")
		}
		return d, nil
	}
	return javascript.Declaration{}, fmt.Errorf("plugin reload: registration %q not found", id)
}
func (a *App) preparePluginReloadCatalog(catalog []tools.ToolDescriptor) error {
	if a.Router == nil {
		for _, desc := range catalog {
			if desc.Schema.Discovery != nil && desc.Schema.Discovery.Mode == protocol.ToolDiscoveryDeferred {
				return errors.New("plugin reload: adding deferred tools requires a router; restart Snow")
			}
		}
		return nil
	}
	if router, ok := a.Router.(interface {
		RefreshMetadata([]tools.DescriptorMetadata) error
	}); ok {
		metadata := make([]tools.DescriptorMetadata, 0, len(catalog))
		for _, desc := range catalog {
			metadata = append(metadata, tools.MetadataFromDescriptor(desc))
		}
		return router.RefreshMetadata(metadata)
	}
	if router, ok := a.Router.(tools.RefreshableRouter); ok {
		return router.Refresh(catalog)
	}
	return errors.New("plugin reload: router cannot refresh atomically")
}
func boundedReloadDiagnostic(message string) string {
	message = strings.ToValidUTF8(message, "�")
	if len(message) <= 2048 {
		return message
	}
	message = message[:2048]
	for !utf8.ValidString(message) {
		message = message[:len(message)-1]
	}
	return message
}
