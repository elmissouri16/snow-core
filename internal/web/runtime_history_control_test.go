package web

import (
	"encoding/json/v2"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestHistoryBranchForkRotatesAuthorityAndLoadsBoundPublicHistory(t *testing.T) {
	m, p, before, log := openHistoryControlRuntime(t, "normal")
	r, _ := m.runtime(p.ID, before.InstanceID)
	r.mu.Lock()
	r.messageEdit.preparation = &protocol.RPCMessageEditPrepared{EditToken: "old-edit"}
	r.versions.preparation = &protocol.RPCBranchRestorePrepared{RestoreToken: "old-restore"}
	r.snapshot.CancelToken = "old-stop"
	r.mu.Unlock()
	after, err := m.HistoryBranchFork(t.Context(), p.ID, before.InstanceID, historyControlTestRequest(before))
	if err != nil {
		t.Fatal(err)
	}
	if after.InstanceID == before.InstanceID || after.SessionID != before.SessionID || after.Mode != "plan" || after.Thinking != "high" || after.Provider != before.Provider || after.Model != before.Model || after.PermissionMode != before.PermissionMode || after.Status != "idle" || after.CancelToken != "" || len(after.Messages) != 2 || after.Messages[0].SourceID != "forked-user" {
		t.Fatalf("incorrect fork projection: %+v", after)
	}
	r.mu.Lock()
	retired := r.retiredEpoch
	oldControls := r.messageEdit.preparation != nil || r.versions.preparation != nil || r.queue.control != nil
	r.mu.Unlock()
	if retired != 1 || oldControls {
		t.Fatal("old history authority retained")
	}
	page, err := m.ListVersions(t.Context(), p.ID, after.InstanceID, after.SessionID, "")
	if err != nil || page.CurrentBranchID != "forked" || page.CurrentTipID != "target-tip" {
		t.Fatalf("new identity not bound: %+v %v", page, err)
	}
	if _, err := m.HistoryBranchFork(t.Context(), p.ID, before.InstanceID, historyControlTestRequest(before)); err == nil {
		t.Fatal("old instance reused")
	}
	raw, _ := json.Marshal(after)
	if strings.Contains(string(raw), "PRIVATE") {
		t.Fatal("private history leaked")
	}
	commands, _ := os.ReadFile(log)
	if strings.Count(string(commands), "history_branch_fork\n") != 1 || strings.Count(string(commands), "branch_messages_page\n") != 1 || strings.Contains(string(commands), "branch_fork\nbranch_fork") || strings.Contains(string(commands), "prompt\n") {
		t.Fatalf("fallback or replay: %s", commands)
	}
}

func TestHistoryMetadataRenameAndDetachedForkKeepParent(t *testing.T) {
	for _, action := range []string{"rename", "detached"} {
		t.Run(action, func(t *testing.T) {
			m, p, before, log := openHistoryControlRuntime(t, "normal")
			r, _ := m.runtime(p.ID, before.InstanceID)
			r.mu.Lock()
			r.messageEdit.preparation = &protocol.RPCMessageEditPrepared{EditToken: "keep-edit"}
			r.mu.Unlock()
			request := historyControlTestRequest(before)
			var result RuntimeHistoryControlResult
			var err error
			if action == "rename" {
				result, err = m.HistoryBranchRename(t.Context(), p.ID, before.InstanceID, request)
			} else {
				result, err = m.HistorySessionFork(t.Context(), p.ID, before.InstanceID, request)
			}
			if err != nil {
				t.Fatal(err)
			}
			after, _ := m.Snapshot(p.ID)
			if after.InstanceID != before.InstanceID || after.SessionID != before.SessionID || after.Mode != before.Mode || after.Thinking != before.Thinking || after.Messages[0].Text != before.Messages[0].Text {
				t.Fatal("metadata operation changed current history")
			}
			r.mu.Lock()
			epoch, retired, edit := r.rootEpoch, r.retiredEpoch, r.messageEdit.preparation
			r.mu.Unlock()
			if epoch != 1 || retired != 0 || edit == nil {
				t.Fatal("metadata operation retired controls or epoch")
			}
			if action == "detached" && (result.ChildSessionID != "detached-child" || after.Revision != before.Revision) {
				t.Fatalf("child became current: %+v", result)
			}
			page, err := m.ListVersions(t.Context(), p.ID, after.InstanceID, after.SessionID, "")
			if err != nil || page.CurrentBranchID != "current" || action == "rename" && page.Versions[1].Name != request.Name {
				t.Fatalf("incorrect metadata read: %+v %v", page, err)
			}
			commands, _ := os.ReadFile(log)
			if strings.Contains(string(commands), "branch_messages_page") || strings.Contains(string(commands), "prompt\n") || strings.Count(string(commands), "session_open\n") != 0 {
				t.Fatalf("metadata operation activated/replayed: %s", commands)
			}
		})
	}
}

func TestHistoryControlsRejectCASAndUnknownNeverRetry(t *testing.T) {
	for _, mode := range []string{"normal", "reject", "unknown", "exit", "wrong-scope", "missing-mode", "wrong-history", "oversized-history", "no-capability"} {
		t.Run(mode, func(t *testing.T) {
			m, p, before, log := openHistoryControlRuntime(t, mode)
			request := historyControlTestRequest(before)
			if mode == "normal" {
				request.TargetTipID = "wrong-tip"
			}
			_, err := m.HistoryBranchFork(t.Context(), p.ID, before.InstanceID, request)
			if err == nil {
				t.Fatal("invalid/unknown mutation accepted")
			}
			after, _ := m.Snapshot(p.ID)
			known := mode == "normal" || mode == "reject" || mode == "no-capability"
			if known && (!errors.Is(err, ErrRuntimeInvalid) || after.Status != "idle") || !known && after.Status != "failed" {
				t.Fatalf("wrong outcome boundary: %v %s", err, after.Status)
			}
			_, _ = m.HistoryBranchFork(t.Context(), p.ID, before.InstanceID, request)
			commands, _ := os.ReadFile(log)
			want := 1
			if mode == "no-capability" {
				want = 0
			}
			if strings.Count(string(commands), "history_branch_fork\n") != want {
				t.Fatalf("mutation retried: %s", commands)
			}
		})
	}
}

func TestHistoryRenameOldNameCASAndRevision(t *testing.T) {
	m, p, before, log := openHistoryControlRuntime(t, "normal")
	request := historyControlTestRequest(before)
	request.ExpectedRevision++
	if _, err := m.HistoryBranchRename(t.Context(), p.ID, before.InstanceID, request); !errors.Is(err, ErrRuntimeInvalid) {
		t.Fatal(err)
	}
	request.ExpectedRevision = before.Revision
	request.OldName = "Stale label"
	if _, err := m.HistoryBranchRename(t.Context(), p.ID, before.InstanceID, request); !errors.Is(err, ErrRuntimeInvalid) {
		t.Fatal(err)
	}
	after, _ := m.Snapshot(p.ID)
	if after.InstanceID != before.InstanceID || after.Status != "idle" {
		t.Fatal("known CAS rejection changed scope")
	}
	commands, _ := os.ReadFile(log)
	if strings.Count(string(commands), "history_branch_rename\n") != 1 {
		t.Fatalf("local revision fence bypassed: %s", commands)
	}
}

func TestHistoryControlNameBounds(t *testing.T) {
	for _, name := range []string{"", " trim", "trim ", "line\nbreak", "C1\u0085control", strings.Repeat("a", 65), strings.Repeat("界", 86)} {
		if validHistoryControlName(name, true) {
			t.Fatalf("invalid branch name accepted %q", name)
		}
	}
	if !validHistoryControlName(strings.Repeat("界", 64), true) || !validHistoryControlName(strings.Repeat("a", 72), false) || validHistoryControlName(strings.Repeat("a", 73), false) {
		t.Fatal("incorrect UTF-8/rune bounds")
	}
}
