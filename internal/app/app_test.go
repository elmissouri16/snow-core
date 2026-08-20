package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/elmissouri16/snow-core/internal/buildinfo"
	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/provider/fake"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/userinput"
	publicmcp "github.com/elmissouri16/snow-core/pkg/mcp"
	publicplugin "github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type appDeferredPlugin struct{}

type appCloseTrackingPlugin struct{ closed *int }

type blockingSessionStore struct {
	*session.MemoryStore
	started chan struct{}
	release chan struct{}
	closed  chan struct{}
}

func (s *blockingSessionStore) Messages() ([]protocol.Message, error) {
	select {
	case <-s.started:
	default:
		close(s.started)
	}
	<-s.release
	return s.MemoryStore.Messages()
}
func (s *blockingSessionStore) Close() error {
	select {
	case <-s.closed:
	default:
		close(s.closed)
	}
	return s.MemoryStore.Close()
}

func (p appCloseTrackingPlugin) Manifest() publicplugin.Manifest {
	return publicplugin.Manifest{ID: "close-tracker", Name: "Close tracker", Version: "1", ProtocolVersion: publicplugin.ProtocolVersion}
}
func (appCloseTrackingPlugin) Register(context.Context, publicplugin.Registrar) error { return nil }
func (p appCloseTrackingPlugin) Close(context.Context) error {
	*p.closed++
	return nil
}

type refreshCatalogProvider struct {
	*fake.Provider
	models        []protocol.Model
	authoritative bool
}

func (p *refreshCatalogProvider) RefreshModels(context.Context) ([]protocol.Model, error) {
	return append([]protocol.Model(nil), p.models...), nil
}
func (p *refreshCatalogProvider) ModelCatalogAuthoritative() bool { return p.authoritative }

func (appDeferredPlugin) Manifest() publicplugin.Manifest {
	return publicplugin.Manifest{ID: "catalog", Name: "Catalog", Version: "1", ProtocolVersion: publicplugin.ProtocolVersion}
}
func (appDeferredPlugin) Register(_ context.Context, registrar publicplugin.Registrar) error {
	return registrar.RegisterTool(publicplugin.ToolDefinition{
		Name: "lookup", Description: "Look up catalog records", Parameters: json.RawMessage(`{"type":"object"}`), Risk: "read",
		Discovery: &protocol.ToolDiscovery{Mode: protocol.ToolDiscoveryDeferred, Keywords: []string{"catalog"}},
		Executor: func(context.Context, publicplugin.ToolContext, json.RawMessage) (publicplugin.ToolResult, error) {
			return publicplugin.ToolResult{}, nil
		},
	})
}
func (appDeferredPlugin) Close(context.Context) error { return nil }

func TestBuildVersionDefaultsAndOverrides(t *testing.T) {
	newApp := func(version string) *App {
		t.Helper()
		a, err := New(context.Background(), Options{
			Provider: "fake", NoSession: true, Permission: "deny", CWD: t.TempDir(),
			NoPlugins: true, NoMCP: true, NoSkills: true, BuildVersion: version,
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = a.Close() })
		return a
	}

	if got := newApp("").BuildVersion; got != buildinfo.Version {
		t.Fatalf("default build version = %q, want %q", got, buildinfo.Version)
	}
	if got := newApp("0.1.0-alpha.test").BuildVersion; got != "0.1.0-alpha.test" {
		t.Fatalf("explicit build version = %q", got)
	}
}

func TestMergePluginSpecsUsesGlobalProjectExplicitPrecedence(t *testing.T) {
	base := publicplugin.PluginSpec{ID: "demo", Command: []string{"global"}, Enabled: true}
	project := publicplugin.PluginSpec{ID: "demo", Command: []string{"project"}, Enabled: false}
	explicit := publicplugin.PluginSpec{ID: "demo", Command: []string{"explicit"}, Enabled: true}
	merged, err := mergePluginSpecs([]publicplugin.PluginSpec{base}, []publicplugin.PluginSpec{project}, []publicplugin.PluginSpec{explicit})
	if err != nil {
		t.Fatal(err)
	}
	if len(merged) != 1 || strings.Join(merged[0].Command, " ") != "explicit" || !merged[0].Enabled {
		t.Fatalf("merged plugins = %+v", merged)
	}
	merged, err = mergePluginSpecs([]publicplugin.PluginSpec{base}, []publicplugin.PluginSpec{project}, nil)
	if err != nil || len(merged) != 1 || merged[0].Enabled || strings.Join(merged[0].Command, " ") != "project" {
		t.Fatalf("disabled project override = %+v, %v", merged, err)
	}
	if _, err := mergePluginSpecs([]publicplugin.PluginSpec{base, base}, nil, nil); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate error = %v", err)
	}
}

func TestNoPluginsAllowsRecoveryFromInvalidPluginConfiguration(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SNOW_HOME", home)
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"plugins":[{"id":"broken","enabled":true}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := New(context.Background(), Options{Provider: "fake", NoSession: true, NoPlugins: true, NoMCP: true, NoSkills: true, Permission: "deny", CWD: t.TempDir(), ConfigPath: configPath})
	if err != nil {
		t.Fatalf("NoPlugins should permit recovery from invalid plugin declarations: %v", err)
	}
	defer a.Close()
}

func TestAppProjectPluginOverrideSuppressesGlobalPlugin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SNOW_HOME", home)
	cwd := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	global := `{"default_project_trust":"allow","plugins":[{"id":"layered","command":["/definitely/missing/global-plugin"],"enabled":true}]}`
	if err := os.WriteFile(configPath, []byte(global), 0o600); err != nil {
		t.Fatal(err)
	}
	projectConfig := filepath.Join(cwd, ".snow", "config.json")
	if err := os.MkdirAll(filepath.Dir(projectConfig), 0o755); err != nil {
		t.Fatal(err)
	}
	project := `{"plugins":[{"id":"layered","command":["/definitely/missing/project-plugin"],"enabled":false}]}`
	if err := os.WriteFile(projectConfig, []byte(project), 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := New(context.Background(), Options{Provider: "fake", NoSession: true, NoMCP: true, NoSkills: true, Permission: "deny", CWD: cwd, ConfigPath: configPath})
	if err != nil {
		t.Fatalf("disabled project override should suppress enabled global plugin: %v", err)
	}
	defer a.Close()
	found := false
	for _, diagnostic := range a.PluginManager.Diagnostics() {
		if diagnostic.PluginID == "layered" && diagnostic.Status == "disabled" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing disabled override diagnostic: %+v", a.PluginManager.Diagnostics())
	}
}

func TestAppIncludesEmbeddedPluginBuilderSkill(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	a, err := New(context.Background(), Options{Provider: "fake", NoSession: true, NoPlugins: true, NoMCP: true, Permission: "deny", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	skill, ok := a.Skills.Get("plugin-builder")
	if !ok || skill.Scope != "builtin" {
		t.Fatalf("plugin-builder = %+v, %v", skill, ok)
	}
	if _, ok := a.Registry.Get("activate_skill"); !ok {
		t.Fatal("embedded skill did not register activation tool")
	}

	without, err := New(context.Background(), Options{Provider: "fake", NoSession: true, NoPlugins: true, NoMCP: true, NoSkills: true, Permission: "deny", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer without.Close()
	if without.Skills != nil {
		t.Fatal("NoSkills retained embedded plugin-builder catalog")
	}
}

// TestAppTrustStoreUsesTrustFileNotAuthFile is a regression test for a real
// startup bug: the trust store was wired to config.DefaultPaths()[1]
// (authPath) instead of [2] (trustPath), so once /login stored a credential in
// auth.json, every app startup failed with "trust: corrupt auth.json".
func TestAppTrustStoreUsesTrustFileNotAuthFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SNOW_HOME", home)

	// Simulate a successful /login: a stored API key in auth.json.
	authPath := filepath.Join(home, "auth.json")
	if err := os.WriteFile(authPath, []byte(`{"opencode-go":{"type":"api_key","key":"sk-test"}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	a, err := New(ctx, Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("app must start with a populated auth.json: %v", err)
	}
	defer a.Close()

	// A trust decision must persist to trust.json and must not clobber auth.json.
	if err := a.Trust.Set(a.CWD(), "allow"); err != nil {
		t.Fatal(err)
	}
	trustData, err := os.ReadFile(filepath.Join(home, "trust.json"))
	if err != nil {
		t.Fatalf("trust.json missing: %v", err)
	}
	if !strings.Contains(string(trustData), "allow") {
		t.Fatalf("trust.json = %q, want an allow decision", trustData)
	}
	authData, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(authData), "sk-test") {
		t.Fatalf("auth.json was clobbered by trust save: %q", authData)
	}
}

func TestAppRejectsInvalidPermissionMode(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	_, err := New(context.Background(), Options{
		Provider: "fake", NoSession: true, Permission: "alow", CWD: t.TempDir(),
	})
	if err == nil || !strings.Contains(err.Error(), "invalid permission mode") {
		t.Fatalf("invalid permission error = %v", err)
	}
}

func TestAppRejectsInvalidPermissionFromConfigBeforePluginStartup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SNOW_HOME", home)
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"permission_mode":"alow"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	closed := 0
	_, err := New(context.Background(), Options{Provider: "fake", NoSession: true, CWD: t.TempDir(), ConfigPath: configPath, GoPlugins: []publicplugin.Plugin{appCloseTrackingPlugin{closed: &closed}}})
	if err == nil || !strings.Contains(err.Error(), "invalid permission mode") {
		t.Fatalf("invalid config error = %v", err)
	}
	if closed != 0 {
		t.Fatalf("plugin was acquired before permission validation; close count=%d", closed)
	}
}

func TestAppNewCleansInitializedPluginsOnLaterFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SNOW_HOME", home)
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"subagents":{"enabled":true,"roles":{"general":{"model":"missing-model"}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	closed := 0
	_, err := New(context.Background(), Options{
		Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir(),
		ConfigPath: configPath, GoPlugins: []publicplugin.Plugin{appCloseTrackingPlugin{closed: &closed}}, NoMCP: true,
	})
	if err == nil || !strings.Contains(err.Error(), "unavailable selection") {
		t.Fatalf("constructor error = %v", err)
	}
	if closed != 1 {
		t.Fatalf("plugin close count = %d, want 1", closed)
	}
}

func TestAppSubagentModelOverride(t *testing.T) {
	enabled := true
	a, err := New(context.Background(), Options{
		Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir(),
		Subagents: &enabled, SubagentProvider: "fake", SubagentModel: "fake-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if a.Cfg.Subagents.DefaultProvider != "fake" || a.Cfg.Subagents.DefaultModel != "fake-1" {
		t.Fatalf("subagent defaults = %s/%s", a.Cfg.Subagents.DefaultProvider, a.Cfg.Subagents.DefaultModel)
	}
	if _, err := New(context.Background(), Options{
		Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir(),
		Subagents: &enabled, SubagentModel: "missing-model",
	}); err == nil || !strings.Contains(err.Error(), "subagent defaults references unavailable selection") {
		t.Fatalf("missing subagent model error = %v", err)
	}
}

func TestAppSubagentConcurrencyOverrideCountsChildrenAndRaisesIdentityCap(t *testing.T) {
	enabled := true
	a, err := New(context.Background(), Options{
		Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir(),
		Subagents: &enabled, SubagentMaxConcurrency: 40,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if a.Cfg.Subagents.MaxConcurrentThreads != 40 || a.Cfg.Subagents.MaxAgentsPerSession != 40 {
		t.Fatalf("subagent limits = %+v", a.Cfg.Subagents)
	}

	_, err = New(context.Background(), Options{
		Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir(),
		Subagents: &enabled, SubagentMaxConcurrency: 10, SubagentMaxAgents: 5,
	})
	if err == nil || !strings.Contains(err.Error(), "below child concurrency") {
		t.Fatalf("explicit inconsistent limits error = %v", err)
	}
}

func TestAppRejectsBlankStartupModelID(t *testing.T) {
	_, err := New(context.Background(), Options{Provider: "fake", Model: "   ", NoSession: true, Permission: "allow", CWD: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "model id must not be blank") {
		t.Fatalf("blank startup model error = %v", err)
	}
}

func TestAppRestoresIndependentProjectModelSelections(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SNOW_HOME", home)
	projectA := filepath.Join(t.TempDir(), "project-a")
	projectB := filepath.Join(t.TempDir(), "project-b")
	if err := os.MkdirAll(projectA, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(projectB, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.DefaultProvider = "fake"
	cfg.DefaultModel = "global-model"
	var err error
	cfg, err = config.WithProjectSelection(cfg, projectA, config.ProjectSelection{Provider: "fake", Model: "model-a", Thinking: "off"})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = config.WithProjectSelection(cfg, projectB, config.ProjectSelection{Provider: "fake", Model: "model-b", Thinking: "off"})
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(home, "config.json")
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatal(err)
	}

	open := func(project string, opts Options) *App {
		t.Helper()
		opts.CWD = project
		opts.ConfigPath = configPath
		opts.NoSession = true
		opts.Permission = "allow"
		a, err := New(context.Background(), opts)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = a.Close() })
		return a
	}
	a := open(projectA, Options{})
	b := open(projectB, Options{})
	if a.ProviderID != "fake" || a.Model.ID != "model-a" || a.Agent.Thinking() != protocol.ThinkingOff {
		t.Fatalf("project A runtime = %s/%s thinking:%s", a.ProviderID, a.Model.ID, a.Agent.Thinking())
	}
	if b.ProviderID != "fake" || b.Model.ID != "model-b" || b.Agent.Thinking() != protocol.ThinkingOff {
		t.Fatalf("project B runtime = %s/%s thinking:%s", b.ProviderID, b.Model.ID, b.Agent.Thinking())
	}
	if a.PersistedCfg.DefaultModel != "global-model" || b.PersistedCfg.DefaultModel != "global-model" {
		t.Fatalf("global model default was replaced: A=%q B=%q", a.PersistedCfg.DefaultModel, b.PersistedCfg.DefaultModel)
	}
	if a.Cfg.PermissionMode != "allow" || a.PersistedCfg.PermissionMode != "ask" {
		t.Fatalf("runtime permission leaked into persisted config: runtime=%q persisted=%q", a.Cfg.PermissionMode, a.PersistedCfg.PermissionMode)
	}

	overridden := open(projectA, Options{Provider: "fake", Model: "cli-model", Thinking: "off", BaseURL: "https://runtime.example"})
	if overridden.Model.ID != "cli-model" {
		t.Fatalf("CLI model = %q, want cli-model", overridden.Model.ID)
	}
	if overridden.Cfg.Providers["fake"].BaseURL != "https://runtime.example" || overridden.PersistedCfg.Providers["fake"].BaseURL != "" {
		t.Fatalf("runtime endpoint leaked into persisted config: runtime=%q persisted=%q", overridden.Cfg.Providers["fake"].BaseURL, overridden.PersistedCfg.Providers["fake"].BaseURL)
	}
}

func TestAppRejectsBlankModelIDWithoutChangingSelection(t *testing.T) {
	a, err := New(context.Background(), Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	before := a.Agent.Model()
	if err := a.SetModel(protocol.Model{Provider: before.Provider, ID: "   "}); err == nil {
		t.Fatal("blank model id was accepted")
	}
	after := a.Agent.Model()
	if after.ID != before.ID || a.Model.ID != before.ID {
		t.Fatalf("selection changed: before=%+v app=%+v agent=%+v", before, a.Model, after)
	}
	_, childModel, err := a.runtimeSelection.childSelection("", "")
	if err != nil || childModel.ID != before.ID {
		t.Fatalf("child selection=%+v err=%v", childModel, err)
	}
}

func TestSetSessionWaitsForActiveSessionReadBeforeClosingOldStore(t *testing.T) {
	a, err := New(context.Background(), Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	initialRelease := make(chan struct{})
	close(initialRelease)
	old := &blockingSessionStore{MemoryStore: session.NewMemoryStore(session.Options{CWD: a.CWD()}), started: make(chan struct{}), release: initialRelease, closed: make(chan struct{})}
	if err := a.SetSession(old); err != nil {
		t.Fatal(err)
	}
	old.started = make(chan struct{})
	old.release = make(chan struct{})
	readDone := make(chan error, 1)
	go func() { _, err := a.Agent.Messages(); readDone <- err }()
	<-old.started
	newStore := session.NewMemoryStore(session.Options{CWD: a.CWD()})
	switchDone := make(chan error, 1)
	go func() { switchDone <- a.SetSession(newStore) }()
	select {
	case err := <-switchDone:
		t.Fatalf("session switched during active read: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	select {
	case <-old.closed:
		t.Fatal("old store closed during active read")
	default:
	}
	close(old.release)
	if err := <-readDone; err != nil {
		t.Fatal(err)
	}
	if err := <-switchDone; err != nil {
		t.Fatal(err)
	}
	select {
	case <-old.closed:
	case <-time.After(time.Second):
		t.Fatal("old store was not closed after read completed")
	}
}

func TestAppRejectsModelFromDifferentProvider(t *testing.T) {
	a, err := New(context.Background(), Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	before := a.Model
	if err := a.SetModel(protocol.Model{Provider: "other", ID: "foreign"}); err == nil {
		t.Fatal("cross-provider model was accepted")
	}
	agentModel := a.Agent.Model()
	if a.Model.Provider != before.Provider || a.Model.ID != before.ID || agentModel.Provider != before.Provider || agentModel.ID != before.ID {
		t.Fatalf("model changed after rejection: app=%+v agent=%+v before=%+v", a.Model, agentModel, before)
	}
}

func TestRefreshProviderModelsReturnsActiveThinkingConflict(t *testing.T) {
	a, err := New(context.Background(), Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	current := a.Agent.Model()
	current.SupportsThinking = true
	current.ThinkingLevels = []protocol.ThinkingLevel{protocol.ThinkingHigh}
	if err := a.Agent.SetModel(current); err != nil {
		t.Fatal(err)
	}
	if err := a.Agent.SetThinking(protocol.ThinkingHigh); err != nil {
		t.Fatal(err)
	}
	a.Model = current
	a.Models = []protocol.Model{current}
	a.modelCatalog["fake"] = []protocol.Model{current}
	refreshed := protocol.Model{Provider: "fake", ID: current.ID, SupportsTools: true}
	a.Providers["fake"] = &refreshCatalogProvider{Provider: fake.NewWithModels([]protocol.Model{refreshed}), models: []protocol.Model{refreshed}}

	err = a.RefreshProviderModels(context.Background(), "fake")
	if err == nil || !strings.Contains(err.Error(), current.ID) || !strings.Contains(err.Error(), "thinking level") || !strings.Contains(err.Error(), "current settings") {
		t.Fatalf("refresh conflict error = %v", err)
	}
	if got := a.Agent.Model(); !got.SupportsThinking || got.ID != current.ID {
		t.Fatalf("active model changed after incompatible refresh: %+v", got)
	}
	if len(a.Models) != 1 || !a.Models[0].SupportsThinking || len(a.modelCatalog["fake"]) != 1 || !a.modelCatalog["fake"][0].SupportsThinking {
		t.Fatalf("catalog snapshots changed after incompatible refresh: models=%+v catalog=%+v", a.Models, a.modelCatalog["fake"])
	}
}

func TestRefreshProviderModelsReplacesUnavailableAuthoritativeModel(t *testing.T) {
	a, err := New(context.Background(), Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	unavailable := a.Agent.Model()
	replacement := protocol.Model{Provider: "fake", ID: "account-model", SupportsTools: true}
	a.Providers["fake"] = &refreshCatalogProvider{
		Provider:      fake.NewWithModels([]protocol.Model{replacement}),
		models:        []protocol.Model{replacement},
		authoritative: true,
	}
	if err := a.RefreshProviderModels(context.Background(), "fake"); err != nil {
		t.Fatal(err)
	}
	if got := a.Agent.Model(); got.ID != replacement.ID {
		t.Fatalf("agent model=%q, want account model %q (old=%q)", got.ID, replacement.ID, unavailable.ID)
	}
	if a.Model.ID != replacement.ID || len(a.Models) != 1 || a.Models[0].ID != replacement.ID {
		t.Fatalf("app model=%+v models=%+v", a.Model, a.Models)
	}
}

func TestAppRejectsInvalidThinkingConfiguration(t *testing.T) {
	_, err := New(context.Background(), Options{
		Provider:   "fake",
		Thinking:   "extreme",
		NoSession:  true,
		Permission: "allow",
		CWD:        t.TempDir(),
	})
	if err == nil || !strings.Contains(err.Error(), "invalid thinking level") {
		t.Fatalf("error = %v, want invalid thinking level", err)
	}
}

func TestReentrantManualUserInputFailsFast(t *testing.T) {
	a, err := New(context.Background(), Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.EnableUserInputReplies()
	result := make(chan error, 1)
	a.Agent.Subscribe(func(ev protocol.AgentEvent) {
		if ev.Type == protocol.EvModeChanged {
			_, err := a.RequestUserInput(context.Background(), protocol.UserInputRequest{ID: "nested", Questions: []protocol.UserInputQuestion{{ID: "q", Question: "answer?"}}})
			result <- err
		}
	})
	a.Agent.Publish(a.Agent.StateEvent())
	select {
	case err := <-result:
		if !errors.Is(err, userinput.ErrUnavailable) {
			t.Fatalf("err=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("reentrant manual request deadlocked")
	}
}

func TestAppUserInputCallbackAndManualReply(t *testing.T) {
	request := protocol.UserInputRequest{ID: "ask-1", Questions: []protocol.UserInputQuestion{{ID: "name", Header: "Name", Question: "What name?"}}}
	t.Run("callback", func(t *testing.T) {
		var seen protocol.UserInputRequest
		a, err := New(context.Background(), Options{
			Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir(),
			UserInputHandler: func(_ context.Context, req protocol.UserInputRequest) (protocol.UserInputResponse, error) {
				seen = req
				return protocol.UserInputResponse{Answers: []protocol.UserInputAnswer{{QuestionID: "name", Answer: "Snow"}}}, nil
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		defer a.Close()
		var event *protocol.UserInputRequest
		a.Agent.Subscribe(func(ev protocol.AgentEvent) {
			if ev.Type == protocol.EvUserInputRequest {
				event = ev.UserInput
			}
		})
		response, err := a.RequestUserInput(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		if seen.ID != request.ID || event == nil || event.ID != request.ID || response.Answers[0].Answer != "Snow" {
			t.Fatalf("seen=%+v event=%+v response=%+v", seen, event, response)
		}
	})

	t.Run("manual", func(t *testing.T) {
		a, err := New(context.Background(), Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		defer a.Close()
		a.EnableUserInputReplies()
		published := make(chan protocol.UserInputRequest, 1)
		a.Agent.Subscribe(func(ev protocol.AgentEvent) {
			if ev.Type == protocol.EvUserInputRequest && ev.UserInput != nil {
				published <- *ev.UserInput
			}
		})
		resolved := make(chan protocol.UserInputResponse, 1)
		go func() {
			response, _ := a.RequestUserInput(context.Background(), request)
			resolved <- response
		}()
		<-published
		if err := a.ReplyUserInput(protocol.UserInputResponse{RequestID: request.ID, Answers: []protocol.UserInputAnswer{{QuestionID: "name", Answer: "Snow"}}}); err != nil {
			t.Fatal(err)
		}
		if response := <-resolved; response.Answers[0].Answer != "Snow" {
			t.Fatalf("response = %+v", response)
		}
	})
}

func TestAppRejectsInvalidResponseConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts Options
		want string
	}{
		{name: "summary", opts: Options{ReasoningSummary: "full"}, want: "invalid reasoning summary"},
		{name: "verbosity", opts: Options{TextVerbosity: "maximum"}, want: "invalid text verbosity"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.opts.Provider = "fake"
			tc.opts.NoSession = true
			tc.opts.Permission = "allow"
			tc.opts.CWD = t.TempDir()
			_, err := New(context.Background(), tc.opts)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestAppCachesProviderModelsAtStartup(t *testing.T) {
	a, err := New(context.Background(), Options{
		Provider:   "fake",
		Model:      "explicit-model",
		NoSession:  true,
		Permission: "allow",
		CWD:        t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if len(a.Models) == 0 {
		t.Fatal("startup should cache the provider model catalog even with an explicit model")
	}
}

func TestAppCombinedModelCatalogHasNoDuplicates(t *testing.T) {
	a, err := New(context.Background(), Options{
		Provider:   "fake",
		NoSession:  true,
		Permission: "allow",
		CWD:        t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	seen := make(map[string]bool)
	for _, model := range a.AllModels {
		key := model.Provider + "/" + model.ID
		if seen[key] {
			t.Fatalf("duplicate combined catalog model %q: %+v", key, a.AllModels)
		}
		seen[key] = true
	}
	if len(a.AllModels) != len(a.Models) {
		t.Fatalf("fake combined catalog has %d models, active catalog has %d", len(a.AllModels), len(a.Models))
	}
}

func TestNormalizeProviderModelsRemovesDuplicateIDs(t *testing.T) {
	models := normalizeProviderModels("test", []protocol.Model{{ID: "one"}, {Provider: "other", ID: "one"}, {ID: ""}, {ID: "two"}})
	if len(models) != 2 || models[0].Provider != "test" || models[0].ID != "one" || models[1].ID != "two" {
		t.Fatalf("normalized models = %+v", models)
	}
}

func TestAppCreatesRouterForInitiallyEmptyMutableMCPCatalog(t *testing.T) {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "mutable", Version: "1"}, nil)
	server.AddTool(&sdkmcp.Tool{Name: "later", InputSchema: json.RawMessage(`{"type":"object"}`)}, nil)
	server.RemoveTools("later")
	httpServer := httptest.NewServer(sdkmcp.NewStreamableHTTPHandler(func(_ *http.Request) *sdkmcp.Server {
		return server
	}, &sdkmcp.StreamableHTTPOptions{Stateless: true}))
	defer httpServer.Close()

	a, err := New(context.Background(), Options{
		Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir(),
		MCPServers: []publicmcp.ServerSpec{{ID: "mutable", URL: httpServer.URL}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if a.Router == nil {
		t.Fatal("mutable MCP tools capability should create an empty deferred router")
	}
	if _, ok := a.Registry.Get("search_tools"); !ok {
		t.Fatal("search_tools should be available before the first tools/list_changed notification")
	}
}

func TestAppChatGPTCatalogProvider(t *testing.T) {
	a, err := New(context.Background(), Options{
		Provider:   "chatgpt",
		NoSession:  true,
		Permission: "allow",
		CWD:        t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if len(a.Models) == 0 || a.Models[0].Provider != "chatgpt" {
		t.Fatalf("chatgpt catalog = %+v", a.Models)
	}
}

// TestAppFakeProviderRoundTrip wires the full app with the fake provider and
// verifies a prompt produces a session with user + assistant messages.
func TestAppFakeProviderRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	a, err := New(ctx, Options{
		Provider:   "fake",
		NoSession:  true,
		Permission: "allow",
		CWD:        t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	var sawTurnDone bool
	a.Agent.Subscribe(func(ev protocol.AgentEvent) {
		if ev.Type == protocol.EvTurnDone {
			sawTurnDone = true
		}
	})

	if err := a.Agent.Prompt(ctx, "hello from app test"); err != nil {
		t.Fatal(err)
	}
	if !sawTurnDone {
		t.Fatal("expected turn_done event")
	}
	msgs, err := a.Session.Messages()
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != protocol.RoleUser {
		t.Fatalf("msg0 role = %s", msgs[0].Role)
	}
}
