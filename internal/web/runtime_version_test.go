package web

import (
	"context"
	"encoding/json/v2"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	clientrpc "github.com/elmissouri16/snow-core/pkg/agentclient/rpc"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func openVersionRuntime(t *testing.T, mode string) (*RuntimeManager, Project, RuntimeSnapshot, string) {
	t.Helper()
	m, projects, log := runtimeTestManager(t, "version")
	m.env = slices.DeleteFunc(m.env, func(s string) bool { return strings.HasPrefix(s, "SNOW_WEB_RUNTIME_TEST_CHILD=") })
	m.env = append(m.env, "SNOW_WEB_VERSION_TEST_CHILD="+mode)
	s, err := m.Open(t.Context(), projects[0], "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	return m, projects[0], s, log
}
func prepareTestVersion(t *testing.T, m *RuntimeManager, p Project, s RuntimeSnapshot) RuntimeVersionRestorePreparation {
	t.Helper()
	prepared, err := m.PrepareVersionRestore(t.Context(), p.ID, s.InstanceID, s.SessionID, "current", "current-tip", "target", "target-tip")
	if err != nil {
		t.Fatal(err)
	}
	return prepared
}

func TestVersionsBoundedReadsNeverSelectOrChangeTranscript(t *testing.T) {
	m, p, before, log := openVersionRuntime(t, "normal")
	page, err := m.ListVersions(t.Context(), p.ID, before.InstanceID, before.SessionID, "")
	if err != nil {
		t.Fatal(err)
	}
	if page.ProjectID != p.ID || page.InstanceID != before.InstanceID || page.SessionID != before.SessionID || page.CurrentBranchID != "current" || page.CurrentTipID != "current-tip" || len(page.Versions) != 2 {
		t.Fatalf("unbound list: %+v", page)
	}
	preview, err := m.PreviewVersion(t.Context(), p.ID, before.InstanceID, before.SessionID, "target", "target-tip", "")
	if err != nil {
		t.Fatal(err)
	}
	if preview.BranchID != "target" || preview.TipID != "target-tip" || len(preview.Messages) != 2 || preview.Messages[0].CanEdit || preview.Messages[1].CanRegenerate {
		t.Fatalf("mutable/wrong preview: %+v", preview)
	}
	serialized, _ := json.Marshal(preview)
	if strings.Contains(string(serialized), "PRIVATE") || strings.Contains(preview.Messages[0].HTML, "<script>") {
		t.Fatal("preview leaked private state or unsafe HTML")
	}
	after, _ := m.Snapshot(p.ID)
	a, _ := json.Marshal(before)
	b, _ := json.Marshal(after)
	if string(a) != string(b) {
		t.Fatalf("read changed current state: before=%s after=%s", a, b)
	}
	commands, _ := os.ReadFile(log)
	if strings.Contains(string(commands), "branches_list") || strings.Contains(string(commands), "branch_select") || strings.Contains(string(commands), "prompt") || strings.Count(string(commands), "messages_page") != 2 {
		t.Fatalf("preview used unbounded/fallback/mutating path: %s", commands)
	}
}

func TestVersionRestoreExplicitIdleSingleUseAuthorityRotation(t *testing.T) {
	m, p, s, log := openVersionRuntime(t, "normal")
	r, err := m.runtime(p.ID, s.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	r.rootEpoch = 1
	r.snapshot.CancelToken = "old-stop"
	r.snapshot.Queue = &RuntimeQueue{Token: "old-queue"}
	r.messageEdit.preparation = &protocol.RPCMessageEditPrepared{EditToken: "old-edit"}
	r.mu.Unlock()
	prepared := prepareTestVersion(t, m, p, s)
	if prepared.ProjectID != p.ID || prepared.InstanceID != s.InstanceID || prepared.SessionID != s.SessionID || prepared.BranchID != "target" || prepared.TipID != "target-tip" || time.Until(prepared.ExpiresAt) <= 0 || time.Until(prepared.ExpiresAt) > 2*time.Minute {
		t.Fatalf("invalid preparation: %+v", prepared)
	}
	before, _ := m.Snapshot(p.ID)
	if _, err := m.CommitVersionRestore(t.Context(), p.ID, s.InstanceID, "other-session", prepared.RestoreToken); !errors.Is(err, ErrRuntimeInvalid) {
		t.Fatalf("cross-session commit: %v", err)
	}
	restored, err := m.CommitVersionRestore(t.Context(), p.ID, s.InstanceID, s.SessionID, prepared.RestoreToken)
	if err != nil {
		t.Fatal(err)
	}
	if restored.InstanceID == s.InstanceID || restored.SessionID != s.SessionID || restored.SessionName != s.SessionName || restored.Provider != s.Provider || restored.Model != s.Model || restored.PermissionMode != s.PermissionMode || restored.Mode != "plan" || restored.Status != "idle" || restored.CancelToken != "" || restored.Queue != nil || len(restored.Messages) != 2 || restored.Messages[0].SourceID != "target-user" || restored.Messages[0].Text == before.Messages[0].Text {
		t.Fatalf("restore projection: %+v", restored)
	}
	if r.messageEdit.preparation != nil || r.versions.preparation != nil || r.turnID != "" || r.busy || r.retiredEpoch != 1 {
		t.Fatal("old authority survived restore")
	}
	if _, err := m.CommitVersionRestore(t.Context(), p.ID, restored.InstanceID, s.SessionID, prepared.RestoreToken); !errors.Is(err, ErrRuntimeInvalid) {
		t.Fatalf("replayed restore: %v", err)
	}
	commands, _ := os.ReadFile(log)
	if strings.Contains(string(commands), "prompt") || strings.Contains(string(commands), "branch_select") || strings.Contains(string(commands), "set_mode") || strings.Count(string(commands), "branch_restore_commit") != 1 {
		t.Fatalf("restore executed work or replayed: %s", commands)
	}
	if err := m.Prompt(t.Context(), p.ID, restored.InstanceID, "explicit after restore"); err != nil {
		t.Fatal(err)
	}
	next := runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool { return s.Status == "idle" && len(s.Messages) == 4 })
	if next.Messages[3].Text != "New explicit answer" {
		t.Fatal("retired epoch rejected the next legitimate root")
	}
}

func TestVersionRestoreRejectionAndUnknownOutcomesKeepPriorTranscript(t *testing.T) {
	for _, mode := range []string{"reject", "unknown", "exit", "wrong-scope", "settings-drift", "missing-mode"} {
		t.Run(mode, func(t *testing.T) {
			m, p, s, log := openVersionRuntime(t, mode)
			prepared := prepareTestVersion(t, m, p, s)
			_, err := m.CommitVersionRestore(t.Context(), p.ID, s.InstanceID, s.SessionID, prepared.RestoreToken)
			after, _ := m.Snapshot(p.ID)
			if after.InstanceID != s.InstanceID || len(after.Messages) != len(s.Messages) || after.Messages[0].Text != s.Messages[0].Text {
				t.Fatalf("unsuccessful restore fabricated success: %+v", after)
			}
			if mode == "reject" {
				if !errors.Is(err, ErrRuntimeInvalid) || after.Status != "idle" {
					t.Fatalf("known rejection poisoned worker: %+v %v", after, err)
				}
			} else if !errors.Is(err, ErrRuntimeUnavailable) || after.Status != "failed" {
				t.Fatalf("unknown outcome revived worker: %+v %v", after, err)
			}
			if _, err := m.CommitVersionRestore(t.Context(), p.ID, s.InstanceID, s.SessionID, prepared.RestoreToken); err == nil {
				t.Fatal("unsuccessful attempt retried")
			}
			commands, _ := os.ReadFile(log)
			if strings.Count(string(commands), "branch_restore_commit") != 1 || strings.Contains(string(commands), "prompt") {
				t.Fatalf("restore replay: %s", commands)
			}
		})
	}
}

func TestVersionsRejectQueuedReviewAndExpiredPreparationBeforeStateReset(t *testing.T) {
	m, p, s, log := openVersionRuntime(t, "normal")
	prepared := prepareTestVersion(t, m, p, s)
	r, _ := m.runtime(p.ID, s.InstanceID)
	r.mu.Lock()
	original := r.versions.preparation
	r.snapshot.Queue = &RuntimeQueue{Token: "queue-token", Revision: 1, Items: []RuntimeQueueItem{{ID: "held", Text: "preserve held text", State: "held"}}}
	r.mu.Unlock()
	if _, err := m.PrepareVersionRestore(t.Context(), p.ID, s.InstanceID, s.SessionID, "current", "current-tip", "target", "target-tip"); !errors.Is(err, ErrRuntimeQueueReview) {
		t.Fatalf("prepare bypasses held review: %v", err)
	}
	if _, err := m.CommitVersionRestore(t.Context(), p.ID, s.InstanceID, s.SessionID, prepared.RestoreToken); !errors.Is(err, ErrRuntimeQueueReview) {
		t.Fatalf("commit bypasses held review: %v", err)
	}
	after, _ := m.Snapshot(p.ID)
	r.mu.Lock()
	preserved := r.versions.preparation == original
	r.snapshot.Queue = nil
	r.versions.expiresAt = time.Now().Add(-time.Second)
	r.mu.Unlock()
	if !preserved || after.InstanceID != s.InstanceID || after.Status != "idle" || after.Queue.Token != "queue-token" {
		t.Fatal("queue rejection reset authority")
	}
	if _, err := m.CommitVersionRestore(t.Context(), p.ID, s.InstanceID, s.SessionID, prepared.RestoreToken); !errors.Is(err, ErrRuntimeInvalid) {
		t.Fatalf("expired token: %v", err)
	}
	commands, _ := os.ReadFile(log)
	if strings.Count(string(commands), "branch_restore_prepare") != 1 || strings.Contains(string(commands), "branch_restore_commit") {
		t.Fatalf("queue/expiry rejection reached mutating RPC: %s", commands)
	}
}

func TestVersionRestoreLifetimeSurvivesBrowserDisconnectWithoutQueuingOtherWork(t *testing.T) {
	m, p, s, log := openVersionRuntime(t, "hold")
	prepared := prepareTestVersion(t, m, p, s)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := m.CommitVersionRestore(ctx, p.ID, s.InstanceID, s.SessionID, prepared.RestoreToken)
		done <- err
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		data, _ := os.ReadFile(log)
		if strings.Contains(string(data), "branch_restore_commit") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("restore not admitted")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := m.Prompt(t.Context(), p.ID, s.InstanceID, "overlap"); !errors.Is(err, ErrRuntimeBusy) {
		t.Fatalf("overlapping admission: %v", err)
	}
	if err := os.WriteFile(log+".release", nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	after, _ := m.Snapshot(p.ID)
	if after.Status != "idle" || after.InstanceID == s.InstanceID {
		t.Fatal("browser cancellation canceled admitted restore")
	}
}

func TestVersionBoundedResponseIdentityAndCapabilityFailClosed(t *testing.T) {
	for _, mode := range []string{"no-capability", "stale-list", "oversized-list", "stale-preview", "oversized-preview"} {
		t.Run(mode, func(t *testing.T) {
			m, p, s, _ := openVersionRuntime(t, mode)
			var err error
			if strings.Contains(mode, "preview") {
				_, err = m.PreviewVersion(t.Context(), p.ID, s.InstanceID, s.SessionID, "target", "target-tip", "")
			} else {
				_, err = m.ListVersions(t.Context(), p.ID, s.InstanceID, s.SessionID, "")
			}
			after, _ := m.Snapshot(p.ID)
			if mode == "no-capability" {
				if !errors.Is(err, ErrRuntimeInvalid) || after.Status != "idle" || after.VersionsEnabled {
					t.Fatalf("legacy capability exposed: %+v %v", after, err)
				}
			} else if !errors.Is(err, ErrRuntimeUnavailable) || after.Status != "failed" || after.Messages[0].Text != s.Messages[0].Text {
				t.Fatalf("malformed bounded page: %+v %v", after, err)
			}
		})
	}
}

func TestVersionOldScopeACKAndRetiredRootCannotReplaceProjection(t *testing.T) {
	r := &liveRuntime{ctx: t.Context(), cancel: func() {}, instanceID: "new-instance", rootEpoch: 2, retiredEpoch: 1, assistant: -1, plan: -1, snapshot: RuntimeSnapshot{SessionID: "session", InstanceID: "new-instance", Status: "idle", Messages: []RuntimeMessage{{Role: "user", Text: "current"}}}}
	c := runtimeVersionRestoreACK{SessionID: "session", History: protocol.RPCBranchMessagesPage{Messages: []protocol.Message{}}, Mode: protocol.ModeDefault}
	if _, err := r.publishVersionRestore("old-instance", c, 1); !errors.Is(err, ErrRuntimeInvalid) || r.snapshot.Messages[0].Text != "current" {
		t.Fatal("old-scope ACK replaced current transcript")
	}
	r.busy = true
	r.snapshot.Status = "running"
	r.consumeEvent(clientrpc.Event{AgentEvent: &protocol.AgentEvent{Type: protocol.EvTextDelta, RootEpoch: 1, TurnSequence: 99, TurnID: "old-root", Text: "stale"}})
	if len(r.snapshot.Messages) != 1 {
		t.Fatal("retired root polluted restored history")
	}
}

func TestVersionKnownACKAfterLifetimeFailureNeverRevives(t *testing.T) {
	for _, status := range []string{"failed", "closing"} {
		t.Run(status, func(t *testing.T) {
			r := &liveRuntime{ctx: t.Context(), cancel: func() {}, instanceID: "old", assistant: -1, plan: -1, snapshot: RuntimeSnapshot{ProjectID: "project", InstanceID: "old", SessionID: "session", Status: status, Messages: []RuntimeMessage{{Role: "user", Text: "original"}}}}
			c := runtimeVersionRestoreACK{SessionID: "session", History: protocol.RPCBranchMessagesPage{Messages: []protocol.Message{{ID: "restored-user", Role: protocol.RoleUser, Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: "restored"}}}}, Total: 1}, Mode: protocol.ModePlan}
			if _, err := r.publishVersionRestore("old", c, 0); !errors.Is(err, ErrRuntimeUnavailable) {
				t.Fatalf("terminal restore reported usable success: %v", err)
			}
			if r.snapshot.Status != status || r.snapshot.InstanceID == "old" || r.snapshot.Messages[0].Text != "restored" || r.snapshot.CancelToken != "" || r.busy {
				t.Fatalf("known late ACK resurrected runtime or lost authoritative history: %+v", r.snapshot)
			}
		})
	}
}

func TestVersionParsersRejectAmbiguousPageBoundaries(t *testing.T) {
	valid := protocol.RPCBranchMessagesPage{SessionID: "session", BranchID: "branch", TipID: "tip", Total: 1, Messages: []protocol.Message{{ID: "user", Role: protocol.RoleUser, Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: "text"}}}}}
	if !validVersionHistory(valid, "session", "branch", "tip", "") {
		t.Fatal("valid bounded page rejected")
	}
	for _, change := range []func(*protocol.RPCBranchMessagesPage){
		func(p *protocol.RPCBranchMessagesPage) { p.SessionID = "other" },
		func(p *protocol.RPCBranchMessagesPage) { p.BranchID = "other" },
		func(p *protocol.RPCBranchMessagesPage) { p.TipID = "other" },
		func(p *protocol.RPCBranchMessagesPage) { p.Start = -1 },
		func(p *protocol.RPCBranchMessagesPage) { p.Total = 0 },
		func(p *protocol.RPCBranchMessagesPage) { p.Messages = append(p.Messages, p.Messages[0]); p.Total = 2 },
		func(p *protocol.RPCBranchMessagesPage) { p.NextCursor = "repeat" },
		func(p *protocol.RPCBranchMessagesPage) { p.NextCursor = "next"; p.Messages = nil },
	} {
		bad := valid
		bad.Messages = slices.Clone(valid.Messages)
		change(&bad)
		if validVersionHistory(bad, "session", "branch", "tip", "repeat") {
			t.Fatalf("invalid history accepted: %+v", bad)
		}
	}
	page := protocol.RPCBranchesPage{SessionID: "session", ActiveBranchID: "main", ActiveTipID: "tip", Branches: []protocol.RPCBranchVersion{{ID: "main", TipID: "tip", Active: true}}}
	if !validVersionsPage(page, "session", "") {
		t.Fatal("valid list rejected")
	}
	page.Branches = append(page.Branches, page.Branches[0])
	if validVersionsPage(page, "session", "") {
		t.Fatal("duplicate branch identity accepted")
	}
}

func TestVersionRestoreAcceptsAuthoritativeModeEffectiveThinking(t *testing.T) {
	state := runtimeVersionState{provider: "fake", model: "fake", permission: "deny", thinking: "off"}
	prepared := protocol.RPCBranchRestorePrepared{SessionID: "session", TargetBranchID: "target", TargetTipID: "tip"}
	committed := protocol.RPCBranchRestoreCommitted{SessionID: "session", BranchID: "target", TipID: "tip", Mode: protocol.ModePlan, Settings: protocol.RPCSettings{Provider: "fake", Model: "fake", PermissionMode: "deny", Thinking: protocol.ThinkingHigh}, History: protocol.RPCBranchMessagesPage{SessionID: "session", BranchID: "target", TipID: "tip"}}
	if !validVersionRestoreACK(committed, prepared, state) {
		t.Fatal("restored Plan Mode effective thinking rejected as model/permission drift")
	}
	r := &liveRuntime{ctx: t.Context(), cancel: func() {}, instanceID: "old", assistant: -1, plan: -1, snapshot: RuntimeSnapshot{ProjectID: "project", InstanceID: "old", SessionID: "session", Provider: "fake", Model: "fake", PermissionMode: "deny", Thinking: "off", Status: "idle"}}
	restored, err := r.publishVersionRestore("old", committed, 0)
	if err != nil || restored.Mode != "plan" || restored.Thinking != "high" {
		t.Fatalf("authoritative restored mode settings lost: %+v %v", restored, err)
	}
}
