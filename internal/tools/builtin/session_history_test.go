package builtin

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestSessionHistoryBindingFollowsSessionSwitch(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	idx := session.NewFileIndex(root)
	source, err := idx.Create(project)
	if err != nil {
		t.Fatal(err)
	}
	message := protocol.NewUserMessage("", "", "switchable history")
	if err := source.Append(session.Entry{Type: session.EntryMessage, Message: &message}); err != nil {
		t.Fatal(err)
	}
	sourceID, tip := source.ID(), source.BranchTip()
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}

	first := session.NewMemoryStore(session.Options{CWD: project, ID: "first"})
	second := session.NewMemoryStore(session.Options{CWD: project, ID: "second"})
	binding := NewSessionBinding(first)
	binding.Set(second)
	engine := session.NewQueryEngine(idx, project)

	search := NewSessionSearch(engine, binding)
	result, err := search.Run(context.Background(), json.RawMessage(`{"query":"switchable"}`), nil)
	if err != nil || result.IsError || !strings.Contains(result.Content[0].Text, sourceID) {
		t.Fatalf("search after switch = %+v, %v", result, err)
	}

	reference := NewSessionReference(engine, binding)
	selfArgs, _ := json.Marshal(map[string]any{"session_id": second.ID(), "branch_id": "main", "tip_id": second.BranchTip()})
	result, err = reference.Run(context.Background(), selfArgs, nil)
	if err != nil || !result.IsError || !strings.Contains(result.Content[0].Text, "cannot reference itself") {
		t.Fatalf("self reference after switch = %+v, %v", result, err)
	}

	args, _ := json.Marshal(map[string]any{"session_id": sourceID, "branch_id": "main", "tip_id": tip, "max_bytes": 4096})
	result, err = reference.Run(context.Background(), args, nil)
	if err != nil || result.IsError {
		t.Fatalf("source reference after switch = %+v, %v", result, err)
	}
}

func TestSessionHistoryToolsSearchCaptureAndLimit(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	idx := session.NewFileIndex(root)
	prior, err := idx.Create(project)
	if err != nil {
		t.Fatal(err)
	}
	message := protocol.NewUserMessage("", "", "remember the aurora decision")
	if err := prior.Append(session.Entry{Type: session.EntryMessage, Message: &message}); err != nil {
		t.Fatal(err)
	}
	priorID, tip := prior.ID(), prior.BranchTip()
	if err := prior.Close(); err != nil {
		t.Fatal(err)
	}
	current := session.NewMemoryStore(session.Options{CWD: project, ID: "current"})
	engine := session.NewQueryEngine(idx, project)

	binding := NewSessionBinding(current)
	search := NewSessionSearch(engine, binding)
	result, err := search.Run(context.Background(), json.RawMessage(`{"query":"aurora"}`), nil)
	if err != nil || result.IsError || !strings.Contains(result.Content[0].Text, priorID) {
		t.Fatalf("search result = %+v, %v", result, err)
	}

	reference := NewSessionReference(engine, binding)
	args, _ := json.Marshal(map[string]any{"session_id": priorID, "branch_id": "main", "tip_id": tip, "max_bytes": 4096})
	for i := range maxSessionReferencesPerBranch {
		result, err = reference.Run(context.Background(), args, nil)
		if err != nil || result.IsError {
			t.Fatalf("reference %d = %+v, %v", i, result, err)
		}
		persisted := protocol.NewToolResultMessage("", "", "call", "session_reference", result.Content, false)
		if err := current.Append(session.Entry{Type: session.EntryMessage, Message: &persisted}); err != nil {
			t.Fatal(err)
		}
	}
	result, err = reference.Run(context.Background(), args, nil)
	if err != nil || !result.IsError || !strings.Contains(result.Content[0].Text, "maximum") {
		t.Fatalf("fourth reference = %+v, %v", result, err)
	}
}
