package sessiondelete

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/elmissouri16/snow-core/internal/artifact"
	"github.com/elmissouri16/snow-core/internal/session"
)

type failedArtifacts struct{}

func (failedArtifacts) DeleteSession(context.Context, string) error {
	return errors.New("cleanup failure")
}

func TestDeleteSharesIdentityCleanupAndPartialError(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete", true: "partial"}[partial], func(t *testing.T) {
			root, cwd, home := t.TempDir(), t.TempDir(), t.TempDir()
			index := session.NewFileIndex(root)
			target, err := index.Create(cwd)
			if err != nil {
				t.Fatal(err)
			}
			if err := target.(session.TitleStore).RenameSession("target"); err != nil {
				t.Fatal(err)
			}
			id, path := target.ID(), target.Path()
			if err = target.Close(); err != nil {
				t.Fatal(err)
			}
			sibling, err := index.Create(cwd)
			if err != nil {
				t.Fatal(err)
			}
			defer sibling.Close()
			if err := sibling.(session.TitleStore).RenameSession("sibling"); err != nil {
				t.Fatal(err)
			}
			store, err := artifact.NewLocalStore(filepath.Join(home, "artifacts"), 1024)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			for _, id := range []string{id, sibling.ID()} {
				if _, err = store.SaveText(t.Context(), id, "call", "private"); err != nil {
					t.Fatal(err)
				}
				if err = os.MkdirAll(filepath.Join(home, "goals", id), 0700); err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(home, "goals", id, "objective.md"), []byte("goal"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			var deleter artifact.SessionDeleter = artifact.ExistingSessionDeleter{Path: filepath.Join(home, "artifacts")}
			if partial {
				deleter = failedArtifacts{}
			}
			err = ByID(t.Context(), root, cwd, id, home, deleter)
			if partial {
				if _, ok := errors.AsType[*CleanupError](err); !ok {
					t.Fatalf("lost partial outcome: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if _, err = os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("database retained", err)
			}
			if _, err = os.Stat(filepath.Join(home, "goals", id)); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("goal retained", err)
			}
			if !partial {
				if ids, err := store.ListIDs(t.Context(), id); err != nil || len(ids) != 0 {
					t.Fatal(ids, err)
				}
			}
			if ids, err := store.ListIDs(t.Context(), sibling.ID()); err != nil || len(ids) != 1 {
				t.Fatal("sibling artifacts lost", ids, err)
			}
			if _, err = os.Stat(sibling.Path()); err != nil {
				t.Fatal("sibling session lost", err)
			}
		})
	}
}
func TestByIDRejectsForeignOpenMalformedAndMissingWithoutCreation(t *testing.T) {
	root, cwd, home := t.TempDir(), t.TempDir(), filepath.Join(t.TempDir(), "missing")
	index := session.NewFileIndex(root)
	foreign, err := index.Create(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer foreign.Close()
	if err := foreign.(session.TitleStore).RenameSession("foreign"); err != nil {
		t.Fatal(err)
	}
	active, err := index.Create(cwd)
	if err != nil {
		t.Fatal(err)
	}
	defer active.Close()
	if err := active.(session.TitleStore).RenameSession("active"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{foreign.ID(), active.ID(), "missing", "../escape", " padded "} {
		if err := ByID(t.Context(), root, cwd, id, home, artifact.ExistingSessionDeleter{Path: filepath.Join(home, "artifacts")}); err == nil {
			t.Fatal("accepted", id)
		}
	}
	if _, err = os.Stat(home); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("created cleanup home", err)
	}
}
