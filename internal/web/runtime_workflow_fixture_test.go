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

func runtimeWorkflowFixture() int {
	mode := os.Getenv("SNOW_WEB_RUNTIME_TEST_MODE")
	emit := func(v any) { b, _ := json.Marshal(v); fmt.Println(string(b)) }
	ready := protocol.NewRPCReady("workflow")
	ready.Capabilities = slices.DeleteFunc(ready.Capabilities, func(c string) bool { return c == "goal_run" })
	if !slices.Contains(ready.Capabilities, "session_model_selection") {
		ready.Capabilities = append(ready.Capabilities, "session_model_selection")
	}
	if mode == "workflow-no-model-selection" {
		ready.Capabilities = slices.DeleteFunc(ready.Capabilities, func(s string) bool { return s == "session_model_selection" })
	}
	if mode == "workflow-legacy" {
		ready.Capabilities = slices.DeleteFunc(ready.Capabilities, func(s string) bool { return s == "model_discovery" })
	}
	emit(ready)
	log, _ := os.OpenFile(os.Getenv("SNOW_WEB_RUNTIME_TEST_LOG"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	defer log.Close()
	cwd, _ := os.Getwd()
	active, provider, model, name, collaboration := "", "host-provider", "host-model", "Original", "default"
	epoch, sequence := uint64(0), uint64(0)
	prompt := ""
	created := 0
	mutated := false
	policy := "ask"
	policies := map[string]string{"saved-ask": "ask", "saved-deny": "deny", "saved-allow": "allow", "saved-invalid": "invalid"}
	event := func(kind protocol.AgentEventType, text string) {
		emit(protocol.AgentEvent{Type: kind, Text: text, RootEpoch: epoch, TurnSequence: sequence, TurnID: fmt.Sprintf("turn-%d-%d", epoch, sequence)})
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var req protocol.RPCRequest
		if json.Unmarshal(scanner.Bytes(), &req) != nil {
			return 1
		}
		fmt.Fprintln(log, req.Type)
		res := protocol.RPCResponse{Type: "response", ID: req.ID, Command: req.Type, Success: true}
		switch req.Type {
		case "session_create", "session_open":
			if prompt != "" {
				return 2
			}
			if req.Type == "session_create" {
				created++
				active = fmt.Sprintf("created-%d", created)
			} else {
				var params struct {
					SessionID string `json:"session_id"`
				}
				_ = json.Unmarshal(req.Params, &params)
				active = params.SessionID
			}
			policy = policies[active]
			if policy == "" {
				policy = "ask"
			}
			policies[active] = policy
			epoch++
			sequence = 0
			emit(protocol.AgentEvent{Type: protocol.EvModeChanged, RootEpoch: epoch, Snapshot: true, Mode: &protocol.CollaborationModeState{Mode: protocol.ModeDefault}})
			res.Data = protocol.RPCSessionSummary{SessionID: active}
			if epoch > 1 {
				mutated = true
			}
			if mode == "workflow-switch-rejected" && epoch > 1 {
				fmt.Fprintln(log, "bound:"+active)
				res.Success = false
				res.Data = nil
			}
		case "session_info":
			if (mode == "workflow-info-rejected" || mode == "workflow-policy-info-rejected") && mutated {
				res.Success = false
				break
			}
			p := provider
			if mode == "workflow-wrong-model" && active == "saved" {
				p = "unexpected"
			}
			res.Data = protocol.RPCSessionInfo{SessionID: active, Name: name, CWD: cwd, Path: "/durable/session.db", Provider: p, Model: model, PermissionMode: policy, CollaborationMode: protocol.CollaborationMode(collaboration), Thinking: protocol.ThinkingHigh}
		case "session_delete":
			var params protocol.RPCSessionDeleteParams
			_ = json.Unmarshal(req.Params, &params)
			res.Data = protocol.RPCSessionDeleteResult{SessionID: params.SessionID, Deleted: true}
			if mode == "workflow-delete-error" {
				res.Success = false
				res.Error = "PRIVATE cleanup error"
			}
			if mode == "workflow-delete-wrong" {
				res.Data = protocol.RPCSessionDeleteResult{SessionID: "other", Deleted: true}
			}
		case "sessions_list":
			list := []protocol.RPCSessionSummary{{SessionID: active, Name: name, Active: true}, {SessionID: "saved", Name: "Saved conversation"}}
			if strings.HasPrefix(mode, "workflow-policy") {
				for id := range policies {
					if id != active {
						list = append(list, protocol.RPCSessionSummary{SessionID: id})
					}
				}
			}
			if mode == "workflow-large" {
				for i := range 1200 {
					list = append(list, protocol.RPCSessionSummary{SessionID: fmt.Sprintf("extra-%d", i), Name: strings.Repeat("x", 300)})
				}
			}
			res.Data = protocol.RPCSessionList{Sessions: list}
		case "messages_page":
			res.Data = protocol.RPCMessagesPage{Messages: []protocol.Message{{ID: "plan", Role: protocol.RoleAssistant, Content: []protocol.ContentBlock{{Type: protocol.BlockPlan, Text: "Saved plan"}}}}}
		case "models_discover":
			models := []protocol.Model{{Provider: "host-provider", ID: "host-model", DisplayName: "Host"}, {Provider: "other", ID: "next", DisplayName: "Other", ContextWindow: 200000}}
			if mode == "workflow-large" {
				models = append(models, protocol.Model{Provider: "other", ID: strings.Repeat("x", 512)}, protocol.Model{Provider: "other", ID: "long-name", DisplayName: strings.Repeat("n", 300)})
			}
			res.Data = protocol.RPCModelDiscovery{Models: models, Partial: true}
		case "models_list":
			res.Data = protocol.RPCModelList{Provider: provider, Current: model, Models: []protocol.Model{{Provider: provider, ID: model}, {Provider: "other", ID: "next"}}}
		case "permission_mode_set":
			mutated = true
			var params protocol.RPCPermissionMode
			_ = json.Unmarshal(req.Params, &params)
			if mode != "workflow-policy-mismatch" {
				policy = params.Mode
				policies[active] = policy
			}
			fmt.Fprintln(log, "policy:"+policy)
			if mode == "workflow-policy-exit" {
				return 18
			}
			if mode == "workflow-policy-rejected" {
				res.Success = false
			}
			if mode == "workflow-policy-browser-disconnect" {
				deadline := time.Now().Add(5 * time.Second)
				for {
					if _, err := os.Stat(os.Getenv("SNOW_WEB_RUNTIME_TEST_LOG") + ".release"); err == nil {
						break
					}
					if time.Now().After(deadline) {
						return 19
					}
					time.Sleep(time.Millisecond)
				}
			}
		case "session_set_model":
			mutated = true
			provider = req.Provider
			model = req.Model
		case "set_mode":
			mutated = true
			collaboration = req.Mode
		case "session_rename":
			mutated = true
			var params struct {
				Name string `json:"name"`
			}
			_ = json.Unmarshal(req.Params, &params)
			name = params.Name
			res.Data = protocol.RPCSessionRenameResult{SessionID: active, Name: name}
		case "usage":
			res.Data = protocol.Usage{Input: 100, Output: 20, Total: 120}
		case "context":
			res.Data = map[string]any{"estimated_input_tokens": 110, "context_window": 200000, "categories": []any{map[string]any{"text": "PRIVATE-CATEGORY"}}}
		case "prompt":
			prompt = req.ID
			sequence++
			if epoch > 1 {
				emit(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: "STALE-EPOCH", RootEpoch: epoch - 1, TurnSequence: 99, TurnID: "old"})
			}
			if req.Message == "hold" {
				event(protocol.EvTextDelta, "Running")
				break
			}
			for _, n := range []int{10, 15} {
				emit(protocol.AgentEvent{Type: protocol.EvUsage, RootEpoch: epoch, TurnSequence: sequence, TurnID: fmt.Sprintf("turn-%d-%d", epoch, sequence), Usage: &protocol.Usage{Input: n, Output: 3, Total: n + 3}})
			}
			emit(protocol.AgentEvent{Type: protocol.EvPlanStarted, RootEpoch: epoch, TurnSequence: sequence, TurnID: fmt.Sprintf("turn-%d-%d", epoch, sequence), Plan: &protocol.PlanItem{ID: "proposed"}})
			emit(protocol.AgentEvent{Type: protocol.EvPlanDelta, Text: "Proposed ", RootEpoch: epoch, TurnSequence: sequence, TurnID: fmt.Sprintf("turn-%d-%d", epoch, sequence), Plan: &protocol.PlanItem{ID: "proposed"}})
			emit(protocol.AgentEvent{Type: protocol.EvPlanCompleted, RootEpoch: epoch, TurnSequence: sequence, TurnID: fmt.Sprintf("turn-%d-%d", epoch, sequence), Plan: &protocol.PlanItem{ID: "proposed", Text: "Proposed plan"}})
			emit(protocol.AgentEvent{Type: protocol.EvTurnDone, RootEpoch: epoch, TurnSequence: sequence, TurnID: fmt.Sprintf("turn-%d-%d", epoch, sequence), Usage: &protocol.Usage{Input: 15, Output: 3, Total: 18}})
			emit(protocol.RPCPromptCompleted{Type: protocol.RPCTypePromptCompleted, RequestID: prompt, Status: protocol.RPCPromptCompletedStatus})
			prompt = ""
		case "abort":
			if prompt != "" {
				emit(res) // ack is intentionally NOT definitive completion
				time.Sleep(70 * time.Millisecond)
				emit(protocol.RPCPromptCompleted{Type: protocol.RPCTypePromptCompleted, RequestID: prompt, Status: protocol.RPCPromptCanceledStatus})
				prompt = ""
				continue
			}
		default:
			res.Success = false
		}
		emit(res)
	}
	return 0
}
