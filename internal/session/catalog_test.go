package session

import (
	"context"
	"crypto/sha256"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func catalogFixture(t *testing.T, root, cwd, name string, messages ...protocol.Message) (string, string) {
	t.Helper()
	store, err := NewFileIndex(root).Create(cwd)
	if err != nil {
		t.Fatal(err)
	}
	s := store.(*SQLiteStore)
	if name != "" {
		if err := s.RenameSession(name); err != nil {
			t.Fatal(err)
		}
	}
	for _, message := range messages {
		if err := s.Append(Entry{Type: EntryMessage, ID: message.ID, Message: &message}); err != nil {
			t.Fatal(err)
		}
	}
	id, path := s.ID(), s.Path()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	return id, path
}

type catalogFileSnapshot struct {
	Hash     [32]byte
	Mode     fs.FileMode
	Size     int64
	Modified time.Time
}

func catalogSnapshot(t *testing.T, root string) map[string]catalogFileSnapshot {
	t.Helper()
	result := map[string]catalogFileSnapshot{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		var data []byte
		if info.Mode().IsRegular() {
			data, err = os.ReadFile(path)
			if err != nil {
				return err
			}
		}
		result[path] = catalogFileSnapshot{sha256.Sum256(data), info.Mode(), info.Size(), info.ModTime()}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestCatalogReadOnlyProjectPagesAndSafeHistory(t *testing.T) {
	root, cwd := t.TempDir(), t.TempDir()
	user := protocol.NewUserMessage("user", "", "hello")
	assistant := protocol.Message{ID: "assistant", Role: protocol.RoleAssistant, Timestamp: 17, Content: []protocol.ContentBlock{
		{Type: protocol.BlockThinking, Text: "private thinking"},
		{Type: protocol.BlockText, Text: "visible"},
		{Type: protocol.BlockProviderData, Text: "private continuity"},
		{Type: protocol.BlockToolCall, Name: "private tool", Arguments: []byte(`{"secret":"private arguments"}`)},
		{Type: protocol.BlockImage, Data: []byte("private image")},
	}}
	tool := protocol.Message{ID: "tool", Role: protocol.RoleTool, Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: "private tool output"}}}
	id, _ := catalogFixture(t, root, cwd, "first", user, assistant, tool)
	catalogFixture(t, root, cwd, "second", protocol.NewUserMessage("other", "", "other"))
	foreignID, foreignPath := catalogFixture(t, root, t.TempDir(), "foreign", user)
	// A foreign database in the project's own directory still fails stored-CWD authorization.
	foreignCopy := filepath.Join(root, EncodeCWD(cwd), "foreign.db")
	data, err := os.ReadFile(foreignPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(foreignCopy, data, 0o600); err != nil {
		t.Fatal(err)
	}
	// Give the copied database a safe lease so stored-CWD validation, not
	// missing-lease exclusion, is the reason this candidate is rejected.
	if err := os.WriteFile(foreignCopy+".lock", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	before := catalogSnapshot(t, root)
	catalog := NewCatalog(root, cwd)
	first, err := catalog.Sessions(t.Context(), 0, 1)
	if err != nil || len(first.Sessions) != 1 || !first.HasMore || first.NextOffset != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := catalog.Sessions(t.Context(), 1, 1)
	if err != nil || len(second.Sessions) != 1 || second.HasMore || first.Sessions[0].SessionID == second.Sessions[0].SessionID {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	messages, err := catalog.Messages(t.Context(), id, 0, 1)
	if err != nil || len(messages.Messages) != 1 || messages.Messages[0].Text != "hello" || !messages.HasMore {
		t.Fatalf("messages=%+v err=%v", messages, err)
	}
	messages, err = catalog.Messages(t.Context(), id, 1, 50)
	if err != nil || len(messages.Messages) != 1 || messages.Messages[0].Text != "visible" || messages.Messages[0].Timestamp != 17 || messages.HasMore {
		t.Fatalf("messages=%+v err=%v", messages, err)
	}
	if _, err := catalog.Messages(t.Context(), foreignID, 0, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign error=%v", err)
	}
	if _, err := catalog.Messages(t.Context(), foreignPath, 0, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("path selector error=%v", err)
	}
	after := catalogSnapshot(t, root)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("catalog changed session files, directories, permissions, timestamps or sidecars")
	}
}

func TestCatalogMissingRootAndPaginationValidation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing")
	catalog := NewCatalog(root, t.TempDir())
	page, err := catalog.Sessions(t.Context(), 0, 0)
	if err != nil || len(page.Sessions) != 0 || page.HasMore {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	if _, err := os.Stat(root); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("root created: %v", err)
	}
	for _, params := range [][2]int{{-1, 1}, {0, -1}, {0, 51}, {maxSessionQueryDepth + 1, 1}} {
		if _, err := catalog.Sessions(t.Context(), params[0], params[1]); err == nil {
			t.Fatalf("accepted %v", params)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	// Existing roots observe cancellation before any database query.
	if _, err := NewCatalog(t.TempDir(), t.TempDir()).Sessions(ctx, 0, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
}

func TestCatalogExcludesActiveChildrenSymlinksAndRecovery(t *testing.T) {
	root, cwd := t.TempDir(), t.TempDir()
	active, err := NewFileIndex(root).Create(cwd)
	if err != nil {
		t.Fatal(err)
	}
	defer active.Close()
	if err := active.Append(Entry{Type: EntryMessage, ID: "active", Message: new(protocol.NewUserMessage("active", "", "active"))}); err != nil {
		t.Fatal(err)
	}
	_, path := catalogFixture(t, root, cwd, "inactive", protocol.NewUserMessage("u", "", "ok"))
	child, err := NewSQLiteStore(path+".agents/child.db", cwd, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := child.RenameSession("child"); err != nil {
		t.Fatal(err)
	}
	if err := child.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path, filepath.Join(filepath.Dir(path), "alias.db")); err != nil {
		t.Fatal(err)
	}
	catalog := NewCatalog(root, cwd)
	page, err := catalog.Sessions(t.Context(), 0, 50)
	if err != nil || len(page.Sessions) != 1 || page.Sessions[0].Name != "inactive" {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	if err := os.WriteFile(path+"-wal", []byte("recovery-needed"), 0o600); err != nil {
		t.Fatal(err)
	}
	page, err = catalog.Sessions(t.Context(), 0, 50)
	if err != nil || len(page.Sessions) != 0 {
		t.Fatalf("recovery page=%+v err=%v", page, err)
	}
}

func TestCatalogBoundsTextAndOmitsOversizedMessagePayload(t *testing.T) {
	root, cwd := t.TempDir(), t.TempDir()
	id, _ := catalogFixture(t, root, cwd, "large",
		protocol.NewUserMessage("u", "", strings.Repeat("😀", protocol.RPCCatalogMaxTextBytes)),
		protocol.NewUserMessage("huge", "", strings.Repeat("x", catalogMaxMessageBytes+1)),
		protocol.NewUserMessage("last", "", "last"))
	catalog := NewCatalog(root, cwd)
	first, err := catalog.Messages(t.Context(), id, 0, 50)
	if err != nil || len(first.Messages) != 1 || len(first.Messages[0].Text) > protocol.RPCCatalogMaxTextBytes || !first.Messages[0].Truncated || !first.HasMore || first.NextOffset != 1 {
		t.Fatalf("first messages=%d next=%d more=%v err=%v", len(first.Messages), first.NextOffset, first.HasMore, err)
	}
	second, err := catalog.Messages(t.Context(), id, first.NextOffset, 50)
	if err != nil || len(second.Messages) != 2 || !second.Messages[0].Truncated || second.Messages[0].Text != "" || second.Messages[1].Text != "last" || second.HasMore {
		t.Fatalf("second=%+v err=%v", second, err)
	}
}

func TestCatalogSavedBranchAndCompactionPreserveDisplayHistory(t *testing.T) {
	root, cwd := t.TempDir(), t.TempDir()
	store, err := NewFileIndex(root).Create(cwd)
	if err != nil {
		t.Fatal(err)
	}
	s := store.(*SQLiteStore)
	for _, entry := range []Entry{
		{Type: EntryMessage, ID: "user", Message: new(protocol.NewUserMessage("user", "", "old user"))},
		{Type: EntryCompaction, ID: "checkpoint", Summary: "private working state", CompactedThrough: "user"},
		{Type: EntryMessage, ID: "answer", Message: &protocol.Message{Role: protocol.RoleAssistant, Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: "answer"}}}},
	} {
		if err := s.Append(entry); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetBranchTip("user"); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(Entry{Type: EntryMessage, ID: "alternate", Message: new(protocol.NewUserMessage("alternate", "", "other branch"))}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetBranchTip("answer"); err != nil {
		t.Fatal(err)
	}
	id := s.ID()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	page, err := NewCatalog(root, cwd).Messages(t.Context(), id, 0, 50)
	if err != nil || len(page.Messages) != 2 || page.Messages[0].Text != "old user" || page.Messages[1].Text != "answer" {
		t.Fatalf("page=%+v err=%v", page, err)
	}
}

func TestCatalogRejectsReplacedAbsoluteRootAndSymlinkedAncestor(t *testing.T) {
	parent, cwd := t.TempDir(), t.TempDir()
	root := filepath.Join(parent, "sessions")
	_, path := catalogFixture(t, root, cwd, "original", protocol.NewUserMessage("u", "", "original"))
	pinned, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer pinned.Close()
	rel, err := filepath.Rel(root, path)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := pinned.Lstat(rel)
	if err != nil {
		t.Fatal(err)
	}
	catalog := NewCatalog(root, cwd)
	if err := catalog.validateCatalogPath(pinned, rel, expected); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(root, root+"-old"); err != nil {
		t.Fatal(err)
	}
	catalogFixture(t, root, cwd, "replacement", protocol.NewUserMessage("u", "", "replacement"))
	if err := catalog.validateCatalogPath(pinned, rel, expected); err == nil {
		t.Fatal("accepted replaced absolute root")
	}
	if _, cleanup, _, err := catalog.open(t.Context(), pinned, rel); err == nil {
		cleanup()
		t.Fatal("opened replacement root database")
	}

	root2 := t.TempDir()
	if err := os.Symlink(filepath.Join(root, EncodeCWD(cwd)), filepath.Join(root2, EncodeCWD(cwd))); err != nil {
		t.Fatal(err)
	}
	page, err := NewCatalog(root2, cwd).Sessions(t.Context(), 0, 50)
	if len(page.Sessions) != 0 {
		t.Fatalf("followed project directory symlink: %+v %v", page, err)
	}
}
