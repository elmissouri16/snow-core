package session

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
	_ "modernc.org/sqlite"
)

func hydrationStores(t *testing.T) map[string]Store {
	t.Helper()
	dir := t.TempDir()
	sqliteStore, err := NewSQLiteStore(filepath.Join(dir, "hydration.db"), dir, Options{ID: "sqlite-hydration"})
	if err != nil {
		t.Fatal(err)
	}
	jsonlStore, err := NewJSONLStore(filepath.Join(dir, "hydration.jsonl"), dir, Options{ID: "jsonl-hydration"})
	if err != nil {
		t.Fatal(err)
	}
	return map[string]Store{
		"memory": NewMemoryStore(Options{ID: "memory-hydration"}),
		"sqlite": sqliteStore,
		"jsonl":  jsonlStore,
	}
}

func appendHydrationFixture(t *testing.T, store Store) {
	t.Helper()
	usage := &protocol.Usage{Input: 120, Output: 30, Total: 150}
	user := protocol.NewUserContentMessage("user-1", "", []protocol.ContentBlock{
		{Type: protocol.BlockText, Text: "  first\n"},
		{Type: protocol.BlockImage, MIMEType: "image/png", Data: []byte("image")},
		{Type: protocol.BlockText, Text: "input  "},
	})
	assistant := protocol.NewAssistantMessage("assistant-1", "", "fake", "fake-model", []protocol.ContentBlock{
		{Type: protocol.BlockThinking, Text: "thinking"},
		{Type: protocol.BlockText, Text: "answer"},
		{Type: protocol.BlockPlan, Text: "old plan"},
		{Type: protocol.BlockToolCall, ToolCallID: "call-1", Name: "bash", Arguments: json.RawMessage(`{"command":"echo hi"}`)},
	}, protocol.StopToolUse, usage)
	tool := protocol.NewToolResultMessage("tool-1", "", "call-1", "bash", []protocol.ContentBlock{protocol.NewTextBlock("hi")}, false)
	tool.ToolDisplay = &protocol.ToolDisplay{Started: true, StartMessage: "echo hi", Output: "hi", DurationMS: 2}
	second := protocol.NewUserMessage("user-2", "", "second input")
	plan := protocol.NewAssistantMessage("assistant-2", "", "fake", "fake-model", []protocol.ContentBlock{
		{Type: protocol.BlockPlan, Text: "latest plan"},
	}, protocol.StopStop, nil)
	transcript, err := json.Marshal(protocol.ToolTranscript{
		ToolName: "activate_skill",
		Display:  protocol.ToolDisplay{Started: true, StartMessage: "review"},
	})
	if err != nil {
		t.Fatal(err)
	}
	entries := []Entry{
		{Type: EntryMessage, ID: user.ID, Message: &user},
		{Type: EntryMessage, ID: assistant.ID, Message: &assistant},
		{Type: EntryMessage, ID: tool.ID, Message: &tool},
		{Type: EntryCompaction, ID: "compact", Summary: "checkpoint", CompactedThrough: user.ID},
		{Type: EntryMessage, ID: second.ID, Message: &second},
		{Type: EntryMessage, ID: plan.ID, Message: &plan},
		{Type: EntryMeta, ID: "skill-meta", Key: MetaToolTranscript, Value: string(transcript)},
	}
	for _, entry := range entries {
		if err := store.Append(entry); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBranchHydrationMatchesBuiltInStores(t *testing.T) {
	stores := hydrationStores(t)
	var reference BranchHydrationSnapshot
	for _, name := range []string{"memory", "sqlite", "jsonl"} {
		store := stores[name]
		t.Run(name, func(t *testing.T) {
			t.Cleanup(func() { _ = store.Close() })
			appendHydrationFixture(t, store)
			snapshot, err := store.(BranchHydrationStore).BranchHydration()
			if err != nil {
				t.Fatal(err)
			}
			if got, want := snapshot.UserInputs, []string{"  first\ninput  ", "second input"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("inputs=%q want=%q", got, want)
			}
			if snapshot.LatestPlan != "latest plan" {
				t.Fatalf("latest plan=%q", snapshot.LatestPlan)
			}
			if !snapshot.ContextUsage.Compacted || snapshot.ContextUsage.EstimatedChars == 0 {
				t.Fatalf("context usage=%+v", snapshot.ContextUsage)
			}
			if len(snapshot.Entries) != 8 || snapshot.Entries[0].ID != "root" || snapshot.Entries[len(snapshot.Entries)-1].ID != "skill-meta" {
				t.Fatalf("entry order=%+v", snapshot.Entries)
			}
			for _, summary := range snapshot.Entries {
				if summary.ID == "tool-1" && !summary.ToolDisplayPresent {
					t.Fatal("durable tool display presence was not projected")
				}
			}
			if name == "memory" {
				reference = snapshot
				return
			}
			if !reflect.DeepEqual(snapshot, reference) {
				t.Fatalf("snapshot differs from memory\n got: %#v\nwant: %#v", snapshot, reference)
			}
		})
	}
}

func TestBranchHydrationPageAndLookupAreBoundedAndDefensive(t *testing.T) {
	for name, store := range hydrationStores(t) {
		t.Run(name, func(t *testing.T) {
			t.Cleanup(func() { _ = store.Close() })
			appendHydrationFixture(t, store)
			snapshot, err := store.(BranchHydrationStore).BranchHydration()
			if err != nil {
				t.Fatal(err)
			}
			pager := store.(BranchEntryPager)
			page, err := pager.BranchEntryPage(snapshot.TipID, 2)
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Entries) != 2 || page.Entries[0].ID != "assistant-2" || page.Entries[1].ID != "skill-meta" || page.OlderCursor != "user-2" {
				t.Fatalf("page=%+v", page)
			}
			if _, err := pager.BranchEntryPage(snapshot.TipID, 0); err == nil {
				t.Fatal("zero page limit accepted")
			}
			lookup := store.(BranchEntryLookup)
			entries, err := lookup.BranchEntriesByID([]string{"user-1", "assistant-2"})
			if err != nil || len(entries) != 2 {
				t.Fatalf("lookup=%+v err=%v", entries, err)
			}
			entries[0].Message.Content[0].Text = "mutated"
			again, err := lookup.BranchEntriesByID([]string{"user-1"})
			if err != nil || again[0].Message.Content[0].Text != "  first\n" {
				t.Fatalf("lookup aliases store: %+v err=%v", again, err)
			}
			if _, err := lookup.BranchEntriesByID([]string{"missing"}); err == nil {
				t.Fatal("missing entry lookup succeeded")
			}
		})
	}
}

func TestHydrationProjectionSupportsLargeToolCallSets(t *testing.T) {
	record := entryHydrationRecord{summary: BranchEntrySummary{
		Type: EntryMessage, Role: protocol.RoleAssistant,
		ToolCallIDs: make([]string, 5000),
	}}
	for i := range record.summary.ToolCallIDs {
		record.summary.ToolCallIDs[i] = "call"
	}
	projection, err := marshalHydrationProjection(record)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := unmarshalHydrationProjection(projection)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.summary.ToolCallIDs) != len(record.summary.ToolCallIDs) {
		t.Fatalf("tool calls=%d want=%d", len(decoded.summary.ToolCallIDs), len(record.summary.ToolCallIDs))
	}
}

func TestHydrationProjectionDecodeOmitsOpaqueBlockData(t *testing.T) {
	message := protocol.NewUserContentMessage("image", "root", []protocol.ContentBlock{
		{Type: protocol.BlockImage, MIMEType: "image/png", Data: []byte(strings.Repeat("x", 1<<20))},
		{Type: protocol.BlockText, Text: "describe this"},
	})
	raw, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	entry := Entry{Type: EntryMessage, ID: message.ID}
	decoded, err := decodeHydrationProjectionRecord(raw, entry)
	if err != nil {
		t.Fatal(err)
	}
	entry.Message = &message
	expected := summarizeHydrationEntry(entry)
	if !reflect.DeepEqual(decoded, expected) {
		t.Fatalf("lightweight projection mismatch:\n got %+v\nwant %+v", decoded, expected)
	}
	projection, err := marshalHydrationProjection(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(projection) > 1024 {
		t.Fatalf("opaque image leaked into projection: %d bytes", len(projection))
	}
}

func TestSQLiteHydrationProjectionVersionMigratesIndependently(t *testing.T) {
	cwd := t.TempDir()
	path := filepath.Join(t.TempDir(), "projection-version.db")
	store, err := NewSQLiteStore(path, cwd, Options{})
	if err != nil {
		t.Fatal(err)
	}
	message := protocol.NewUserMessage("version-user", store.BranchTip(), "versioned")
	if err := store.Append(Entry{Type: EntryMessage, ID: message.ID, Message: &message}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE entry_hydration_projection SET projection_version=1; UPDATE session_meta SET hydration_projection_version=1`); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenSQLiteStore(path, cwd, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var stale int
	if err := reopened.db.QueryRow(`SELECT count(*) FROM entry_hydration_projection WHERE projection_version<>?`, entryHydrationProjectionVersion).Scan(&stale); err != nil {
		t.Fatal(err)
	}
	if stale != 0 {
		t.Fatalf("stale projection rows=%d", stale)
	}
	snapshot, err := reopened.BranchHydration()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snapshot.UserInputs, []string{"versioned"}) {
		t.Fatalf("inputs=%v", snapshot.UserInputs)
	}
}

func TestSQLiteHydrationProjectionRepairsStaleRowsOnDemand(t *testing.T) {
	cwd := t.TempDir()
	path := filepath.Join(t.TempDir(), "repair.db")
	store, err := NewSQLiteStore(path, cwd, Options{})
	if err != nil {
		t.Fatal(err)
	}
	message := protocol.NewUserMessage("repair-user", store.BranchTip(), "repair me")
	if err := store.Append(Entry{Type: EntryMessage, ID: message.ID, Message: &message}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE entry_hydration_projection SET projection_version=1 WHERE entry_id=?`, message.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenSQLiteStore(path, cwd, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var version int
	if err := reopened.db.QueryRow(`SELECT projection_version FROM entry_hydration_projection WHERE entry_id=?`, message.ID).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 1 {
		t.Fatalf("open eagerly scanned projections: version=%d", version)
	}
	snapshot, err := reopened.BranchHydration()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snapshot.UserInputs, []string{"repair me"}) {
		t.Fatalf("inputs=%v", snapshot.UserInputs)
	}
	if err := reopened.db.QueryRow(`SELECT projection_version FROM entry_hydration_projection WHERE entry_id=?`, message.ID).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != entryHydrationProjectionVersion {
		t.Fatalf("projection version=%d want %d", version, entryHydrationProjectionVersion)
	}
	for name, projection := range map[string][]byte{
		"empty":     {},
		"malformed": {0xff},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := reopened.db.Exec(`UPDATE entry_hydration_projection SET projection_version=?, projection=? WHERE entry_id=?`, entryHydrationProjectionVersion, projection, message.ID); err != nil {
				t.Fatal(err)
			}
			snapshot, err := reopened.BranchHydration()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(snapshot.UserInputs, []string{"repair me"}) {
				t.Fatalf("inputs=%v", snapshot.UserInputs)
			}
		})
	}
}

func TestSQLiteHydrationProjectionBackfillsOlderSchema(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "migration.db")
	store, err := NewSQLiteStore(path, dir, Options{ID: "migration"})
	if err != nil {
		t.Fatal(err)
	}
	appendHydrationFixture(t, store)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE session_meta SET version=10; DROP TABLE entry_hydration_projection`); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenSQLiteStore(path, dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if reopened.Header().Version != SessionVersion {
		t.Fatalf("version=%d want=%d", reopened.Header().Version, SessionVersion)
	}
	snapshot, err := reopened.BranchHydration()
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Entries) != 8 || snapshot.LatestPlan != "latest plan" {
		t.Fatalf("backfilled snapshot=%+v", snapshot)
	}
	var projections int
	if err := reopened.db.QueryRow(`SELECT count(*) FROM entry_hydration_projection`).Scan(&projections); err != nil {
		t.Fatal(err)
	}
	if projections != 8 {
		t.Fatalf("projection rows=%d want=8", projections)
	}
	var projectionVersion int
	if err := reopened.db.QueryRow(`SELECT hydration_projection_version FROM session_meta WHERE singleton=1`).Scan(&projectionVersion); err != nil {
		t.Fatal(err)
	}
	if projectionVersion != entryHydrationProjectionVersion {
		t.Fatalf("metadata projection version=%d want=%d", projectionVersion, entryHydrationProjectionVersion)
	}
}

func TestSQLiteAppendRollsBackWhenHydrationProjectionFails(t *testing.T) {
	dir := t.TempDir()
	store, err := NewSQLiteStore(filepath.Join(dir, "atomic.db"), dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.db.Exec(`
		CREATE TRIGGER fail_hydration_projection
		BEFORE INSERT ON entry_hydration_projection
		WHEN NEW.entry_id='fail'
		BEGIN SELECT RAISE(ABORT, 'projection failure'); END`); err != nil {
		t.Fatal(err)
	}
	message := protocol.NewUserMessage("fail", "root", "must roll back")
	err = store.Append(Entry{Type: EntryMessage, ID: message.ID, ParentID: "root", Message: &message})
	if err == nil || !strings.Contains(err.Error(), "projection failure") {
		t.Fatalf("append error=%v", err)
	}
	if store.BranchTip() != "root" {
		t.Fatalf("tip advanced to %q", store.BranchTip())
	}
	var entries int
	if err := store.db.QueryRow(`SELECT count(*) FROM entries WHERE id='fail'`).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if entries != 0 {
		t.Fatalf("failed entry persisted: %d", entries)
	}
}
