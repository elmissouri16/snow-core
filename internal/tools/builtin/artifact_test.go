package builtin

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/snow-core/snow/internal/artifact"
	"github.com/snow-core/snow/internal/session"
)

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
