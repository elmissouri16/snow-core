package app

import (
	"context"
	"testing"
)

func TestModelDiscoverySnapshotsAreDefensive(t *testing.T) {
	a, err := New(context.Background(), Options{Provider: "fake", NoSession: true, Permission: "deny", NoPlugins: true, NoMCP: true, NoSkills: true})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	_, _, active := a.ActiveModelsSnapshot()
	if len(active) == 0 {
		t.Fatal("active model snapshot is empty")
	}
	active[0].ID = "mutated"
	active[0].ThinkingLevels = append(active[0].ThinkingLevels, "mutated")
	if _, _, models := a.ActiveModelsSnapshot(); models[0].ID == "mutated" {
		t.Fatal("active model snapshot aliases runtime catalog")
	}

	children := a.SubagentModels()
	if len(children) == 0 {
		t.Fatal("subagent model snapshot is empty")
	}
	children[0].ID = "mutated"
	if got := a.SubagentModels()[0].ID; got == "mutated" {
		t.Fatal("subagent model snapshot aliases runtime catalog")
	}

	a.runtimeSelection.mu.Lock()
	a.runtimeSelection.catalogs[a.runtimeSelection.provider] = nil
	a.runtimeSelection.mu.Unlock()
	_, _, empty := a.ActiveModelsSnapshot()
	if empty == nil || len(empty) != 0 {
		t.Fatalf("empty catalog snapshot = %#v, want non-nil empty slice", empty)
	}
}
