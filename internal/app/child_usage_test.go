package app

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/elmissouri16/snow-core/internal/agent"
	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/provider/fake"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestLegacyChildUsageReopenPreservesActualHistory(t *testing.T) {
	cwd := t.TempDir()
	path := filepath.Join(cwd, "child.db")
	store, err := session.NewSQLiteStore(path, cwd, session.Options{})
	if err != nil {
		t.Fatal(err)
	}
	// Reproduce the old persisted fork prefix. Its EUR cost and omitted cache
	// metric must not poison the actual child's USD cost or known-cache result.
	inherited := protocol.Usage{Input: 80, Output: 20, Total: 100, Cost: &protocol.Cost{Currency: "EUR", Total: 0.5}}
	user := protocol.NewUserMessage("fork-1-parent-user", "root", "inherited task")
	assistant := protocol.NewAssistantMessage("fork-2-parent-assistant", user.ID, "fake", "m", []protocol.ContentBlock{protocol.NewTextBlock("inherited answer")}, protocol.StopStop, &inherited)
	for _, message := range []protocol.Message{user, assistant} {
		if err := store.Append(session.Entry{Type: session.EntryMessage, ID: message.ID, Message: &message}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	actual := protocol.Usage{Input: 8, Output: 2, Total: 10, CacheReadKnown: true, Cost: &protocol.Cost{Currency: "USD", Total: 0.05}}
	makeRuntime := func() (*childAgentRuntime, *fake.Provider) {
		t.Helper()
		opened, err := session.OpenSQLiteStore(path, cwd, session.Options{})
		if err != nil {
			t.Fatal(err)
		}
		provider := fake.New([]fake.Step{{Kind: fake.StepText, Text: "child answer"}, {Kind: fake.StepUsage, Usage: &actual}, {Kind: fake.StepDone, Stop: protocol.StopStop}})
		child, err := agent.New(agent.Options{Provider: provider, Registry: tools.NewRegistry(), Session: opened, Permission: permission.NewService(permission.ModeDeny, nil), Model: protocol.Model{Provider: "fake", ID: "m", ContextWindow: 128000}})
		if err != nil {
			t.Fatal(err)
		}
		return &childAgentRuntime{Agent: child, store: opened}, provider
	}
	child, provider := makeRuntime()
	usage, err := child.Usage()
	if err != nil || !reflect.DeepEqual(usage, protocol.Usage{}) || provider.CallCount() != 0 {
		t.Fatalf("legacy reopen before request: usage=%+v err=%v calls=%d", usage, err, provider.CallCount())
	}
	before, err := child.Messages()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Prompt(t.Context(), "fresh task"); err != nil {
		t.Fatal(err)
	}
	// Auxiliary requests must survive filtering too, even after a compaction.
	if err := child.store.Append(session.Entry{Type: session.EntryMeta, ID: "aux", Key: session.MetaProviderUsage, Value: `{"input":3,"output":1,"total_tokens":4,"cache_read_known":true}`}); err != nil {
		t.Fatal(err)
	}
	if err := child.store.Append(session.Entry{Type: session.EntryCompaction, ID: "compact", Summary: "working state", CompactedThrough: assistant.ID}); err != nil {
		t.Fatal(err)
	}
	want := (protocol.Usage{}).Add(actual).Add(protocol.Usage{Input: 3, Output: 1, Total: 4, CacheReadKnown: true})
	usage, err = child.Usage()
	if err != nil || !reflect.DeepEqual(usage, want) {
		t.Fatalf("actual incurred usage=%+v want=%+v err=%v", usage, want, err)
	}
	after, err := child.Messages()
	if err != nil || !reflect.DeepEqual(before, after[:len(before)]) || after[1].Usage.Cost.Currency != "EUR" {
		t.Fatalf("legacy transcript changed: %+v err=%v", after, err)
	}
	child.Close()
	child, provider = makeRuntime()
	defer child.Close()
	usage, err = child.Usage()
	if err != nil || !reflect.DeepEqual(usage, want) || provider.CallCount() != 0 {
		t.Fatalf("durable reopen: usage=%+v want=%+v err=%v calls=%d", usage, want, err, provider.CallCount())
	}
	if err := child.Prompt(t.Context(), "next actual task"); err != nil {
		t.Fatal(err)
	}
	want = want.Add(actual)
	usage, err = child.Usage()
	if err != nil || !reflect.DeepEqual(usage, want) {
		t.Fatalf("continued child usage=%+v want=%+v err=%v", usage, want, err)
	}
}

func TestChildUsageOnlyExcludesInitialConsecutiveForkMessages(t *testing.T) {
	usage := protocol.Usage{Total: 10}
	for _, ids := range [][]string{{"actual", "fork-1-later"}, {"fork-2-out-of-order", "fork-1-later"}, {"fork-1-inherited", "actual", "fork-2-later"}} {
		store := session.NewMemoryStore(session.Options{})
		defer store.Close()
		for _, id := range ids {
			message := protocol.NewAssistantMessage(id, "", "fake", "m", []protocol.ContentBlock{protocol.NewTextBlock("answer")}, protocol.StopStop, &usage)
			if err := store.Append(session.Entry{Type: session.EntryMessage, ID: id, Message: &message}); err != nil {
				t.Fatal(err)
			}
		}
		entries, err := store.BranchEntries()
		if err != nil {
			t.Fatal(err)
		}
		got, err := session.ChildIncurredUsage(entries)
		if err != nil || got.Total != 20 || got.Requests != 2 {
			t.Fatalf("ids=%v usage=%+v err=%v", ids, got, err)
		}
	}
}
