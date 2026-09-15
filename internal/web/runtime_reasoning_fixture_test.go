package web

import (
	"bufio"
	"encoding/json/v2"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func init() {
	if os.Getenv("SNOW_WEB_REASONING_TEST_CHILD") == "1" {
		os.Exit(reasoningWorkerFixture())
	}
}

// This network-free wire fixture exercises manager behavior, not application
// persistence. Actual CLI/config bytes must be verified by cmd/snow integration.
func reasoningWorkerFixture() int {
	mode := os.Getenv("SNOW_WEB_RUNTIME_TEST_MODE")
	emit := func(v any) { data, _ := json.Marshal(v); fmt.Println(string(data)) }
	ready := protocol.NewRPCReady("reasoning-fixture")
	ready.Capabilities = []string{"session_management", "prompt_completion", "permission_interaction", "user_input", "session_inventory", "session_create", "session_open", "session_info", "messages_page", "settings", "session_reasoning", "model_discovery", "prompt", "abort"}
	if mode == "no-discovery" {
		ready.Capabilities = slices.DeleteFunc(ready.Capabilities, func(c string) bool { return c == "model_discovery" })
	}
	if mode == "no-settings" {
		ready.Capabilities = slices.DeleteFunc(ready.Capabilities, func(c string) bool { return c == "session_reasoning" })
	}
	emit(ready)
	log, err := os.OpenFile(os.Getenv("SNOW_WEB_RUNTIME_TEST_LOG"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return 10
	}
	defer log.Close()
	cwd, _ := os.Getwd()
	active := "reasoning-session"
	collaboration := protocol.ModeDefault
	base := protocol.ThinkingLow
	var plan *protocol.ThinkingLevel
	thinking := func() protocol.ThinkingLevel {
		if collaboration == protocol.ModePlan {
			if plan != nil {
				return *plan
			}
			return protocol.ThinkingMedium
		}
		return base
	}
	summary, verbosity := protocol.ReasoningSummaryAuto, protocol.TextVerbosityLow
	current := func() protocol.RPCSessionReasoning {
		v := protocol.RPCSessionReasoning{RPCSessionReasoningState: protocol.RPCSessionReasoningState{SessionID: active, BranchID: "reasoning-branch", TipID: "", Provider: "fixture", Model: "no-name-guesses", PermissionMode: "ask", Mode: collaboration, Thinking: thinking(), ReasoningSummary: summary, TextVerbosity: verbosity}, ThinkingLevels: []protocol.ThinkingLevel{protocol.ThinkingOff, protocol.ThinkingLow, protocol.ThinkingMedium, protocol.ThinkingHigh}, ReasoningSummaries: protocol.KnownReasoningSummaries(), TextVerbosities: protocol.KnownTextVerbosities()}
		if mode == "unknown-summary" {
			v.ReasoningSummaries = nil
		}
		if mode == "unsupported" {
			v.ThinkingLevels = []protocol.ThinkingLevel{protocol.ThinkingOff}
			v.ReasoningSummaries = nil
			v.TextVerbosities = nil
		}
		return v
	}
	mutated := false
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var req protocol.RPCRequest
		if json.Unmarshal(scanner.Bytes(), &req) != nil {
			return 11
		}
		fmt.Fprintln(log, req.Type)
		res := protocol.RPCResponse{Type: "response", ID: req.ID, Command: req.Type, Success: true}
		switch req.Type {
		case "session_create", "session_open":
			res.Data = protocol.RPCSessionSummary{SessionID: active}
		case "abort":
		case "messages_page":
			res.Data = protocol.RPCMessagesPage{}
		case "session_info":
			session := active
			if mutated && mode == "wrong-session" {
				session = "other-session"
			}
			info := protocol.RPCSessionInfo{SessionID: session, CWD: cwd, Path: "/fixture/session.db", Provider: "fixture", Model: "no-name-guesses", PermissionMode: "ask", CollaborationMode: collaboration, Thinking: thinking(), ThinkingLevels: []protocol.ThinkingLevel{protocol.ThinkingOff, protocol.ThinkingLow, protocol.ThinkingMedium, protocol.ThinkingHigh}, ReasoningSummary: summary, TextVerbosity: verbosity}
			if mode == "pending-input" {
				info.PendingInputs.Total = 1
			}
			res.Data = info
		case "session_reasoning_get":
			res.Data = current()
			if mutated && mode == "read-rejected" {
				res.Success = false
			}
		case "set_mode":
			collaboration = protocol.CollaborationMode(req.Mode)
		case "session_reasoning_set":
			fmt.Fprintln(log, "params:"+string(req.Params))
			var params protocol.RPCSessionReasoningSetParams
			if json.Unmarshal(req.Params, &params, json.RejectUnknownMembers(true)) != nil || params.Expected != current().RPCSessionReasoningState || !reasoningField(params.Field) {
				return 12
			}
			mutated = true
			if mode != "mismatch" {
				switch params.Field {
				case "thinking":
					level := protocol.ThinkingLevel(params.Value)
					if collaboration == protocol.ModePlan {
						plan = new(level)
					} else {
						base = level
					}
				case "reasoning_summary":
					summary = protocol.ReasoningSummary(params.Value)
				case "text_verbosity":
					verbosity = protocol.TextVerbosity(params.Value)
				}
			}
			if mode == "exit" {
				return 0
			}
			if mode == "rejected" {
				res.Success = false
			}
			res.Data = current()
		default:
			if strings.HasPrefix(req.Type, "goal_") {
				return 14
			}
			res.Success = false
		}
		emit(res)
	}
	return 0
}
