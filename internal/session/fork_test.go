package session

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestFileIndexCreateForkMaterializesIndependentSession(t *testing.T) {
	root := t.TempDir()
	cwd := t.TempDir()
	index := NewFileIndex(root)
	source, err := index.Create(cwd)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()

	user := Entry{Type: EntryMessage, ID: "user-1", Message: new(protocol.NewUserMessage("user-1", "root", "design one"))}
	assistant := Entry{Type: EntryMessage, ID: "assistant-1", Message: new(protocol.NewAssistantMessage("assistant-1", "user-1", "fake", "fake-1", []protocol.ContentBlock{protocol.NewTextBlock("answer one")}, protocol.StopStop, nil))}
	if err := source.(BatchStore).AppendBatch([]Entry{user, assistant}); err != nil {
		t.Fatal(err)
	}
	if err := source.(MetadataStore).SetMetadata("after", "source-only"); err != nil {
		t.Fatal(err)
	}
	if err := source.(ThreadStateStore).SetCollaborationMode(protocol.ModePlan); err != nil {
		t.Fatal(err)
	}

	forked, result, err := index.CreateFork(cwd, source, protocol.SessionForkOptions{FromEntryID: "assistant-1", Name: "Independent fork"})
	if err != nil {
		t.Fatal(err)
	}
	defer forked.Close()
	if result.SessionID == source.ID() || result.SessionPath == source.Path() {
		t.Fatalf("fork identity not independent: %+v", result)
	}
	stagingPattern := filepath.Join(filepath.Dir(result.SessionPath), "."+filepath.Base(result.SessionPath)+".tmp-*")
	if staging, err := filepath.Glob(stagingPattern); err != nil || len(staging) != 0 {
		t.Fatalf("fork staging files=%v err=%v", staging, err)
	}
	if result.SourceSessionID != source.ID() || result.SourceBranchID != "main" || result.SourceEntryID != "assistant-1" {
		t.Fatalf("source provenance = %+v", result)
	}
	header := forked.Header()
	if header.ParentSessionID != source.ID() || header.ParentBranchID != "main" || header.ForkEntryID != "assistant-1" {
		t.Fatalf("header provenance = %+v", header)
	}
	messages, err := forked.Messages()
	if err != nil || len(messages) != 2 || messages[1].ID != "assistant-1" {
		t.Fatalf("fork messages = %+v, err=%v", messages, err)
	}
	if value, ok, err := forked.(MetadataStore).Metadata("after"); err != nil || ok || value != "" {
		t.Fatalf("post-boundary metadata leaked: value=%q ok=%v err=%v", value, ok, err)
	}
	if mode, err := forked.(ThreadStateStore).CollaborationMode(); err != nil || mode != protocol.ModeDefault {
		t.Fatalf("historical fork mode = %q, err=%v", mode, err)
	}

	childMessage := Entry{Type: EntryMessage, ID: "child-user", Message: new(protocol.NewUserMessage("child-user", "assistant-1", "child only"))}
	if err := forked.Append(childMessage); err != nil {
		t.Fatal(err)
	}
	sourceMessages, err := source.Messages()
	if err != nil || len(sourceMessages) != 2 {
		t.Fatalf("source changed after child append: messages=%d err=%v", len(sourceMessages), err)
	}

	path := result.SessionPath
	if err := forked.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenSQLiteStore(path, cwd, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got := reopened.Header(); got.ParentSessionID != source.ID() || got.ForkEntryID != "assistant-1" {
		t.Fatalf("reopened provenance = %+v", got)
	}
}

func TestCreateForkHistoricalEntryUsesDefaultMode(t *testing.T) {
	root := t.TempDir()
	cwd := t.TempDir()
	index := NewFileIndex(root)
	source := NewMemoryStore(Options{CWD: cwd, Name: "source"})
	defer source.Close()
	if err := source.Append(Entry{Type: EntryMessage, ID: "u1", Message: new(protocol.NewUserMessage("u1", "root", "one"))}); err != nil {
		t.Fatal(err)
	}
	if err := source.Append(Entry{Type: EntryMessage, ID: "a1", Message: new(protocol.NewAssistantMessage("a1", "u1", "fake", "fake-1", []protocol.ContentBlock{protocol.NewTextBlock("one")}, protocol.StopStop, nil))}); err != nil {
		t.Fatal(err)
	}
	if err := source.Append(Entry{Type: EntryMessage, ID: "u2", Message: new(protocol.NewUserMessage("u2", "a1", "two"))}); err != nil {
		t.Fatal(err)
	}
	if err := source.SetCollaborationMode(protocol.ModePlan); err != nil {
		t.Fatal(err)
	}
	forked, _, err := index.CreateFork(cwd, source, protocol.SessionForkOptions{FromEntryID: "a1"})
	if err != nil {
		t.Fatal(err)
	}
	defer forked.Close()
	if mode, err := forked.(ThreadStateStore).CollaborationMode(); err != nil || mode != protocol.ModeDefault {
		t.Fatalf("historical mode = %q, err=%v", mode, err)
	}
	current, _, err := index.CreateFork(cwd, source, protocol.SessionForkOptions{FromEntryID: "u2", Name: "current"})
	if err != nil {
		t.Fatal(err)
	}
	defer current.Close()
	if mode, err := current.(ThreadStateStore).CollaborationMode(); err != nil || mode != protocol.ModePlan {
		t.Fatalf("current-tip mode = %q, err=%v", mode, err)
	}
}

func TestCreateForkRejectsIncompleteToolBoundaryAndDestinationCollision(t *testing.T) {
	root := t.TempDir()
	cwd := t.TempDir()
	index := NewFileIndex(root)
	source := NewMemoryStore(Options{CWD: cwd})
	defer source.Close()
	assistant := protocol.NewAssistantMessage("a1", "root", "fake", "fake-1", []protocol.ContentBlock{{Type: protocol.BlockToolCall, ToolCallID: "call-1", Name: "read"}}, protocol.StopToolUse, nil)
	if err := source.Append(Entry{Type: EntryMessage, ID: "a1", Message: &assistant}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := index.CreateFork(cwd, source, protocol.SessionForkOptions{FromEntryID: "a1"}); !errors.Is(err, ErrInvalidForkBoundary) {
		t.Fatalf("incomplete fork error = %v", err)
	}

	destination := filepath.Join(t.TempDir(), "existing.db")
	if err := os.WriteFile(destination, []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := index.CreateFork(cwd, source, protocol.SessionForkOptions{FromEntryID: "root", DestinationPath: destination}); !errors.Is(err, ErrDestinationExists) {
		t.Fatalf("collision error = %v", err)
	}
}

func TestForkArtifactIDsRecognizesOnlyRetainedMarkersInCheckpoint(t *testing.T) {
	store := NewMemoryStore(Options{})
	trusted := "artifact-0123456789abcdef0123456789abcdef"
	untrusted := "artifact-ffffffffffffffffffffffffffffffff"
	if err := store.Append(Entry{Type: EntryCompaction, ID: "checkpoint", Summary: "# Working State Checkpoint\n\nuser text " + untrusted + "\nFull retained tool result: " + trusted, CompactedThrough: "root"}); err != nil {
		t.Fatal(err)
	}
	ids, err := ForkArtifactIDs(store)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != trusted {
		t.Fatalf("fork artifact IDs=%v", ids)
	}
}

func TestForkArtifactIDsOrdersByLatestOccurrence(t *testing.T) {
	store := NewMemoryStore(Options{})
	first := "artifact-0123456789abcdef0123456789abcdef"
	second := "artifact-ffffffffffffffffffffffffffffffff"
	for index, value := range []string{first, second, first} {
		id := fmt.Sprintf("m%d", index)
		parent := store.BranchTip()
		message := protocol.NewUserMessage(id, parent, "Full retained tool result: "+value)
		if err := store.Append(Entry{Type: EntryMessage, ID: id, ParentID: parent, Message: &message}); err != nil {
			t.Fatal(err)
		}
	}
	ids, err := ForkArtifactIDs(store)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != second || ids[1] != first {
		t.Fatalf("artifact IDs by latest occurrence=%v", ids)
	}
}
