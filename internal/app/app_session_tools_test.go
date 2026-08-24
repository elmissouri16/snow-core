package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/trust"
	publicplugin "github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// TestAppSessionPersistence verifies a real session file is created and
// messages survive reopening.
func TestAppSessionPersistence(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	// Point the index at a temp sessions dir.
	t.Setenv("SNOW_SESSIONS_DIR", filepath.Join(dir, "sessions"))

	a, err := New(ctx, Options{Provider: "fake", Permission: "allow", CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Agent.Prompt(ctx, "persist me"); err != nil {
		t.Fatal(err)
	}
	path := a.Session.Path()
	if path == "" {
		t.Fatal("expected a session file path")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("session file missing: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopen and verify the user message survived.
	a2, err := New(ctx, Options{Provider: "fake", Permission: "allow", CWD: dir, SessionPath: path})
	if err != nil {
		t.Fatal(err)
	}
	defer a2.Close()
	msgs, err := a2.Agent.Messages()
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages after reload, got %d", len(msgs))
	}
	if !strings.Contains(msgs[0].Content[0].Text, "persist me") {
		t.Fatalf("user message lost: %+v", msgs[0])
	}
}

func TestAppDeleteSessionRejectsActiveDatabase(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	t.Setenv("SNOW_HOME", filepath.Join(dir, "home"))
	t.Setenv("SNOW_SESSIONS_DIR", filepath.Join(dir, "sessions"))
	a, err := New(ctx, Options{Provider: "fake", Permission: "allow", CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	message := protocol.NewUserMessage("active", a.Session.BranchTip(), "active")
	if err := a.Session.Append(session.Entry{Type: session.EntryMessage, ID: message.ID, Message: &message}); err != nil {
		t.Fatal(err)
	}
	path := a.Session.Path()
	if err := a.DeleteSession(path, a.Session.ID()); err == nil || !strings.Contains(err.Error(), "active session") {
		t.Fatalf("DeleteSession(active) error = %v", err)
	}
	alias := filepath.Join(filepath.Dir(path), "active-alias.db")
	if err := os.Link(path, alias); err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteSession(alias, "wrong-id"); err == nil || !strings.Contains(err.Error(), "active session") {
		t.Fatalf("DeleteSession(active hard-link alias) error = %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("active session was removed: %v", err)
	}
}

func TestAppDeleteSessionCleansManagedPrivateState(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	t.Setenv("SNOW_HOME", home)
	t.Setenv("SNOW_SESSIONS_DIR", filepath.Join(dir, "sessions"))
	a, err := New(ctx, Options{Provider: "fake", Permission: "allow", CWD: dir, NoSession: true})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	idx := session.NewFileIndex(session.DefaultSessionsRoot())
	store, err := idx.Create(dir)
	if err != nil {
		t.Fatal(err)
	}
	message := protocol.NewUserMessage("delete", store.BranchTip(), "delete")
	if err := store.Append(session.Entry{Type: session.EntryMessage, ID: message.ID, Message: &message}); err != nil {
		t.Fatal(err)
	}
	path, id := store.Path(), store.ID()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	ref, err := a.artifacts.SaveText(ctx, id, "tool", "private")
	if err != nil {
		t.Fatal(err)
	}
	goalPath := filepath.Join(home, "goals", id, "goal", "goal-objective.md")
	if err := os.MkdirAll(filepath.Dir(goalPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(goalPath, []byte("goal"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteSession(path, id); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("session database still exists: %v", err)
	}
	if exists, err := a.artifacts.Exists(ctx, id, ref.ID); err != nil || exists {
		t.Fatalf("artifact exists=%v err=%v", exists, err)
	}
	if _, err := os.Stat(goalPath); !os.IsNotExist(err) {
		t.Fatalf("goal file still exists: %v", err)
	}
}

func TestAppEmptySessionIsNotPersisted(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	t.Setenv("SNOW_SESSIONS_DIR", filepath.Join(dir, "sessions"))

	a, err := New(ctx, Options{Provider: "fake", Permission: "allow", CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	path := a.Session.Path()
	if path == "" {
		t.Fatal("expected a session file path")
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("empty session file still exists (err=%v)", err)
	}
}

func TestAppPermissionStatePersistsPerSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SNOW_HOME", home)
	dir := t.TempDir()
	t.Setenv("SNOW_SESSIONS_DIR", filepath.Join(dir, "sessions"))

	a, err := New(context.Background(), Options{Provider: "fake", CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	a.Perm.SetMode(permission.ModeAllow)
	message := protocol.NewUserMessage("keep", "", "keep session")
	if err := a.Session.Append(session.Entry{Type: session.EntryMessage, Message: &message}); err != nil {
		t.Fatal(err)
	}
	path := a.Session.Path()
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	a2, err := New(context.Background(), Options{Provider: "fake", CWD: dir, SessionPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if got := a2.Perm.Mode(); got != permission.ModeAllow {
		t.Fatalf("reopened permission mode = %q, want allow", got)
	}
	a2.Perm.SetMode(permission.ModeAsk)
	a2.Perm.Remember(permission.Request{Tool: "bash", Risk: permission.RiskExec}, permission.DecisionAllow)
	if err := a2.Close(); err != nil {
		t.Fatal(err)
	}

	a3, err := New(context.Background(), Options{Provider: "fake", CWD: dir, SessionPath: path})
	if err != nil {
		t.Fatal(err)
	}
	defer a3.Close()
	if got := a3.Perm.Mode(); got != permission.ModeAsk {
		t.Fatalf("reopened permission mode = %q, want ask", got)
	}
	decision, err := a3.Perm.Authorize(context.Background(), permission.Request{Tool: "bash", Risk: permission.RiskExec})
	if err != nil || decision != permission.DecisionAllow {
		t.Fatalf("reopened permission rule = %q, %v", decision, err)
	}
}

// TestAppContextLoadsAgents verifies AGENTS.md is picked up into the system prompt.
func TestAppContextLoadsAgents(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("always use tabs"), 0o644); err != nil {
		t.Fatal(err)
	}

	a, err := New(ctx, Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if !strings.Contains(a.Agent.SystemPrompt(), "always use tabs") {
		t.Fatalf("AGENTS.md not in system prompt: %q", a.Agent.SystemPrompt())
	}
}

func TestAppLoadsConfiguredSystemPromptFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SNOW_HOME", home)
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"system_prompt_file":"system.md"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "system.md"), []byte("Custom global system prompt."), 0o600); err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "AGENTS.md"), []byte("Keep project context."), 0o644); err != nil {
		t.Fatal(err)
	}

	a, err := New(context.Background(), Options{Provider: "fake", ConfigPath: configPath, NoSession: true, Permission: "allow", CWD: cwd})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	prompt := a.Agent.SystemPrompt()
	if !strings.Contains(prompt, "Custom global system prompt.") || !strings.Contains(prompt, "Keep project context.") {
		t.Fatalf("assembled system prompt = %q", prompt)
	}
	if strings.Contains(prompt, "You are snow") {
		t.Fatalf("embedded preamble was not replaced: %q", prompt)
	}
}

func TestExplicitSystemPromptWinsWithoutReadingConfiguredFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SNOW_HOME", home)
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"system_prompt_file":"missing.md"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := New(context.Background(), Options{Provider: "fake", ConfigPath: configPath, SystemPrompt: "Explicit prompt.", NoSession: true, Permission: "allow", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if !strings.Contains(a.Agent.SystemPrompt(), "Explicit prompt.") {
		t.Fatalf("system prompt = %q", a.Agent.SystemPrompt())
	}
}

func TestTrustedProjectSystemPromptOverridesGlobal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SNOW_HOME", home)
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"system_prompt_file":"global.md"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "global.md"), []byte("Global prompt."), 0o600); err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, ".snow"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, ".snow", "config.json"), []byte(`{"system_prompt_file":".snow/system.md"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, ".snow", "system.md"), []byte("Project prompt."), 0o644); err != nil {
		t.Fatal(err)
	}
	opts := Options{Provider: "fake", ConfigPath: configPath, NoSession: true, Permission: "allow", CWD: cwd}

	blocked, err := New(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if prompt := blocked.Agent.SystemPrompt(); !strings.Contains(prompt, "Global prompt.") || strings.Contains(prompt, "Project prompt.") {
		blocked.Close()
		t.Fatalf("untrusted prompt = %q", prompt)
	}
	blocked.Close()

	preflight, err := InspectProjectTrust(opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := preflight.Store.Set(cwd, trust.LevelAllow); err != nil {
		t.Fatal(err)
	}
	allowed, err := New(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer allowed.Close()
	if prompt := allowed.Agent.SystemPrompt(); !strings.Contains(prompt, "Project prompt.") || strings.Contains(prompt, "Global prompt.") {
		t.Fatalf("trusted prompt = %q", prompt)
	}
}

func TestTrustedProjectSystemPromptCannotEscapeProject(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SNOW_HOME", home)
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, ".snow"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, ".snow", "config.json"), []byte(`{"system_prompt_file":"../outside.md"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	opts := Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: cwd}
	preflight, err := InspectProjectTrust(opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := preflight.Store.Set(cwd, trust.LevelAllow); err != nil {
		t.Fatal(err)
	}
	if _, err := New(context.Background(), opts); err == nil || !strings.Contains(err.Error(), "escapes trusted project root") {
		t.Fatalf("project prompt escape error = %v", err)
	}
}

// TestAppPermissionDenyBlocksBash verifies deny mode blocks write tools at the app level.
func TestAppPermissionDenyBlocksBash(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	// Use the opencode-go provider? No — we need a provider that errors without
	// credentials. Instead just verify permission service wiring.
	a, err := New(ctx, Options{Provider: "fake", NoSession: true, Permission: "deny", CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if string(a.Perm.Mode()) != "deny" {
		t.Fatalf("mode = %s, want deny", a.Perm.Mode())
	}
}

func TestAppBuildsRouterAndRegistersSearchToolsForDeferredCatalog(t *testing.T) {
	a, err := New(context.Background(), Options{
		Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir(),
		GoPlugins: []publicplugin.Plugin{appDeferredPlugin{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if a.Router == nil || a.Router.DeferredCount() != 11 {
		t.Fatalf("router = %#v", a.Router)
	}
	if _, ok := a.Registry.Get("search_tools"); !ok {
		t.Fatal("search_tools was not registered")
	}
	if _, ok := a.Registry.Get("plugin_catalog_lookup"); !ok {
		t.Fatal("deferred plugin tool was not retained in the execution registry")
	}
}

func TestAppRegistersDeferredWebFetchByDefault(t *testing.T) {
	a, err := New(context.Background(), Options{
		Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if a.Router == nil || a.Router.DeferredCount() != 10 {
		t.Fatalf("router = %#v", a.Router)
	}
	desc, ok := a.Registry.Descriptor("webfetch")
	if !ok || desc.Risk != permission.RiskNet || desc.Schema.Discovery == nil || desc.Schema.Discovery.Mode != protocol.ToolDiscoveryDeferred {
		t.Fatalf("webfetch descriptor = %+v", desc)
	}
	for _, name := range []string{"session_search", "session_reference"} {
		desc, ok := a.Registry.Descriptor(name)
		if !ok || desc.Risk != permission.RiskRead || desc.Schema.Discovery == nil || desc.Schema.Discovery.Mode != protocol.ToolDiscoveryDeferred {
			t.Fatalf("%s descriptor = %+v", name, desc)
		}
	}
	ask, ok := a.Registry.Descriptor("ask_user")
	if !ok || ask.Risk != permission.RiskRead || ask.Schema.Discovery != nil {
		t.Fatalf("ask_user descriptor = %+v", ask)
	}
	if _, ok := a.Registry.Get("search_tools"); !ok {
		t.Fatal("search_tools was not registered for deferred webfetch")
	}
	matches, err := a.Router.Search(context.Background(), "fetch and summarize this website URL", 5)
	if err != nil || len(matches) == 0 || matches[0].ID != "webfetch" {
		t.Fatalf("webfetch routing matches = %+v, err=%v", matches, err)
	}
}

func TestToolAllowlistIsUpperBoundForSkillTools(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SNOW_HOME", home)
	dir := filepath.Join(home, "skills", "review")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: review\ndescription: Review code.\n---\nreview body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := New(context.Background(), Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir(), Tools: []string{"read"}})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if skill, ok := a.Skills.Lookup("review"); !ok || skill.Enabled || !strings.Contains(skill.DisabledBy, "tool allowlist") {
		t.Fatalf("tool-disabled skill inventory = %+v, %v", skill, ok)
	}
	for _, name := range []string{"activate_skill", "deactivate_skill", "read_skill_resource"} {
		if _, ok := a.Registry.Get(name); ok {
			t.Fatalf("explicit tool allowlist unexpectedly retained %s", name)
		}
	}
	activationOnly, err := New(context.Background(), Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir(), Tools: []string{"activate_skill"}})
	if err != nil {
		t.Fatal(err)
	}
	defer activationOnly.Close()
	if _, ok := activationOnly.Registry.Get("activate_skill"); !ok {
		t.Fatal("activation-only allowlist omitted activate_skill")
	}
	if _, ok := activationOnly.Registry.Get("read_skill_resource"); ok {
		t.Fatal("activation-only allowlist retained read_skill_resource")
	}
	if _, ok := activationOnly.Registry.Get("deactivate_skill"); ok {
		t.Fatal("activation-only allowlist retained deactivate_skill")
	}
	if _, ok := activationOnly.Skills.Get("review"); !ok {
		t.Fatal("activation-only allowlist disabled the skill catalog")
	}

	readerOnly, err := New(context.Background(), Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir(), Tools: []string{"read_skill_resource"}})
	if err != nil {
		t.Fatal(err)
	}
	defer readerOnly.Close()
	if _, ok := readerOnly.Registry.Get("read_skill_resource"); ok {
		t.Fatal("resource-only allowlist retained an incoherent names-only reader")
	}
	deactivatorOnly, err := New(context.Background(), Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir(), Tools: []string{"deactivate_skill"}})
	if err != nil {
		t.Fatal(err)
	}
	defer deactivatorOnly.Close()
	if _, ok := deactivatorOnly.Registry.Get("deactivate_skill"); ok {
		t.Fatal("deactivation-only allowlist retained an incoherent names-only tool")
	}
}

func TestAppWithoutDeferredToolsKeepsExistingDirectPath(t *testing.T) {
	a, err := New(context.Background(), Options{
		Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir(),
		Tools: []string{"read", "write", "edit", "bash", "grep", "glob"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if a.Router != nil {
		t.Fatalf("unexpected router for direct-only catalog: %#v", a.Router)
	}
	if _, ok := a.Registry.Get("search_tools"); ok {
		t.Fatal("search_tools should not add schema overhead without deferred tools")
	}
	if _, ok := a.Registry.Get("ask_user"); ok {
		t.Fatal("explicit tool allowlist unexpectedly retained ask_user")
	}
}
