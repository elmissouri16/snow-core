package session

import (
	json "encoding/json/v2"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func catalogToolOwner(id string, calls ...string) protocol.Message {
	owner := protocol.Message{ID: id, Role: protocol.RoleAssistant}
	for _, call := range calls {
		owner.Content = append(owner.Content, protocol.ContentBlock{Type: protocol.BlockToolCall, ToolCallID: call, Name: "read", Arguments: []byte(`{"private":"arguments"}`)})
	}
	return owner
}
func catalogToolResult(id, call, text string) protocol.Message {
	return protocol.Message{ID: id, Role: protocol.RoleTool, ToolCallID: call, PublicToolResult: &protocol.ToolResultPreview{Text: text}, Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: "PRIVATE_CONTENT"}}}
}

func TestCatalogToolHistoryOptInPaginationPrivacyAndReadOnly(t *testing.T) {
	root, cwd := t.TempDir(), t.TempDir()
	legacy := catalogToolResult("legacy", "legacy", "not explicit")
	legacy.PublicToolResult = nil
	first := catalogToolOwner("first", "reused", "legacy", "pending")
	first.Content = append([]protocol.ContentBlock{{Type: protocol.BlockThinking, Text: "PRIVATE_THINKING"}, {Type: protocol.BlockProviderData, Data: []byte("PRIVATE_CONTINUITY")}}, first.Content...)
	failed := catalogToolResult("failed", "reused", "")
	failed.IsError = true
	messages := []protocol.Message{
		catalogToolResult("orphan", "reused", "ORPHAN"),
		protocol.NewUserMessage("user", "", "hello"), first,
		catalogToolResult("first-result", "reused", "explicit first"), legacy,
		protocol.NewUserMessage("boundary", "", "boundary"), catalogToolResult("late", "pending", "PRIVATE_LATE"),
		catalogToolOwner("second", "reused"), failed,
		catalogToolOwner("duplicates", "same", "same"), catalogToolResult("ambiguous", "same", "PRIVATE_AMBIGUOUS"),
		catalogToolOwner("unresolved", "never"),
	}
	id, _ := catalogFixture(t, root, cwd, "tools", messages...)
	before := catalogSnapshot(t, root)
	catalog := NewCatalog(root, cwd)
	legacyPage, err := catalog.Messages(t.Context(), id, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(legacyPage.Messages) != 1 || len(legacyPage.Messages[0].Tools) != 0 || legacyPage.ToolsTruncated {
		t.Fatalf("legacy=%+v", legacyPage)
	}
	page, err := catalog.Messages(t.Context(), id, 1, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 1 || page.Messages[0].ID != "first" || page.Messages[0].Text != "" || page.NextOffset != 2 || !page.HasMore {
		t.Fatalf("page=%+v", page)
	}
	tools := page.Messages[0].Tools
	if len(tools) != 3 || tools[0].Output != "explicit first" || tools[0].ResultID != "first-result" || !tools[0].OutputAvailable || tools[1].OutputAvailable || tools[1].Status != "completed" || tools[2].Status != "unresolved" {
		t.Fatalf("tools=%+v", tools)
	}
	full, err := catalog.Messages(t.Context(), id, 0, 50, true)
	if err != nil {
		t.Fatal(err)
	}
	expected, truncated := protocol.ProjectHistoryTools(messages)
	if full.ToolsTruncated != truncated || len(full.Messages) != 6 || full.NextOffset != 6 || full.HasMore {
		t.Fatalf("full=%+v", full)
	}
	for _, message := range full.Messages {
		if !reflect.DeepEqual(message.Tools, expected[message.ID]) {
			t.Fatalf("%s tools=%+v expected=%+v", message.ID, message.Tools, expected[message.ID])
		}
	}
	if !reflect.DeepEqual(tools, full.Messages[1].Tools) {
		t.Fatal("page-end result or IDs changed across pagination")
	}
	encoded, err := json.Marshal(full)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"PRIVATE", "arguments", "ORPHAN", "provider_data", "tool_call_id", "public_tool_result"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("leaked %q", secret)
		}
	}
	if !reflect.DeepEqual(before, catalogSnapshot(t, root)) {
		t.Fatal("catalog tool read wrote session files")
	}
}

func TestCatalogToolHistoryRespectsBranchAndCompaction(t *testing.T) {
	root, cwd := t.TempDir(), t.TempDir()
	store, err := NewFileIndex(root).Create(cwd)
	if err != nil {
		t.Fatal(err)
	}
	s := store.(*SQLiteStore)
	owner := catalogToolOwner("owner", "call")
	selected := catalogToolResult("selected-result", "call", "selected public")
	appendEntry := func(entry Entry) {
		t.Helper()
		if err := s.Append(entry); err != nil {
			t.Fatal(err)
		}
	}
	appendEntry(Entry{ID: owner.ID, Type: EntryMessage, Message: &owner})
	appendEntry(Entry{ID: "checkpoint", Type: EntryCompaction, Summary: "PRIVATE_CHECKPOINT", CompactedThrough: "owner"})
	appendEntry(Entry{ID: selected.ID, Type: EntryMessage, Message: &selected})
	if err := s.SetBranchTip("owner"); err != nil {
		t.Fatal(err)
	}
	alternate := catalogToolResult("other-result", "call", "PRIVATE_OTHER_BRANCH")
	appendEntry(Entry{ID: alternate.ID, Type: EntryMessage, Message: &alternate})
	if err := s.SetBranchTip(selected.ID); err != nil {
		t.Fatal(err)
	}
	id := s.ID()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	page, err := NewCatalog(root, cwd).Messages(t.Context(), id, 0, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 1 || len(page.Messages[0].Tools) != 1 || page.Messages[0].Tools[0].ResultID != selected.ID || page.Messages[0].Tools[0].Output != "selected public" || page.HasMore {
		t.Fatalf("page=%+v", page)
	}
}

func TestCatalogToolHistoryBoundsRowsAndDecodeBytes(t *testing.T) {
	for _, scenario := range []string{"rows", "bytes", "owner-bytes"} {
		t.Run(scenario, func(t *testing.T) {
			root, cwd := t.TempDir(), t.TempDir()
			messages := []protocol.Message{catalogToolOwner("owner", "call")}
			if scenario != "owner-bytes" {
				// The omitted prefix contains a real matching result, not just unrelated
				// noise. Keeping the recent duplicate would falsely resolve this call.
				messages = append(messages, catalogToolResult("hidden-match", "call", "older conflicting result"))
			}
			switch scenario {
			case "rows":
				for i := range catalogMaxHistoryToolRows + 10 {
					messages = append(messages, catalogToolResult(fmt.Sprintf("result-%d", i), "unknown", "explicit"))
				}
				messages = append(messages, catalogToolResult("recent", "call", "recent public"))
			case "bytes":
				for i := range 5 {
					result := catalogToolResult(fmt.Sprintf("result-%d", i), "unknown", "explicit")
					result.Content[0].Text = strings.Repeat("x", 2<<20)
					messages = append(messages, result)
				}
				messages = append(messages, catalogToolResult("recent", "call", "recent public"))
			case "owner-bytes":
				for i := range 5 {
					owner := catalogToolOwner(fmt.Sprintf("owner-%d", i), "call")
					owner.Content = append(owner.Content, protocol.ContentBlock{Type: protocol.BlockThinking, Text: strings.Repeat("x", 2<<20)})
					messages = append(messages, owner)
				}
			}
			id, _ := catalogFixture(t, root, cwd, "bounded", messages...)
			page, err := NewCatalog(root, cwd).Messages(t.Context(), id, 0, 50, true)
			if err != nil {
				t.Fatal(err)
			}
			if !page.ToolsTruncated {
				t.Fatalf("omission not surfaced: %+v", page)
			}
			if scenario != "owner-bytes" {
				tool := page.Messages[0].Tools[0]
				if tool.Status != "unresolved" || tool.ResultID != "" || tool.OutputAvailable || tool.Output != "" || !tool.Truncated {
					t.Fatalf("incomplete interval falsely resolved: %+v", tool)
				}
			}
		})
	}
}

func TestCatalogPublicBlockDecoderSkipsPrivateFields(t *testing.T) {
	for _, raw := range []string{
		`{"type":"thinking","text":{"not":"a string"},"data":{"not":"base64"}}`,
		`{"type":"provider_data","text":123,"data":[{}]}`,
		`{"type":"tool_call","tool_call_id":"call","name":"read","arguments":{"deep":[1,2,3]},"text":{"private":true}}`,
	} {
		var block catalogHistoryBlock
		if err := json.Unmarshal([]byte(raw), &block); err != nil {
			t.Fatal(err)
		}
		if block.Text != "" {
			t.Fatal("decoded private text")
		}
	}
}

func TestCatalogToolHistoryIncompleteIntervalsDoNotAffectCompleteOwners(t *testing.T) {
	root, cwd := t.TempDir(), t.TempDir()
	oversized := catalogToolResult("oversized", "call", "unavailable duplicate")
	oversized.Content[0].Text = strings.Repeat("x", catalogMaxMessageBytes+1)
	messages := []protocol.Message{
		catalogToolOwner("incomplete", "call"), oversized,
		catalogToolResult("visible-duplicate", "call", "must not win"),
		catalogToolOwner("complete", "call"), catalogToolResult("complete-result", "call", "complete public"),
	}
	id, _ := catalogFixture(t, root, cwd, "intervals", messages...)
	page, err := NewCatalog(root, cwd).Messages(t.Context(), id, 0, 50, true)
	if err != nil {
		t.Fatal(err)
	}
	if !page.ToolsTruncated || len(page.Messages) != 2 {
		t.Fatalf("page=%+v", page)
	}
	incomplete, complete := page.Messages[0].Tools[0], page.Messages[1].Tools[0]
	if incomplete.Status != "unresolved" || incomplete.OutputAvailable || incomplete.ResultID != "" || !incomplete.Truncated {
		t.Fatalf("incomplete=%+v", incomplete)
	}
	if complete.Status != "completed" || complete.Output != "complete public" || complete.Truncated {
		t.Fatalf("complete=%+v", complete)
	}
}

func TestCatalogToolHistoryEntireOwnerIntervalCanBeRowOmitted(t *testing.T) {
	root, cwd := t.TempDir(), t.TempDir()
	messages := []protocol.Message{catalogToolOwner("older", "call"), catalogToolResult("hidden", "call", "hidden actual result"), catalogToolOwner("recent", "call")}
	for i := range catalogMaxHistoryToolRows {
		messages = append(messages, catalogToolResult(fmt.Sprintf("noise-%d", i), "unknown", "unrelated"))
	}
	id, _ := catalogFixture(t, root, cwd, "rows", messages...)
	page, err := NewCatalog(root, cwd).Messages(t.Context(), id, 0, 50, true)
	if err != nil {
		t.Fatal(err)
	}
	older := page.Messages[0].Tools[0]
	if !page.ToolsTruncated || older.Status != "unresolved" || !older.Truncated || older.OutputAvailable {
		t.Fatalf("omitted owner=%+v page truncated=%v", older, page.ToolsTruncated)
	}
	if page.Messages[1].Tools[0].Truncated {
		t.Fatal("complete recent interval falsely marked omitted")
	}
}

func TestCatalogToolHistoryChecksResultNameAndDuplicateResults(t *testing.T) {
	for _, scenario := range []string{"wrong-name", "oversized-name", "duplicate", "filtered-duplicate"} {
		t.Run(scenario, func(t *testing.T) {
			root, cwd := t.TempDir(), t.TempDir()
			owner := catalogToolOwner("owner", "call")
			result := catalogToolResult("result", "call", "must not resolve")
			switch scenario {
			case "wrong-name", "filtered-duplicate":
				result.ToolName = "bash"
			case "oversized-name":
				result.ToolName = strings.Repeat("x", 1<<20)
			case "duplicate":
				result.ToolName = "read"
			}
			messages := []protocol.Message{owner, result}
			if scenario == "duplicate" || scenario == "filtered-duplicate" {
				messages = append(messages, catalogToolResult("another", "call", "ambiguous public"))
			}
			id, _ := catalogFixture(t, root, cwd, "names", messages...)
			page, err := NewCatalog(root, cwd).Messages(t.Context(), id, 0, 1, true)
			if err != nil {
				t.Fatal(err)
			}
			tool := page.Messages[0].Tools[0]
			if !page.ToolsTruncated || tool.Status != "unresolved" || tool.ResultID != "" || tool.OutputAvailable || tool.Output != "" {
				t.Fatalf("falsely definitive: %+v", tool)
			}
		})
	}
}

func TestCatalogToolHistoryPreservesUnknownOutcome(t *testing.T) {
	for _, preview := range []*protocol.ToolResultPreview{nil, {Text: "PRIVATE_UNKNOWN_PREVIEW", Truncated: true}} {
		root, cwd := t.TempDir(), t.TempDir()
		owner := catalogToolOwner("owner", "call")
		result := catalogToolResult("recovery", "call", "")
		result.ToolOutcomeUnknown, result.IsError, result.PublicToolResult = true, true, preview
		id, _ := catalogFixture(t, root, cwd, "recovered", owner, result)
		before := catalogSnapshot(t, root)
		page, err := NewCatalog(root, cwd).Messages(t.Context(), id, 0, 1, true)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Messages) != 1 || len(page.Messages[0].Tools) != 1 {
			t.Fatalf("missing history: %+v", page)
		}
		tool := page.Messages[0].Tools[0]
		if tool.Status != "unresolved" || tool.ResultID != "" || tool.OutputAvailable || tool.Output != "" || tool.Truncated || page.ToolsTruncated {
			t.Fatalf("catalog lost recovery provenance: %+v", page)
		}
		if !reflect.DeepEqual(before, catalogSnapshot(t, root)) {
			t.Fatal("catalog recovery projection changed durable files")
		}
	}
}
