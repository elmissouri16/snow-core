package auth

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFileStoreUpdateSerializesStoreInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	a, _ := NewFileStore(path)
	b, _ := NewFileStore(path)
	_ = a.Put("p", Credential{Type: CredentialOAuth, Access: "old"})
	var transforms atomic.Int32
	var wg sync.WaitGroup
	for _, store := range []*FileStore{a, b} {
		wg.Add(1)
		go func(s *FileStore) {
			defer wg.Done()
			_, _, err := s.Update("p", func(c Credential, ok bool) (Credential, bool, error) {
				transforms.Add(1)
				if c.Access == "old" {
					time.Sleep(20 * time.Millisecond)
					c.Access = "new"
					return c, true, nil
				}
				return c, false, nil
			})
			if err != nil {
				t.Error(err)
			}
		}(store)
	}
	wg.Wait()
	got, _ := a.Get("p")
	if got.Access != "new" {
		t.Fatalf("got=%+v", got)
	}
	if transforms.Load() != 2 {
		t.Fatalf("transforms=%d", transforms.Load())
	}
	for _, file := range []string{path, path + ".lock"} {
		info, err := os.Stat(file)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode=%o", file, info.Mode().Perm())
		}
	}
}
