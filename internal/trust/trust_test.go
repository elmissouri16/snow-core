package trust

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trust.json")
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set("/tmp/proj", LevelAllow); err != nil {
		t.Fatal(err)
	}
	s2, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if lvl, ok := s2.Get("/tmp/proj"); !ok || lvl != LevelAllow {
		t.Fatalf("round trip = %q %v", lvl, ok)
	}
}

func TestStoreParentWalk(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "trust.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set("/tmp", LevelDeny); err != nil {
		t.Fatal(err)
	}
	// /tmp/a/b inherits the /tmp decision.
	if lvl, ok := s.Get("/tmp/a/b"); !ok || lvl != LevelDeny {
		t.Fatalf("parent walk = %q %v", lvl, ok)
	}
}

func TestStoreMissingReturnsNone(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("/some/unknown/path"); ok {
		t.Fatal("expected no decision")
	}
}

func TestResolvePolicyAliasesAndPrecedence(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := New(filepath.Join(t.TempDir(), "trust.json"))
	if err != nil {
		t.Fatal(err)
	}
	for configured, want := range map[string]Level{"allow": LevelAllow, "always": LevelAllow, "deny": LevelDeny, "never": LevelDeny} {
		got, err := Resolve(child, configured, store)
		if err != nil || got.Prompt || got.Level != want {
			t.Fatalf("Resolve(%q) = %+v, %v", configured, got, err)
		}
	}
	ask, err := Resolve(child, "ask", store)
	if err != nil || !ask.Prompt || ask.Level != LevelAsk {
		t.Fatalf("ask resolution = %+v, %v", ask, err)
	}
	if _, err := Resolve(child, "maybe", store); err == nil {
		t.Fatal("invalid policy accepted")
	}
	if err := store.Set(root, LevelAllow); err != nil {
		t.Fatal(err)
	}
	inherited, err := Resolve(child, "deny", store)
	if err != nil || inherited.Level != LevelAllow || inherited.Prompt {
		t.Fatalf("inherited resolution = %+v, %v", inherited, err)
	}
	if err := store.Set(child, LevelDeny); err != nil {
		t.Fatal(err)
	}
	exact, err := Resolve(child, "allow", store)
	if err != nil || exact.Level != LevelDeny {
		t.Fatalf("exact resolution = %+v, %v", exact, err)
	}
}

func TestCanonicalPathResolvesSymlinkAliases(t *testing.T) {
	real := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(real, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	store, err := New(filepath.Join(t.TempDir(), "trust.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(alias, LevelAllow); err != nil {
		t.Fatal(err)
	}
	if level, ok := store.Get(real); !ok || level != LevelAllow {
		t.Fatalf("canonical alias lookup = %q %v", level, ok)
	}
}

func TestStoredCanonicalDecisionDoesNotRetargetWithSymlink(t *testing.T) {
	realA := t.TempDir()
	realB := t.TempDir()
	aliasRoot := t.TempDir()
	alias := filepath.Join(aliasRoot, "project")
	if err := os.Symlink(realA, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	path := filepath.Join(t.TempDir(), "trust.json")
	store, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(alias, LevelAllow); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realB, alias); err != nil {
		t.Fatal(err)
	}
	reloaded, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := Resolve(alias, "ask", reloaded)
	if err != nil {
		t.Fatal(err)
	}
	if !resolution.Prompt || resolution.Level != LevelAsk {
		t.Fatalf("retargeted symlink inherited old allow: %+v", resolution)
	}
}

func TestConcurrentStoreInstancesMergeDecisions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trust.json")
	first, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	projects := []string{t.TempDir(), t.TempDir()}
	stores := []*Store{first, second}
	var wg sync.WaitGroup
	errs := make(chan error, len(stores))
	for i := range stores {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- stores[i].Set(projects[i], LevelAllow)
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	reloaded, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, project := range projects {
		if level, ok := reloaded.Get(project); !ok || level != LevelAllow {
			t.Fatalf("merged decision for %s = %q %v", project, level, ok)
		}
	}
}

func TestFailedPersistenceDoesNotPublishDecision(t *testing.T) {
	s, err := New("")
	if err != nil {
		t.Fatal(err)
	}
	first := t.TempDir()
	second := t.TempDir()
	if err := s.Set(first, LevelAllow); err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.path = filepath.Join(blocker, "trust.json")
	if err := s.Set(second, LevelDeny); err == nil {
		t.Fatal("expected persistence failure")
	}
	if _, ok := s.Get(second); ok {
		t.Fatal("failed decision was published in memory")
	}
	if level, ok := s.Get(first); !ok || level != LevelAllow {
		t.Fatalf("prior decision lost: %q %v", level, ok)
	}
}

func TestFilePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trust.json")
	s, _ := New(path)
	if err := s.Set("/x", LevelAllow); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", fi.Mode().Perm())
	}
}
