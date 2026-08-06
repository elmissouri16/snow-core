package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/snow-core/snow/pkg/protocol"
)

func msg(id, parent, text string) Entry {
	m := protocol.NewUserMessage(id, parent, text)
	return Entry{Type: EntryMessage, ID: id, ParentID: parent, Message: &m}
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

func TestEncodeCWD(t *testing.T) {
	cases := map[string]string{
		"/":           "root",
		"/tmp/foo":    "tmp-foo",
		"/Users/el/x": "Users-el-x",
		"/a/b/c/d":    "a-b-c-d",
	}
	for in, want := range cases {
		if got := EncodeCWD(in); got != want {
			t.Errorf("EncodeCWD(%q) = %q, want %q", in, got, want)
		}
	}
}
