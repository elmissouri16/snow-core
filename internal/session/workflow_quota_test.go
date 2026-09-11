package session

import (
	"fmt"
	"strings"
	"testing"
)

func TestWorkflowHistoryQuotasIncludeOtherBranchesAndOwners(t *testing.T) {
	for _, quota := range []string{"records", "bytes"} {
		t.Run(quota, func(t *testing.T) {
			workflowTestStores(t, func(t *testing.T, store Store, workflow WorkflowStateStore) {
				// Fill an unrelated owner's orphaned branch. Quotas are session-global,
				// even when the active ancestry contains none of these records.
				if _, err := store.(BranchStore).ForkBranch("root"); err != nil {
					t.Fatal(err)
				}
				raw, _, err := encodeWorkflowUpdate("other", WorkflowUpdate{Delete: []string{"unused"}})
				if err != nil {
					t.Fatal(err)
				}
				var entries []Entry
				if quota == "records" {
					entries = make([]Entry, MaxWorkflowHistoryRecords)
					for i := range entries {
						entries[i] = Entry{Type: EntryMeta, ID: fmt.Sprintf("workflow-%d", i), Key: MetaPluginWorkflow, Value: raw}
					}
				} else {
					// Whitespace remains valid JSON but consumes actual persisted bytes.
					// Use non-ASCII whitespace-free content in separate validation tests;
					// this fixture reaches exactly the history byte cap without large state.
					padded := raw + strings.Repeat(" ", MaxWorkflowUpdateBytes-len(raw))
					entries = make([]Entry, MaxWorkflowHistoryBytes/MaxWorkflowUpdateBytes)
					for i := range entries {
						entries[i] = Entry{Type: EntryMeta, ID: fmt.Sprintf("workflow-%d", i), Key: MetaPluginWorkflow, Value: padded}
					}
				}
				if err := store.(BatchStore).AppendBatch(entries); err != nil {
					t.Fatal(err)
				}
				if err := store.(BranchStore).SelectBranch("main"); err != nil {
					t.Fatal(err)
				}
				state, err := workflow.WorkflowState(t.Context(), "profiles")
				if err != nil {
					t.Fatal(err)
				}
				if len(state.Values) != 0 || state.TipID != "root" {
					t.Fatal("orphan branch contaminated state")
				}
				if _, err := workflow.ApplyWorkflowState(t.Context(), "profiles", state.BranchID, state.TipID, workflowValues("key", `1`)); err == nil {
					t.Fatal("global quota not enforced")
				}
				if store.BranchTip() != "root" {
					t.Fatal("quota failure moved branch")
				}
			})
		})
	}
}

func TestWorkflowRecordValidation(t *testing.T) {
	for _, raw := range []string{
		`{}`, `null`, `[]`,
		`{"version":1,"plugin_id":"profiles","update":{"set":{"key":1}},"future":true}`,
		`{"version":1,"plugin_id":"profiles","update":{"set":{"key":1},"future":true}}`,
		`{"version":1,"plugin_id":"profiles","update":{"set":{"key":1,"key":2}}}`,
		`{"version":1,"plugin_id":"bad/plugin","update":{"delete":["key"]}}`,
		strings.Repeat(" ", MaxWorkflowUpdateBytes+1),
	} {
		if _, err := decodeWorkflowRecord(raw); err == nil {
			t.Fatal("malformed record accepted")
		}
	}
	for _, restriction := range []*ToolRestriction{
		{Allow: new([]string{})}, {}, {Deny: []string{"read"}},
	} {
		raw, _, err := encodeWorkflowUpdate("profiles", WorkflowUpdate{Restriction: restriction})
		if err != nil {
			t.Fatal(err)
		}
		record, err := decodeWorkflowRecord(raw)
		if err != nil {
			t.Fatal(err)
		}
		if (record.Update.Restriction.Allow == nil) != (restriction.Allow == nil) {
			t.Fatal("nil versus empty allow lost")
		}
	}
	names := make([]string, MaxWorkflowToolNames+1)
	for i := range names {
		names[i] = fmt.Sprintf("tool_%d", i)
	}
	if _, _, err := encodeWorkflowUpdate("profiles", WorkflowUpdate{Restriction: &ToolRestriction{Deny: names}}); err == nil {
		t.Fatal("tool list bound not enforced")
	}
	// The last two-byte rune takes this key beyond 128 bytes.
	if err := validateWorkflowKey(strings.Repeat("é", 64)); err != nil {
		t.Fatal(err)
	}
	if err := validateWorkflowKey(strings.Repeat("é", 65)); err == nil {
		t.Fatal("UTF-8 key counted as runes")
	}
}
