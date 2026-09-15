package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/agentclient/process"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestCatalogCLIHelperProcess(t *testing.T) {
	if os.Getenv("SNOW_CATALOG_TEST_HELPER") != "1" {
		return
	}
	os.Args = []string{"snow", "--mode", "rpc", "--rpc-startup", "catalog"}
	if err := run(); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

func TestCatalogCLIUsesOnlyReadOnlyStorage(t *testing.T) {
	cwd, home, sessions := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("SNOW_HOME", home)
	t.Setenv("SNOW_SESSIONS_DIR", sessions)
	t.Setenv("SNOW_CATALOG_TEST_HELPER", "1")
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte("invalid config must not be read"), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := session.NewFileIndex(sessions).Create(cwd)
	if err != nil {
		t.Fatal(err)
	}
	message := protocol.Message{Role: "user", Content: []protocol.ContentBlock{{Type: "text", Text: "Saved CLI catalog fixture"}}}
	if err := store.Append(session.Entry{Type: session.EntryMessage, Message: &message}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	worker, err := process.Start(ctx, process.Options{Executable: executable, Args: []string{"-test.run=^TestCatalogCLIHelperProcess$"}, Dir: cwd})
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close()
	if ready := worker.Client.Ready(); !slices.Equal(ready.Capabilities, []string{"runtime_free_catalog", "catalog_sessions", "catalog_messages", "catalog_public_tools", "catalog_image", "history_images"}) {
		t.Fatalf("catalog handshake = %+v", ready)
	}
	response, err := worker.Client.Call(ctx, protocol.RPCRequest{Type: "catalog_sessions"})
	if err != nil || !response.Success {
		t.Fatalf("catalog sessions: %+v, %v", response, err)
	}
	for _, command := range []string{"prompt", "session_open", "session_delete"} {
		response, err := worker.Client.Call(ctx, protocol.RPCRequest{Type: command})
		if err != nil || response.Success {
			t.Fatalf("catalog accepted mutation %s: %v", command, err)
		}
	}
	if err := worker.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(home)
	if err != nil || len(entries) != 1 || entries[0].Name() != "config.json" {
		t.Fatal("catalog loaded or mutated runtime home")
	}
}

func TestCatalogCLIRejectsRuntimeFlags(t *testing.T) {
	for _, args := range [][]string{
		{"--rpc-startup", "catalog"},
		{"--mode", "rpc", "--rpc-startup", "unknown"},
		{"--mode", "rpc", "--rpc-startup", "catalog", "--provider", "fake"},
		{"--mode", "rpc", "--rpc-startup", "catalog", "--no-session"},
		{"--mode", "rpc", "--rpc-startup", "catalog", "-p", "never execute"},
		{"--mode", "rpc", "--rpc-startup", "catalog", "resume"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			previous := os.Args
			os.Args = append([]string{"snow"}, args...)
			t.Cleanup(func() { os.Args = previous })
			if err := run(); err == nil {
				t.Fatal("catalog runtime flags accepted")
			}
		})
	}
}
