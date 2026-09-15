//go:build darwin || linux

package hostops

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestCreateWithUnavailableConfiguredGit(t *testing.T) {
	for _, kind := range []string{"missing", "non-executable", "directory", "relative"} {
		t.Run(kind, func(t *testing.T) {
			helper, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			git := filepath.Join(t.TempDir(), "fictional-git")
			switch kind {
			case "non-executable":
				if err := os.WriteFile(git, []byte("not executable"), 0600); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(git, 0700); err != nil {
					t.Fatal(err)
				}
			case "relative":
				candidate := filepath.Join(filepath.Dir(git), "fictional-relative-git")
				if err := os.WriteFile(candidate, []byte("not executed"), 0700); err != nil {
					t.Fatal(err)
				}
				t.Setenv("PATH", filepath.Dir(git))
				git = filepath.Base(candidate)
			}
			service, err := New(Options{GitExecutable: git, HelperExecutable: helper})
			if err != nil {
				t.Fatalf("CREATE construction depends on unavailable Git: %v", err)
			}
			prepared, err := service.Prepare(t.Context(), protocol.RPCProjectPrepareParams{OperationID: "create-no-git", Parent: testParent(t), Leaf: "project"})
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Close()
			assertUnavailableClonePreservesCreate(t, prepared)
		})
	}
}

func TestStartCloneRevalidatesConfiguredGitBeforeAdmittingExecution(t *testing.T) {
	helper, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	git := filepath.Join(t.TempDir(), "fictional-git")
	// Merely an executable fixture file; this test never releases a clone gate.
	if err := os.WriteFile(git, []byte("not executed"), 0700); err != nil {
		t.Fatal(err)
	}
	service, err := New(Options{GitExecutable: git, HelperExecutable: helper})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := service.Prepare(t.Context(), protocol.RPCProjectPrepareParams{OperationID: "removed-git", Parent: testParent(t), Leaf: "project"})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	if err := os.Remove(git); err != nil {
		t.Fatal(err)
	}
	assertUnavailableClonePreservesCreate(t, prepared)
}

func assertUnavailableClonePreservesCreate(t *testing.T, prepared *Prepared) {
	t.Helper()
	result := prepared.Result()
	clone, err := prepared.StartClone(t.Context(), protocol.RPCProjectCloneStartParams{OperationID: result.OperationID, Child: result.Child, URL: "https://example.invalid/fictional"})
	if clone != nil {
		clone.Cancel()
		<-clone.Done()
		t.Fatal("unavailable Git admitted an execution handle")
	}
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("clone error=%v, want fixed unavailable", err)
	}
	if !matchesPath(result.Child) {
		t.Fatal("CREATE identity changed after clone rejection")
	}
	entries, err := os.ReadDir(result.Child.Path)
	if err != nil || len(entries) != 0 {
		t.Fatalf("CREATE content changed: %v, %v", entries, err)
	}
}
