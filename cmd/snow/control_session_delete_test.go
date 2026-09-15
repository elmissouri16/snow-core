package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/elmissouri16/snow-core/internal/artifact"
	"github.com/elmissouri16/snow-core/internal/session"
)

func TestControlCLISessionDeleteRuntimeFreeAndForeignProcessLease(t *testing.T) {
	f := newControlCLIFixture(t)
	var err error
	f.cwd, err = filepath.EvalSymlinks(f.cwd)
	if err != nil {
		t.Fatal(err)
	}
	var providerCalls atomic.Int64
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { providerCalls.Add(1); w.WriteHeader(500) }))
	defer provider.Close()
	// Malformed configuration and auth must not even be parsed on this path.
	for _, path := range []string{filepath.Join(f.snow, "config.json"), filepath.Join(f.snow, "auth.json")} {
		if err := os.WriteFile(path, []byte("INVALID PRIVATE "+provider.URL), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(f.cwd, ".snow"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.cwd, ".snow", "config.json"), []byte("INVALID PRIVATE PROJECT"), 0600); err != nil {
		t.Fatal(err)
	}
	index := session.NewFileIndex(f.sessions)
	target, err := index.Create(f.cwd)
	if err != nil {
		t.Fatal(err)
	}
	if err := target.(session.TitleStore).RenameSession("target"); err != nil {
		t.Fatal(err)
	}
	id, path := target.ID(), target.Path()
	defer target.Close()
	foreign, err := index.Create(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := foreign.(session.TitleStore).RenameSession("foreign"); err != nil {
		t.Fatal(err)
	}
	foreignID := foreign.ID()
	if err = foreign.Close(); err != nil {
		t.Fatal(err)
	}
	artifacts, err := artifact.NewLocalStore(filepath.Join(f.snow, "artifacts"), 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer artifacts.Close()
	if _, err = artifacts.SaveText(t.Context(), id, "call", "PRIVATE"); err != nil {
		t.Fatal(err)
	}
	goalDir := filepath.Join(f.snow, "goals", id)
	if err = os.MkdirAll(goalDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(goalDir, "objective.md"), []byte("PRIVATE"), 0600); err != nil {
		t.Fatal(err)
	}
	run := func(sessionID string) bool {
		t.Helper()
		out, stderr, err := f.run(t, `{"type":"session_delete","params":{"session_id":"`+sessionID+`"}}`+"\n", "--mode", "rpc", "--rpc-startup", "control")
		if err != nil {
			t.Fatalf("%v %s", err, stderr)
		}
		if strings.Contains(out+stderr, "PRIVATE") || strings.Contains(out+stderr, f.snow) {
			t.Fatal("private details leaked")
		}
		frames := controlCLIFrames(t, out)
		if len(frames) != 2 {
			t.Fatal(out)
		}
		return frames[1]["success"] == true
	}
	// The deleting subprocess must respect the parent process's active DB lease.
	if run(id) {
		t.Fatal("deleted foreign-process active session")
	}
	if _, err = os.Stat(path); err != nil {
		t.Fatal("active session lost", err)
	}
	if run(foreignID) {
		t.Fatal("deleted foreign workspace")
	}
	if err = target.Close(); err != nil {
		t.Fatal(err)
	}
	if infos, e := index.List(f.cwd); e != nil || len(infos) != 1 {
		t.Fatalf("inventory before deletion: %+v %v", infos, e)
	}
	if !run(id) {
		t.Fatal("cold deletion failed")
	}
	if _, err = os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("session remains", err)
	}
	if _, err = os.Stat(goalDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("goal remains", err)
	}
	if ids, err := artifacts.ListIDs(t.Context(), id); err != nil || len(ids) != 0 {
		t.Fatal("artifact remains", ids, err)
	}
	if _, err = os.Stat(foreign.Path()); err != nil {
		t.Fatal("foreign session lost", err)
	}
	if providerCalls.Load() != 0 {
		t.Fatal("contacted provider")
	}
	for _, dir := range []string{"debug", "diagnostics", "cache"} {
		if _, err = os.Stat(filepath.Join(f.snow, dir)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("created runtime files", dir, err)
		}
	}
	infos, err := index.List(f.cwd)
	if err != nil || len(infos) != 0 {
		t.Fatal("deletion activated a replacement session", infos, err)
	}
}
