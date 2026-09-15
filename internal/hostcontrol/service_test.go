package hostcontrol

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func hostFixture(t *testing.T) (*Service, string, string) {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", dir)
	t.Setenv("SNOW_HOME", filepath.Join(dir, ".snow"))
	t.Setenv("OPENCODE_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	service, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	return service, dir, filepath.Join(dir, ".snow", "config.json")
}
func writeHostFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}
func hostGet(t *testing.T, s *Service, scope, cwd string) protocol.HostDefaultsResponse {
	t.Helper()
	got, err := s.GetDefaults(t.Context(), protocol.HostDefaultsRequest{Scope: scope, CWD: cwd})
	if err != nil {
		t.Fatal(err)
	}
	return got
}
func stringSet(value string) *protocol.HostStringOperation {
	return &protocol.HostStringOperation{Op: "set", Value: new(value)}
}

func TestHostReadNoFilesystemWrites(t *testing.T) {
	service, dir, _ := hostFixture(t)
	got := hostGet(t, service, "global", "")
	if got.AppliesTo != "future_runtime" || got.Global.ProviderModel.Effective.Provider != "opencode-zen" || got.Global.Thinking.Effective != "off" || got.Global.Thinking.Source != "builtin" || got.Global.Thinking.Explicit != nil {
		t.Fatalf("unexpected defaults: %+v", got)
	}
	statuses, err := service.ProviderStatus(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses.Providers) != 4 {
		t.Fatal("provider list")
	}
	for _, status := range statuses.Providers {
		if !status.CheckedLocally {
			t.Fatal("not local")
		}
		if status.ProviderID == "opencode-zen" && (status.State != "configured" || status.Reason != "anonymous_access") {
			t.Fatal("missing anonymous Zen")
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".snow")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("GET created config directory")
	}
}

func TestHostScopesResetUnknownPreservationAndCAS(t *testing.T) {
	service, dir, path := hostFixture(t)
	project := filepath.Join(dir, "project")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	writeHostFile(t, filepath.Join(project, ".snow", "config.json"), `{"thinking":"ultra","default_provider":"project-secret-canary"}`)
	writeHostFile(t, path, `{"unrelated":{"secret":"config-canary"},"providers":{"local":{"type":"openai-compatible","base_url":"https://endpoint-canary.invalid","headers":{"Authorization":"header-canary"},"unknown":7}},"project_selections":{`+mustJSON(t, project)+`:{"unknown":{"nested":"preserve"}}}}`)
	global := hostGet(t, service, "global", "")
	response, err := service.UpdateDefaults(t.Context(), protocol.HostDefaultsUpdateRequest{Scope: "global", Revision: global.Revision, Global: &protocol.HostGlobalDefaultsPatch{ProviderModel: &protocol.HostProviderModelOperation{Op: "set", Value: &protocol.HostProviderModel{Provider: "local", Model: "local-model"}}, Thinking: stringSet("high"), ReasoningSummary: stringSet("detailed"), TextVerbosity: stringSet("high")}})
	if err != nil {
		t.Fatal(err)
	}
	if response.Global.ProviderModel.Source != "global" || response.Global.Thinking.Effective != "high" {
		t.Fatal("global mutation")
	}
	if _, err = service.UpdateDefaults(t.Context(), protocol.HostDefaultsUpdateRequest{Scope: "global", Revision: global.Revision, Global: &protocol.HostGlobalDefaultsPatch{Thinking: stringSet("low")}}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatal("stale CAS accepted")
	}
	globalRevisionBeforeProject := response.Revision
	selected := hostGet(t, service, "project", project)
	if selected.Project.Thinking.Source != "global" || selected.Project.Thinking.Effective != "high" || selected.Project.Thinking.Explicit != nil {
		t.Fatal("project file affected operator selection")
	}
	response, err = service.UpdateDefaults(t.Context(), protocol.HostDefaultsUpdateRequest{Scope: "project", CWD: project, Revision: selected.Revision, Project: &protocol.HostProjectDefaultsPatch{Thinking: stringSet("medium")}})
	if err != nil {
		t.Fatal(err)
	}
	if response.Project.Thinking.Source != "project" || response.Project.Thinking.Effective != "medium" {
		t.Fatal("project override")
	}
	if hostGet(t, service, "global", "").Revision != globalRevisionBeforeProject {
		t.Fatal("project changed global scope revision")
	}
	response, err = service.UpdateDefaults(t.Context(), protocol.HostDefaultsUpdateRequest{Scope: "project", CWD: project, Revision: response.Revision, Project: &protocol.HostProjectDefaultsPatch{Thinking: &protocol.HostStringOperation{Op: "reset"}}})
	if err != nil {
		t.Fatal(err)
	}
	if response.Project.Thinking.Source != "global" || response.Project.Thinking.Explicit != nil {
		t.Fatal("reset did not inherit")
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"reasoning_summary", "text_verbosity", "canary", "unknown", "headers"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("project DTO exposed %s", forbidden)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, preserved := range []string{"config-canary", "endpoint-canary", "header-canary", "preserve"} {
		if !strings.Contains(string(data), preserved) {
			t.Fatalf("lost unrelated %s", preserved)
		}
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("mode not 0600")
	}
}
func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestHostScopedRevisionDoesNotHashSecrets(t *testing.T) {
	service, _, path := hostFixture(t)
	writeHostFile(t, path, `{"thinking":"high","secret":"first-canary"}`)
	first := hostGet(t, service, "global", "")
	writeHostFile(t, path, `{"thinking":"high","secret":"second-canary"}`)
	second := hostGet(t, service, "global", "")
	if first.Revision != second.Revision {
		t.Fatal("revision depends on secret/unrelated fields")
	}
	changed, err := service.UpdateDefaults(t.Context(), protocol.HostDefaultsUpdateRequest{Scope: "global", Revision: first.Revision, Global: &protocol.HostGlobalDefaultsPatch{Thinking: stringSet("high")}})
	if err != nil {
		t.Fatal(err)
	}
	if changed.Revision == first.Revision {
		t.Fatal("same-value mutation not fenced")
	}
}

func TestHostInvalidAndUnavailableAreFixedErrors(t *testing.T) {
	service, dir, path := hostFixture(t)
	current := hostGet(t, service, "global", "")
	tests := []protocol.HostDefaultsUpdateRequest{
		{Scope: "global", Revision: current.Revision, Global: &protocol.HostGlobalDefaultsPatch{Thinking: stringSet("not-an-enum-canary")}},
		{Scope: "global", Revision: current.Revision, Global: &protocol.HostGlobalDefaultsPatch{ProviderModel: &protocol.HostProviderModelOperation{Op: "set", Value: &protocol.HostProviderModel{Provider: "missing", Model: "x"}}}},
		{Scope: "global", Revision: current.Revision, Global: &protocol.HostGlobalDefaultsPatch{Thinking: &protocol.HostStringOperation{Op: "reset", Value: new("high")}}},
		{Scope: "project", CWD: dir, Revision: current.Revision, Global: &protocol.HostGlobalDefaultsPatch{}},
		{Scope: "global", CWD: dir, Revision: current.Revision, Global: &protocol.HostGlobalDefaultsPatch{}},
	}
	for _, request := range tests {
		if _, err := service.UpdateDefaults(t.Context(), request); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("invalid request error: %v", err)
		}
	}
	for _, data := range []string{`{"secret":"parse-canary",`, "null", strings.Repeat(" ", 4<<20+1), `{"thinking":"high","thinking":"low"}`} {
		writeHostFile(t, path, data)
		if _, err := service.GetDefaults(t.Context(), protocol.HostDefaultsRequest{Scope: "global"}); !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "canary") {
			t.Fatalf("unavailable error: %v", err)
		}
	}
}

func TestHostParallelCASOnlyOneWins(t *testing.T) {
	service, _, _ := hostFixture(t)
	current := hostGet(t, service, "global", "")
	var wg sync.WaitGroup
	results := make(chan error, 12)
	for range 12 {
		wg.Go(func() {
			_, err := service.UpdateDefaults(t.Context(), protocol.HostDefaultsUpdateRequest{Scope: "global", Revision: current.Revision, Global: &protocol.HostGlobalDefaultsPatch{Thinking: stringSet("high")}})
			results <- err
		})
	}
	wg.Wait()
	close(results)
	success, conflicts := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrRevisionConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 11 {
		t.Fatalf("success %d conflicts %d", success, conflicts)
	}
}

func TestHostCancellationAndSymlinkRejection(t *testing.T) {
	service, dir, path := hostFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := service.GetDefaults(ctx, protocol.HostDefaultsRequest{Scope: "global"}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := service.ProviderStatus(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	current := hostGet(t, service, "global", "")
	if _, err := service.UpdateDefaults(ctx, protocol.HostDefaultsUpdateRequest{Scope: "global", Revision: current.Revision, Global: &protocol.HostGlobalDefaultsPatch{}}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	outside := filepath.Join(dir, "outside")
	writeHostFile(t, outside, `{"thinking":"low"}`)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetDefaults(t.Context(), protocol.HostDefaultsRequest{Scope: "global"}); !errors.Is(err, ErrUnavailable) {
		t.Fatal("followed config symlink")
	}
	if _, err := service.UpdateDefaults(t.Context(), protocol.HostDefaultsUpdateRequest{Scope: "global", Revision: current.Revision, Global: &protocol.HostGlobalDefaultsPatch{Thinking: stringSet("high")}}); !errors.Is(err, ErrUnavailable) {
		t.Fatal("wrote config symlink")
	}
	data, err := os.ReadFile(outside)
	if err != nil || string(data) != `{"thinking":"low"}` {
		t.Fatal("outside changed")
	}
}

func TestHostProjectCanonicalScopeAndInheritanceRevisions(t *testing.T) {
	service, dir, path := hostFixture(t)
	project := filepath.Join(dir, "project")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "project-alias")
	if err := os.Symlink(project, alias); err != nil {
		t.Fatal(err)
	}
	writeHostFile(t, path, `{"thinking":"high","text_verbosity":"high"}`)
	original := hostGet(t, service, "project", project)
	aliased := hostGet(t, service, "project", alias)
	if original.Revision != aliased.Revision || aliased.CWD != project {
		t.Fatal("project scope not canonical")
	}
	global := hostGet(t, service, "global", "")
	next, err := service.UpdateDefaults(t.Context(), protocol.HostDefaultsUpdateRequest{Scope: "global", Revision: global.Revision, Global: &protocol.HostGlobalDefaultsPatch{TextVerbosity: stringSet("low")}})
	if err != nil {
		t.Fatal(err)
	}
	if hostGet(t, service, "project", project).Revision != original.Revision {
		t.Fatal("unrelated global field changed project revision")
	}
	_, err = service.UpdateDefaults(t.Context(), protocol.HostDefaultsUpdateRequest{Scope: "global", Revision: next.Revision, Global: &protocol.HostGlobalDefaultsPatch{Thinking: stringSet("low")}})
	if err != nil {
		t.Fatal(err)
	}
	if hostGet(t, service, "project", project).Revision == original.Revision {
		t.Fatal("inherited thinking changed without project CAS invalidation")
	}
	_, err = service.UpdateDefaults(t.Context(), protocol.HostDefaultsUpdateRequest{Scope: "project", CWD: project, Revision: original.Revision, Project: &protocol.HostProjectDefaultsPatch{Thinking: stringSet("medium")}})
	if !errors.Is(err, ErrRevisionConflict) {
		t.Fatal("stale inherited defaults accepted")
	}
}

func TestHostGlobalResetRemovesExplicitAllowlistOnly(t *testing.T) {
	service, _, path := hostFixture(t)
	writeHostFile(t, path, `{"default_provider":"chatgpt","default_model":"model-one","thinking":"high","reasoning_summary":"detailed","text_verbosity":"high","permission":"deny","unknown":"preserve-canary"}`)
	current := hostGet(t, service, "global", "")
	reset := &protocol.HostStringOperation{Op: "reset"}
	next, err := service.UpdateDefaults(t.Context(), protocol.HostDefaultsUpdateRequest{Scope: "global", Revision: current.Revision, Global: &protocol.HostGlobalDefaultsPatch{ProviderModel: &protocol.HostProviderModelOperation{Op: "reset"}, Thinking: reset, ReasoningSummary: reset, TextVerbosity: reset}})
	if err != nil {
		t.Fatal(err)
	}
	if next.Global.ProviderModel.Explicit != nil || next.Global.ProviderModel.Source != "builtin" || next.Global.ProviderModel.Effective.Provider != "opencode-zen" || next.Global.Thinking.Explicit != nil || next.Global.Thinking.Effective != "off" || next.Global.ReasoningSummary.Effective != "auto" || next.Global.TextVerbosity.Effective != "low" {
		t.Fatal("global reset did not restore inheritance")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if json.Unmarshal(data, &document) != nil {
		t.Fatal("invalid persisted document")
	}
	for _, key := range []string{"default_provider", "default_model", "thinking", "reasoning_summary", "text_verbosity"} {
		if _, exists := document[key]; exists {
			t.Fatalf("reset retained %s", key)
		}
	}
	if document["permission"] != "deny" || document["unknown"] != "preserve-canary" {
		t.Fatal("reset changed unrelated settings")
	}
}
