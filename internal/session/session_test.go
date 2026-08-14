package session

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/snow-core/snow/pkg/protocol"
)

func msg(id, parent, text string) Entry {
	m := protocol.NewUserMessage(id, parent, text)
	return Entry{Type: EntryMessage, ID: id, ParentID: parent, Message: &m}
}

func TestSuggestedTitleAndMemoryRename(t *testing.T) {
	long := "  ##   Review\n\tthe session title implementation and " + strings.Repeat("details ", 20)
	title := SuggestedTitle(long)
	if !strings.HasPrefix(title, "Review the session title implementation") || len([]rune(title)) > maxSessionTitleRunes || !strings.HasSuffix(title, "…") {
		t.Fatalf("suggested title = %q", title)
	}
	s := NewMemoryStore(Options{})
	if err := s.RenameSession("  Manual title  "); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.SessionTitle(); got != "Manual title" || s.Header().Name != got {
		t.Fatalf("memory title = %q header=%q", got, s.Header().Name)
	}
	for _, invalid := range []string{"", strings.Repeat("x", maxSessionTitleRunes+1), "bad\nname"} {
		if err := s.RenameSession(invalid); err == nil {
			t.Fatalf("RenameSession(%q) succeeded", invalid)
		}
	}
}

func TestMemoryInitialTitleIsAtomicAndManualWins(t *testing.T) {
	s := NewMemoryStore(Options{})
	entry := msg("first", "", "first prompt")
	if err := s.AppendWithInitialTitle(entry, "First prompt"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.SessionTitle(); got != "First prompt" {
		t.Fatalf("initial title = %q", got)
	}
	if err := s.RenameSession("Manual"); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendWithInitialTitle(msg("second", "", "second"), "Second"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.SessionTitle(); got != "Manual" {
		t.Fatalf("manual title replaced = %q", got)
	}
}

func TestMemoryAppendAndLinearize(t *testing.T) {
	s := NewMemoryStore(Options{CWD: "/tmp"})
	if s.BranchTip() != "root" {
		t.Fatalf("expected root tip, got %q", s.BranchTip())
	}
	if err := s.Append(msg("a", "", "first")); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(msg("b", "", "second")); err != nil {
		t.Fatal(err)
	}
	msgs, err := s.Messages()
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Content[0].Text != "first" || msgs[1].Content[0].Text != "second" {
		t.Fatalf("wrong order: %v", msgs)
	}
}

func TestMemoryAppendNormalizesTopologyAndClonesMessages(t *testing.T) {
	s := NewMemoryStore(Options{})
	entry := msg("a", "", "first")
	entry.Message.Content[0].Data = []byte("image")
	entry.Message.Content[0].Arguments = []byte(`{"value":1}`)
	if err := s.Append(entry); err != nil {
		t.Fatal(err)
	}
	entry.Message.Content[0].Text = "caller mutation"
	entry.Message.Content[0].Data[0] = 'X'
	entry.Message.Content[0].Arguments[0] = 'X'
	if err := s.Append(msg("b", "", "second")); err != nil {
		t.Fatal(err)
	}

	messages, err := s.Messages()
	if err != nil {
		t.Fatal(err)
	}
	if messages[0].ParentID != "root" || messages[1].ParentID != "a" {
		t.Fatalf("parents = %q, %q", messages[0].ParentID, messages[1].ParentID)
	}
	if messages[0].Content[0].Text != "first" || string(messages[0].Content[0].Data) != "image" || string(messages[0].Content[0].Arguments) != `{"value":1}` {
		t.Fatalf("stored message aliased caller: %+v", messages[0])
	}
	messages[0].Content[0].Text = "returned mutation"
	messages[0].Content[0].Data[0] = 'Y'
	again, err := s.Messages()
	if err != nil {
		t.Fatal(err)
	}
	if again[0].Content[0].Text != "first" || string(again[0].Content[0].Data) != "image" {
		t.Fatalf("returned message aliased store: %+v", again[0])
	}
}

func TestMemoryDuplicateID(t *testing.T) {
	s := NewMemoryStore(Options{})
	if err := s.Append(msg("x", "", "a")); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(msg("x", "", "b")); err == nil {
		t.Fatal("expected duplicate id error")
	}
}

func TestMemoryUnknownParent(t *testing.T) {
	s := NewMemoryStore(Options{})
	e := msg("a", "nope", "x")
	if err := s.Append(e); err == nil {
		t.Fatal("expected unknown parent error")
	}
}

func TestMemoryBranchAndFork(t *testing.T) {
	s := NewMemoryStore(Options{})
	_ = s.Append(msg("a", "", "base"))
	// Branch 1: b -> a
	if err := s.SetBranchTip("a"); err != nil {
		t.Fatal(err)
	}
	_ = s.Append(msg("b", "", "branch1"))
	// Back to a, branch 2: c -> a
	if err := s.SetBranchTip("a"); err != nil {
		t.Fatal(err)
	}
	_ = s.Append(msg("c", "", "branch2"))
	msgs, _ := s.Messages()
	if len(msgs) != 2 || msgs[1].Content[0].Text != "branch2" {
		t.Fatalf("wrong tip messages: %v", msgs)
	}
	// Fork from a gives only base.
	f, err := s.Fork("a")
	if err != nil {
		t.Fatal(err)
	}
	fmsgs, _ := f.Messages()
	if len(fmsgs) != 1 || fmsgs[0].Content[0].Text != "base" {
		t.Fatalf("fork from a should have 1 message: %v", fmsgs)
	}
}

func TestMemorySetBranchTipUnknown(t *testing.T) {
	s := NewMemoryStore(Options{})
	if err := s.SetBranchTip("ghost"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestJSONLRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	s, err := NewJSONLStore(path, "/tmp/work", Options{Name: "test"})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Append(msg("a", "", "one"))
	_ = s.Append(msg("b", "", "two"))
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s2, err := NewJSONLStore(path, "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if s2.Header().Name != "test" {
		t.Fatalf("header name lost: %+v", s2.Header())
	}
	if s2.Header().Version != SessionVersion {
		t.Fatalf("wrong version %d", s2.Header().Version)
	}
	msgs, err := s2.Messages()
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages after reload, got %d", len(msgs))
	}
	if s2.BranchTip() != "b" {
		t.Fatalf("tip after reload = %q, want b", s2.BranchTip())
	}
}

func TestJSONLAppendContinues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	s, _ := NewJSONLStore(path, "/tmp/work", Options{})
	_ = s.Append(msg("a", "", "one"))
	s.Close()

	s2, _ := NewJSONLStore(path, "", Options{})
	defer s2.Close()
	if err := s2.Append(msg("b", "", "two")); err != nil {
		t.Fatal(err)
	}
	msgs, _ := s2.Messages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
}

func TestJSONLCorruptLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.jsonl")
	if err := os.WriteFile(path, []byte("{\"header\":{\"v\":1,\"id\":\"i\",\"created_at\":0,\"cwd\":\"/tmp\"}}\nnot json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewJSONLStore(path, "", Options{}); err == nil {
		t.Fatal("expected corrupt line error")
	}
}

func TestJSONLUnsupportedVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v.jsonl")
	if err := os.WriteFile(path, []byte("{\"header\":{\"v\":99,\"id\":\"i\",\"created_at\":0,\"cwd\":\"/tmp\"}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewJSONLStore(path, "", Options{}); err == nil {
		t.Fatal("expected unsupported version error")
	}
}

func TestMemoryContextProjectionFallsBackOnUnknownBoundary(t *testing.T) {
	s := NewMemoryStore(Options{CWD: "/tmp"})
	defer s.Close()
	for _, entry := range []Entry{msg("a", "", "one"), msg("b", "", "two"), msg("c", "", "three")} {
		if err := s.Append(entry); err != nil {
			t.Fatal(err)
		}
	}
	// A marker referencing a missing entry (corrupt/hand-edited data) must
	// hide the prefix after the marker, not resurface it below the summary.
	if err := s.Append(Entry{Type: EntryCompaction, Summary: "summary", CompactedThrough: "ghost"}); err != nil {
		t.Fatal(err)
	}
	projected, err := s.ContextMessages()
	if err != nil {
		t.Fatal(err)
	}
	if len(projected) != 1 || projected[0].Role != protocol.RoleCustom {
		t.Fatalf("projected = %+v, want summary only", projected)
	}
}

func TestFileIndexCreateOpenList(t *testing.T) {
	root := t.TempDir()
	idx := NewFileIndex(root)
	wd := filepath.Join(root, "..", "work") // ensure encode uses abs
	cwd, _ := filepath.Abs(".")
	_ = wd

	s1, err := idx.Create(cwd)
	if err != nil {
		t.Fatal(err)
	}
	_ = s1.Append(msg("a", "", "hi"))
	got := s1.Path()
	s1.Close()

	// Re-open via index
	s2, err := idx.Open(got)
	if err != nil {
		t.Fatal(err)
	}
	s2.Close()

	list, err := idx.List(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 session listed, got %d", len(list))
	}
	if list[0].Messages != 1 {
		t.Fatalf("expected 1 message in listing, got %d", list[0].Messages)
	}
	if list[0].Path != got {
		t.Fatalf("wrong path in listing: %s vs %s", list[0].Path, got)
	}
}

func TestFileIndexListIsReadOnlyAndKeepsRootOnlyFile(t *testing.T) {
	root := t.TempDir()
	cwd := filepath.Join(root, "project")
	path := filepath.Join(root, EncodeCWD(cwd), "root-only.db")
	store, err := NewSQLiteStore(path, cwd, Options{})
	if err != nil {
		t.Fatal(err)
	}
	store.deleteIfEmpty = false
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", sqliteExistingDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode=DELETE`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	listed, err := NewFileIndex(root).List(cwd)
	if err != nil || len(listed) != 0 {
		t.Fatalf("listed=%+v err=%v", listed, err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("read-only listing removed root-only database: %v", err)
	}
	if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("listing mutated database: before=%+v after=%+v", before, after)
	}
	ro, err := sql.Open("sqlite", sqliteReadOnlyDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	var mode string
	if err := ro.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil || strings.ToLower(mode) != "delete" {
		t.Fatalf("journal mode=%q err=%v", mode, err)
	}
}

func TestFileIndexListsGoalOnlySession(t *testing.T) {
	root := t.TempDir()
	cwd := t.TempDir()
	store, err := NewFileIndex(root).Create(cwd)
	if err != nil {
		t.Fatal(err)
	}
	sqliteStore := store.(*SQLiteStore)
	if err := sqliteStore.CreateGoal(protocol.ThreadGoal{GoalID: "goal", Objective: "work", Status: protocol.GoalPaused, CreatedAt: 1, UpdatedAt: 1}, false); err != nil {
		t.Fatal(err)
	}
	if err := sqliteStore.Close(); err != nil {
		t.Fatal(err)
	}
	list, err := NewFileIndex(root).List(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Messages != 0 {
		t.Fatalf("goal-only listing = %+v", list)
	}
}

func TestFileIndexLegacyCollisionFiltersByStoredCWD(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "a-b", "c")
	second := filepath.Join(root, "a", "b-c")
	if legacyEncodeCWD(first) != legacyEncodeCWD(second) {
		t.Fatalf("test paths did not produce a legacy collision")
	}
	legacyDir := filepath.Join(root, "sessions", legacyEncodeCWD(first))
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i, cwd := range []string{first, second} {
		store, err := NewSQLiteStore(filepath.Join(legacyDir, string(rune('a'+i))+".db"), cwd, Options{})
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Append(msg(string(rune('a'+i)), "", cwd)); err != nil {
			t.Fatal(err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
	}
	list, err := NewFileIndex(filepath.Join(root, "sessions")).List(first)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || !sameCWD(list[0].CWD, first) {
		t.Fatalf("legacy collision listing = %+v", list)
	}
}

func TestLegacyEncodeCWDReproducesTrailingHyphenEdgeCase(t *testing.T) {
	if got := legacyEncodeCleanedCWD("/tmp/repo-"); got != "tmp-repo-" {
		t.Fatalf("trailing-hyphen encoding = %q", got)
	}
}

func TestFileIndexFindsTrailingHyphenLegacyDirectory(t *testing.T) {
	root := t.TempDir()
	cwd := filepath.Join(root, "repo-")
	legacyDir := filepath.Join(root, "sessions", legacyEncodeCWD(cwd))
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := NewSQLiteStore(filepath.Join(legacyDir, "legacy.db"), cwd, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Append(msg("legacy", "", "hello")); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	list, err := NewFileIndex(filepath.Join(root, "sessions")).List(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Path != filepath.Join(legacyDir, "legacy.db") {
		t.Fatalf("legacy listing = %+v", list)
	}
}

func TestEncodeCWDIsDeterministicAndCollisionResistant(t *testing.T) {
	first, second := "/a-b/c", "/a/b-c"
	if legacyEncodeCWD(first) != legacyEncodeCWD(second) {
		t.Fatal("test paths should collide under the legacy encoding")
	}
	firstEncoded := EncodeCWD(first)
	if firstEncoded != EncodeCWD(first) {
		t.Fatal("encoding is not deterministic")
	}
	if firstEncoded == EncodeCWD(second) {
		t.Fatalf("collision-resistant encodings match: %q", firstEncoded)
	}
	if !strings.HasPrefix(firstEncoded, "cwd-v2-") || len(firstEncoded) != len("cwd-v2-")+64 {
		t.Fatalf("encoding %q is not a fixed-size v2 hash", firstEncoded)
	}
}
