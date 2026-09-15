//go:build darwin || linux

package main

import (
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/web"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// These tests reuse only the real CLI-worker launch/profile fixtures. SQLite,
// managed core admission, JSONL RPC, history projection and built-in tool gates
// are production implementations. The fake provider and every durable file
// belong to a private temporary HOME; no provider network or user manager runs.
type historyControlsRealFixture struct {
	directory, cwd string
	seed           versionsFixtureSeed
	manager        *web.RuntimeManager
	project        web.Project
	initial        web.RuntimeSnapshot
}

func newHistoryControlsRealFixture(t *testing.T, attention bool) historyControlsRealFixture {
	t.Helper()
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
	for _, name := range []string{messageEditFixtureEnv, policyFixtureEnv, permissionFixtureEnv, streamFixtureEnv, cancelFixtureEnv, managerExecutionEnv} {
		t.Setenv(name, "")
	}
	if attention {
		t.Setenv(policyFixtureEnv, directory)
	} else {
		t.Setenv(messageEditFixtureEnv, directory)
	}
	seed := seedWebVersionsFixture(t, cwd)
	registry, err := web.OpenRegistry(t.Context(), filepath.Join(directory, "manager"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	project, err := registry.Add(t.Context(), "Fictional ordinary history controls", cwd)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	manager := web.NewRuntimeManager(t.Context(), executable, filepath.Join(directory, "sessions"))
	t.Cleanup(func() { _ = manager.Close() })
	snapshot, err := manager.Open(t.Context(), project, seed.sessionID, "fake", "fake-1")
	if err != nil {
		exit, _ := os.ReadFile(filepath.Join(directory, "worker-exit"))
		t.Fatalf("open real worker: %v (exit %s)", err, exit)
	}
	if !snapshot.VersionsEnabled || !snapshot.HistoryControlEnabled || snapshot.Mode != "default" || snapshot.PermissionMode != "deny" || snapshot.Status != "idle" {
		t.Fatalf("incorrect fixed worker profile: %+v", snapshot)
	}
	return historyControlsRealFixture{directory: directory, cwd: cwd, seed: seed, manager: manager, project: project, initial: snapshot}
}

func (f historyControlsRealFixture) request(t *testing.T, name string) web.RuntimeHistoryControlRequest {
	t.Helper()
	current, ok := f.manager.Snapshot(f.project.ID)
	if !ok {
		t.Fatal("current runtime missing")
	}
	page, err := f.manager.ListVersions(t.Context(), f.project.ID, current.InstanceID, current.SessionID, "")
	if err != nil {
		t.Fatal(err)
	}
	var target web.RuntimeVersion
	for _, branch := range page.Versions {
		if branch.BranchID == "main" {
			target = branch
		}
	}
	if target.BranchID == "" {
		t.Fatal("saved target missing")
	}
	preview, err := f.manager.PreviewVersion(t.Context(), f.project.ID, current.InstanceID, current.SessionID, target.BranchID, target.TipID, "")
	if err != nil {
		t.Fatal(err)
	}
	if page.Revision != preview.Revision || preview.Revision != current.Revision {
		t.Fatal("public selection not bound to one snapshot revision")
	}
	return web.RuntimeHistoryControlRequest{SessionID: current.SessionID, SourceBranchID: page.CurrentBranchID, SourceTipID: page.CurrentTipID, TargetBranchID: target.BranchID, TargetTipID: target.TipID, ExpectedRevision: preview.Revision, Name: name, OldName: target.Name}
}

func TestWebHistoryControlsRealWorkerPassiveForkRenameAndDetached(t *testing.T) {
	f := newHistoryControlsRealFixture(t, false)
	request := f.request(t, "Reviewed saved history")
	renamed, err := f.manager.HistoryBranchRename(t.Context(), f.project.ID, f.initial.InstanceID, request)
	if err != nil {
		t.Fatal(err)
	}
	afterRename, _ := f.manager.Snapshot(f.project.ID)
	expected := f.initial
	expected.Revision = afterRename.Revision
	if !reflect.DeepEqual(expected, afterRename) || renamed.BranchID != "main" || renamed.Name != request.Name {
		t.Fatal("rename changed current history, mode, policy or instance")
	}
	page, err := f.manager.ListVersions(t.Context(), f.project.ID, afterRename.InstanceID, afterRename.SessionID, "")
	if err != nil {
		t.Fatal(err)
	}
	if page.CurrentBranchID != f.seed.sourceBranch || page.CurrentTipID != f.seed.sourceTip {
		t.Fatal("rename selected its inactive target")
	}
	found := false
	for _, branch := range page.Versions {
		if branch.BranchID == "main" {
			found = branch.Name == request.Name && branch.TipID == f.seed.targetTip
		}
	}
	if !found {
		t.Fatal("renamed target label not visible through bounded Versions")
	}

	request = f.request(t, "Detached reviewed conversation")
	detached, err := f.manager.HistorySessionFork(t.Context(), f.project.ID, afterRename.InstanceID, request)
	if err != nil {
		t.Fatal(err)
	}
	afterDetached, _ := f.manager.Snapshot(f.project.ID)
	if !reflect.DeepEqual(afterRename, afterDetached) || detached.ChildSessionID == "" || detached.ChildSessionID == f.initial.SessionID || detached.SessionID != f.initial.SessionID || detached.InstanceID != f.initial.InstanceID {
		t.Fatal("detached child became current or modified the parent")
	}
	inventory, err := session.NewFileIndex(filepath.Join(f.directory, "sessions")).List(f.cwd)
	if err != nil {
		t.Fatal(err)
	}
	childIndex := slices.IndexFunc(inventory, func(info session.SessionInfo) bool { return info.ID == detached.ChildSessionID })
	if len(inventory) != 2 || childIndex < 0 || inventory[childIndex].Name != request.Name {
		t.Fatalf("detached child missing from read-only inventory: %+v", inventory)
	}
	assertWebVersionsNoExecution(t, f.directory, f.cwd)

	request = f.request(t, "Active ordinary branch")
	forked, err := f.manager.HistoryBranchFork(t.Context(), f.project.ID, afterDetached.InstanceID, request)
	if err != nil {
		t.Fatal(err)
	}
	if forked.InstanceID == afterDetached.InstanceID || forked.SessionID != f.initial.SessionID || forked.Mode != "plan" || forked.Thinking != "off" || forked.Provider != f.initial.Provider || forked.Model != f.initial.Model || forked.PermissionMode != f.initial.PermissionMode || forked.Status != "idle" || forked.CancelToken != "" || forked.Queue != nil {
		t.Fatalf("fork lost mode/effective thinking/policy or retained old controls: %+v", forked)
	}
	assertWebVersionsPublicTool(t, forked.Messages)
	for _, message := range forked.Messages {
		if message.SourceID == "fictional-fork-user" || message.SourceID == "fictional-fork-final" {
			t.Fatal("fork loaded outgoing rather than selected history")
		}
	}
	raw, _ := json.Marshal(forked)
	if strings.Contains(string(raw), "PRIVATE-VERSION") {
		t.Fatal("fork exposed raw tool payload or provider-private history")
	}
	page, err = f.manager.ListVersions(t.Context(), f.project.ID, forked.InstanceID, forked.SessionID, "")
	if err != nil || len(page.Versions) != 3 || page.CurrentBranchID == f.seed.sourceBranch || page.CurrentBranchID == "main" || page.CurrentTipID != f.seed.targetTip {
		t.Fatalf("fork not bound to its new active saved tip: %+v %v", page, err)
	}
	newBranch := page.CurrentBranchID
	if err := f.manager.Prompt(t.Context(), f.project.ID, f.initial.InstanceID, "stale prompt must not execute"); !errors.Is(err, web.ErrRuntimeInvalid) {
		t.Fatalf("stale instance authorizes work: %v", err)
	}
	assertWebVersionsNoExecution(t, f.directory, f.cwd)
	if err := f.manager.CloseProject(t.Context(), f.project.ID, forked.InstanceID); err != nil {
		t.Fatal(err)
	}

	// Independently reopen the durable database after the worker releases it.
	// Both original histories and exact provider continuity remain append-only.
	store, err := session.OpenSQLiteStore(f.seed.path, f.cwd, session.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []session.BranchVersionSnapshot{f.seed.target, f.seed.source} {
		got, err := store.BranchVersion(t.Context(), want.Branch.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got.Entries, want.Entries) || got.Branch.TipID != want.Branch.TipID || got.Mode != want.Mode {
			t.Fatal("ordinary history control rewrote persisted ancestry or mode")
		}
	}
	if store.ActiveBranchID() != newBranch || store.BranchTip() != f.seed.targetTip {
		t.Fatal("fork active cursor was not durable")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	child, err := session.OpenSQLiteStore(inventory[childIndex].Path, f.cwd, session.Options{})
	if err != nil {
		t.Fatal(err)
	}
	childVersion, err := child.BranchVersion(t.Context(), child.ActiveBranchID())
	if err != nil {
		t.Fatal(err)
	}
	if childVersion.Mode != protocol.ModePlan || !reflect.DeepEqual(childVersion.Messages(), f.seed.target.Messages()) || child.Header().ParentSessionID != f.seed.sessionID || child.Header().ParentBranchID != "main" {
		t.Fatal("detached copy lost selected history, provenance or saved mode")
	}
	if err := child.Close(); err != nil {
		t.Fatal(err)
	}
	assertWebVersionsNoExecution(t, f.directory, f.cwd)

	// Opening the detached child is a separate explicit user operation. It does
	// not replay a provider/tool call and does not silently select the parent.
	opened, err := f.manager.Open(t.Context(), f.project, detached.ChildSessionID, "fake", "fake-1")
	if err != nil {
		t.Fatal(err)
	}
	if opened.SessionID != detached.ChildSessionID || opened.Mode != "plan" || opened.PermissionMode != "deny" || opened.Status != "idle" {
		t.Fatal("explicit child open lost saved mode or started work")
	}
	assertWebVersionsNoExecution(t, f.directory, f.cwd)
}

func TestWebHistoryControlsRealWorkerSourceTargetAndNameCAS(t *testing.T) {
	f := newHistoryControlsRealFixture(t, false)
	cases := []struct {
		name   string
		change func(*web.RuntimeHistoryControlRequest)
	}{
		{"source branch", func(p *web.RuntimeHistoryControlRequest) { p.SourceBranchID = "main" }},
		{"source tip", func(p *web.RuntimeHistoryControlRequest) { p.SourceTipID = f.seed.targetTip }},
		{"target branch", func(p *web.RuntimeHistoryControlRequest) { p.TargetBranchID = "missing-saved-branch" }},
		{"target saved tip", func(p *web.RuntimeHistoryControlRequest) { p.TargetTipID = "fictional-user-0" }},
		{"session", func(p *web.RuntimeHistoryControlRequest) { p.SessionID = "wrong-session" }},
		{"snapshot revision", func(p *web.RuntimeHistoryControlRequest) { p.ExpectedRevision++ }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			request := f.request(t, "Must not be created")
			test.change(&request)
			if _, err := f.manager.HistoryBranchFork(t.Context(), f.project.ID, f.initial.InstanceID, request); !errors.Is(err, web.ErrRuntimeInvalid) {
				t.Fatalf("stale CAS accepted or unknown: %v", err)
			}
			current, _ := f.manager.Snapshot(f.project.ID)
			expected := f.initial
			expected.Revision = current.Revision
			if !reflect.DeepEqual(expected, current) {
				t.Fatal("known CAS rejection mutated current authority/history")
			}
		})
	}
	request := f.request(t, "Must not rename")
	request.OldName = "Obsolete target label"
	if _, err := f.manager.HistoryBranchRename(t.Context(), f.project.ID, f.initial.InstanceID, request); !errors.Is(err, web.ErrRuntimeInvalid) {
		t.Fatalf("stale label accepted: %v", err)
	}
	request = f.request(t, "Must not detach")
	request.TargetTipID = "fictional-user-0"
	if _, err := f.manager.HistorySessionFork(t.Context(), f.project.ID, f.initial.InstanceID, request); !errors.Is(err, web.ErrRuntimeInvalid) {
		t.Fatalf("arbitrary historical entry accepted as saved tip: %v", err)
	}
	inventory, err := session.NewFileIndex(filepath.Join(f.directory, "sessions")).List(f.cwd)
	if err != nil || len(inventory) != 1 {
		t.Fatalf("rejected operation created a child: %+v %v", inventory, err)
	}
	page, err := f.manager.ListVersions(t.Context(), f.project.ID, f.initial.InstanceID, f.initial.SessionID, "")
	if err != nil || len(page.Versions) != 2 || page.CurrentBranchID != f.seed.sourceBranch || page.CurrentTipID != f.seed.sourceTip {
		t.Fatal("rejected operation changed durable branch metadata")
	}
	assertWebVersionsNoExecution(t, f.directory, f.cwd)
}

// Core emits new-root metadata during the managed fork before its ACK. The
// deterministic web fixture tests both ACK/event orderings; this actual worker
// regression proves its next prompt's real Ask permission remains visible.
func TestWebHistoryControlsRealWorkerNextPromptAskAfterFork(t *testing.T) {
	f := newHistoryControlsRealFixture(t, true)
	if err := f.manager.SetPermissionMode(t.Context(), f.project.ID, f.initial.InstanceID, "ask", false); err != nil {
		t.Fatal(err)
	}
	request := f.request(t, "Explicit fork before Ask prompt")
	forked, err := f.manager.HistoryBranchFork(t.Context(), f.project.ID, f.initial.InstanceID, request)
	if err != nil {
		t.Fatal(err)
	}
	if forked.Mode != "plan" || forked.PermissionMode != "ask" || forked.InstanceID == f.initial.InstanceID {
		t.Fatal("fork changed permissions or lost new mode/instance")
	}
	if err := f.manager.SetMode(t.Context(), f.project.ID, forked.InstanceID, "default"); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		prompt   string
		decision protocol.PermissionDecision
		answer   string
	}{
		{"write-fork-allow", protocol.PermissionAllow, "tool succeeded"},
		{"write-fork-deny", protocol.PermissionDeny, "tool denied"},
	} {
		if err := f.manager.Prompt(t.Context(), f.project.ID, forked.InstanceID, test.prompt); err != nil {
			t.Fatal(err)
		}
		pending := policyFixtureWait(t, f.manager, f.project.ID, func(s web.RuntimeSnapshot) bool { return s.Permission != nil })
		if pending.Permission.Tool != "write" || pending.PermissionMode != "ask" || pending.Mode != "default" {
			t.Fatal("new-root Ask attention lost mode/policy/tool")
		}
		path := filepath.Join(f.cwd, test.prompt+".txt")
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("tool ran before explicit approval")
		}
		if err := f.manager.ReplyPermission(t.Context(), f.project.ID, f.initial.InstanceID, pending.Permission.ID, protocol.PermissionAllow); !errors.Is(err, web.ErrRuntimeInvalid) {
			t.Fatalf("outgoing instance authorized new-root attention: %v", err)
		}
		if err := f.manager.ReplyPermission(t.Context(), f.project.ID, forked.InstanceID, pending.Permission.ID, test.decision); err != nil {
			t.Fatal(err)
		}
		done := policyFixtureWait(t, f.manager, f.project.ID, func(s web.RuntimeSnapshot) bool { return s.Status == "idle" })
		if done.Permission != nil || done.PermissionMode != "ask" || len(done.Messages) == 0 || done.Messages[len(done.Messages)-1].Text != test.answer {
			t.Fatal("new-root answer/completion missing or permission escalated")
		}
		data, err := os.ReadFile(path)
		if test.decision == protocol.PermissionAllow {
			if err != nil || string(data) != "policy-write-safe\n" {
				t.Fatalf("explicitly approved write failed: %q %v", data, err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatal("denied write ran")
		}
	}
	if _, err := os.Stat(filepath.Join(f.cwd, "fictional-restored-tool-must-not-run.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("fork replayed historical tool effects")
	}
}
