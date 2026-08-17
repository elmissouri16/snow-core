package session

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/snow-core/snow/pkg/protocol"
	_ "modernc.org/sqlite"
)

func TestSQLiteRepeatedCompactionProjectionSurvivesReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "repeated-compact.db")
	store, err := NewSQLiteStore(path, t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range []Entry{msg("old-a", "", "old a"), msg("old-b", "", "old b"), msg("tail-a", "", "tail a")} {
		if err := store.Append(entry); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Append(Entry{Type: EntryCompaction, ID: "compact-one", Summary: "first checkpoint sentinel", CompactedThrough: "old-b"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ContextMessages(); err != nil { // warm the projection cache
		t.Fatal(err)
	}
	if err := store.Append(msg("tail-b", "", "tail b")); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(Entry{Type: EntryCompaction, ID: "compact-two", Summary: "latest checkpoint sentinel", CompactedThrough: "tail-a"}); err != nil {
		t.Fatal(err)
	}
	assertLatest := func(projected []protocol.Message) {
		t.Helper()
		custom := 0
		for _, message := range projected {
			if message.Role != protocol.RoleCustom {
				continue
			}
			custom++
			text := message.Content[0].Text
			if !strings.Contains(text, "latest checkpoint sentinel") || strings.Contains(text, "first checkpoint sentinel") {
				t.Fatalf("wrong active checkpoint: %q", text)
			}
		}
		if custom != 1 || len(projected) == 0 || projected[0].ID != "compaction-compact-two" {
			t.Fatalf("projected=%+v", projected)
		}
	}
	projected, err := store.ContextMessages()
	if err != nil {
		t.Fatal(err)
	}
	assertLatest(projected)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = NewSQLiteStore(path, "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	projected, err = store.ContextMessages()
	if err != nil {
		t.Fatal(err)
	}
	assertLatest(projected)
}

func TestSQLiteContextCacheAdvancesAndPreservesOwnership(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "context-cache.db"), t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, entry := range []Entry{msg("a", "", "one"), msg("b", "", "two")} {
		if err := store.Append(entry); err != nil {
			t.Fatal(err)
		}
	}
	first, err := store.ContextMessages()
	if err != nil {
		t.Fatal(err)
	}
	if store.contextCacheKey.tip != "b" || len(store.contextCacheEntries) == 0 {
		t.Fatalf("cache key=%+v entries=%d", store.contextCacheKey, len(store.contextCacheEntries))
	}
	cachedEntries := len(store.contextCacheEntries)
	if err := store.Append(msg("c", "", "three")); err != nil {
		t.Fatal(err)
	}
	if store.contextCacheKey.tip != "c" || len(store.contextCacheEntries) != cachedEntries+1 {
		t.Fatalf("advanced cache key=%+v entries=%d, want tip c and %d entries", store.contextCacheKey, len(store.contextCacheEntries), cachedEntries+1)
	}
	first[0].Content[0].Text = "mutated"
	second, err := store.ContextMessages()
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 3 || second[0].Content[0].Text != "one" || second[2].Content[0].Text != "three" {
		t.Fatalf("cached projection was mutated or stale: %+v", second)
	}
}

func TestSQLiteContextCacheInvalidatesAtCompactionBoundary(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "context-cache-compact.db"), t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, entry := range []Entry{msg("a", "", "old"), msg("b", "", "boundary"), msg("c", "", "keep")} {
		if err := store.Append(entry); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.ContextMessages(); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(Entry{Type: EntryCompaction, ID: "compact", Summary: "summary", CompactedThrough: "b"}); err != nil {
		t.Fatal(err)
	}
	if len(store.contextCacheEntries) != 0 {
		t.Fatalf("compaction retained %d cached entries", len(store.contextCacheEntries))
	}
	projected, err := store.ContextMessages()
	if err != nil {
		t.Fatal(err)
	}
	if len(projected) != 2 || projected[0].Role != protocol.RoleCustom || projected[1].ID != "c" {
		t.Fatalf("compacted projection = %+v", projected)
	}
	if err := store.Append(msg("d", "", "after")); err != nil {
		t.Fatal(err)
	}
	if store.contextCacheKey.tip != "d" {
		t.Fatalf("post-compaction cache tip = %q", store.contextCacheKey.tip)
	}
	projected, err = store.ContextMessages()
	if err != nil || len(projected) != 3 || projected[2].ID != "d" {
		t.Fatalf("post-compaction projection = %+v, err=%v", projected, err)
	}
}

func TestSQLiteAggregateUsageAndReferenceCount(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "aggregates.db"), t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	usage := protocol.Usage{Input: 3, Output: 4, Total: 7}
	assistant := protocol.NewAssistantMessage("assistant", "", "p", "m", []protocol.ContentBlock{protocol.NewTextBlock("done")}, protocol.StopStop, &usage)
	if err := store.Append(Entry{Type: EntryMessage, ID: assistant.ID, Message: &assistant}); err != nil {
		t.Fatal(err)
	}
	tool := protocol.NewToolResultMessage("reference", "", "call", "session_reference", []protocol.ContentBlock{protocol.NewTextBlock(`{"source_session_id":"prior"}`)}, false)
	if err := store.Append(Entry{Type: EntryMessage, ID: tool.ID, Message: &tool}); err != nil {
		t.Fatal(err)
	}
	gotUsage, err := store.AggregateUsage()
	if err != nil || gotUsage.Input != 3 || gotUsage.Output != 4 || gotUsage.Total != 7 {
		t.Fatalf("usage=%+v err=%v", gotUsage, err)
	}
	count, err := store.CountSessionReferences()
	if err != nil || count != 1 {
		t.Fatalf("reference count=%d err=%v", count, err)
	}
}

func TestSQLiteContextProjectionDoesNotDecodeCompactedPrefix(t *testing.T) {
	path := filepath.Join(t.TempDir(), "compact-prefix.db")
	store, err := NewSQLiteStore(path, t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, entry := range []Entry{msg("a", "", "old"), msg("b", "", "boundary"), msg("c", "", "keep")} {
		if err := store.Append(entry); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Append(Entry{Type: EntryCompaction, Summary: "summary", CompactedThrough: "b"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE entries SET message='{bad json' WHERE id='a'`); err != nil {
		t.Fatal(err)
	}
	projected, err := store.ContextMessages()
	if err != nil || len(projected) != 2 || projected[1].ID != "c" {
		t.Fatalf("projection=%+v err=%v", projected, err)
	}
}

func TestSQLiteContextProjectionClampsUnknownBoundary(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "unknown-boundary.db"), t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Append(msg("a", "", "old")); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(Entry{Type: EntryCompaction, Summary: "safe", CompactedThrough: "missing"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE entries SET message='{bad json' WHERE id='a'`); err != nil {
		t.Fatal(err)
	}
	projected, err := store.ContextMessages()
	if err != nil || len(projected) != 1 || projected[0].Role != protocol.RoleCustom {
		t.Fatalf("projection=%+v err=%v", projected, err)
	}
}

func TestDeleteBranchForRollbackRemovesSubagentRows(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "rollback.db"), t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Append(msg("a", "", "one")); err != nil {
		t.Fatal(err)
	}
	branch, err := store.ForkBranch("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SelectBranch("main"); err != nil {
		t.Fatal(err)
	}
	record := testSubagentRecord()
	record.ParentBranchID = branch.ID
	if err := store.PutSubagent(record); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteBranchForRollback(branch.ID); err != nil {
		t.Fatal(err)
	}
	list, err := store.ListSubagents()
	if err != nil || len(list) != 0 {
		t.Fatalf("subagents=%+v err=%v", list, err)
	}
}

func TestSQLiteEmptySessionIsNotSaved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.db")
	st, err := NewSQLiteStore(path, "/tmp/work", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected database while session is open: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		if _, err := os.Stat(path + suffix); !os.IsNotExist(err) {
			t.Fatalf("empty session file %q still exists (err=%v)", path+suffix, err)
		}
	}
}

func TestSQLiteRejectsEmptyExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.db")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSQLiteStore(path, "", Options{}); err == nil {
		t.Fatal("expected empty existing database to be rejected")
	}
}

func TestSQLiteRejectsSchemaWithoutMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "incomplete.db")
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE unrelated (id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSQLiteStore(path, "", Options{}); err == nil {
		t.Fatal("expected existing schema without metadata to be rejected")
	}
}

func TestSQLiteRejectsJSONLFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.jsonl")
	if err := os.WriteFile(path, []byte("not sqlite\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSQLiteStore(path, "", Options{}); err == nil {
		t.Fatal("expected non-SQLite session to be rejected")
	}
}

func TestSQLiteMetadataRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.db")
	st, err := NewSQLiteStore(path, "/tmp/work", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Append(msg("message", "", "keep this session")); err != nil {
		t.Fatal(err)
	}
	if err := st.SetMetadata("permission_state", `{"mode":"ask","rules":{"bash|exec":"allow"}}`); err != nil {
		t.Fatal(err)
	}
	value, ok, err := st.Metadata("permission_state")
	if err != nil || !ok || value == "" {
		t.Fatalf("metadata = %q, %v, %v", value, ok, err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = NewSQLiteStore(path, "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	value, ok, err = st.Metadata("permission_state")
	if err != nil || !ok || value != `{"mode":"ask","rules":{"bash|exec":"allow"}}` {
		t.Fatalf("reopened metadata = %q, %v, %v", value, ok, err)
	}
}

func TestSQLiteStoresNonMessageEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.db")
	st, err := NewSQLiteStore(path, "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Append(Entry{ID: "meta", Type: EntryMeta, Key: "mode", Value: "test"}); err != nil {
		t.Fatal(err)
	}
	m := protocol.NewUserMessage("message", "", "hello")
	if err := st.Append(Entry{ID: "message", Type: EntryMessage, Message: &m}); err != nil {
		t.Fatal(err)
	}
	messages, err := st.Messages()
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].Content[0].Text != "hello" {
		t.Fatalf("messages = %+v", messages)
	}
	entries, err := st.BranchEntries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 || entries[1].Type != EntryMeta || entries[1].Key != "mode" || entries[1].Value != "test" || entries[2].Type != EntryMessage {
		t.Fatalf("branch entries = %+v", entries)
	}
}
