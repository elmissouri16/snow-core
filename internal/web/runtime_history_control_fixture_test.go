package web

import (
	"bufio"
	"encoding/json/v2"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func init() {
	if os.Getenv("SNOW_WEB_HISTORY_CONTROL_TEST_CHILD") != "" {
		os.Exit(runtimeHistoryControlFixture())
	}
}

func openHistoryControlRuntime(t *testing.T, mode string) (*RuntimeManager, Project, RuntimeSnapshot, string) {
	t.Helper()
	m, projects, log := runtimeTestManager(t, "history-control")
	m.env = slices.DeleteFunc(m.env, func(s string) bool { return strings.HasPrefix(s, "SNOW_WEB_RUNTIME_TEST_CHILD=") })
	m.env = append(m.env, "SNOW_WEB_HISTORY_CONTROL_TEST_CHILD="+mode)
	snapshot, err := m.Open(t.Context(), projects[0], "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	r, err := m.runtime(projects[0].ID, snapshot.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	r.rootEpoch = 1
	r.mu.Unlock()
	return m, projects[0], snapshot, log
}

func historyControlTestRequest(s RuntimeSnapshot) RuntimeHistoryControlRequest {
	return RuntimeHistoryControlRequest{SessionID: s.SessionID, SourceBranchID: "current", SourceTipID: "current-tip", TargetBranchID: "target", TargetTipID: "target-tip", ExpectedRevision: s.Revision, Name: "New branch", OldName: "Earlier"}
}

func runtimeHistoryControlFixture() int {
	mode := os.Getenv("SNOW_WEB_HISTORY_CONTROL_TEST_CHILD")
	emit := func(value any) { data, _ := json.Marshal(value); fmt.Println(string(data)) }
	ready := protocol.NewRPCReady("history-control-fixture")
	for _, capability := range []string{runtimeVersionsCapability, runtimeHistoryControlCapability} {
		if !slices.Contains(ready.Capabilities, capability) {
			ready.Capabilities = append(ready.Capabilities, capability)
		}
	}
	if mode == "no-capability" {
		ready.Capabilities = slices.DeleteFunc(ready.Capabilities, func(c string) bool { return c == runtimeHistoryControlCapability })
	}
	if !strings.HasPrefix(mode, "epoch-") {
		ready.Capabilities = slices.DeleteFunc(ready.Capabilities, func(c string) bool { return c == "goal_run" })
	}
	emit(ready)
	log, _ := os.OpenFile(os.Getenv("SNOW_WEB_RUNTIME_TEST_LOG"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	defer log.Close()
	cwd, _ := os.Getwd()
	active, tip, label := "current", "current-tip", "Earlier"
	collaboration := protocol.ModeDefault
	if strings.HasPrefix(mode, "epoch-") {
		collaboration = protocol.ModePlan
	}
	emitMode := func() {
		emit(protocol.AgentEvent{Type: protocol.EvModeChanged, RootEpoch: 2, Snapshot: true, Mode: &protocol.CollaborationModeState{Mode: protocol.ModeDefault, ReasoningEffort: protocol.ThinkingOff}})
	}
	history := func(branch, tip string) protocol.RPCBranchMessagesPage {
		return protocol.RPCBranchMessagesPage{SessionID: "history-session", BranchID: branch, TipID: tip, Start: 0, Total: 2, Messages: []protocol.Message{{ID: branch + "-user", Role: protocol.RoleUser, Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: branch + " transcript"}}}, {ID: branch + "-answer", Role: protocol.RoleAssistant, StopReason: protocol.StopStop, Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: "Answer"}, {Type: protocol.BlockThinking, Text: "PRIVATE THINKING"}, {Type: protocol.BlockProviderData, Text: "PRIVATE CONTINUITY"}}}}}
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		var req protocol.RPCRequest
		if json.Unmarshal(scanner.Bytes(), &req) != nil {
			return 2
		}
		fmt.Fprintln(log, req.Type)
		res := protocol.RPCResponse{Type: "response", ID: req.ID, Command: req.Type, Success: true}
		switch req.Type {
		case "session_create", "session_open":
			res.Data = protocol.RPCSessionSummary{SessionID: "history-session"}
		case "abort":
		case "session_info":
			res.Data = protocol.RPCSessionInfo{SessionID: "history-session", Name: "History chat", CWD: cwd, Path: "/durable/history", Provider: "fake", Model: "fake", PermissionMode: "deny", Thinking: protocol.ThinkingOff, CollaborationMode: collaboration}
		case "usage":
			res.Data = protocol.Usage{}
		case "context":
			res.Data = map[string]int{}
		case "goal_inspect":
			if mode == "epoch-after-ack" && active == "forked" {
				emitMode()
			}
			res.Data = protocol.RPCGoalInspection{SessionID: "history-session", BranchID: active, TipID: tip}
		case "messages_page":
			page := history(active, tip)
			res.Data = protocol.RPCMessagesPage{Messages: page.Messages, Total: page.Total, HistoryTools: map[string][]protocol.RPCHistoryTool{}}
		case "branches_page":
			var p protocol.RPCBranchesPageParams
			if json.Unmarshal(req.Params, &p) != nil || p.SessionID != "history-session" || p.Limit > 100 {
				return 3
			}
			branches := []protocol.RPCBranchVersion{{ID: "current", TipID: "current-tip", Name: "Current", Active: active == "current"}, {ID: "target", TipID: "target-tip", Name: label}}
			if active == "forked" {
				branches = append(branches, protocol.RPCBranchVersion{ID: active, TipID: tip, Name: "New branch", Active: true})
			}
			res.Data = protocol.RPCBranchesPage{SessionID: p.SessionID, ActiveBranchID: active, ActiveTipID: tip, Branches: branches}
		case "branch_messages_page":
			var p protocol.RPCBranchMessagesPageParams
			if json.Unmarshal(req.Params, &p) != nil || p.SessionID != "history-session" || p.Limit > 64 {
				return 4
			}
			if (p.BranchID != "target" && p.BranchID != "forked") || p.TipID != "target-tip" {
				res.Success = false
				break
			}
			page := history(p.BranchID, p.TipID)
			if mode == "wrong-history" {
				page.BranchID = "other"
			}
			if mode == "oversized-history" {
				page.Total = 100001
			}
			res.Data = page
		case "history_branch_fork", "history_session_fork", "history_branch_rename":
			var p RuntimeHistoryControlRequest
			if json.Unmarshal(req.Params, &p) != nil {
				return 5
			}
			if p.SessionID != "history-session" || p.SourceBranchID != active || p.SourceTipID != tip || p.TargetBranchID != "target" || p.TargetTipID != "target-tip" || req.Type == "history_branch_rename" && p.OldName != label || mode == "reject" {
				res.Success = false
				res.ErrorCode = protocol.RPCHistoryControlRejectedErrorCode
				break
			}
			if mode == "unknown" {
				res.Success = false
				res.ErrorCode = protocol.RPCHistoryControlUnknownErrorCode
				break
			}
			if mode == "exit" {
				return 0
			}
			switch req.Type {
			case "history_branch_fork":
				if mode == "epoch-before-ack" {
					emitMode()
					deadline := time.Now().Add(5 * time.Second)
					for {
						if _, err := os.Stat(os.Getenv("SNOW_WEB_RUNTIME_TEST_LOG") + ".release"); err == nil {
							break
						}
						if time.Now().After(deadline) {
							return 6
						}
						time.Sleep(time.Millisecond)
					}
				}
				active, tip, collaboration = "forked", "target-tip", protocol.ModePlan
				ack := protocol.RPCManagedBranchForkResult{SessionID: "history-session", BranchID: active, TipID: tip, RootEpoch: 2, Mode: collaboration, ReasoningEffort: protocol.ThinkingHigh, Branch: protocol.RPCBranchVersion{ID: active, TipID: tip, ParentID: "target", ForkedFromID: tip, Name: p.Name, Active: true}}
				if strings.HasPrefix(mode, "epoch-") {
					collaboration = protocol.ModeDefault
					ack.Mode = collaboration
					ack.ReasoningEffort = protocol.ThinkingOff
				}
				if mode == "wrong-scope" {
					ack.SessionID = "other-session"
				}
				if mode == "missing-mode" {
					ack.Mode = ""
				}
				res.Data = ack
			case "history_session_fork":
				res.Data = protocol.RPCManagedSessionForkResult{SessionID: "detached-child", Name: p.Name, Branch: protocol.RPCBranchVersion{ID: "main", TipID: "copied-tip", Active: true}, SourceSessionID: p.SessionID, SourceBranchID: p.TargetBranchID, SourceTipID: p.TargetTipID, Mode: protocol.ModePlan, RootEpoch: 1}
			case "history_branch_rename":
				label = p.Name
				res.Data = protocol.RPCManagedBranchRenameResult{SessionID: p.SessionID, BranchID: active, TipID: tip, Branch: protocol.RPCBranchVersion{ID: p.TargetBranchID, TipID: p.TargetTipID, Name: label}, RootEpoch: 1}
			}
		case "prompt", "goal_run":
			runID := ""
			if req.Type == "goal_run" {
				runID = "restored-goal-run"
				res.Data = protocol.RPCGoalRunAccepted{GoalRunID: runID, GoalID: "restored-goal", SessionID: "history-session", BranchID: active}
			}
			emit(protocol.AgentEvent{Type: protocol.EvTextDelta, GoalRunID: runID, RootEpoch: 2, TurnSequence: 1, TurnID: "restored-root", Text: "New explicit answer"})
			emit(protocol.AgentEvent{Type: protocol.EvPermissionRequest, GoalRunID: runID, RootEpoch: 2, TurnSequence: 1, TurnID: "restored-root", Permission: &protocol.Permission{Request: protocol.PermissionRequest{ID: "restored-permission", Tool: "bash"}}})
		default:
			res.Success = false
		}
		emit(res)
	}
	return 0
}
