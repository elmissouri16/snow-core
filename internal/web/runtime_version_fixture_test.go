package web

import (
	"bufio"
	"encoding/json/v2"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func init() {
	if os.Getenv("SNOW_WEB_VERSION_TEST_CHILD") != "" {
		os.Exit(runtimeVersionFixture())
	}
}

func runtimeVersionFixture() int {
	mode := os.Getenv("SNOW_WEB_VERSION_TEST_CHILD")
	emit := func(v any) { data, _ := json.Marshal(v); fmt.Println(string(data)) }
	ready := protocol.NewRPCReady("version-fixture")
	if !strings.HasPrefix(mode, "epoch-") {
		ready.Capabilities = slices.DeleteFunc(ready.Capabilities, func(c string) bool { return c == "goal_run" })
	}
	if mode == "no-capability" {
		ready.Capabilities = slices.DeleteFunc(ready.Capabilities, func(c string) bool { return c == runtimeVersionsCapability })
	} else if !slices.Contains(ready.Capabilities, runtimeVersionsCapability) {
		ready.Capabilities = append(ready.Capabilities, runtimeVersionsCapability)
	}
	emit(ready)
	log, _ := os.OpenFile(os.Getenv("SNOW_WEB_RUNTIME_TEST_LOG"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	defer log.Close()
	cwd, _ := os.Getwd()
	active, activeTip := "current", "current-tip"
	collaboration := protocol.ModeDefault
	if strings.HasPrefix(mode, "epoch-") {
		collaboration = protocol.ModePlan
	}
	token := ""
	serial := 0
	emitRestoredMode := func() {
		emit(protocol.AgentEvent{Type: protocol.EvModeChanged, RootEpoch: 2, Snapshot: true, Mode: &protocol.CollaborationModeState{Mode: protocol.ModeDefault, ReasoningEffort: protocol.ThinkingOff}})
	}
	history := func(branch, tip string) protocol.RPCBranchMessagesPage {
		text := "Current transcript"
		if branch == "target" {
			text = "Earlier version <script>alert(1)</script>"
		}
		return protocol.RPCBranchMessagesPage{SessionID: "version-session", BranchID: branch, TipID: tip, Start: 0, Total: 2, Messages: []protocol.Message{{ID: branch + "-user", Role: protocol.RoleUser, Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: text}}}, {ID: branch + "-answer", Role: protocol.RoleAssistant, StopReason: protocol.StopStop, Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: "**Answer**"}, {Type: protocol.BlockThinking, Text: "PRIVATE THINKING"}, {Type: protocol.BlockProviderData, Text: "PRIVATE CONTINUITY"}}}}}
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
			res.Data = protocol.RPCSessionSummary{SessionID: "version-session"}
		case "abort":
		case "session_info":
			res.Data = protocol.RPCSessionInfo{SessionID: "version-session", Name: "Version chat", CWD: cwd, Path: "/durable/version", Provider: "fake", Model: "fake", PermissionMode: "deny", Thinking: protocol.ThinkingOff, CollaborationMode: collaboration}
		case "goal_inspect":
			// Commit's read-only goal refresh starts only after restore publication.
			// This places the new epoch event deterministically after the ACK path.
			if mode == "epoch-after-ack" && active == "target" {
				emitRestoredMode()
			}
			res.Data = protocol.RPCGoalInspection{SessionID: "version-session", BranchID: active, TipID: activeTip}
		case "usage":
			res.Data = protocol.Usage{}
		case "context":
			res.Data = map[string]int{}
		case "messages_page":
			p := history(active, activeTip)
			res.Data = protocol.RPCMessagesPage{Messages: p.Messages, Total: p.Total, HistoryTools: map[string][]protocol.RPCHistoryTool{}}
		case "branches_page":
			var p protocol.RPCBranchesPageParams
			if json.Unmarshal(req.Params, &p) != nil || p.SessionID != "version-session" || p.Limit > 100 {
				return 3
			}
			page := protocol.RPCBranchesPage{SessionID: p.SessionID, ActiveBranchID: active, ActiveTipID: activeTip, Branches: []protocol.RPCBranchVersion{{ID: "current", TipID: "current-tip", Name: "Current", Active: active == "current"}, {ID: "target", TipID: "target-tip", Name: "Earlier", Active: active == "target"}}}
			if mode == "stale-list" {
				page.SessionID = "other-session"
			}
			if mode == "oversized-list" {
				page.Branches = make([]protocol.RPCBranchVersion, 101)
			}
			res.Data = page
		case "branch_messages_page":
			var p protocol.RPCBranchMessagesPageParams
			if json.Unmarshal(req.Params, &p) != nil || p.SessionID != "version-session" || p.Limit > 64 {
				return 4
			}
			if p.BranchID != "target" || p.TipID != "target-tip" {
				res.Success = false
				break
			}
			page := history(p.BranchID, p.TipID)
			if mode == "stale-preview" {
				page.TipID = "changed-tip"
			}
			if mode == "oversized-preview" {
				page.Messages[0].Content[0].Text = strings.Repeat("x", runtimeVersionWireBytes+1)
			}
			res.Data = page
		case "branch_restore_prepare":
			var p protocol.RPCBranchRestorePrepareParams
			if json.Unmarshal(req.Params, &p) != nil {
				return 5
			}
			if p.SessionID != "version-session" || p.SourceBranchID != active || p.SourceTipID != activeTip || p.TargetBranchID != "target" || p.TargetTipID != "target-tip" {
				res.Success = false
				res.ErrorCode = protocol.RPCBranchRestoreRejectedErrorCode
				break
			}
			serial++
			token = fmt.Sprintf("restore-token-%d", serial)
			res.Data = protocol.RPCBranchRestorePrepared{RPCBranchRestorePrepareParams: p, RestoreToken: token, ExpiresAt: time.Now().Add(2 * time.Minute).UnixMilli()}
		case "branch_restore_commit":
			var p protocol.RPCBranchRestoreCommitParams
			if json.Unmarshal(req.Params, &p) != nil {
				return 6
			}
			if p.SessionID != "version-session" || p.RestoreToken != token || token == "" {
				res.Success = false
				res.ErrorCode = protocol.RPCBranchRestoreRejectedErrorCode
				break
			}
			token = ""
			if mode == "epoch-before-ack" {
				emitRestoredMode()
			}
			if mode == "hold" || mode == "epoch-before-ack" {
				deadline := time.Now().Add(5 * time.Second)
				for {
					if _, err := os.Stat(os.Getenv("SNOW_WEB_RUNTIME_TEST_LOG") + ".release"); err == nil {
						break
					}
					if time.Now().After(deadline) {
						return 7
					}
					time.Sleep(time.Millisecond)
				}
			}
			if mode == "exit" {
				return 0
			}
			if mode == "reject" || mode == "unknown" {
				res.Success = false
				if mode == "reject" {
					res.ErrorCode = protocol.RPCBranchRestoreRejectedErrorCode
				} else {
					res.ErrorCode = protocol.RPCBranchRestoreUnknownErrorCode
				}
				break
			}
			active, activeTip = "target", "target-tip"
			collaboration = protocol.ModePlan
			if strings.HasPrefix(mode, "epoch-") {
				collaboration = protocol.ModeDefault
			}
			result := runtimeVersionRestoreACK{SessionID: "version-session", BranchID: active, TipID: activeTip, History: history(active, activeTip), Settings: protocol.RPCSettings{Provider: "fake", Model: "fake", PermissionMode: "deny", Thinking: protocol.ThinkingOff}, Mode: collaboration}
			if mode == "wrong-scope" {
				result.SessionID = "other-session"
			}
			if mode == "settings-drift" {
				result.Settings.PermissionMode = "allow"
			}
			if mode == "missing-mode" {
				result.Mode = ""
			}
			res.Data = result
		case "prompt", "goal_run":
			runID := ""
			if req.Type == "goal_run" {
				runID = "restored-goal-run"
				res.Data = protocol.RPCGoalRunAccepted{GoalRunID: runID, GoalID: "restored-goal", SessionID: "version-session", BranchID: active}
			}
			emit(protocol.AgentEvent{Type: protocol.EvTextDelta, GoalRunID: runID, RootEpoch: 2, TurnSequence: 1, TurnID: "restored-root", Text: "New explicit answer"})
			if strings.HasPrefix(mode, "epoch-") {
				emit(protocol.AgentEvent{Type: protocol.EvPermissionRequest, GoalRunID: runID, RootEpoch: 2, TurnSequence: 1, TurnID: "restored-root", Permission: &protocol.Permission{Request: protocol.PermissionRequest{ID: "restored-permission", Tool: "bash"}}})
			} else {
				emit(protocol.RPCPromptCompleted{Type: protocol.RPCTypePromptCompleted, RequestID: req.ID, Status: protocol.RPCPromptCompletedStatus})
			}
		default:
			res.Success = false
		}
		emit(res)
	}
	return 0
}
