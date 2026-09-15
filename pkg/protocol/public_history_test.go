package protocol

import (
	"bytes"
	json "encoding/json/v2"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func historyOwner(id string, calls ...string) Message {
	message := Message{ID: id, Role: RoleAssistant}
	for _, call := range calls {
		message.Content = append(message.Content, ContentBlock{Type: BlockToolCall, ToolCallID: call, Name: "read"})
	}
	return message
}

func historyResult(id, call, text string) Message {
	return Message{ID: id, Role: RoleTool, ToolCallID: call, PublicToolResult: &ToolResultPreview{Text: text}}
}

func TestProjectHistoryToolsOwnershipAndPrivacy(t *testing.T) {
	legacy := historyResult("legacy", "old", "ignored")
	legacy.PublicToolResult = nil
	legacy.Content = []ContentBlock{{Type: BlockText, Text: "private fallback"}, {Type: BlockThinking, Text: "private thinking"}, {Type: BlockProviderData, Data: []byte("private continuity")}}
	failed := historyResult("failed", "reuse", "explicit public")
	failed.IsError = true
	messages := []Message{
		historyResult("orphan", "reuse", "orphan output"),
		historyOwner("first", "reuse", "old", "pending"),
		historyResult("first-result", "reuse", "first public"), legacy,
		{ID: "user", Role: RoleUser},
		historyResult("past-user", "pending", "no match across user"),
		historyOwner("second", "reuse"), failed,
		historyOwner("dupes", "duplicate", "duplicate"), historyResult("ambiguous", "duplicate", "no ambiguous output"),
		historyOwner("interrupted", "future"), historyOwner("boundary"), historyResult("past-assistant", "future", "no match across assistant"),
		historyResult("forward", "next", "no forward matching"), historyOwner("next", "next"),
	}
	before, err := json.Marshal(messages)
	if err != nil {
		t.Fatal(err)
	}
	tools, clipped := ProjectHistoryTools(messages)
	if clipped {
		t.Fatal("unexpected clipping")
	}
	after, err := json.Marshal(messages)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("projector mutated messages")
	}
	first := tools["first"]
	if len(first) != 3 || first[0].Status != "completed" || first[0].ResultID != "first-result" || first[0].Output != "first public" || !first[0].OutputAvailable {
		t.Fatalf("first=%+v", first)
	}
	if first[1].Status != "completed" || first[1].ResultID != "legacy" || first[1].OutputAvailable || first[1].Output != "" {
		t.Fatalf("legacy=%+v", first[1])
	}
	if first[2].Status != "unresolved" || first[2].ResultID != "" {
		t.Fatalf("pending=%+v", first[2])
	}
	if tools["second"][0].Status != "failed" || tools["second"][0].Output != "explicit public" || tools["second"][0].ID == first[0].ID {
		t.Fatalf("reused=%+v", tools["second"])
	}
	for _, owner := range []string{"dupes", "interrupted", "next"} {
		for _, tool := range tools[owner] {
			if tool.Status != "unresolved" || tool.OutputAvailable || tool.ResultID != "" {
				t.Fatalf("invalid ownership: %+v", tool)
			}
		}
	}
	encoded, err := json.Marshal(tools)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"private", "orphan output", "no match", "no ambiguous", "no forward"} {
		if strings.Contains(string(encoded), private) {
			t.Fatalf("leaked %q", private)
		}
	}
	again, _ := ProjectHistoryTools(messages[1:4])
	if again["first"][0].ID != first[0].ID {
		t.Fatal("presentation ID changed with page position")
	}
}

func TestProjectHistoryToolsBoundsPreferRecent(t *testing.T) {
	var messages []Message
	for i := range 70 {
		id := fmt.Sprint(i)
		owner := historyOwner("owner-"+id, "call")
		owner.Content[0].Name = strings.Repeat("界", 100)
		messages = append(messages, owner, historyResult("result-"+id, "call", strings.Repeat("界", 4000)))
	}
	tools, clipped := ProjectHistoryTools(messages)
	if !clipped || len(tools) != RPCHistoryMaxTools {
		t.Fatalf("owners=%d clipped=%v", len(tools), clipped)
	}
	for i := range 6 {
		if _, ok := tools[fmt.Sprintf("owner-%d", i)]; ok {
			t.Fatal("kept old tool")
		}
	}
	count, total := 0, 0
	for _, owner := range tools {
		for _, tool := range owner {
			count++
			total += len(tool.Output)
			if len(tool.Tool) > 128 || len(tool.Output) > 8192 || !utf8.ValidString(tool.Tool+tool.Output) || !tool.Truncated || !tool.OutputAvailable {
				t.Fatalf("invalid bound: %+v", tool)
			}
		}
	}
	if count != 64 || total > RPCHistoryMaxTotalOutputBytes || tools["owner-69"][0].Output == "" || tools["owner-6"][0].Output != "" {
		t.Fatalf("count=%d total=%d", count, total)
	}
}

func TestProjectHistoryToolsRejectsOversizedIDsAndPreservesEmptyPreview(t *testing.T) {
	huge := strings.Repeat("x", RPCHistoryMaxIDBytes+1)
	messages := []Message{historyOwner(huge, "call"), historyOwner("oversized-call", huge), historyOwner("valid", "call"), historyResult(huge, "call", "hidden"), historyOwner("empty", "call"), historyResult("empty-result", "call", "")}
	tools, clipped := ProjectHistoryTools(messages)
	if !clipped || len(tools) != 2 || tools["valid"][0].Status != "unresolved" {
		t.Fatalf("tools=%+v clipped=%v", tools, clipped)
	}
	if tool := tools["empty"][0]; !tool.OutputAvailable || tool.Output != "" || tool.Status != "completed" {
		t.Fatalf("empty=%+v", tool)
	}
}

func TestCatalogToolFieldsAreAdditive(t *testing.T) {
	legacy, err := json.Marshal(RPCCatalogMessagesParams{SessionID: "session"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(legacy), "include_tools") {
		t.Fatal(string(legacy))
	}
	page, err := json.Marshal(RPCCatalogMessagesPage{Messages: []RPCCatalogMessage{{ID: "id", Role: "assistant"}}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(page), "tools") {
		t.Fatal(string(page))
	}
	opted, err := json.Marshal(RPCCatalogMessagesParams{SessionID: "session", IncludeTools: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(opted), `"include_tools":true`) {
		t.Fatal(string(opted))
	}
}

func TestProjectHistoryToolsRecentCallsAndInvalidUTF8(t *testing.T) {
	owner := historyOwner("owner")
	// The oldest duplicate lies outside the selected 64 calls. It must still
	// make the retained duplicate ambiguous rather than claim its output.
	owner.Content = append(owner.Content, ContentBlock{Type: BlockToolCall, ToolCallID: "duplicate", Name: "old"})
	for i := range RPCHistoryMaxTools {
		owner.Content = append(owner.Content, ContentBlock{Type: BlockToolCall, ToolCallID: fmt.Sprint(i), Name: "read"})
	}
	owner.Content[len(owner.Content)-1].ToolCallID = "duplicate"
	owner.Content[len(owner.Content)-2].Name = strings.Repeat("\xff", RPCHistoryMaxNameBytes*2)
	messages := []Message{owner, historyResult("ambiguous", "duplicate", "must not match"), historyResult("invalid", "62", strings.Repeat("\xff", RPCHistoryMaxOutputBytes*2))}
	tools, clipped := ProjectHistoryTools(messages)
	got := tools[owner.ID]
	if !clipped || len(got) != RPCHistoryMaxTools || got[len(got)-1].Status != "unresolved" {
		t.Fatalf("count=%d clipped=%v", len(got), clipped)
	}
	invalid := got[len(got)-2]
	if !invalid.Truncated || len(invalid.Tool) > RPCHistoryMaxNameBytes || len(invalid.Output) > RPCHistoryMaxOutputBytes || !utf8.ValidString(invalid.Tool+invalid.Output) {
		t.Fatalf("invalid UTF-8 bound: %+v", invalid)
	}
}

func TestProjectHistoryToolsRejectsAmbiguousResultsBeforeFiltering(t *testing.T) {
	for _, scenario := range []string{"duplicate-call-result", "duplicate-result-id", "invalid-id-before-valid", "name-mismatch-before-valid", "name-mismatch-only", "oversized-name"} {
		t.Run(scenario, func(t *testing.T) {
			owner := historyOwner("owner", "first", "second")
			first := historyResult("first-result", "first", "must not become definitive")
			var results []Message
			switch scenario {
			case "duplicate-call-result":
				results = []Message{first, historyResult("duplicate", "first", "conflicting output")}
			case "duplicate-result-id":
				results = []Message{first, historyResult("first-result", "second", "reused result identity")}
			case "invalid-id-before-valid":
				invalid := historyResult(strings.Repeat("x", RPCHistoryMaxIDBytes+1), "first", "invalid identity")
				results = []Message{invalid, first}
			case "name-mismatch-before-valid":
				mismatch := historyResult("mismatch", "first", "wrong tool")
				mismatch.ToolName = "bash"
				results = []Message{mismatch, first}
			case "name-mismatch-only":
				first.ToolName = "bash"
				results = []Message{first}
			case "oversized-name":
				owner.Content[0].Name = strings.Repeat("x", RPCHistoryMaxNameBytes+1)
				first.ToolName = owner.Content[0].Name
				results = []Message{first}
			}
			tools, omitted := ProjectHistoryTools(append([]Message{owner}, results...))
			if !omitted {
				t.Fatal("did not surface omitted ambiguous result")
			}
			for _, tool := range tools[owner.ID] {
				if tool.Status != "unresolved" || tool.ResultID != "" || tool.OutputAvailable || tool.Output != "" {
					t.Fatalf("falsely definitive: %+v", tool)
				}
			}
		})
	}
}

func TestProjectHistoryToolsDuplicateOwnerIDsAreOmitted(t *testing.T) {
	messages := []Message{historyOwner("duplicate", "call"), historyResult("result", "call", "wrong owner output"), historyOwner("unique", "call"), historyResult("unique-result", "call", "unique output")}
	// A duplicate assistant with no tools is still an ambiguous owner identity.
	messages = append(messages, Message{ID: "duplicate", Role: RoleAssistant})
	tools, omitted := ProjectHistoryTools(messages)
	if !omitted || len(tools) != 1 || tools["unique"][0].Output != "unique output" {
		t.Fatalf("tools=%+v omitted=%v", tools, omitted)
	}
	for range RPCHistoryMaxTools + 1 {
		messages = append(messages, historyOwner("duplicate", "call"))
	}
	tools, omitted = ProjectHistoryTools(messages)
	if !omitted || len(tools) != 1 || tools["unique"][0].Output != "unique output" {
		t.Fatalf("duplicate owners consumed budget: %+v", tools)
	}
}

func TestProjectHistoryToolsIdentityUsesToolCallOrdinal(t *testing.T) {
	owner := historyOwner("owner", "one", "two")
	withPrivate := owner.Clone()
	withPrivate.Content = []ContentBlock{
		{Type: BlockProviderData, Data: []byte("private")},
		{Type: BlockThinking, Text: "reasoning"},
		owner.Content[0],
		{Type: BlockProviderData, Data: []byte("more private")},
		{Type: BlockText, Text: "visible"},
		owner.Content[1],
	}
	plain, _ := ProjectHistoryTools([]Message{owner})
	projected, _ := ProjectHistoryTools([]Message{withPrivate})
	for i := range plain[owner.ID] {
		if plain[owner.ID][i].ID != projected[owner.ID][i].ID {
			t.Fatal("private block stripping changed stable ID")
		}
	}
	if plain[owner.ID][0].ID == plain[owner.ID][1].ID {
		t.Fatal("tool ordinals collided")
	}
}

func TestProjectHistoryToolsEmptyLegacyResultNameRemainsCompatible(t *testing.T) {
	owner := historyOwner("owner", "legacy", "named")
	legacy := historyResult("legacy-result", "legacy", "")
	legacy.PublicToolResult = nil
	named := historyResult("named-result", "named", "explicit public")
	named.ToolName = "read"
	tools, omitted := ProjectHistoryTools([]Message{owner, legacy, named})
	if omitted || tools[owner.ID][0].Status != "completed" || tools[owner.ID][0].OutputAvailable || tools[owner.ID][1].Output != "explicit public" {
		t.Fatalf("tools=%+v omitted=%v", tools, omitted)
	}
}

func TestProjectHistoryToolsExplicitUnknownOutcomeOverridesResult(t *testing.T) {
	for _, isError := range []bool{false, true} {
		for _, preview := range []*ToolResultPreview{nil, {Text: "must not claim completion", Truncated: true}} {
			result := historyResult("recovery", "call", "")
			result.IsError, result.ToolOutcomeUnknown, result.PublicToolResult = isError, true, preview
			result.Content = []ContentBlock{NewTextBlock("private recovery instructions")}
			messages := []Message{historyOwner("owner", "call"), result}
			before, err := json.Marshal(messages)
			if err != nil {
				t.Fatal(err)
			}
			tools, truncated := ProjectHistoryTools(messages)
			tool := tools["owner"][0]
			if tool.Status != "unresolved" || tool.ResultID != "" || tool.OutputAvailable || tool.Output != "" || tool.Truncated || truncated {
				t.Fatalf("unknown outcome became definitive: %+v", tool)
			}
			after, err := json.Marshal(messages)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("projection modified recovery provenance")
			}
		}
	}
}

func TestProjectHistoryToolsDoesNotInferUnknownOutcomeFromLegacyContent(t *testing.T) {
	result := historyResult("legacy", "call", "")
	result.IsError = true
	result.PublicToolResult = nil
	result.Content = []ContentBlock{NewTextBlock("Error: the previous Snow process ended after this tool was dispatched, so its external outcome is unknown. Inspect the current state before retrying; for potentially harmful or costly repetition, ask the user first.")}
	tools, _ := ProjectHistoryTools([]Message{historyOwner("owner", "call"), result})
	if tool := tools["owner"][0]; tool.Status != "failed" || tool.ResultID != result.ID || tool.OutputAvailable || tool.Output != "" {
		t.Fatalf("unmarked legacy result reinterpreted or leaked: %+v", tool)
	}
}
