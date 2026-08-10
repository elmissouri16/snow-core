package session

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/snow-core/snow/pkg/protocol"
	_ "modernc.org/sqlite"
)

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
	if err != nil || len(projected) != 3 || projected[0].Content[0].Text != "Conversation summary:\nold conversation summary" {
		t.Fatalf("reloaded projected = %+v, err=%v", projected, err)
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
}
