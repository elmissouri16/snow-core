package session

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/snow-core/snow/pkg/protocol"
	_ "modernc.org/sqlite"
)

func TestValidateSQLiteSessionIsReadOnly(t *testing.T) {
	validPath := filepath.Join(t.TempDir(), "valid.db")
	valid, err := NewSQLiteStore(validPath, t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := valid.Append(msg("valid", "", "keep")); err != nil {
		t.Fatal(err)
	}
	if err := valid.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSQLiteSession(validPath); err != nil {
		t.Fatalf("valid session rejected: %v", err)
	}
	if err := os.Chmod(validPath, 0o644); err != nil {
		t.Fatal(err)
	}
	opened, err := OpenSQLiteStore(validPath, t.TempDir(), Options{})
	if err != nil {
		t.Fatalf("valid existing session did not open: %v", err)
	}
	if err := opened.Close(); err != nil {
		t.Fatal(err)
	}
	validInfo, err := os.Stat(validPath)
	if err != nil {
		t.Fatal(err)
	}
	if validInfo.Mode().Perm() != 0o644 {
		t.Fatalf("existing-only open changed path mode to %o", validInfo.Mode().Perm())
	}
	emptyPath := filepath.Join(t.TempDir(), "empty-owned.db")
	emptyOwner, err := NewSQLiteStore(emptyPath, t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	emptyOpened, err := OpenSQLiteStore(emptyPath, t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := emptyOpened.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(emptyPath); err != nil {
		t.Fatalf("existing-only close deleted an empty session path: %v", err)
	}
	if err := emptyOwner.Close(); err != nil {
		t.Fatal(err)
	}

	missingPath := filepath.Join(t.TempDir(), "missing.db")
	if _, err := OpenSQLiteStore(missingPath, t.TempDir(), Options{}); err == nil {
		t.Fatal("existing-only open created a missing session")
	}
	if _, err := os.Stat(missingPath); !os.IsNotExist(err) {
		t.Fatalf("existing-only open created missing path: %v", err)
	}

	invalidPath := filepath.Join(t.TempDir(), "unrelated.db")
	db, err := sql.Open("sqlite", sqliteDSN(invalidPath))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE unrelated(value TEXT); INSERT INTO unrelated(value) VALUES ('keep')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(invalidPath, 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(invalidPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateSQLiteSession(invalidPath); err == nil {
		t.Fatal("unrelated SQLite database accepted as a Snow session")
	}
	if _, err := OpenSQLiteStore(invalidPath, t.TempDir(), Options{}); err == nil {
		t.Fatal("unrelated SQLite database opened as a Snow session")
	}
	after, err := os.ReadFile(invalidPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("validation changed unrelated SQLite database contents")
	}
	info, err := os.Stat(invalidPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("validation changed unrelated database mode to %o", info.Mode().Perm())
	}

	lookalikePath := filepath.Join(t.TempDir(), "lookalike.db")
	lookalike, err := sql.Open("sqlite", lookalikePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lookalike.Exec(`CREATE TABLE session_meta(singleton INTEGER PRIMARY KEY, version INTEGER, session_id TEXT, created_at INTEGER, cwd TEXT, name TEXT, branch_tip TEXT); INSERT INTO session_meta VALUES(1,8,'fake',1,'/tmp','','root')`); err != nil {
		t.Fatal(err)
	}
	if err := lookalike.Close(); err != nil {
		t.Fatal(err)
	}
	lookalikeBefore, err := os.ReadFile(lookalikePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OpenSQLiteStore(lookalikePath, t.TempDir(), Options{}); err == nil {
		t.Fatal("metadata-only lookalike opened as a Snow session")
	}
	lookalikeAfter, err := os.ReadFile(lookalikePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(lookalikeAfter) != string(lookalikeBefore) {
		t.Fatal("existing-only open mutated a metadata-only lookalike")
	}
}

func TestSQLiteSessionTitlePersistsAndKeepsEmptySession(t *testing.T) {
	root := t.TempDir()
	cwd := t.TempDir()
	idx := NewFileIndex(root)
	st, err := idx.Create(cwd)
	if err != nil {
		t.Fatal(err)
	}
	path := st.Path()
	titles := st.(TitleStore)
	if err := titles.RenameSession("  Manual title  "); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("named empty session was removed: %v", err)
	}
	infos, err := idx.List(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 || infos[0].Name != "Manual title" || infos[0].Messages != 0 {
		t.Fatalf("listed session = %+v", infos)
	}
	reopened, err := idx.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got := reopened.Header().Name; got != "Manual title" {
		t.Fatalf("reopened title = %q", got)
	}
}

func TestSQLiteAppendWithInitialTitlePreservesManualRename(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.db")
	st, err := NewSQLiteStore(path, t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.AppendWithInitialTitle(msg("first", "", "first"), "First prompt"); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.SessionTitle(); got != "First prompt" {
		t.Fatalf("initial title = %q", got)
	}
	if err := st.RenameSession("Manual"); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendWithInitialTitle(msg("second", "", "second"), "Second prompt"); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.SessionTitle(); got != "Manual" {
		t.Fatalf("manual title replaced = %q", got)
	}
}

func TestSQLiteRoundTripAndBranch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.db")
	st, err := NewSQLiteStore(path, "/tmp/work", Options{Name: "sqlite"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Append(msg("a", "", "one")); err != nil {
		t.Fatal(err)
	}
	if err := st.Append(msg("b", "", "two")); err != nil {
		t.Fatal(err)
	}
	if err := st.SetBranchTip("a"); err != nil {
		t.Fatal(err)
	}
	if err := st.Append(msg("c", "", "branch")); err != nil {
		t.Fatal(err)
	}
	if got := st.BranchTip(); got != "c" {
		t.Fatalf("tip = %q, want c", got)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	mode, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode.Mode().Perm() != 0o600 {
		t.Fatalf("database mode = %o, want 600", mode.Mode().Perm())
	}

	st, err = NewSQLiteStore(path, "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if st.Header().Name != "sqlite" || st.Header().CWD != "/tmp/work" {
		t.Fatalf("header = %+v", st.Header())
	}
	messages, err := st.Messages()
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || messages[0].Content[0].Text != "one" || messages[1].Content[0].Text != "branch" {
		t.Fatalf("messages = %+v", messages)
	}

	fork, err := st.Fork("a")
	if err != nil {
		t.Fatal(err)
	}
	defer fork.Close()
	forkMessages, err := fork.Messages()
	if err != nil {
		t.Fatal(err)
	}
	if len(forkMessages) != 1 || forkMessages[0].Content[0].Text != "one" {
		t.Fatalf("fork messages = %+v", forkMessages)
	}
}

func TestSQLiteReadNormalizesHistoricalMessageTopology(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.db")
	store, err := NewSQLiteStore(path, t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Append(msg("entry-id", "", "text")); err != nil {
		t.Fatal(err)
	}
	mismatched := protocol.NewUserMessage("wrong-id", "wrong-parent", "text")
	raw, err := json.Marshal(mismatched)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE entries SET message=? WHERE id='entry-id'`, raw); err != nil {
		t.Fatal(err)
	}
	messages, err := store.Messages()
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].ID != "entry-id" || messages[0].ParentID != "root" {
		t.Fatalf("normalized messages = %+v", messages)
	}
	store.Close()
}

func TestSQLiteClosePreservesGoalOnlyAndBranchOnlyState(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*SQLiteStore) error
	}{
		{name: "goal", setup: func(st *SQLiteStore) error {
			return st.CreateGoal(protocol.ThreadGoal{GoalID: "goal", Objective: "work", Status: protocol.GoalPaused, CreatedAt: 1, UpdatedAt: 1}, false)
		}},
		{name: "branch", setup: func(st *SQLiteStore) error {
			_, err := st.ForkBranch("root")
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "session.db")
			st, err := NewSQLiteStore(path, t.TempDir(), Options{})
			if err != nil {
				t.Fatal(err)
			}
			if err := test.setup(st); err != nil {
				t.Fatal(err)
			}
			if err := st.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("meaningful session removed on close: %v", err)
			}
			reopened, err := NewSQLiteStore(path, "", Options{})
			if err != nil {
				t.Fatal(err)
			}
			if err := reopened.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSQLiteMigratesVersionOneToMainBranch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE session_meta (
			singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
			version INTEGER NOT NULL,
			session_id TEXT NOT NULL UNIQUE,
			created_at INTEGER NOT NULL,
			cwd TEXT NOT NULL,
			name TEXT NOT NULL DEFAULT '',
			branch_tip TEXT NOT NULL
		);
		CREATE TABLE entries (
			seq INTEGER PRIMARY KEY AUTOINCREMENT,
			id TEXT NOT NULL UNIQUE,
			parent_id TEXT NOT NULL DEFAULT '',
			entry_type TEXT NOT NULL,
			message BLOB,
			summary TEXT NOT NULL DEFAULT '',
			meta_key TEXT NOT NULL DEFAULT '',
			meta_value TEXT NOT NULL DEFAULT ''
		);
		INSERT INTO session_meta(singleton, version, session_id, created_at, cwd, name, branch_tip)
		VALUES(1, 1, 'legacy', 1, '/tmp/work', '', 'a');
		INSERT INTO entries(id, parent_id, entry_type, message, summary, meta_key, meta_value)
		VALUES('root', '', 'meta', NULL, '', 'root', 'legacy');`)
	if err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	message := protocol.NewUserMessage("a", "root", "legacy message")
	raw, _ := json.Marshal(message)
	if _, err := db.Exec(`INSERT INTO entries(id, parent_id, entry_type, message, summary, meta_key, meta_value) VALUES('a', 'root', 'message', ?, '', '', '')`, raw); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	st, err := NewSQLiteStore(path, "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if st.Header().Version != SessionVersion || st.BranchTip() != "a" {
		t.Fatalf("migrated header=%+v tip=%q", st.Header(), st.BranchTip())
	}
	branches, err := st.Branches()
	if err != nil || len(branches) != 1 || branches[0].ID != "main" || !branches[0].Active {
		t.Fatalf("migrated branches=%+v err=%v", branches, err)
	}
}

func TestSQLiteMigratesVersionSixMultibranchTopology(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v6.db")
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE session_meta(singleton INTEGER PRIMARY KEY,version INTEGER NOT NULL,session_id TEXT NOT NULL UNIQUE,created_at INTEGER NOT NULL,cwd TEXT NOT NULL,name TEXT NOT NULL DEFAULT '',branch_tip TEXT NOT NULL);
CREATE TABLE entries(seq INTEGER PRIMARY KEY AUTOINCREMENT,id TEXT NOT NULL UNIQUE,parent_id TEXT NOT NULL DEFAULT '',entry_type TEXT NOT NULL,message BLOB,summary TEXT NOT NULL DEFAULT '',compacted_through TEXT NOT NULL DEFAULT '',meta_key TEXT NOT NULL DEFAULT '',meta_value TEXT NOT NULL DEFAULT '');
CREATE TABLE session_branches(branch_id TEXT PRIMARY KEY,tip_id TEXT NOT NULL,created_at INTEGER NOT NULL,updated_at INTEGER NOT NULL,active INTEGER NOT NULL DEFAULT 0);
INSERT INTO session_meta VALUES(1,6,'v6',1,'/tmp','', 'root'); INSERT INTO entries(id,parent_id,entry_type,meta_key,meta_value) VALUES('root','','meta','root','v6');
INSERT INTO session_branches VALUES('main','root',1,1,0); INSERT INTO session_branches VALUES('branch-old','root',2,2,1);`)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	st, err := NewSQLiteStore(path, "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	branches, err := st.Branches()
	if err != nil || len(branches) != 2 {
		t.Fatalf("branches=%+v err=%v", branches, err)
	}
	for _, branch := range branches {
		if branch.ID == "main" && branch.Name != "main" {
			t.Fatalf("main=%+v", branch)
		}
		if branch.ID == "branch-old" && (branch.Name != "branch-old" || branch.ParentID != "main") {
			t.Fatalf("legacy=%+v", branch)
		}
	}
}

func TestSQLiteDurableBranchesShareEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "branches.db")
	st, err := NewSQLiteStore(path, "/tmp/work", Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, entry := range []Entry{msg("a", "", "one"), msg("b", "", "two")} {
		if err := st.Append(entry); err != nil {
			t.Fatal(err)
		}
	}
	fork, err := st.ForkBranch("a")
	if err != nil {
		t.Fatal(err)
	}
	if !fork.Active || fork.Messages != 1 {
		t.Fatalf("fork = %+v", fork)
	}
	divergent := msg("c", "", "branch")
	if err := st.Append(divergent); err != nil {
		t.Fatal(err)
	}
	branches, err := st.Branches()
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 2 {
		t.Fatalf("branches = %+v", branches)
	}
	if err := st.SelectBranch("main"); err != nil {
		t.Fatal(err)
	}
	mainMessages, err := st.Messages()
	if err != nil || len(mainMessages) != 2 || mainMessages[1].Content[0].Text != "two" {
		t.Fatalf("main messages = %+v, err=%v", mainMessages, err)
	}
	if err := st.SelectBranch(fork.ID); err != nil {
		t.Fatal(err)
	}
	branchMessages, err := st.Messages()
	if err != nil || len(branchMessages) != 2 || branchMessages[1].Content[0].Text != "branch" {
		t.Fatalf("fork messages = %+v, err=%v", branchMessages, err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = NewSQLiteStore(path, "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	branches, err = st.Branches()
	if err != nil {
		t.Fatal(err)
	}
	var active string
	for _, branch := range branches {
		if branch.Active {
			active = branch.ID
		}
	}
	if active != fork.ID || st.BranchTip() != "c" {
		t.Fatalf("reopened active branch = %q tip=%q, want %q/c", active, st.BranchTip(), fork.ID)
	}
}

func TestSQLiteCompactionProjectionSurvivesReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "compact.db")
	st, err := NewSQLiteStore(path, "/tmp/work", Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range []Entry{msg("a", "", "old one"), msg("b", "", "old two"), msg("c", "", "keep one"), msg("d", "", "keep two")} {
		if err := st.Append(entry); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Append(Entry{Type: EntryCompaction, Summary: "old conversation summary", CompactedThrough: "b"}); err != nil {
		t.Fatal(err)
	}
	full, err := st.Messages()
	if err != nil || len(full) != 4 {
		t.Fatalf("full messages = %d, err=%v", len(full), err)
	}
	projected, err := st.ContextMessages()
	if err != nil || len(projected) != 3 || projected[0].Role != protocol.RoleCustom || projected[1].Content[0].Text != "keep one" {
		t.Fatalf("projected = %+v, err=%v", projected, err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = NewSQLiteStore(path, "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	projected, err = st.ContextMessages()
	if err != nil || len(projected) != 3 || projected[0].Content[0].Text != "Working-state checkpoint for compacted history:\nold conversation summary" {
		t.Fatalf("reloaded projected = %+v, err=%v", projected, err)
	}
}

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
