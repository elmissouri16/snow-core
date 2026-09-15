package rpc

import (
	"bytes"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/agent"
	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestHistoryControlRPCMetadataAndStaleBinding(t *testing.T) {
	a, p, _ := goalRunRPCApp(t)
	binding := protocol.RPCHistoryControlBinding{SessionID: a.Session.ID(), SourceBranchID: "main", TargetBranchID: "main", SourceTipID: a.Session.BranchTip(), TargetTipID: a.Session.BranchTip()}
	var output bytes.Buffer
	srv := New(t.Context(), a, strings.NewReader(""), &output)
	call := func(command string, params any) error {
		t.Helper()
		raw, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		return srv.handle(t.Context(), Request{ID: "history", Type: command, Params: raw})
	}
	epoch := a.Agent.RootEpoch()
	if err := call("history_session_fork", protocol.RPCManagedSessionForkParams{RPCHistoryControlBinding: binding, Name: "Detached copy"}); err != nil {
		t.Fatal(err)
	}
	var detached struct {
		Data protocol.RPCManagedSessionForkResult `json:"data"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &detached); err != nil {
		t.Fatal(err)
	}
	if detached.Data.SessionID == binding.SessionID || detached.Data.SessionID == "" || a.Session.ID() != binding.SessionID || a.Agent.RootEpoch() != epoch {
		t.Fatalf("detached fork activated: %+v", detached)
	}
	output.Reset()
	if err := call("history_branch_fork", protocol.RPCManagedBranchForkParams{RPCHistoryControlBinding: binding, Name: "New branch"}); err != nil {
		t.Fatal(err)
	}
	var fork struct {
		Data protocol.RPCManagedBranchForkResult `json:"data"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &fork); err != nil {
		t.Fatal(err)
	}
	if fork.Data.BranchID == "main" || fork.Data.BranchID != a.Session.(session.ActiveBranchStore).ActiveBranchID() || fork.Data.RootEpoch <= epoch {
		t.Fatalf("fork=%+v", fork)
	}
	tip, epoch := a.Session.BranchTip(), a.Agent.RootEpoch()
	if err := call("history_branch_fork", protocol.RPCManagedBranchForkParams{RPCHistoryControlBinding: binding, Name: "Stale duplicate"}); !errors.Is(err, app.ErrHistoryControlRejected) {
		t.Fatalf("stale=%v", err)
	}
	if a.Session.BranchTip() != tip || a.Agent.RootEpoch() != epoch {
		t.Fatal("stale request mutated cursor")
	}
	binding.SourceBranchID, binding.TargetBranchID = fork.Data.BranchID, fork.Data.BranchID
	binding.SourceTipID, binding.TargetTipID = tip, tip
	output.Reset()
	if err := call("history_branch_rename", protocol.RPCManagedBranchRenameParams{RPCHistoryControlBinding: binding, OldName: "New branch", Name: "Renamed branch"}); err != nil {
		t.Fatal(err)
	}
	var rename struct {
		Data protocol.RPCManagedBranchRenameResult `json:"data"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &rename); err != nil {
		t.Fatal(err)
	}
	if rename.Data.Branch.Name != "Renamed branch" || a.Agent.RootEpoch() != epoch || p.calls.Load() != 0 || srv.promptDone != nil {
		t.Fatalf("rename changed execution: %+v", rename)
	}
	if bytes.Contains(output.Bytes(), []byte(`"messages"`)) {
		t.Fatal("history control leaked raw history")
	}
}

func TestManagedRuntimeStrictOriginalFrames(t *testing.T) {
	commands := []struct{ command, params string }{
		{"history_branch_fork", `{"session_id":"s","source_branch_id":"main","source_tip_id":"","target_branch_id":"main","target_tip_id":"","name":"Branch"}`},
		{"history_session_fork", `{"session_id":"s","source_branch_id":"main","source_tip_id":"","target_branch_id":"main","target_tip_id":"","name":"Session"}`},
		{"history_branch_rename", `{"session_id":"s","source_branch_id":"main","source_tip_id":"","target_branch_id":"main","target_tip_id":"","old_name":"main","name":"Renamed"}`},
		{"compaction_start", `{"session_id":"s","branch_id":"main","expected_tip_id":""}`},
		{"managed_steer", `{"session_id":"s","turn_id":"t","root_epoch":1,"request_id":"q","text":"literal"}`},
	}
	for _, command := range commands {
		for name, mutate := range map[string]func(string) string{
			"unknown envelope":      func(s string) string { return strings.TrimSuffix(s, "}") + `,"unknown":"PRIVATE"}` },
			"empty legacy envelope": func(s string) string { return strings.TrimSuffix(s, "}") + `,"message":""}` },
			"duplicate id":          func(s string) string { return strings.Replace(s, `"id":"request"`, `"id":"request","id":"other"`, 1) },
			"missing id":            func(s string) string { return strings.Replace(s, `"id":"request",`, "", 1) },
			"duplicate param": func(s string) string {
				return strings.Replace(s, `"session_id":"s"`, `"session_id":"s","session_id":"other"`, 1)
			},
			"unknown param": func(s string) string {
				return strings.Replace(s, `"session_id":"s"`, `"session_id":"s","secret":"PRIVATE"`, 1)
			},
			"null binding": func(s string) string { return strings.Replace(s, `"session_id":"s"`, `"session_id":null`, 1) },
		} {
			t.Run(command.command+"/"+name, func(t *testing.T) {
				a, p, _ := goalRunRPCApp(t)
				before, epoch := a.Session.BranchTip(), a.Agent.RootEpoch()
				frame := mutate(fmt.Sprintf(`{"id":"request","type":%q,"params":%s}`, command.command, command.params))
				var output bytes.Buffer
				srv := New(t.Context(), a, strings.NewReader(frame+"\n"), &output)
				if err := srv.Serve(t.Context()); err != nil {
					t.Fatal(err)
				}
				if !bytes.Contains(output.Bytes(), []byte(`"success":false`)) || bytes.Contains(output.Bytes(), []byte("PRIVATE")) || a.Session.BranchTip() != before || a.Agent.RootEpoch() != epoch || p.calls.Load() != 0 {
					t.Fatalf("malformed request mutation/leak: %s", output.Bytes())
				}
			})
		}
	}
}

func TestManagedRuntimeRequiresPresentEmptyTipAssertions(t *testing.T) {
	for _, command := range []string{"history_branch_fork", "history_session_fork", "history_branch_rename", "compaction_start"} {
		t.Run(command, func(t *testing.T) {
			a, p, _ := goalRunRPCApp(t)
			before := a.Session.BranchTip()
			var params map[string]any
			if command == "compaction_start" {
				params = map[string]any{"session_id": a.Session.ID(), "branch_id": "main", "expected_tip_id": ""}
			} else {
				params = map[string]any{"session_id": a.Session.ID(), "source_branch_id": "main", "source_tip_id": "", "target_branch_id": "main", "target_tip_id": "", "name": "fork"}
				if command == "history_branch_rename" {
					params["old_name"] = "main"
				}
			}
			keys := []string{"source_tip_id", "target_tip_id"}
			if command == "compaction_start" {
				keys = []string{"expected_tip_id"}
			}
			for _, key := range keys {
				delete(params, key)
				var output bytes.Buffer
				srv := New(t.Context(), a, strings.NewReader(""), &output)
				raw, _ := json.Marshal(params)
				err := srv.handle(t.Context(), Request{ID: "missing-tip", Type: command, Params: raw})
				if err == nil || len(output.Bytes()) != 0 || p.calls.Load() != 0 || a.Session.BranchTip() != before {
					t.Fatalf("missing %s accepted: %v", key, err)
				}
				params[key] = ""
			}
		})
	}
}

func TestManagedRuntimeErrorCodesUnknownWins(t *testing.T) {
	for _, test := range []struct {
		err  error
		code string
	}{
		{errors.Join(app.ErrHistoryControlRejected, app.ErrHistoryControlUnknown), protocol.RPCHistoryControlUnknownErrorCode},
		{errors.Join(app.ErrCompactionRejected, app.ErrCompactionOutcomeUnknown), protocol.RPCCompactionUnknownErrorCode},
		{errors.Join(app.ErrGoalRunRejected, app.ErrGoalRunOutcomeUnknown), protocol.RPCGoalRunUnknownErrorCode},
		{errors.Join(agent.ErrManagedSteerRejected, agent.ErrManagedSteerStale), protocol.RPCManagedSteerStaleErrorCode},
	} {
		if got := rpcErrorCode(test.err); got != test.code {
			t.Fatalf("code=%s want=%s", got, test.code)
		}
	}
}
