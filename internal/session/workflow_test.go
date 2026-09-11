package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func workflowTestStores(t *testing.T, test func(*testing.T, Store, WorkflowStateStore)) {
	t.Helper()
	for _, kind := range []string{"memory", "sqlite"} {
		t.Run(kind, func(t *testing.T) {
			var store Store = NewMemoryStore(Options{})
			if kind == "sqlite" {
				opened, err := NewSQLiteStore(filepath.Join(t.TempDir(), "workflow.db"), t.TempDir(), Options{})
				if err != nil {
					t.Fatal(err)
				}
				store = opened
			}
			t.Cleanup(func() { _ = store.Close() })
			test(t, store, store.(WorkflowStateStore))
		})
	}
}

func workflowApply(t *testing.T, workflow WorkflowStateStore, pluginID string, update WorkflowUpdate) WorkflowState {
	t.Helper()
	state, err := workflow.WorkflowState(t.Context(), pluginID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := workflow.ApplyWorkflowState(t.Context(), pluginID, state.BranchID, state.TipID, update)
	if err != nil {
		t.Fatal(err)
	}
	if result.TipID == state.TipID {
		t.Fatal("successful update did not move tip")
	}
	return result
}

func workflowValues(key, value string) WorkflowUpdate {
	return WorkflowUpdate{Set: map[string]json.RawMessage{key: json.RawMessage(value)}}
}

func TestWorkflowProjectionBranchCompactionAndIsolation(t *testing.T) {
	workflowTestStores(t, func(t *testing.T, store Store, workflow WorkflowStateStore) {
		initial := workflowApply(t, workflow, "profiles", WorkflowUpdate{
			Set:         map[string]json.RawMessage{"profile": json.RawMessage(`"reviewer"`), "nullable": json.RawMessage(`null`)},
			Restriction: &ToolRestriction{Allow: new([]string{"read", "grep"}), Deny: []string{"bash"}},
		})
		workflowApply(t, workflow, "other", workflowValues("profile", `"other"`))
		afterOther, err := workflow.WorkflowState(t.Context(), "profiles")
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(initial.Values, afterOther.Values) {
			t.Fatal("other namespace changed values")
		}
		branch, err := store.(BranchStore).ForkBranch(initial.TipID)
		if err != nil {
			t.Fatal(err)
		}
		forked := workflowApply(t, workflow, "profiles", WorkflowUpdate{Delete: []string{"nullable"}, Set: map[string]json.RawMessage{"profile": json.RawMessage(`"architect"`)}, Restriction: &ToolRestriction{Allow: new([]string{})}})
		if _, ok := forked.Values["nullable"]; ok {
			t.Fatal("delete retained key")
		}
		if forked.Restriction == nil || forked.Restriction.Allow == nil || len(*forked.Restriction.Allow) != 0 {
			t.Fatal("empty allowlist not retained")
		}
		// Mutating a returned snapshot must not change persisted state.
		forked.Values["profile"][1] = 'X'
		*forked.Restriction.Allow = append(*forked.Restriction.Allow, "bash")
		got, err := workflow.WorkflowState(t.Context(), "profiles")
		if err != nil || string(got.Values["profile"]) != `"architect"` || len(*got.Restriction.Allow) != 0 {
			t.Fatalf("snapshot alias: %+v %v", got, err)
		}
		if err := store.Append(Entry{Type: EntryCompaction, ID: "compact", Summary: "checkpoint", CompactedThrough: initial.TipID}); err != nil {
			t.Fatal(err)
		}
		got, err = workflow.WorkflowState(t.Context(), "profiles")
		if err != nil || string(got.Values["profile"]) != `"architect"` {
			t.Fatalf("compaction lost workflow: %+v %v", got, err)
		}
		messages, err := store.Messages()
		if err != nil || len(messages) != 0 {
			t.Fatalf("workflow leaked into transcript: %v %v", messages, err)
		}
		contextMessages, err := store.(ContextStore).ContextMessages()
		if err != nil {
			t.Fatal(err)
		}
		for _, message := range contextMessages {
			if strings.Contains(fmt.Sprint(message), "architect") {
				t.Fatal("workflow leaked into provider context")
			}
		}
		if err := store.(BranchStore).SelectBranch("main"); err != nil {
			t.Fatal(err)
		}
		main, err := workflow.WorkflowState(t.Context(), "profiles")
		if err != nil || string(main.Values["profile"]) != `"reviewer"` || string(main.Values["nullable"]) != "null" {
			t.Fatalf("sibling contaminated: %+v %v", main, err)
		}
		if err := store.(BranchStore).SelectBranch(branch.ID); err != nil {
			t.Fatal(err)
		}
		workflowApply(t, workflow, "profiles", WorkflowUpdate{ClearRestriction: true})
		got, err = workflow.WorkflowState(t.Context(), "profiles")
		if err != nil || got.Restriction != nil {
			t.Fatalf("clear: %+v %v", got, err)
		}
		if err := store.SetBranchTip(initial.TipID); err != nil {
			t.Fatal(err)
		}
		rewound, err := workflow.WorkflowState(t.Context(), "profiles")
		if err != nil || !reflect.DeepEqual(rewound.Values, initial.Values) || !reflect.DeepEqual(rewound.Restriction, initial.Restriction) {
			t.Fatalf("rewind: %+v %v", rewound, err)
		}
	})
}

func TestWorkflowRejectedUpdatesDoNotMoveTip(t *testing.T) {
	workflowTestStores(t, func(t *testing.T, store Store, workflow WorkflowStateStore) {
		state := workflowApply(t, workflow, "profiles", workflowValues("profile", `"reviewer"`))
		invalid := []WorkflowUpdate{
			{}, workflowValues("", `1`), workflowValues(strings.Repeat("k", 129), `1`), workflowValues("nul\x00", `1`), workflowValues("\xff", `1`),
			workflowValues("key", `{`), workflowValues("key", `{"x":1,"x":2}`), workflowValues("key", "\""+strings.Repeat("x", MaxWorkflowValueBytes)+"\""),
			{Delete: []string{"a", "a"}}, {Set: map[string]json.RawMessage{"a": json.RawMessage(`1`)}, Delete: []string{"a"}},
			{Restriction: &ToolRestriction{}, ClearRestriction: true}, {Restriction: &ToolRestriction{Deny: []string{"*"}}},
			{Restriction: &ToolRestriction{Allow: new([]string{"read", "read"})}},
			{Delete: make([]string, MaxWorkflowMutations+1)},
		}
		hugeBatch := WorkflowUpdate{Set: make(map[string]json.RawMessage)}
		for i := range 3 {
			hugeBatch.Set[fmt.Sprint(i)] = json.RawMessage(`"` + strings.Repeat("x", 60000) + `"`)
		}
		invalid = append(invalid, hugeBatch)
		for i, update := range invalid {
			if _, err := workflow.ApplyWorkflowState(t.Context(), "profiles", state.BranchID, state.TipID, update); err == nil {
				t.Errorf("invalid %d accepted", i)
			}
			if store.BranchTip() != state.TipID {
				t.Fatalf("invalid %d moved tip", i)
			}
		}
		if _, err := workflow.ApplyWorkflowState(t.Context(), "bad/plugin", state.BranchID, state.TipID, workflowValues("key", `1`)); err == nil {
			t.Fatal("bad owner accepted")
		}
		for _, expected := range [][2]string{{"wrong", state.TipID}, {state.BranchID, "wrong"}} {
			if _, err := workflow.ApplyWorkflowState(t.Context(), "profiles", expected[0], expected[1], workflowValues("key", `1`)); !errors.Is(err, ErrConflict) {
				t.Fatalf("CAS: %v", err)
			}
		}
		canceled, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := workflow.ApplyWorkflowState(canceled, "profiles", state.BranchID, state.TipID, workflowValues("key", `1`)); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel: %v", err)
		}
		if _, err := workflow.WorkflowState(canceled, "profiles"); !errors.Is(err, context.Canceled) {
			t.Fatalf("read cancel: %v", err)
		}
		entries, err := store.(BranchEntryStore).BranchEntries()
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 2 || store.BranchTip() != state.TipID {
			t.Fatal("failed updates changed history")
		}
	})
}

func TestWorkflowLiveQuotasAtomic(t *testing.T) {
	workflowTestStores(t, func(t *testing.T, store Store, workflow WorkflowStateStore) {
		for batch := range MaxWorkflowKeys / MaxWorkflowMutations {
			update := WorkflowUpdate{Set: make(map[string]json.RawMessage)}
			for i := range MaxWorkflowMutations {
				update.Set[fmt.Sprintf("key-%d", batch*MaxWorkflowMutations+i)] = json.RawMessage(`null`)
			}
			workflowApply(t, workflow, "keys", update)
		}
		state, err := workflow.WorkflowState(t.Context(), "keys")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := workflow.ApplyWorkflowState(t.Context(), "keys", state.BranchID, state.TipID, workflowValues("extra", `1`)); err == nil {
			t.Fatal("key quota not enforced")
		}
		if store.BranchTip() != state.TipID {
			t.Fatal("key quota moved tip")
		}
		// A delete frees one key within the same atomic update.
		workflowApply(t, workflow, "keys", WorkflowUpdate{Set: map[string]json.RawMessage{"extra": json.RawMessage(`1`)}, Delete: []string{"key-0"}})
		for i := range 15 {
			workflowApply(t, workflow, "bytes", workflowValues(fmt.Sprint(i), `"`+strings.Repeat("x", MaxWorkflowValueBytes-2)+`"`))
		}
		state, err = workflow.WorkflowState(t.Context(), "bytes")
		if err != nil {
			t.Fatal(err)
		}
		tooLarge := workflowValues("extra", `"`+strings.Repeat("x", MaxWorkflowValueBytes-2)+`"`)
		tooLarge.Restriction = &ToolRestriction{Allow: new([]string{})}
		if _, err := workflow.ApplyWorkflowState(t.Context(), "bytes", state.BranchID, state.TipID, tooLarge); err == nil {
			t.Fatal("byte quota not enforced")
		}
		got, err := workflow.WorkflowState(t.Context(), "bytes")
		if err != nil || got.Restriction != nil || got.TipID != state.TipID || len(got.Values) != 15 {
			t.Fatalf("quota partially committed policy: %+v %v", got, err)
		}
	})
}

func TestWorkflowConcurrentCAS(t *testing.T) {
	workflowTestStores(t, func(t *testing.T, store Store, workflow WorkflowStateStore) {
		state, err := workflow.WorkflowState(t.Context(), "profiles")
		if err != nil {
			t.Fatal(err)
		}
		var won atomic.Int32
		var wg sync.WaitGroup
		for range 8 {
			wg.Go(func() {
				_, err := workflow.ApplyWorkflowState(t.Context(), "profiles", state.BranchID, state.TipID, workflowValues("value", `1`))
				if err == nil {
					won.Add(1)
				} else if !errors.Is(err, ErrConflict) {
					t.Errorf("CAS returned %v", err)
				}
			})
		}
		wg.Wait()
		if won.Load() != 1 {
			t.Fatalf("CAS winners %d", won.Load())
		}
	})
}

func TestWorkflowMalformedJournalFailsClosed(t *testing.T) {
	workflowTestStores(t, func(t *testing.T, store Store, workflow WorkflowStateStore) {
		if err := store.Append(Entry{Type: EntryMeta, Key: MetaPluginWorkflow, Value: `{"version":2,"plugin_id":"profiles","update":{"delete":["key"]}}`}); err != nil {
			t.Fatal(err)
		}
		tip := store.BranchTip()
		if _, err := workflow.WorkflowState(t.Context(), "profiles"); err == nil {
			t.Fatal("future record accepted")
		}
		if _, err := workflow.ApplyWorkflowState(t.Context(), "profiles", store.(interface{ ActiveBranchID() string }).ActiveBranchID(), tip, workflowValues("value", `1`)); err == nil {
			t.Fatal("write over future record accepted")
		}
		if store.BranchTip() != tip {
			t.Fatal("malformed journal moved tip")
		}
	})
}

func TestWorkflowSQLiteReopenAndDetachedFork(t *testing.T) {
	cwd := t.TempDir()
	path := filepath.Join(t.TempDir(), "workflow.db")
	store, err := NewSQLiteStore(path, cwd, Options{})
	if err != nil {
		t.Fatal(err)
	}
	initial := workflowApply(t, store, "profiles", WorkflowUpdate{Set: map[string]json.RawMessage{"profile": json.RawMessage(`"reviewer"`)}, Restriction: &ToolRestriction{Allow: new([]string{})}})
	workflowApply(t, store, "profiles", workflowValues("profile", `"architect"`))
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenSQLiteStore(path, cwd, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	state, err := reopened.WorkflowState(t.Context(), "profiles")
	if err != nil || string(state.Values["profile"]) != `"architect"` {
		t.Fatalf("reopen: %+v %v", state, err)
	}
	index := NewFileIndex(t.TempDir())
	forked, _, err := index.CreateFork(cwd, reopened, protocol.SessionForkOptions{FromEntryID: initial.TipID})
	if err != nil {
		t.Fatal(err)
	}
	defer forked.Close()
	forkState, err := forked.(WorkflowStateStore).WorkflowState(t.Context(), "profiles")
	if err != nil || !reflect.DeepEqual(forkState.Values, initial.Values) || !reflect.DeepEqual(forkState.Restriction, initial.Restriction) {
		t.Fatalf("detached fork: %+v %v", forkState, err)
	}
	if reopened.BranchTip() != state.TipID {
		t.Fatal("detached fork moved source")
	}
}

func TestWorkflowSQLiteStaleHandleCAS(t *testing.T) {
	cwd := t.TempDir()
	path := filepath.Join(t.TempDir(), "workflow.db")
	first, err := NewSQLiteStore(path, cwd, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := NewSQLiteStore(path, cwd, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	stale, err := second.WorkflowState(t.Context(), "profiles")
	if err != nil {
		t.Fatal(err)
	}
	committed := workflowApply(t, first, "profiles", workflowValues("value", `1`))
	if _, err := second.ApplyWorkflowState(t.Context(), "profiles", stale.BranchID, stale.TipID, workflowValues("value", `2`)); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale handle: %v", err)
	}
	got, err := first.WorkflowState(t.Context(), "profiles")
	if err != nil || got.TipID != committed.TipID || string(got.Values["value"]) != `1` {
		t.Fatalf("stale update changed history: %+v %v", got, err)
	}
}

func TestWorkflowSQLiteStaleHandleAfterActiveBranchSwitch(t *testing.T) {
	cwd := t.TempDir()
	path := filepath.Join(t.TempDir(), "workflow.db")
	first, err := NewSQLiteStore(path, cwd, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	initial := workflowApply(t, first, "profiles", workflowValues("value", `1`))
	second, err := NewSQLiteStore(path, cwd, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	stale, err := second.WorkflowState(t.Context(), "profiles")
	if err != nil {
		t.Fatal(err)
	}
	branch, err := first.ForkBranch(initial.TipID)
	if err != nil {
		t.Fatal(err)
	}
	var before int
	if err := first.db.QueryRowContext(t.Context(), `SELECT count(*) FROM entries`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := second.ApplyWorkflowState(t.Context(), "profiles", stale.BranchID, stale.TipID, workflowValues("value", `2`)); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale active branch: %v", err)
	}
	var after int
	if err := first.db.QueryRowContext(t.Context(), `SELECT count(*) FROM entries`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("rejected update appended history: before=%d after=%d", before, after)
	}
	active, err := first.WorkflowState(t.Context(), "profiles")
	if err != nil || active.BranchID != branch.ID || string(active.Values["value"]) != `1` {
		t.Fatalf("active branch changed: %+v %v", active, err)
	}
	if err := first.SelectBranch(stale.BranchID); err != nil {
		t.Fatal(err)
	}
	original, err := first.WorkflowState(t.Context(), "profiles")
	if err != nil || original.TipID != stale.TipID || string(original.Values["value"]) != `1` {
		t.Fatalf("inactive branch changed: %+v %v", original, err)
	}
}
