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

// The helper runs before testing flag parsing because production worker startup
// deliberately accepts no executable argument injection hook. Only the child's
// explicit replacement environment selects this local, network-free fixture.
func init() {
	if os.Getenv("SNOW_WEB_RUNTIME_TEST_CHILD") == "1" {
		os.Exit(runtimeWorkerFixture())
	}
}

func runtimeWorkerFixture() int {
	if strings.HasPrefix(os.Getenv("SNOW_WEB_RUNTIME_TEST_MODE"), "queue-") {
		return runtimeQueueFixture()
	}
	if strings.HasPrefix(os.Getenv("SNOW_WEB_RUNTIME_TEST_MODE"), "edit-") || strings.HasPrefix(os.Getenv("SNOW_WEB_RUNTIME_TEST_MODE"), "regenerate-") {
		return runtimeMessageEditFixture()
	}
	if strings.HasPrefix(os.Getenv("SNOW_WEB_RUNTIME_TEST_MODE"), "workflow") {
		return runtimeWorkflowFixture()
	}
	for _, flag := range []string{"--no-session", "--managed-explicit-goals", "--no-plugins", "--no-mcp", "--no-skills", "--no-subagents", "--no-debug"} {
		if !slices.Contains(os.Args, flag) {
			return 10
		}
	}
	if slices.Contains(os.Args, "--permission") {
		return 11 // An explicit override prevents session policy restoration.
	}
	for flag, want := range map[string]string{"--mode": "rpc", "--rpc-startup": "eager", "--tools": "read,glob,grep,write,edit,bash,ask_user,get_goal,create_goal,update_goal,process_start,process_status,process_logs,process_stop,process_list"} {
		index := slices.Index(os.Args, flag)
		if index < 0 || index+1 >= len(os.Args) || os.Args[index+1] != want {
			return 11
		}
	}
	mode := os.Getenv("SNOW_WEB_RUNTIME_TEST_MODE")
	emit := func(value any) {
		// Production root events carry AgentRef metadata even with subagents disabled.
		if event, ok := value.(protocol.AgentEvent); ok && event.Agent == nil && mode != "legacy" {
			event.Agent = &protocol.AgentRef{ThreadID: "root-thread", Path: protocol.RootAgentPath, Depth: 0, Role: "root"}
			value = event
		}
		data, err := json.Marshal(value)
		if err != nil {
			os.Exit(12)
		}
		fmt.Fprintln(os.Stdout, string(data))
	}
	ready := protocol.NewRPCReady("test-worker")
	ready.Capabilities = slices.DeleteFunc(ready.Capabilities, func(s string) bool {
		return s == "usage" || s == "context_report" || s == "model_discovery" || s == "goal_run"
	})
	if mode == "nocap" {
		ready.Capabilities = nil
	}
	emit(ready)
	cwd, _ := os.Getwd()
	if mode == "hang" {
		time.Sleep(time.Hour)
		return 0
	}
	var log *os.File
	if path := os.Getenv("SNOW_WEB_RUNTIME_TEST_LOG"); path != "" {
		log, _ = os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if log != nil {
			defer log.Close()
		}
	}
	active := ""
	promptID := ""
	complete := func(status protocol.RPCPromptStatus) {
		if promptID != "" {
			emit(protocol.RPCPromptCompleted{Type: protocol.RPCTypePromptCompleted, RequestID: promptID, Status: status, Error: "SECRET-COMPLETION"})
			promptID = ""
		}
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		var request protocol.RPCRequest
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			return 13
		}
		if log != nil {
			fmt.Fprintln(log, request.Type)
		}
		response := protocol.RPCResponse{Type: "response", ID: request.ID, Command: request.Type, Success: true}
		switch request.Type {
		case "session_create":
			active = "session-created"
			response.Data = protocol.RPCSessionSummary{SessionID: active, Active: true}
		case "session_open":
			var params struct {
				SessionID string `json:"session_id"`
			}
			_ = json.Unmarshal(request.Params, &params)
			active = params.SessionID
			response.Data = protocol.RPCSessionSummary{SessionID: active, Active: true}
		case "session_info":
			permission, path := "ask", "/private/session.db"
			if mode == "badpermission" {
				permission = "invalid"
			}
			if mode == "memory" {
				path = ""
			}
			response.Data = protocol.RPCSessionInfo{SessionID: active, CWD: cwd, Path: path, Provider: "host-provider", Model: "host-model", PermissionMode: permission}
		case "messages_page":
			response.Data = protocol.RPCMessagesPage{Messages: []protocol.Message{
				{ID: "history-user", Role: protocol.RoleUser, Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: "prior user"}}},
				{ID: "history-assistant", Role: protocol.RoleAssistant, Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: "prior assistant"}, {Type: protocol.BlockThinking, Text: "SECRET-THINKING"}, {Type: protocol.BlockProviderData, Data: []byte("SECRET-PROVIDER")}, {Type: protocol.BlockToolCall, Arguments: []byte(`{"secret":"SECRET-ARGS"}`)}}},
				{ID: "history-tool", Role: protocol.RoleTool, Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: "SECRET-TOOL"}}},
			}}
		case "fixture_complete":
			complete(protocol.RPCPromptCompletedStatus)
		case "abort":
			if promptID != "" && strings.HasPrefix(mode, "cancel-") {
				runtimeCancelAbortFixture(mode, response, emit, complete)
				continue
			}
			complete(protocol.RPCPromptCanceledStatus)
		case "permission_reply":
			emit(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: "approved or denied"})
			complete(protocol.RPCPromptCompletedStatus)
		case "user_input_reply":
			emit(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: "answered"})
			complete(protocol.RPCPromptCompletedStatus)
		case "fixture_recovery_exit":
			return 17 // Test-only EOF gate, released after the admission ack is observed.
		case "prompt":
			promptID = request.ID
			if strings.HasPrefix(request.Message, "cancel-") {
				runtimeCancelPromptFixture(request, response, emit, complete)
				continue
			}
			if request.Message == "ack-exit" {
				emit(protocol.AgentEvent{Type: protocol.EvToolStart, ToolCallID: "unfinished", ToolName: "bash"})
				emit(protocol.AgentEvent{Type: protocol.EvPermissionRequest, Permission: &protocol.Permission{Request: protocol.PermissionRequest{ID: "old-permission", Tool: "bash"}}})
				emit(response)
				continue // Wait for fixture_recovery_exit; never assume scheduler timing.
			}
			if request.Message == "exit" {
				fmt.Fprintln(os.Stderr, "SECRET-STDERR")
				return 17
			}
			emit(protocol.AgentEvent{Type: protocol.EvThinkingDelta, Text: "SECRET-THINKING"})
			emit(protocol.AgentEvent{Type: protocol.EvToolEnd, ToolOutput: "SECRET-OUTPUT"})
			emit(protocol.AgentEvent{Type: protocol.EvError, Message: "SECRET-ERROR"})
			emit(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: "SECRET-CHILD", Agent: &protocol.AgentRef{ThreadID: "child-thread", ParentThreadID: "root-thread", Path: "/root/child", ParentPath: protocol.RootAgentPath, Depth: 1}})

			// Invalid/child interaction events must not create browser prompts or
			// fail an otherwise healthy root turn.
			for _, ref := range []*protocol.AgentRef{
				{ThreadID: "child-thread", ParentThreadID: "root-thread", Path: "/root/child", ParentPath: protocol.RootAgentPath, Depth: 1},
				{},
				{ThreadID: "wrong-depth", Path: protocol.RootAgentPath, Depth: 1},
				{ThreadID: "wrong-parent", Path: protocol.RootAgentPath, ParentPath: protocol.RootAgentPath, ParentThreadID: "other"},
			} {
				emit(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: "SECRET-NONROOT", Agent: ref})
				emit(protocol.AgentEvent{Type: protocol.EvPermissionRequest, Agent: ref})
				emit(protocol.AgentEvent{Type: protocol.EvUserInputRequest, Agent: ref})
			}
			switch request.Message {
			case "permission", "truncated":
				permission := protocol.PermissionRequest{ID: "permission-1", Tool: "bash", Risk: "high", Reason: "Execute a shell command", Paths: []string{"/public/path"}, Args: []byte(`{"command":"SECRET-RAW-ARGS"}`), Effects: []protocol.PermissionEffect{{Type: "process", Command: "echo hello"}}}
				if request.Message == "truncated" {
					permission.Paths = make([]string, 17)
				}
				emit(protocol.AgentEvent{Type: protocol.EvPermissionRequest, Permission: &protocol.Permission{Request: permission}})
			case "input":
				emit(protocol.AgentEvent{Type: protocol.EvUserInputRequest, UserInput: &protocol.UserInputRequest{ID: "input-1", Questions: []protocol.UserInputQuestion{{ID: "question-1", Header: "Choose", Question: "Which option?", ChoicesOnly: true, Options: []protocol.UserInputOption{{Label: "one", Description: "First"}, {Label: "two"}}}}}})
			case "hold":
				emit(protocol.AgentEvent{Type: protocol.EvTurnDone}) // not definitive completion
			case "fail":
				complete(protocol.RPCPromptFailedStatus)
			default:
				emit(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: "public answer"})
				complete(protocol.RPCPromptCompletedStatus) // intentionally before admission ack
			}
		default:
			response.Success = false
			response.Error = "SECRET-REMOTE-ERROR"
		}
		if mode == "slow" && strings.HasPrefix(request.Type, "session_") {
			time.Sleep(30 * time.Millisecond)
		}
		emit(response)
	}
	return 0
}
