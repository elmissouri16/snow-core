//go:build darwin || linux

package web

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func restartRecoveryManager(t *testing.T, registry *Registry, log string) *RuntimeManager {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	manager := NewRuntimeManager(t.Context(), executable, filepath.Join(filepath.Dir(log), "sessions"), registry)
	manager.env = append(manager.env, "SNOW_WEB_RUNTIME_TEST_CHILD=1", "SNOW_WEB_RUNTIME_TEST_MODE=", "SNOW_WEB_RUNTIME_TEST_LOG="+log)
	t.Cleanup(func() { _ = manager.Close() })
	return manager
}

func restartRecoveryAssertCold(t *testing.T, manager *RuntimeManager, projects []Project, log string) {
	t.Helper()
	for _, project := range projects {
		if _, found := manager.Snapshot(project.ID); found {
			t.Fatal("read restored live runtime authority")
		}
	}
	manager.mu.Lock()
	workers := len(manager.workers)
	manager.mu.Unlock()
	if workers != 0 {
		t.Fatalf("read started %d workers", workers)
	}
	if _, err := os.Stat(log); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read spawned worker or dispatched commands: %v", err)
	}
}

// The subprocess speaks real RPC through production worker lifecycle code, but
// its history is fixed fixture data, not a persisted transcript. This regression
// proves manager metadata/restart authority and no replay, not transcript durability.
func TestRuntimeRestartRecoveryWithRegistryAndExplicitReopen(t *testing.T) {
	base := t.TempDir()
	managerPath := filepath.Join(base, "manager")
	registry := registryTestOpen(t, managerPath)
	var projects []Project
	for _, name := range []string{"one", "two"} {
		project, err := registry.Add(t.Context(), name, registryTestDirectory(t, base, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := registry.SetProjectSkills(t.Context(), project, false); err != nil {
			t.Fatal(err)
		}
		project.SkillsEnabled = false
		projects = append(projects, project)
	}
	logBefore, logAfter := filepath.Join(base, "before.log"), filepath.Join(base, "after.log")
	manager := restartRecoveryManager(t, registry, logBefore)
	shellBefore := testShell(t)
	shellBefore.registry, shellBefore.runtimes, shellBefore.catalog = registry, manager, &fakeCatalog{}
	if err := shellBefore.restoreAccess(t.Context(), registry); err != nil {
		t.Fatal(err)
	}
	cookie := pairBrowser(t, shellBefore, shellBefore.initialCode)
	before := make([]RuntimeSnapshot, len(projects))
	hints := make([]RecoveryHint, len(projects))
	for i, project := range projects {
		snapshot, err := manager.Open(t.Context(), project, "saved-"+project.Name, "", "")
		if err != nil {
			t.Fatal(err)
		}
		before[i] = snapshot
		prompt := "hold"
		if i == 1 {
			prompt = "input" // Old attention authority must not survive restart.
		}
		if err := manager.Prompt(t.Context(), project.ID, snapshot.InstanceID, prompt); err != nil {
			t.Fatal(err)
		}
		active := runtimeWait(t, manager, project.ID, func(s RuntimeSnapshot) bool {
			return s.Recovery.State == RecoveryAdmitted && (i == 0 || s.Input != nil)
		})
		if active.Recovery.SessionID != snapshot.SessionID {
			t.Fatal("admitted hint is not bound to the actual session")
		}
		var found bool
		hints[i], found, err = registry.LoadRecovery(t.Context(), project.ID)
		if err != nil || !found || hints[i].State != RecoveryAdmitted {
			t.Fatalf("admission not recorded: %+v %v %v", hints[i], found, err)
		}
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	for i, project := range projects {
		stored, found, err := registry.LoadRecovery(t.Context(), project.ID)
		if err != nil || !found || stored != hints[i] {
			t.Fatalf("shutdown fabricated completion/cancellation: %+v %v %v", stored, found, err)
		}
	}
	commandsBefore, err := os.ReadFile(logBefore)
	if err != nil || strings.Count(string(commandsBefore), "prompt\n") != 2 {
		t.Fatalf("expected two explicitly admitted prompts: %q %v", commandsBefore, err)
	}
	// Registration ordering is (created_at, id), not insertion order when
	// adjacent adds share a millisecond. Compare the persisted projection.
	registeredBefore, err := registry.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Close(); err != nil {
		t.Fatal(err)
	}
	registry = registryTestOpen(t, managerPath)
	manager = restartRecoveryManager(t, registry, logAfter)
	shellAfter := testShell(t)
	shellAfter.registry, shellAfter.runtimes, shellAfter.catalog = registry, manager, &fakeCatalog{}
	if err := shellAfter.restoreAccess(t.Context(), registry); err != nil {
		t.Fatal(err)
	}
	registered, err := registry.List(t.Context())
	if err != nil || !slices.Equal(registered, registeredBefore) {
		t.Fatalf("restart changed registrations: %+v %v", registered, err)
	}
	for range 3 {
		for i, project := range projects {
			hint, found, err := manager.Recovery(t.Context(), project.ID)
			if err != nil || !found || hint != hints[i] {
				t.Fatalf("restart changed conservative evidence: %+v %v %v", hint, found, err)
			}
			for _, suffix := range []string{"", "&session=" + hint.SessionID} {
				page := request(t, shellAfter, http.MethodGet, "/?view=projects&project="+project.ID+suffix, nil, cookie)
				if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), hint.Message()) {
					t.Fatalf("paired recovery read failed after restart: %d", page.Code)
				}
			}
		}
		restartRecoveryAssertCold(t, manager, projects, logAfter)
	}
	unchanged, err := os.ReadFile(logBefore)
	if err != nil || string(unchanged) != string(commandsBefore) {
		t.Fatal("recovery reads dispatched commands to former workers")
	}
	for i, project := range projects {
		// Explicit identity-bound activation is the only operation allowed to
		// create a replacement worker. It does not accept or replay old prompts.
		page := request(t, shellAfter, http.MethodPost, "/projects/"+project.ID+"/runtime/open", url.Values{
			"csrf": {csrfFor(t, shellAfter, cookie)}, "session_id": {hints[i].SessionID}, "confirm": {"activate"},
		}, cookie)
		if page.Code != http.StatusOK {
			t.Fatalf("explicit reopen: %d %s", page.Code, page.Body.String())
		}
		reopened, found := manager.Snapshot(project.ID)
		if !found || reopened.SessionID != before[i].SessionID || reopened.InstanceID == "" || reopened.InstanceID == before[i].InstanceID || reopened.Status != "idle" || reopened.Input != nil || reopened.Permission != nil || len(reopened.Activities) != 0 || reopened.Recovery != hints[i] {
			t.Fatalf("reopen restored old authority or lost evidence: %+v", reopened)
		}
		if err := manager.Prompt(t.Context(), project.ID, before[i].InstanceID, "must not replay"); !errors.Is(err, ErrRuntimeInvalid) {
			t.Fatalf("old instance retained prompt authority: %v", err)
		}
	}
	commandsAfter, err := os.ReadFile(logAfter)
	if err != nil {
		t.Fatal(err)
	}
	const startup = "session_open\nabort\nsession_info\nmessages_page\n"
	if string(commandsAfter) != startup+startup {
		t.Fatalf("reopen issued unexpected/replayed commands: %q", commandsAfter)
	}
}
