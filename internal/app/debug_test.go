package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func newDebugTestApp(t *testing.T, opts Options) *App {
	t.Helper()
	if opts.CWD == "" {
		opts.CWD = t.TempDir()
	}
	opts.Provider = "fake"
	opts.NoSession = true
	opts.NoPlugins = true
	opts.NoMCP = true
	opts.NoSkills = true
	opts.Permission = "deny"
	a, err := New(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

func TestDebugEnablementDefaultsOffAndRuntimeOverrideWins(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	cfg := config.Default()
	cfg.Debug.Enabled = true
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatal(err)
	}

	fromConfig := newDebugTestApp(t, Options{ConfigPath: configPath})
	if !fromConfig.DebugStatus().Enabled {
		t.Fatal("persisted debug enablement was not applied")
	}
	fromConfig.SetDebugEnabled(false)

	disabled := false
	overridden := newDebugTestApp(t, Options{ConfigPath: configPath, Debug: &disabled})
	if overridden.DebugStatus().Enabled {
		t.Fatal("runtime false override did not win")
	}

	defaultApp := newDebugTestApp(t, Options{ConfigPath: filepath.Join(home, "missing.json")})
	if defaultApp.DebugStatus().Enabled {
		t.Fatal("debug diagnostics must default off")
	}
}

func TestCreateDebugDumpCapturesFullSessionButExcludesPrivateData(t *testing.T) {
	enabled := true
	a := newDebugTestApp(t, Options{Debug: &enabled, BuildVersion: "test-version", APIKey: "unlabeled-runtime-secret"})

	user := protocol.NewUserMessage("user-1", "", "investigate this api_key=super-secret-value")
	assistant := protocol.Message{
		ID: "assistant-1", ParentID: user.ID, Role: protocol.RoleAssistant, Provider: "fake", Model: "fake-1", StopReason: protocol.StopToolUse,
		Content: []protocol.ContentBlock{
			{Type: protocol.BlockThinking, Text: "private reasoning detail"},
			{Type: protocol.BlockToolCall, ToolCallID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"important.txt","authorization":"Bearer hidden-token"}`)},
			{Type: protocol.BlockProviderData, Data: []byte("opaque-provider-continuity")},
		},
	}
	result := protocol.NewToolResultMessage("result-1", assistant.ID, "call-1", "read", []protocol.ContentBlock{{Type: protocol.BlockText, Text: "complete unique tool output"}}, false)
	for _, message := range []protocol.Message{user, assistant, result} {
		message := message
		if err := a.Session.Append(session.Entry{Type: session.EntryMessage, ID: message.ID, ParentID: message.ParentID, Message: &message}); err != nil {
			t.Fatal(err)
		}
	}
	a.Agent.Publish(protocol.AgentEvent{Type: protocol.EvError, Message: "provider failed with password=hunter2 and unlabeled-runtime-secret"})
	if err := a.Agent.DrainEvents(context.Background()); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "diagnostic.json")
	resolved, err := a.CreateDebugDump(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != path {
		t.Fatalf("resolved path=%q want=%q", resolved, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, required := range []string{"snow-diagnostic-v1", "Sensitive diagnostic data", "private reasoning detail", "important.txt", "complete unique tool output", "provider failed", "provider_data_blocks_omitted", "test-version"} {
		if !strings.Contains(text, required) {
			t.Fatalf("dump missing %q", required)
		}
	}
	for _, forbidden := range []string{"super-secret-value", "hidden-token", "hunter2", "unlabeled-runtime-secret", "opaque-provider-continuity", `"type": "provider_data"`} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("dump leaked %q", forbidden)
		}
	}
	if status := a.DebugStatus(); status.EventCount == 0 {
		t.Fatalf("event was not captured: %+v", status)
	}
}

func TestCreateDebugDumpHoldsPromptAdmissionForStableSession(t *testing.T) {
	enabled := true
	a := newDebugTestApp(t, Options{Debug: &enabled})
	store := &blockingSessionStore{
		MemoryStore: session.NewMemoryStore(session.Options{CWD: a.CWD()}),
		started:     make(chan struct{}),
		release:     make(chan struct{}),
		closed:      make(chan struct{}),
	}
	if err := a.SetSession(store); err != nil {
		t.Fatal(err)
	}

	dumpPath := filepath.Join(t.TempDir(), "stable.json")
	dumpDone := make(chan error, 1)
	go func() {
		_, err := a.CreateDebugDump(context.Background(), dumpPath)
		dumpDone <- err
	}()
	select {
	case <-store.started:
	case <-time.After(time.Second):
		t.Fatal("dump did not begin reading the session")
	}
	promptDone := make(chan error, 1)
	go func() { promptDone <- a.Agent.Prompt(context.Background(), "after snapshot") }()
	select {
	case err := <-promptDone:
		t.Fatalf("prompt crossed active diagnostic snapshot: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(store.release)
	if err := <-dumpDone; err != nil {
		t.Fatal(err)
	}
	if err := <-promptDone; err != nil {
		t.Fatal(err)
	}
}

func TestDefaultDebugDumpPathSanitizesSessionID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SNOW_HOME", home)
	a := &App{cwd: t.TempDir()}
	path, err := a.resolveDebugDumpPath("", `../../escape\\name`)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(home, "diagnostics") + string(os.PathSeparator)
	if !strings.HasPrefix(path, root) || strings.Contains(filepath.Base(path), "..") {
		t.Fatalf("unsafe generated path %q (root %q)", path, root)
	}
}

func TestCreateDebugDumpRejectsNonRegularDestination(t *testing.T) {
	enabled := true
	a := newDebugTestApp(t, Options{Debug: &enabled})
	if _, err := a.CreateDebugDump(context.Background(), t.TempDir()); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("directory dump error=%v", err)
	}
}
