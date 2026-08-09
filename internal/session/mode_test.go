package session

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/snow-core/snow/pkg/protocol"
)

func exerciseThreadModes(t *testing.T, st Store) {
	t.Helper()
	state, ok := st.(ThreadStateStore)
	if !ok {
		t.Fatal("store does not implement ThreadStateStore")
	}
	if got, err := state.CollaborationMode(); err != nil || got != protocol.ModeDefault {
		t.Fatalf("default mode = %q, %v", got, err)
	}
	if err := state.SetCollaborationMode(protocol.ModePlan); err != nil {
		t.Fatal(err)
	}
	branches := st.(BranchStore)
	fork, err := branches.ForkBranch("root")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := state.CollaborationMode(); got != protocol.ModePlan {
		t.Fatalf("fork mode = %q, want plan", got)
	}
	if err := state.SetCollaborationMode(protocol.ModeDefault); err != nil {
		t.Fatal(err)
	}
	if err := branches.SelectBranch("main"); err != nil {
		t.Fatal(err)
	}
	if got, _ := state.CollaborationMode(); got != protocol.ModePlan {
		t.Fatalf("main mode = %q, want plan (fork %s)", got, fork.ID)
	}
}

func TestMemoryStoreThreadModes(t *testing.T) {
	exerciseThreadModes(t, NewMemoryStore(Options{}))
}

func TestSQLiteMigratesVersionTwoThreadState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v2.db")
	st, err := NewSQLiteStore(path, t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	msg := protocol.NewUserMessage("keep-v2", "", "keep")
	if err := st.Append(Entry{Type: EntryMessage, ID: msg.ID, Message: &msg}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE session_meta SET version = 2; DROP TABLE thread_state;`); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	_ = db.Close()
	migrated, err := NewSQLiteStore(path, t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer migrated.Close()
	if migrated.Header().Version != SessionVersion {
		t.Fatalf("version = %d", migrated.Header().Version)
	}
	if mode, err := migrated.CollaborationMode(); err != nil || mode != protocol.ModeDefault {
		t.Fatalf("mode = %q, err=%v", mode, err)
	}
}

func TestSQLiteStoreThreadModesPersistAndFork(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.db")
	st, err := NewSQLiteStore(path, t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	exerciseThreadModes(t, st)
	msg := protocol.NewUserMessage("keep", "", "keep session")
	if err := st.Append(Entry{Type: EntryMessage, ID: msg.ID, Message: &msg}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewSQLiteStore(path, t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if reopened.Header().Version != SessionVersion {
		t.Fatalf("version = %d", reopened.Header().Version)
	}
	if got, _ := reopened.CollaborationMode(); got != protocol.ModePlan {
		t.Fatalf("reopened mode = %q", got)
	}
}
