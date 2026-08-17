package config

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	publicmcp "github.com/snow-core/snow/pkg/mcp"
	publicplugin "github.com/snow-core/snow/pkg/plugin"
)

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultProvider != "opencode-go" {
		t.Fatalf("default provider = %q", cfg.DefaultProvider)
	}
	if cfg.PermissionMode != "ask" {
		t.Fatalf("default permission = %q", cfg.PermissionMode)
	}
	if cfg.ReasoningSummary != "auto" || cfg.TextVerbosity != "low" {
		t.Fatalf("response defaults = summary:%q verbosity:%q", cfg.ReasoningSummary, cfg.TextVerbosity)
	}
	if !cfg.TUI.Mouse {
		t.Fatal("default TUI did not keep wheel scrolling inside Snow")
	}
	if _, ok := cfg.Providers["openai-compatible"]; !ok {
		t.Fatalf("openai-compatible provider missing from defaults: %+v", cfg.Providers)
	}
}

func TestSandboxDefaultsAndValidation(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Sandbox.Executable != "smolvm" || cfg.Sandbox.DefaultImage != DefaultUbuntuImage || cfg.Sandbox.CPUs != 2 || cfg.Sandbox.MemoryMiB != 2048 || cfg.Sandbox.GuestCWD != "/workspace" {
		t.Fatalf("sandbox defaults = %+v", cfg.Sandbox)
	}
	if strings.Join(cfg.Sandbox.EnvAllowlist, ",") != "LANG,LC_ALL,TERM" {
		t.Fatalf("sandbox environment defaults = %#v", cfg.Sandbox.EnvAllowlist)
	}

	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"sandbox":{"executable":"/opt/smolvm","default_image":"dev@sha256:abc","cpus":4,"memory_mib":4096,"storage_gib":40,"overlay_gib":20,"guest_cwd":"/work","env_allowlist":["LANG"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Sandbox.Executable != "/opt/smolvm" || loaded.Sandbox.DefaultImage != "dev@sha256:abc" || loaded.Sandbox.StorageGiB != 40 || loaded.Sandbox.OverlayGiB != 20 || loaded.Sandbox.GuestCWD != "/work" {
		t.Fatalf("loaded sandbox = %+v", loaded.Sandbox)
	}
	if err := os.WriteFile(path, []byte(`{"sandbox":{"cpus":0,"memory_mib":64}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "memory_mib") {
		t.Fatalf("invalid sandbox memory accepted: %v", err)
	}
	for _, body := range []string{
		`{"sandbox":{"guest_cwd":"relative"}}`,
		`{"sandbox":{"storage_gib":-1}}`,
		`{"sandbox":{"overlay_gib":1048577}}`,
		`{"sandbox":{"env_allowlist":["PATH","bad-name"]}}`,
		`{"sandbox":{"env_allowlist":["TERM","TERM"]}}`,
	} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Fatalf("invalid sandbox config accepted: %s", body)
		}
	}
}

func TestMouseAppModeSurvivesSaveAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := Default()
	cfg.TUI.Mouse = true
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.TUI.Mouse {
		t.Fatal("saved application mouse mode reverted to the native default")
	}
}

func TestSectionUpdatesPreserveUnknownFieldsAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"future":{"kept":true},"thinking":"high"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UpdateMCPServers(path, true, func(servers map[string]publicmcp.ServerSpec) error {
		servers["demo"] = publicmcp.ServerSpec{Command: "demo-mcp"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := UpdateSkills(path, func(skills *SkillsConfig) error {
		skills.Overrides["review"] = false
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["future"]; !ok || string(raw["thinking"]) != `"high"` {
		t.Fatalf("unknown fields were not preserved: %s", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MCPServers["demo"].Command != "demo-mcp" || cfg.Skills.Overrides["review"] {
		t.Fatalf("updated config = %+v", cfg)
	}
}

func TestPluginManagementPreservesUnknownFieldsAndStagesDisabled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	original := `{"future":{"kept":true},"plugins":[{"id":"demo","command":["python3","plugin.py"],"enabled":true,"env":["TOKEN=secret"],"future_plugin":{"kept":true}}]}`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetPluginEnabled(path, true, "demo", false); err != nil {
		t.Fatal(err)
	}
	assertPluginUnknownFields(t, path)
	declarations, err := LoadPluginDeclarations(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(declarations) != 1 || declarations[0].Enabled {
		t.Fatalf("declarations after disable = %+v", declarations)
	}
	if err := AddPlugin(path, true, publicplugin.PluginSpec{ID: "demo", Command: []string{"node", "plugin.mjs"}, Enabled: false}, true); err != nil {
		t.Fatal(err)
	}
	assertPluginUnknownFields(t, path)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	declarations, err = LoadPluginDeclarations(path)
	if err != nil || len(declarations) != 1 || strings.Join(declarations[0].Command, " ") != "node plugin.mjs" || len(declarations[0].Env) != 0 || strings.Contains(string(data), "TOKEN=secret") {
		t.Fatalf("replacement did not clear old known fields: declarations=%+v err=%v data=%s", declarations, err, data)
	}
	if err := RemovePlugin(path, true, "demo"); err != nil {
		t.Fatal(err)
	}
	declarations, err = LoadPluginDeclarations(path)
	if err != nil || len(declarations) != 0 {
		t.Fatalf("declarations after remove = %+v, %v", declarations, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("global plugin config mode = %o", info.Mode().Perm())
	}
}

func assertPluginUnknownFields(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	if _, ok := root["future"]; !ok {
		t.Fatalf("unknown top-level field lost: %s", data)
	}
	var plugins []map[string]json.RawMessage
	if err := json.Unmarshal(root["plugins"], &plugins); err != nil {
		t.Fatal(err)
	}
	if len(plugins) != 1 || plugins[0]["future_plugin"] == nil {
		t.Fatalf("unknown plugin field lost: %s", data)
	}
}

func TestManagementRejectsNullConfigRootWithoutPanic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("null\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := AddPlugin(path, true, publicplugin.PluginSpec{ID: "demo", Command: []string{"demo"}}, false)
	if err == nil || !strings.Contains(err.Error(), "root must be a JSON object") {
		t.Fatalf("null-root error = %v", err)
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil || string(data) != "null\n" {
		t.Fatalf("null-root mutation changed file: %q, %v", data, readErr)
	}
}

func TestPluginManagementRejectsDuplicatesWithoutMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	original := []byte(`{"plugins":[{"id":"same","command":["one"]},{"id":"same","command":["two"]}]}`)
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetPluginEnabled(path, true, "same", true); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate update error = %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(original) {
		t.Fatalf("failed update changed file:\n%s", after)
	}
}

func TestProjectPluginMutationRetainsExistingModeAndRequiresDeclaration(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".snow", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"plugins":[]}`), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := SetPluginEnabled(path, false, "demo", false); err == nil || !strings.Contains(err.Error(), "target scope") {
		t.Fatalf("missing project declaration error = %v", err)
	}
	if err := AddPlugin(path, false, publicplugin.PluginSpec{ID: "demo", Command: []string{"demo"}, Enabled: true}, false); err != nil {
		t.Fatal(err)
	}
	if err := SetPluginEnabled(path, false, "demo", false); err != nil {
		t.Fatal(err)
	}
	declarations, err := LoadPluginDeclarations(path)
	if err != nil || len(declarations) != 1 || declarations[0].Enabled {
		t.Fatalf("project declaration = %+v, %v", declarations, err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("project mode = %o", info.Mode().Perm())
	}
}

func TestProjectSkillUpdateUsesTriStatePolicy(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".snow", "config.json")
	if err := UpdateProjectSkills(path, func(skills *ProjectSkillsConfig) error {
		disabled := true
		skills.Disabled = &disabled
		skills.Overrides["review"] = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	extensions, err := LoadProjectExtensions(path)
	if err != nil {
		t.Fatal(err)
	}
	if extensions.Skills.Disabled == nil || !*extensions.Skills.Disabled || !extensions.Skills.Overrides["review"] {
		t.Fatalf("project skills = %+v", extensions.Skills)
	}
}

func TestSubagentDefaultsAndValidation(t *testing.T) {
	cfg := Default()
	if cfg.Subagents.Enabled || cfg.Subagents.MaxConcurrentThreads != 4 || cfg.Subagents.MaxDepth != 1 || cfg.Subagents.AllowMutation || !cfg.Subagents.Durable {
		t.Fatalf("defaults=%+v", cfg.Subagents)
	}
	if err := cfg.Subagents.ValidateSubagents(); err != nil {
		t.Fatal(err)
	}
	if cfg.Subagents.DefaultRole != "general" || cfg.Subagents.DefaultProvider != "" || cfg.Subagents.DefaultModel != "" {
		t.Fatalf("subagent defaults: role=%q selection=%s/%s", cfg.Subagents.DefaultRole, cfg.Subagents.DefaultProvider, cfg.Subagents.DefaultModel)
	}
	if !hasTool(cfg.Subagents.Roles["general"].Tools, "bash") {
		t.Fatal("general role is not shell-capable")
	}
	if hasTool(cfg.Subagents.Roles["explorer"].Tools, "bash") {
		t.Fatal("explorer role unexpectedly exposes bash")
	}
	bad := cfg.Subagents
	bad.MaxConcurrentThreads = 0
	if err := bad.ValidateSubagents(); err == nil {
		t.Fatal("accepted zero concurrency")
	}
	bad = cfg.Subagents
	bad.MaxConcurrentThreads = MaxConcurrentSubagents + 1
	bad.MaxAgentsPerSession = bad.MaxConcurrentThreads
	if err := bad.ValidateSubagents(); err == nil {
		t.Fatal("accepted excessive concurrency")
	}
	bad = cfg.Subagents
	bad.MaxDepth = 9
	if err := bad.ValidateSubagents(); err == nil {
		t.Fatal("accepted excessive depth")
	}
	bad = cfg.Subagents
	bad.MaxAgentsPerSession = bad.MaxConcurrentThreads - 1
	if err := bad.ValidateSubagents(); err == nil {
		t.Fatal("accepted agent limit below child concurrency")
	}
	bad = cfg.Subagents
	bad.MaxResultBytes = 1
	if err := bad.ValidateSubagents(); err == nil {
		t.Fatal("accepted unsafe result cap")
	}
	bad = cfg.Subagents
	bad.DefaultProvider = "   "
	if err := bad.ValidateSubagents(); err == nil {
		t.Fatal("accepted blank default provider")
	}
	bad = cfg.Subagents
	bad.DefaultModel = "   "
	if err := bad.ValidateSubagents(); err == nil {
		t.Fatal("accepted blank default model")
	}
	bad = cfg.Subagents
	bad.DefaultRole = "default"
	bad.Roles = map[string]AgentRole{"default": {}}
	if err := bad.ValidateSubagents(); err == nil || !strings.Contains(err.Error(), `use "general"`) {
		t.Fatalf("renamed default role error = %v", err)
	}
	bad = cfg.Subagents
	bad.DefaultRole = "worker"
	bad.Roles = map[string]AgentRole{"worker": {}}
	if err := bad.ValidateSubagents(); err == nil || !strings.Contains(err.Error(), `use "implementer"`) {
		t.Fatalf("renamed worker role error = %v", err)
	}
}

func TestSubagentBashCapabilityIsIndependentFromMutation(t *testing.T) {
	cfg := Default().Subagents
	cfg.Roles = map[string]AgentRole{
		"general": {Tools: []string{"read", "bash"}},
		"shell":   {Tools: []string{"bash"}},
	}
	if err := cfg.ValidateSubagents(); err != nil {
		t.Fatalf("bash-only role rejected: %v", err)
	}
	for _, mutationTool := range []string{"write", "edit"} {
		cfg.Roles["shell"] = AgentRole{Tools: []string{"bash", mutationTool}}
		if err := cfg.ValidateSubagents(); err == nil {
			t.Fatalf("%s accepted without role mutation opt-in", mutationTool)
		}
	}
}

func hasTool(tools []string, want string) bool {
	for _, name := range tools {
		if name == want {
			return true
		}
	}
	return false
}

func TestLoadOlderConfigGetsResponseDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"default_provider":"fake","thinking":"off"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ReasoningSummary != "auto" || cfg.TextVerbosity != "low" {
		t.Fatalf("older config defaults = summary:%q verbosity:%q", cfg.ReasoningSummary, cfg.TextVerbosity)
	}
}

func TestLoadAndSaveRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := Default()
	cfg.DefaultProvider = "fake"
	cfg.DefaultModel = "m2"
	cfg.PermissionMode = "deny"
	cfg.SystemPromptFile = "system.md"
	cfg.Providers = map[string]ProviderConfig{
		"opencode-go": {BaseURL: "https://example.com/v1", DefaultModel: "kimi-k2.6", StreamIdleTimeoutMS: 123000},
	}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.DefaultProvider != "fake" || got.DefaultModel != "m2" || got.PermissionMode != "deny" || got.SystemPromptFile != "system.md" {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	pc, ok := got.Providers["opencode-go"]
	if !ok || pc.BaseURL != "https://example.com/v1" || pc.DefaultModel != "kimi-k2.6" || pc.StreamIdleTimeoutMS != 123000 {
		t.Fatalf("provider config mismatch: %+v", got.Providers)
	}
}

func TestLoadCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestTUIThemeValidation(t *testing.T) {
	for _, theme := range []string{"", "default", "dark", "light", "high-contrast"} {
		if err := ValidateTUITheme(theme); err != nil {
			t.Fatalf("theme %q rejected: %v", theme, err)
		}
	}
	if err := ValidateTUITheme("solarized"); err != nil {
		t.Fatalf("custom theme name rejected: %v", err)
	}
	if err := ValidateTUITheme("../escape"); err == nil {
		t.Fatal("unsafe theme name accepted")
	}
}

func TestValidateProviderProfileID(t *testing.T) {
	for _, valid := range []string{"x-provider", "local_2", "team.gateway"} {
		if err := ValidateProviderProfileID(valid); err != nil {
			t.Fatalf("valid %q: %v", valid, err)
		}
	}
	for _, invalid := range []string{"", "X Provider", "-leading", "chatgpt"} {
		if err := ValidateProviderProfileID(invalid); err == nil {
			t.Fatalf("invalid profile %q accepted", invalid)
		}
	}
}

func TestDefaults(t *testing.T) {
	cfg := Default()
	if cfg.ToolOutputLimit() != DefaultToolOutputBytes {
		t.Fatal("wrong tool output limit")
	}
	if cfg.BashTimeout() != DefaultBashTimeout {
		t.Fatal("wrong bash timeout")
	}
}

func TestOverridesFromEnv(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	dir := GlobalDir()
	if dir == "" {
		t.Fatal("expected global dir")
	}
}

func TestLoadMCPAndSkillsAndTrustedProjectExtensions(t *testing.T) {
	global := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(global, []byte(`{"mcp_servers":{"remote":{"url":"https://example.test/mcp"}},"skills":{"dirs":["/opt/skills"],"include_claude":true}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(global)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MCPServers["remote"].URL == "" || len(cfg.Skills.Dirs) != 1 || !cfg.Skills.IncludeClaude {
		t.Fatalf("config = %+v", cfg)
	}
	project := filepath.Join(t.TempDir(), "project.json")
	if err := os.WriteFile(project, []byte(`{"mcp_servers":{"local":{"command":"mcp-local"}},"system_prompt_file":".snow/system.md"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	merged, err := LoadWithProject(global, project, true)
	if err != nil {
		t.Fatal(err)
	}
	if merged.MCPServers["local"].Command != "mcp-local" || merged.MCPServers["remote"].URL == "" {
		t.Fatalf("merged = %+v", merged.MCPServers)
	}
	projectExtensions, err := LoadProjectExtensions(project)
	if err != nil {
		t.Fatal(err)
	}
	if projectExtensions.SystemPromptFile == nil || *projectExtensions.SystemPromptFile != ".snow/system.md" {
		t.Fatalf("project system prompt = %#v", projectExtensions.SystemPromptFile)
	}
	blocked, err := LoadWithProject(global, project, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := blocked.MCPServers["local"]; ok {
		t.Fatal("untrusted project MCP server loaded")
	}
}

func TestSystemPromptFileValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"system_prompt_file":"   "}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("blank global system prompt path was accepted")
	}

	project := filepath.Join(t.TempDir(), "project.json")
	if err := os.WriteFile(project, []byte(`{"system_prompt_file":""}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProjectExtensions(project); err == nil {
		t.Fatal("empty project system prompt path was accepted")
	}
}

func TestProjectSelectionsAreIndependentAndOverlayDefaults(t *testing.T) {
	projectA := filepath.Join(t.TempDir(), "project-a")
	projectB := filepath.Join(t.TempDir(), "project-b")
	cfg := Default()
	cfg.DefaultProvider = "opencode-go"
	cfg.DefaultModel = "global-model"
	cfg.Thinking = "off"

	var err error
	cfg, err = WithProjectSelection(cfg, projectA, ProjectSelection{Provider: "fake", Model: "model-a", Thinking: "high"})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = WithProjectSelection(cfg, projectB, ProjectSelection{Provider: "openai-compatible", Model: "model-b", Thinking: "low"})
	if err != nil {
		t.Fatal(err)
	}

	effectiveA := cfg
	if found, err := ApplyProjectSelection(&effectiveA, projectA); err != nil || !found {
		t.Fatalf("apply project A: found=%v err=%v", found, err)
	}
	if effectiveA.DefaultProvider != "fake" || effectiveA.DefaultModel != "model-a" || effectiveA.Thinking != "high" {
		t.Fatalf("project A selection = %s/%s thinking:%s", effectiveA.DefaultProvider, effectiveA.DefaultModel, effectiveA.Thinking)
	}

	effectiveB := cfg
	if found, err := ApplyProjectSelection(&effectiveB, projectB); err != nil || !found {
		t.Fatalf("apply project B: found=%v err=%v", found, err)
	}
	if effectiveB.DefaultProvider != "openai-compatible" || effectiveB.DefaultModel != "model-b" || effectiveB.Thinking != "low" {
		t.Fatalf("project B selection = %s/%s thinking:%s", effectiveB.DefaultProvider, effectiveB.DefaultModel, effectiveB.Thinking)
	}
	if cfg.DefaultProvider != "opencode-go" || cfg.DefaultModel != "global-model" || cfg.Thinking != "off" {
		t.Fatalf("global defaults changed = %s/%s thinking:%s", cfg.DefaultProvider, cfg.DefaultModel, cfg.Thinking)
	}

	path := filepath.Join(t.TempDir(), "config.json")
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.ProjectSelections) != 2 {
		t.Fatalf("reloaded project selections = %+v", reloaded.ProjectSelections)
	}
}

func TestUpdateMergesConcurrentProjectAndGlobalChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Save(path, Default()); err != nil {
		t.Fatal(err)
	}
	const projects = 12
	projectRoot := t.TempDir()
	var wg sync.WaitGroup
	errs := make(chan error, projects+3)
	for i := range projects {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cwd := filepath.Join(projectRoot, fmt.Sprintf("project-%d", i))
			_, err := SaveProjectSelection(path, cwd, ProjectSelection{Provider: "fake", Model: fmt.Sprintf("model-%d", i), Thinking: "off"})
			errs <- err
		}()
	}
	mutations := []func(*Config){
		func(cfg *Config) { cfg.PermissionMode = "deny" },
		func(cfg *Config) { cfg.ReasoningSummary = "detailed" },
		func(cfg *Config) { cfg.TUI.Theme = "dark" },
	}
	for _, mutation := range mutations {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := Update(path, func(cfg *Config) error {
				mutation(cfg)
				return nil
			})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.ProjectSelections) != projects {
		t.Fatalf("project selections = %d, want %d: %+v", len(cfg.ProjectSelections), projects, cfg.ProjectSelections)
	}
	if cfg.PermissionMode != "deny" || cfg.ReasoningSummary != "detailed" || cfg.TUI.Theme != "dark" {
		t.Fatalf("concurrent global settings were lost: permission=%q summary=%q theme=%q", cfg.PermissionMode, cfg.ReasoningSummary, cfg.TUI.Theme)
	}
}

func TestUpdateHelperProcess(t *testing.T) {
	if os.Getenv("SNOW_CONFIG_UPDATE_HELPER") != "1" {
		return
	}
	path := os.Getenv("SNOW_CONFIG_UPDATE_PATH")
	kind := os.Getenv("SNOW_CONFIG_UPDATE_KIND")
	var err error
	switch kind {
	case "project":
		_, err = SaveProjectSelection(path, os.Getenv("SNOW_CONFIG_UPDATE_PROJECT"), ProjectSelection{Provider: "fake", Model: os.Getenv("SNOW_CONFIG_UPDATE_MODEL"), Thinking: "off"})
	case "theme":
		_, err = Update(path, func(cfg *Config) error {
			cfg.TUI.Theme = "dark"
			return nil
		})
	case "summary":
		_, err = Update(path, func(cfg *Config) error {
			cfg.ReasoningSummary = "detailed"
			return nil
		})
	default:
		err = fmt.Errorf("unknown helper kind %q", kind)
	}
	if err != nil {
		t.Fatal(err)
	}
}

func TestUpdateSerializesMixedWritersAcrossProcesses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Save(path, Default()); err != nil {
		t.Fatal(err)
	}
	projectRoot := t.TempDir()
	type helper struct {
		kind    string
		project string
		model   string
	}
	helpers := []helper{
		{kind: "project", project: filepath.Join(projectRoot, "a"), model: "model-a"},
		{kind: "project", project: filepath.Join(projectRoot, "b"), model: "model-b"},
		{kind: "project", project: filepath.Join(projectRoot, "c"), model: "model-c"},
		{kind: "theme"},
		{kind: "summary"},
	}
	commands := make([]*exec.Cmd, 0, len(helpers))
	for _, helper := range helpers {
		cmd := exec.Command(os.Args[0], "-test.run=^TestUpdateHelperProcess$")
		cmd.Env = append(os.Environ(),
			"SNOW_CONFIG_UPDATE_HELPER=1",
			"SNOW_CONFIG_UPDATE_PATH="+path,
			"SNOW_CONFIG_UPDATE_KIND="+helper.kind,
			"SNOW_CONFIG_UPDATE_PROJECT="+helper.project,
			"SNOW_CONFIG_UPDATE_MODEL="+helper.model,
		)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		commands = append(commands, cmd)
	}
	for _, cmd := range commands {
		if err := cmd.Wait(); err != nil {
			t.Fatalf("helper failed: %v", err)
		}
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.ProjectSelections) != 3 || cfg.TUI.Theme != "dark" || cfg.ReasoningSummary != "detailed" {
		t.Fatalf("cross-process updates were lost: projects=%+v theme=%q summary=%q", cfg.ProjectSelections, cfg.TUI.Theme, cfg.ReasoningSummary)
	}
}

func TestLoadRejectsInvalidProjectThinking(t *testing.T) {
	project := filepath.Join(t.TempDir(), "project")
	path := filepath.Join(t.TempDir(), "config.json")
	data, err := json.Marshal(map[string]any{
		"project_selections": map[string]any{
			project: map[string]any{"provider": "fake", "model": "fake-1", "thinking": "extreme"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "invalid thinking level") {
		t.Fatalf("invalid project thinking error = %v", err)
	}
}
