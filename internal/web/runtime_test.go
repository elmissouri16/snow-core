package web

import (
	"context"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func runtimeTestManager(t *testing.T, mode string) (*RuntimeManager, []Project, string) {
	t.Helper()
	base := t.TempDir()
	registry := registryTestOpen(t, filepath.Join(base, "manager"))
	projects := make([]Project, 3)
	for i, name := range []string{"one", "two", "three"} {
		path := registryTestDirectory(t, base, name)
		project, err := registry.Add(t.Context(), name, path)
		if err != nil {
			t.Fatal(err)
		}
		projects[i] = project
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	m := NewRuntimeManager(t.Context(), executable, filepath.Join(base, "sessions"))
	log := filepath.Join(base, "worker.log")
	m.env = append(m.env, "SNOW_WEB_RUNTIME_TEST_CHILD=1", "SNOW_WEB_RUNTIME_TEST_MODE="+mode, "SNOW_WEB_RUNTIME_TEST_LOG="+log)
	t.Cleanup(func() { _ = m.Close() })
	return m, projects, log
}

func runtimeWait(t *testing.T, m *RuntimeManager, projectID string, predicate func(RuntimeSnapshot) bool) RuntimeSnapshot {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, ok := m.Snapshot(projectID)
		if ok && predicate(snapshot) {
			return snapshot
		}
		time.Sleep(time.Millisecond)
	}
	snapshot, _ := m.Snapshot(projectID)
	t.Fatalf("snapshot never reached desired state: %+v", snapshot)
	return RuntimeSnapshot{}
}

func TestRuntimeExplicitActivationAndDurableSwitch(t *testing.T) {
	m, projects, log := runtimeTestManager(t, "")
	if _, ok := m.Snapshot(projects[0].ID); ok {
		t.Fatal("construction activated runtime")
	}
	if _, err := os.Stat(log); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("construction started worker")
	}
	snapshot, err := m.Open(t.Context(), projects[0], "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != "idle" || snapshot.SessionID != "session-created" || snapshot.Provider != "host-provider" || snapshot.Model != "host-model" {
		t.Fatalf("snapshot: %+v", snapshot)
	}
	if len(snapshot.Messages) != 2 || snapshot.Messages[1].Text != "prior assistant" {
		t.Fatalf("history: %+v", snapshot.Messages)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "session_create\nabort\nsession_info\nmessages_page\n" {
		t.Fatalf("startup operations: %q", data)
	}
	snapshot.Messages[0].Text = "tampered"
	again, _ := m.Snapshot(projects[0].ID)
	if again.Messages[0].Text != "prior user" {
		t.Fatal("snapshot mutation leaked")
	}
	same, err := m.Open(t.Context(), projects[0], "session-created", "", "")
	if err != nil || same.SessionID != snapshot.SessionID {
		t.Fatal("same-session open not idempotent", err)
	}
	if _, err := m.Open(t.Context(), projects[0], "other", "", ""); !errors.Is(err, ErrRuntimeBusy) {
		t.Fatalf("implicit switch: %v", err)
	}
	if err := m.CloseProject(t.Context(), projects[0].ID, runtimeTestInstance(t, m, projects[0].ID)); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Snapshot(projects[0].ID); ok {
		t.Fatal("closed snapshot retained")
	}
	reopened, err := m.Open(t.Context(), projects[0], "session-existing", "", "")
	if err != nil || reopened.SessionID != "session-existing" {
		t.Fatalf("reopen: %+v %v", reopened, err)
	}
}

func TestRuntimePromptStreamAndDefinitiveCompletion(t *testing.T) {
	m, projects, _ := runtimeTestManager(t, "")
	project := projects[0]
	if _, err := m.Open(t.Context(), project, "", "", ""); err != nil {
		t.Fatal(err)
	}
	requestCtx, cancel := context.WithCancel(t.Context())
	if err := m.Prompt(requestCtx, project.ID, runtimeTestInstance(t, m, project.ID), "hold"); err != nil {
		t.Fatal(err)
	}
	cancel()
	time.Sleep(10 * time.Millisecond)
	snapshot, _ := m.Snapshot(project.ID)
	if snapshot.Status != "running" {
		t.Fatalf("turn_done/browser cancel ended prompt: %+v", snapshot)
	}
	if err := m.Prompt(t.Context(), project.ID, runtimeTestInstance(t, m, project.ID), "overlap"); !errors.Is(err, ErrRuntimeBusy) {
		t.Fatalf("overlap = %v", err)
	}
	if err := m.Abort(t.Context(), project.ID, runtimeTestInstance(t, m, project.ID)); err != nil {
		t.Fatal(err)
	}
	runtimeWait(t, m, project.ID, func(s RuntimeSnapshot) bool { return s.Status == "idle" })
	if err := m.Prompt(t.Context(), project.ID, runtimeTestInstance(t, m, project.ID), "complete"); err != nil {
		t.Fatal(err)
	}
	snapshot = runtimeWait(t, m, project.ID, func(s RuntimeSnapshot) bool { return s.Status == "idle" })
	if snapshot.Messages[len(snapshot.Messages)-1].Text != "public answer" {
		t.Fatalf("answer: %+v", snapshot.Messages)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "SECRET") {
		t.Fatalf("private stream data leaked: %s", encoded)
	}
	if !strings.Contains(string(encoded), `"role":"assistant"`) || !strings.Contains(string(encoded), `"project_id"`) {
		t.Fatalf("unexpected JSON projection: %s", encoded)
	}
	if err := m.Prompt(t.Context(), project.ID, runtimeTestInstance(t, m, project.ID), "fail"); err != nil {
		t.Fatal(err)
	}
	failed := runtimeWait(t, m, project.ID, func(s RuntimeSnapshot) bool { return s.Status == "idle" && s.Error != "" })
	if strings.Contains(failed.Error, "SECRET") {
		t.Fatal("remote completion error escaped")
	}
	if !strings.Contains(failed.Error, "Reasona failed: upstream returned 503") {
		t.Fatalf("public prompt error missing: %q", failed.Error)
	}
}

func TestRuntimePermissionAndInputReplies(t *testing.T) {
	m, projects, _ := runtimeTestManager(t, "")
	id := projects[0].ID
	if _, err := m.Open(t.Context(), projects[0], "", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := m.Prompt(t.Context(), id, runtimeTestInstance(t, m, id), "permission"); err != nil {
		t.Fatal(err)
	}
	snapshot := runtimeWait(t, m, id, func(s RuntimeSnapshot) bool { return s.Permission != nil })
	if snapshot.Permission.Effects[0].Command != "echo hello" {
		t.Fatal("permission lacks required public effect")
	}
	encoded, _ := json.Marshal(snapshot)
	if strings.Contains(string(encoded), "SECRET") {
		t.Fatalf("raw args leaked: %s", encoded)
	}
	snapshot.Permission.Paths[0] = "tampered"
	snapshot.Permission.Effects[0].Command = "tampered"
	again, _ := m.Snapshot(id)
	if again.Permission.Paths[0] == "tampered" || again.Permission.Effects[0].Command == "tampered" {
		t.Fatal("permission not immutable")
	}
	for _, decision := range []protocol.PermissionDecision{protocol.PermissionAllowAlways, protocol.PermissionAllowSession, "bad"} {
		if err := m.ReplyPermission(t.Context(), id, runtimeTestInstance(t, m, id), "permission-1", decision); !errors.Is(err, ErrRuntimeInvalid) {
			t.Fatalf("unsafe decision: %v", err)
		}
	}
	if err := m.ReplyPermission(t.Context(), id, runtimeTestInstance(t, m, id), "stale", protocol.PermissionAllow); !errors.Is(err, ErrRuntimeInvalid) {
		t.Fatal("stale permission accepted")
	}
	if err := m.ReplyPermission(t.Context(), id, runtimeTestInstance(t, m, id), "permission-1", protocol.PermissionAllow); err != nil {
		t.Fatal(err)
	}
	runtimeWait(t, m, id, func(s RuntimeSnapshot) bool { return s.Status == "idle" })
	if err := m.ReplyPermission(t.Context(), id, runtimeTestInstance(t, m, id), "permission-1", protocol.PermissionAllow); !errors.Is(err, ErrRuntimeInvalid) {
		t.Fatal("duplicate permission accepted")
	}
	if err := m.Prompt(t.Context(), id, runtimeTestInstance(t, m, id), "input"); err != nil {
		t.Fatal(err)
	}
	snapshot = runtimeWait(t, m, id, func(s RuntimeSnapshot) bool { return s.Input != nil })
	snapshot.Input.Questions[0].Options[0].Label = "tampered"
	again, _ = m.Snapshot(id)
	if again.Input.Questions[0].Options[0].Label != "one" {
		t.Fatal("input not immutable")
	}
	response := protocol.UserInputResponse{RequestID: "input-1", Answers: []protocol.UserInputAnswer{{QuestionID: "question-1", Answer: "bad"}}}
	if err := m.ReplyInput(t.Context(), id, runtimeTestInstance(t, m, id), response); !errors.Is(err, ErrRuntimeInvalid) {
		t.Fatalf("invalid choice accepted: %v", err)
	}
	response.Answers[0].Answer = "one"
	if err := m.ReplyInput(t.Context(), id, runtimeTestInstance(t, m, id), response); err != nil {
		t.Fatal(err)
	}
	runtimeWait(t, m, id, func(s RuntimeSnapshot) bool { return s.Status == "idle" })
	if err := m.Prompt(t.Context(), id, runtimeTestInstance(t, m, id), "truncated"); err != nil {
		t.Fatal(err)
	}
	runtimeWait(t, m, id, func(s RuntimeSnapshot) bool { return s.Permission != nil })
	if err := m.ReplyPermission(t.Context(), id, runtimeTestInstance(t, m, id), "permission-1", protocol.PermissionAllow); !errors.Is(err, ErrRuntimeInvalid) {
		t.Fatal("truncated permission was allowed")
	}
	if err := m.ReplyPermission(t.Context(), id, runtimeTestInstance(t, m, id), "permission-1", protocol.PermissionDeny); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeWorkerLimitAndCloseReaps(t *testing.T) {
	m, projects, _ := runtimeTestManager(t, "")
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, project := range projects[:2] {
		wg.Go(func() { _, err := m.Open(t.Context(), project, "", "", ""); errs <- err })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := m.Open(t.Context(), projects[2], "", "", ""); !errors.Is(err, ErrRuntimeLimit) {
		t.Fatalf("worker cap: %v", err)
	}
	r, err := m.runtime(projects[0].ID, runtimeTestInstance(t, m, projects[0].ID))
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-r.worker.Client.Done():
	default:
		t.Fatal("close did not join worker client")
	}
	select {
	case <-r.drained:
	default:
		t.Fatal("close did not join event drain")
	}
	if _, err := m.Open(t.Context(), projects[0], "", "", ""); !errors.Is(err, ErrRuntimeClosed) {
		t.Fatalf("opened closed manager: %v", err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeFailClosedAndChangedIdentity(t *testing.T) {
	for _, mode := range []string{"nocap", "badpermission", "memory"} {
		t.Run(mode, func(t *testing.T) {
			m, projects, _ := runtimeTestManager(t, mode)
			if _, err := m.Open(t.Context(), projects[0], "", "", ""); !errors.Is(err, ErrRuntimeUnavailable) {
				t.Fatalf("unsafe startup accepted: %v", err)
			}
			if _, ok := m.Snapshot(projects[0].ID); ok {
				t.Fatal("failed activation retained")
			}
		})
	}
	m, projects, _ := runtimeTestManager(t, "")
	project := projects[0]
	if err := os.Rename(project.Path, project.Path+"-moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(project.Path, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Open(t.Context(), project, "", "", ""); !errors.Is(err, ErrProjectInvalid) {
		t.Fatalf("replaced identity accepted: %v", err)
	}
}

func TestRuntimeWorkerExitAndBoundedProjection(t *testing.T) {
	m, projects, _ := runtimeTestManager(t, "")
	id := projects[0].ID
	if _, err := m.Open(t.Context(), projects[0], "", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := m.Prompt(t.Context(), id, runtimeTestInstance(t, m, id), "exit"); !errors.Is(err, ErrRuntimeUnavailable) {
		t.Fatalf("worker exit: %v", err)
	}
	snapshot := runtimeWait(t, m, id, func(s RuntimeSnapshot) bool { return s.Status == "failed" })
	if strings.Contains(snapshot.Error, "SECRET") {
		t.Fatal("stderr/error leaked")
	}
	r := &liveRuntime{assistant: -1}
	for range 120 {
		r.addMessage(RuntimeMessage{Role: "assistant", Text: strings.Repeat("hello", 20000)})
	}
	total := 0
	for _, message := range r.snapshot.Messages {
		total += len(message.Text)
		if len(message.Text) > runtimeMessageBytes {
			t.Fatal("message exceeded bound")
		}
	}
	if total > runtimeHistoryBytes || len(r.snapshot.Messages) > runtimeHistoryCount || !r.snapshot.HistoryTruncated {
		t.Fatal("history not bounded")
	}
	if !slices.ContainsFunc(r.snapshot.Messages, func(m RuntimeMessage) bool { return m.Truncated }) {
		t.Fatal("missing truncation marker")
	}
}

func TestRuntimeConcurrentCloseDuringActivation(t *testing.T) {
	m, projects, _ := runtimeTestManager(t, "slow")
	opened := make(chan error, 1)
	go func() { _, err := m.Open(t.Context(), projects[0], "", "", ""); opened <- err }()
	runtimeWait(t, m, projects[0].ID, func(s RuntimeSnapshot) bool { return s.Status == "opening" })
	if err := m.CloseProject(t.Context(), projects[0].ID, runtimeTestInstance(t, m, projects[0].ID)); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-opened:
		if err == nil {
			t.Fatal("canceled activation succeeded")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("close deadlocked with activation")
	}
}

func runtimeTestInstance(t *testing.T, m *RuntimeManager, projectID string) string {
	t.Helper()
	snapshot, ok := m.Snapshot(projectID)
	if !ok || snapshot.InstanceID == "" {
		t.Fatal("runtime instance missing")
	}
	return snapshot.InstanceID
}

func TestRuntimeInstanceRejectsStaleControlsAfterReopen(t *testing.T) {
	m, projects, log := runtimeTestManager(t, "")
	project := projects[0]
	old, err := m.Open(t.Context(), project, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(old.InstanceID) < 26 {
		t.Fatal("instance identity lacks random-token length")
	}
	if err := m.Prompt(t.Context(), project.ID, old.InstanceID, "permission"); err != nil {
		t.Fatal(err)
	}
	oldPermission := runtimeWait(t, m, project.ID, func(s RuntimeSnapshot) bool { return s.Permission != nil }).Permission.ID
	if err := m.ReplyPermission(t.Context(), project.ID, old.InstanceID, oldPermission, protocol.PermissionDeny); err != nil {
		t.Fatal(err)
	}
	runtimeWait(t, m, project.ID, func(s RuntimeSnapshot) bool { return s.Status == "idle" })
	if err := m.Prompt(t.Context(), project.ID, old.InstanceID, "input"); err != nil {
		t.Fatal(err)
	}
	oldInput := runtimeWait(t, m, project.ID, func(s RuntimeSnapshot) bool { return s.Input != nil }).Input.ID
	if err := m.CloseProject(t.Context(), project.ID, old.InstanceID); err != nil {
		t.Fatal(err)
	}
	current, err := m.Open(t.Context(), project, old.SessionID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if current.InstanceID == old.InstanceID {
		t.Fatal("replacement reused instance identity")
	}
	if err := m.Prompt(t.Context(), project.ID, current.InstanceID, "permission"); err != nil {
		t.Fatal(err)
	}
	before := runtimeWait(t, m, project.ID, func(s RuntimeSnapshot) bool { return s.Permission != nil })
	if before.Permission.ID != oldPermission {
		t.Fatal("fixture must reuse worker-local request ID")
	}
	beforeLog, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	stale := []struct {
		name string
		call func(string) error
	}{
		{"prompt", func(instance string) error { return m.Prompt(t.Context(), project.ID, instance, "must not execute") }},
		{"abort", func(instance string) error { return m.Abort(t.Context(), project.ID, instance) }},
		{"close", func(instance string) error { return m.CloseProject(t.Context(), project.ID, instance) }},
		{"permission", func(instance string) error {
			return m.ReplyPermission(t.Context(), project.ID, instance, oldPermission, protocol.PermissionAllow)
		}},
		{"input", func(instance string) error {
			return m.ReplyInput(t.Context(), project.ID, instance, protocol.UserInputResponse{RequestID: oldInput, Answers: []protocol.UserInputAnswer{{QuestionID: "question-1", Answer: "one"}}})
		}},
	}
	for _, operation := range stale {
		for _, instance := range []string{old.InstanceID, ""} {
			if err := operation.call(instance); !errors.Is(err, ErrRuntimeInvalid) {
				t.Fatalf("%s accepted stale/missing instance: %v", operation.name, err)
			}
		}
	}
	after, ok := m.Snapshot(project.ID)
	if !ok || after.InstanceID != current.InstanceID || after.Revision != before.Revision || after.Permission == nil || after.Status != "permission" {
		t.Fatalf("stale controls disturbed replacement: %+v", after)
	}
	afterLog, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if string(beforeLog) != string(afterLog) {
		t.Fatalf("stale controls reached replacement RPC: before=%q after=%q", beforeLog, afterLog)
	}
	if err := m.ReplyPermission(t.Context(), project.ID, current.InstanceID, oldPermission, protocol.PermissionDeny); err != nil {
		t.Fatal(err)
	}
	runtimeWait(t, m, project.ID, func(s RuntimeSnapshot) bool { return s.Status == "idle" })
	if err := m.Prompt(t.Context(), project.ID, current.InstanceID, "input"); err != nil {
		t.Fatal(err)
	}
	input := runtimeWait(t, m, project.ID, func(s RuntimeSnapshot) bool { return s.Input != nil })
	if input.Input.ID != oldInput {
		t.Fatal("fixture must reuse input request ID")
	}
	answer := protocol.UserInputResponse{RequestID: oldInput, Answers: []protocol.UserInputAnswer{{QuestionID: "question-1", Answer: "one"}}}
	if err := m.ReplyInput(t.Context(), project.ID, old.InstanceID, answer); !errors.Is(err, ErrRuntimeInvalid) {
		t.Fatalf("stale instance answered replacement input: %v", err)
	}
	unchanged, _ := m.Snapshot(project.ID)
	if unchanged.Revision != input.Revision || unchanged.Input == nil {
		t.Fatal("stale input reply changed replacement state")
	}
	if err := m.ReplyInput(t.Context(), project.ID, current.InstanceID, answer); err != nil {
		t.Fatal(err)
	}
	runtimeWait(t, m, project.ID, func(s RuntimeSnapshot) bool { return s.Status == "idle" })
}

func TestRuntimeLegacyUntaggedRootEvents(t *testing.T) {
	m, projects, _ := runtimeTestManager(t, "legacy")
	project := projects[0]
	snapshot, err := m.Open(t.Context(), project, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Prompt(t.Context(), project.ID, snapshot.InstanceID, "complete"); err != nil {
		t.Fatal(err)
	}
	answer := runtimeWait(t, m, project.ID, func(s RuntimeSnapshot) bool { return s.Status == "idle" })
	if got := answer.Messages[len(answer.Messages)-1]; got.Role != "assistant" || got.Text != "public answer" {
		t.Fatalf("legacy root answer: %+v", answer.Messages)
	}
}
