//go:build darwin || linux

package main

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/web"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type versionsFixtureSeed struct {
	sessionID, path, sourceBranch, sourceTip, targetTip string
	target, source                                      session.BranchVersionSnapshot
}

// Seed only fictional persisted messages through the real SQLite/app branch
// APIs before any worker opens the session. Owner 63 and result 64 deliberately
// straddle the production 64-message preview boundary.
func seedWebVersionsFixture(t *testing.T, cwd string) versionsFixtureSeed {
	t.Helper()
	a, err := app.New(t.Context(), app.Options{CWD: cwd, Provider: "fake", Model: "fake-1", Permission: "deny", NoPlugins: true, NoMCP: true, NoSkills: true, Subagents: new(false), Debug: new(false), ManagedExplicitGoals: true})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := a.SetPermissionMode("deny"); err != nil {
		t.Fatal(err)
	}
	if err := a.Agent.SetMode(protocol.ModePlan); err != nil {
		t.Fatal(err)
	}
	appendMessage := func(message protocol.Message) {
		t.Helper()
		message.ParentID = a.Session.BranchTip()
		if err := a.Session.Append(session.Entry{ID: message.ID, Type: session.EntryMessage, Message: &message}); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 31 {
		appendMessage(protocol.NewUserMessage(fmt.Sprintf("fictional-user-%d", i), "", fmt.Sprintf("fictional request %d", i)))
		appendMessage(protocol.Message{ID: fmt.Sprintf("fictional-assistant-%d", i), Role: protocol.RoleAssistant, StopReason: protocol.StopStop, Content: []protocol.ContentBlock{protocol.NewTextBlock(fmt.Sprintf("fictional answer %d", i))}})
	}
	appendMessage(protocol.NewUserMessage("fictional-tool-user", "", "fictional saved tool turn"))
	appendMessage(protocol.Message{ID: "fictional-owner", Role: protocol.RoleAssistant, StopReason: protocol.StopToolUse, Content: []protocol.ContentBlock{
		{Type: protocol.BlockProviderData, Data: []byte("PRIVATE-VERSION-CONTINUITY")},
		{Type: protocol.BlockThinking, Text: "PRIVATE-VERSION-THINKING"},
		{Type: protocol.BlockToolCall, ToolCallID: "fictional-call", Name: "write", Arguments: []byte(`{"path":"fictional-restored-tool-must-not-run.txt","content":"PRIVATE-VERSION-ARGS"}`)},
	}})
	appendMessage(protocol.Message{ID: "fictional-result", Role: protocol.RoleTool, ToolCallID: "fictional-call", ToolName: "write", Content: []protocol.ContentBlock{protocol.NewTextBlock("PRIVATE-VERSION-RAW-RESULT")}, PublicToolResult: &protocol.ToolResultPreview{Text: "fictional public result"}})
	appendMessage(protocol.Message{ID: "fictional-original-final", Role: protocol.RoleAssistant, StopReason: protocol.StopStop, Content: []protocol.ContentBlock{protocol.NewTextBlock("fictional original final answer")}})
	targetTip := a.Session.BranchTip()
	fork, err := a.ForkBranch("")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Agent.SetMode(protocol.ModeDefault); err != nil {
		t.Fatal(err)
	}
	appendMessage(protocol.NewUserMessage("fictional-fork-user", "", "fictional fork continuation"))
	appendMessage(protocol.Message{ID: "fictional-fork-final", Role: protocol.RoleAssistant, StopReason: protocol.StopStop, Content: []protocol.ContentBlock{protocol.NewTextBlock("fictional fork answer")}})
	versions := a.Session.(session.BranchVersionStore)
	target, err := versions.BranchVersion(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	source, err := versions.BranchVersion(t.Context(), fork.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(target.Messages()) != 66 || target.Mode != protocol.ModePlan || source.Mode != protocol.ModeDefault {
		t.Fatal("fixture seed lost exact page/mode boundary")
	}
	return versionsFixtureSeed{sessionID: a.Session.ID(), path: a.Session.Path(), sourceBranch: fork.ID, sourceTip: a.Session.BranchTip(), targetTip: targetTip, target: target, source: source}
}

func TestWebVersionsRealWorkerReadOnlyPreviewAndIdleRestore(t *testing.T) {
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(directory, "project-a")
	if err := os.Mkdir(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", directory)
	t.Setenv("SNOW_HOME", filepath.Join(directory, "home"))
	t.Setenv("SNOW_SESSIONS_DIR", filepath.Join(directory, "sessions"))
	t.Setenv(messageEditFixtureEnv, directory)
	for _, name := range []string{policyFixtureEnv, permissionFixtureEnv, streamFixtureEnv, cancelFixtureEnv, managerExecutionEnv} {
		t.Setenv(name, "")
	}
	seed := seedWebVersionsFixture(t, cwd)
	registry, err := web.OpenRegistry(t.Context(), filepath.Join(directory, "manager"))
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	project, err := registry.Add(t.Context(), "fictional versions", cwd)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	manager := web.NewRuntimeManager(t.Context(), executable, filepath.Join(directory, "sessions"))
	defer manager.Close()
	snapshot, err := manager.Open(t.Context(), project, seed.sessionID, "fake", "fake-1")
	if err != nil {
		exit, _ := os.ReadFile(filepath.Join(directory, "worker-exit"))
		t.Fatalf("open: %v; worker exit %s", err, exit)
	}
	if !snapshot.VersionsEnabled || snapshot.Mode != "default" || snapshot.PermissionMode != "deny" || snapshot.Status != "idle" {
		t.Fatalf("worker launch/profile did not restore source: mode=%s permission=%s status=%s versions=%t", snapshot.Mode, snapshot.PermissionMode, snapshot.Status, snapshot.VersionsEnabled)
	}
	before := snapshot
	listed, err := manager.ListVersions(t.Context(), project.ID, snapshot.InstanceID, seed.sessionID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Versions) != 2 || listed.CurrentBranchID != seed.sourceBranch || listed.CurrentTipID != seed.sourceTip {
		t.Fatalf("list not bound to persisted source: %+v", listed)
	}
	preview, err := manager.PreviewVersion(t.Context(), project.ID, snapshot.InstanceID, seed.sessionID, "main", seed.targetTip, "")
	if err != nil {
		t.Fatal(err)
	}
	if !preview.HasMore || preview.NextCursor == "" {
		t.Fatal("first preview did not preserve page boundary")
	}
	assertWebVersionsPublicTool(t, preview.Messages)
	for _, message := range preview.Messages {
		if message.CanEdit || message.CanRegenerate {
			t.Fatal("read-only preview exposed mutation authority")
		}
	}
	tail, err := manager.PreviewVersion(t.Context(), project.ID, snapshot.InstanceID, seed.sessionID, "main", seed.targetTip, preview.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if tail.HasMore || tail.NextCursor != "" || len(tail.Messages) != 1 || tail.Messages[0].SourceID != "fictional-original-final" {
		t.Fatalf("wrong exact-tip continuation: %+v", tail)
	}
	serialized, _ := json.Marshal(preview)
	if strings.Contains(string(serialized), "PRIVATE-VERSION") {
		t.Fatal("preview exposed private stored blocks or raw tool payload")
	}
	after, _ := manager.Snapshot(project.ID)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("list/preview mutated active runtime snapshot")
	}
	prepared, err := manager.PrepareVersionRestore(t.Context(), project.ID, snapshot.InstanceID, seed.sessionID, seed.sourceBranch, seed.sourceTip, "main", seed.targetTip)
	if err != nil {
		t.Fatal(err)
	}
	after, _ = manager.Snapshot(project.ID)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("restore preparation selected a branch")
	}
	restored, err := manager.CommitVersionRestore(t.Context(), project.ID, snapshot.InstanceID, seed.sessionID, prepared.RestoreToken)
	if err != nil {
		t.Fatal(err)
	}
	if restored.InstanceID == snapshot.InstanceID || restored.SessionID != snapshot.SessionID || restored.SessionName != snapshot.SessionName || restored.Provider != snapshot.Provider || restored.Model != snapshot.Model || restored.PermissionMode != snapshot.PermissionMode || restored.Mode != "plan" || restored.Status != "idle" || restored.CancelToken != "" || restored.Queue != nil {
		t.Fatalf("restore ACK lost exact settings/idle authority rotation: %+v", restored)
	}
	assertWebVersionsPublicTool(t, restored.Messages)
	for _, message := range restored.Messages {
		if message.SourceID == "fictional-fork-user" || message.SourceID == "fictional-fork-final" {
			t.Fatal("restored history retained fork continuation")
		}
	}
	serialized, _ = json.Marshal(restored)
	if strings.Contains(string(serialized), "PRIVATE-VERSION") {
		t.Fatal("restore ACK exposed private history")
	}
	if err := manager.Prompt(t.Context(), project.ID, snapshot.InstanceID, "stale must not execute"); !errors.Is(err, web.ErrRuntimeInvalid) {
		t.Fatalf("old prompt authority accepted: %v", err)
	}
	if err := manager.SetPermissionMode(t.Context(), project.ID, snapshot.InstanceID, "allow", true); !errors.Is(err, web.ErrRuntimeInvalid) {
		t.Fatalf("old policy authority accepted: %v", err)
	}
	if _, err := manager.CommitVersionRestore(t.Context(), project.ID, restored.InstanceID, seed.sessionID, prepared.RestoreToken); !errors.Is(err, web.ErrRuntimeInvalid) {
		t.Fatalf("consumed restore token accepted: %v", err)
	}
	listed, err = manager.ListVersions(t.Context(), project.ID, restored.InstanceID, seed.sessionID, "")
	if err != nil || listed.CurrentBranchID != "main" || listed.CurrentTipID != seed.targetTip || len(listed.Versions) != 2 {
		t.Fatalf("restore deleted or misselected a branch: %+v %v", listed, err)
	}
	if err := manager.CloseProject(t.Context(), project.ID, restored.InstanceID); err != nil {
		t.Fatal(err)
	}
	assertWebVersionsNoExecution(t, directory, cwd)
	// Reopen the real durable store independently after the worker releases it:
	// exact IDs, parent links, opaque provider state and both continuations remain.
	store, err := session.OpenSQLiteStore(seed.path, cwd, session.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []session.BranchVersionSnapshot{seed.target, seed.source} {
		actual, err := store.BranchVersion(t.Context(), expected.Branch.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(actual.Entries, expected.Entries) || actual.Branch.TipID != expected.Branch.TipID || actual.Mode != expected.Mode {
			t.Fatal("restore mutated exact append-only history, parent links or saved mode")
		}
	}
	if store.ActiveBranchID() != "main" || store.BranchTip() != seed.targetTip {
		t.Fatal("durable active identity did not survive worker close")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := manager.Open(t.Context(), project, seed.sessionID, "fake", "fake-1")
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Mode != "plan" || reopened.Status != "idle" || reopened.PermissionMode != restored.PermissionMode {
		t.Fatalf("reopen resumed work or changed saved authority: %+v", reopened)
	}
	assertWebVersionsPublicTool(t, reopened.Messages)
	if err := manager.CloseProject(t.Context(), project.ID, reopened.InstanceID); err != nil {
		t.Fatal(err)
	}
	assertWebVersionsNoExecution(t, directory, cwd)
}

func assertWebVersionsPublicTool(t *testing.T, messages []web.RuntimeMessage) {
	t.Helper()
	found := false
	for _, message := range messages {
		for _, tool := range message.Tools {
			if tool.OwnerID == "fictional-owner" {
				found = true
				if tool.Status != "completed" || tool.ResultID != "fictional-result" || !tool.OutputAvailable || tool.Output != "fictional public result" {
					t.Fatalf("lost cross-page public tool outcome: %+v", tool)
				}
			}
		}
	}
	if !found {
		t.Fatal("missing cross-page tool owner")
	}
}
func assertWebVersionsNoExecution(t *testing.T, directory, cwd string) {
	t.Helper()
	// The shared real-worker fake logs before every provider request. No explicit
	// prompt is sent on any worker, avoiding its per-worker counter overwrite issue.
	calls, err := filepath.Glob(filepath.Join(directory, "context-*.json"))
	if err != nil || len(calls) != 0 {
		t.Fatalf("restore/open invoked provider or goal: %v %v", calls, err)
	}
	if _, err := os.Stat(filepath.Join(cwd, "fictional-restored-tool-must-not-run.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("historical tool executed: %v", err)
	}
}

// BUG151: a real restore emits the new root's ModeChanged before the commit
// response. Retiring that newly observed epoch hid the next prompt's permission
// request and answer even though the actual agent was waiting for approval.
// Keep this separate from the zero-execution fixture: every write below follows
// an explicit new user prompt and a real per-request permission decision.
func TestWebVersionsRealWorkerNextPromptPermissionAfterRestore(t *testing.T) {
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(directory, "project-a")
	if err := os.Mkdir(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", directory)
	t.Setenv("SNOW_HOME", filepath.Join(directory, "home"))
	t.Setenv("SNOW_SESSIONS_DIR", filepath.Join(directory, "sessions"))
	t.Setenv(policyFixtureEnv, directory)
	for _, name := range []string{messageEditFixtureEnv, permissionFixtureEnv, streamFixtureEnv, cancelFixtureEnv, managerExecutionEnv} {
		t.Setenv(name, "")
	}
	seed := seedWebVersionsFixture(t, cwd)
	registry, err := web.OpenRegistry(t.Context(), filepath.Join(directory, "manager"))
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	project, err := registry.Add(t.Context(), "fictional post-restore attention", cwd)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	manager := web.NewRuntimeManager(t.Context(), executable, filepath.Join(directory, "sessions"))
	defer manager.Close()
	source, err := manager.Open(t.Context(), project, seed.sessionID, "fake", "fake-1")
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := manager.PrepareVersionRestore(t.Context(), project.ID, source.InstanceID, seed.sessionID, seed.sourceBranch, seed.sourceTip, "main", seed.targetTip)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := manager.CommitVersionRestore(t.Context(), project.ID, source.InstanceID, seed.sessionID, prepared.RestoreToken)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Status != "idle" || restored.Mode != "plan" || restored.InstanceID == source.InstanceID {
		t.Fatal("restore did not acknowledge the saved idle mode/new instance")
	}
	if err := manager.SetMode(t.Context(), project.ID, restored.InstanceID, "default"); err != nil {
		t.Fatal(err)
	}
	if err := manager.SetPermissionMode(t.Context(), project.ID, restored.InstanceID, "ask", false); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		prompt   string
		decision protocol.PermissionDecision
		answer   string
	}{
		{"write-restored-allow", protocol.PermissionAllow, "tool succeeded"},
		{"write-restored-deny", protocol.PermissionDeny, "tool denied"},
	} {
		if err := manager.Prompt(t.Context(), project.ID, restored.InstanceID, test.prompt); err != nil {
			t.Fatal(err)
		}
		pending := policyFixtureWait(t, manager, project.ID, func(s web.RuntimeSnapshot) bool { return s.Permission != nil })
		if pending.Permission.Tool != "write" || pending.PermissionMode != "ask" || pending.Mode != "default" {
			t.Fatal("post-restore attention lost actual mode/policy/tool")
		}
		path := filepath.Join(cwd, test.prompt+".txt")
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("write executed before visible approval")
		}
		if err := manager.ReplyPermission(t.Context(), project.ID, source.InstanceID, pending.Permission.ID, protocol.PermissionAllow); !errors.Is(err, web.ErrRuntimeInvalid) {
			t.Fatalf("old instance accepted the new prompt's approval: %v", err)
		}
		if err := manager.ReplyPermission(t.Context(), project.ID, restored.InstanceID, pending.Permission.ID, test.decision); err != nil {
			t.Fatal(err)
		}
		done := policyFixtureWait(t, manager, project.ID, func(s web.RuntimeSnapshot) bool { return s.Status == "idle" })
		if done.Permission != nil || done.PermissionMode != "ask" || len(done.Messages) == 0 || done.Messages[len(done.Messages)-1].Text != test.answer {
			t.Fatalf("new-epoch completion was hidden or policy escalated: status=%s permission=%s messages=%d", done.Status, done.PermissionMode, len(done.Messages))
		}
		data, err := os.ReadFile(path)
		if test.decision == protocol.PermissionAllow {
			if err != nil || string(data) != "policy-write-safe\n" {
				t.Fatalf("approved real write failed: %q %v", data, err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatal("denied real write executed")
		}
	}
	if _, err := os.Stat(filepath.Join(cwd, "fictional-restored-tool-must-not-run.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("restore replayed a historical tool")
	}
}
