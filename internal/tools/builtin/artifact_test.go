package builtin

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/artifact"
	"github.com/elmissouri16/snow-core/internal/session"
)

func TestArtifactLineWindowDoesNotSplitWholeInput(t *testing.T) {
	text := "first\nsecond\nthird\n"
	got, start, end, total := artifactLineWindow(text, 2, 2)
	if got != "second\nthird" || start != 1 || end != 3 || total != 4 {
		t.Fatalf("window=%q start=%d end=%d total=%d", got, start, end, total)
	}
	got, start, end, total = artifactLineWindow(text, 4, 1)
	if got != "" || start != 3 || end != 4 || total != 4 {
		t.Fatalf("trailing window=%q start=%d end=%d total=%d", got, start, end, total)
	}
}

func TestArtifactGrepSkipsOversizedLineAndContinues(t *testing.T) {
	store, err := artifact.NewLocalStore(filepath.Join(t.TempDir(), "artifacts"), 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	sessionStore := session.NewMemoryStore(session.Options{ID: "current"})
	binding := NewSessionBinding(sessionStore)
	text := strings.Repeat("x", maxSearchLineBytes+1) + "\nneedle later\n"
	ref, err := store.SaveText(context.Background(), sessionStore.ID(), "call", text)
	if err != nil {
		t.Fatal(err)
	}
	result, err := NewArtifactGrep(store, binding).Run(context.Background(), json.RawMessage(`{"artifact_id":"`+ref.ID+`","pattern":"needle"}`), nil)
	if err != nil || result.IsError || !strings.Contains(result.Content[0].Text, "2:needle later") || !strings.Contains(result.Content[0].Text, "line(s)") {
		t.Fatalf("grep=%+v err=%v", result, err)
	}
}

func TestArtifactToolsReadAndSearchCurrentSession(t *testing.T) {
	store, err := artifact.NewLocalStore(filepath.Join(t.TempDir(), "artifacts"), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	sessionStore := session.NewMemoryStore(session.Options{ID: "current"})
	binding := NewSessionBinding(sessionStore)
	ref, err := store.SaveText(context.Background(), sessionStore.ID(), "call", "first\nneedle here\nlast")
	if err != nil {
		t.Fatal(err)
	}
	read := NewArtifactRead(store, binding)
	result, err := read.Run(context.Background(), json.RawMessage(`{"artifact_id":"`+ref.ID+`","offset":2,"limit":1}`), nil)
	if err != nil || result.IsError || !strings.Contains(result.Content[0].Text, "needle here") || strings.Contains(result.Content[0].Text, "first") {
		t.Fatalf("read=%+v err=%v", result, err)
	}
	grep := NewArtifactGrep(store, binding)
	result, err = grep.Run(context.Background(), json.RawMessage(`{"artifact_id":"`+ref.ID+`","pattern":"needle"}`), nil)
	if err != nil || result.IsError || !strings.Contains(result.Content[0].Text, "2:needle here") {
		t.Fatalf("grep=%+v err=%v", result, err)
	}

	binding.Set(session.NewMemoryStore(session.Options{ID: "other"}))
	result, err = read.Run(context.Background(), json.RawMessage(`{"artifact_id":"`+ref.ID+`"}`), nil)
	if err != nil || !result.IsError || !strings.Contains(result.Content[0].Text, "not found") {
		t.Fatalf("cross-session read=%+v err=%v", result, err)
	}
}
