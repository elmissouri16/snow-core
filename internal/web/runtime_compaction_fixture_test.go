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
	if os.Getenv("SNOW_WEB_COMPACTION_TEST_CHILD") == "1" {
		os.Exit(runtimeCompactionFixture())
	}
}

// A protocol worker, not a provider mock. Native provider/cleanup races are
// covered by the core and cmd/snow worker tests; this gates transport ordering.
func runtimeCompactionFixture() int {
	mode := os.Getenv("SNOW_WEB_RUNTIME_TEST_MODE")
	logPath := os.Getenv("SNOW_WEB_RUNTIME_TEST_LOG")
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return 1
	}
	defer log.Close()
	emit := func(v any) { data, _ := json.Marshal(v); fmt.Println(string(data)) }
	ready := protocol.NewRPCReady("compaction-fixture")
	ready.Capabilities = slices.DeleteFunc(ready.Capabilities, func(c string) bool { return c == "model_discovery" })
	for _, c := range []string{"compaction_run", "goal_run", "messages_public_history", "usage", "context_report"} {
		if !slices.Contains(ready.Capabilities, c) {
			ready.Capabilities = append(ready.Capabilities, c)
		}
	}
	emit(ready)
	cwd, _ := os.Getwd()
	requestID := ""
	finished := false
	promptDone := false
	wait := func(suffix string) bool {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(logPath + suffix); err == nil {
				return true
			}
			time.Sleep(time.Millisecond)
		}
		return false
	}
	complete := func(status string) {
		c := compactionComplete(status).CompactionCompleted
		c.RequestID = requestID
		if status == "noop" {
			c.SummarizedMessages = 0
		}
		emit(c)
		finished = true
	}
	scan := bufio.NewScanner(os.Stdin)
	scan.Buffer(make([]byte, 4096), 1<<20)
	for scan.Scan() {
		var req protocol.RPCRequest
		if json.Unmarshal(scan.Bytes(), &req) != nil {
			return 2
		}
		fmt.Fprintln(log, req.Type)
		res := protocol.RPCResponse{Type: "response", ID: req.ID, Command: req.Type, Success: true}
		switch req.Type {
		case "session_create", "session_open":
			res.Data = protocol.RPCSessionSummary{SessionID: "session"}
		case "session_info":
			res.Data = protocol.RPCSessionInfo{SessionID: "session", CWD: cwd, Path: "/durable/session", Provider: "host-provider", Model: "host-model", PermissionMode: "ask"}
		case "goal_inspect":
			inspection := protocol.RPCGoalInspection{SessionID: "session", BranchID: "main", TipID: "tip"}
			if promptDone {
				if strings.Contains(mode, "gated") && !wait(".scope") {
					return 10
				}
				inspection.TipID = "after-prompt"
				if mode == "prompt-scope-malformed" {
					inspection.BranchID = "wrong-branch"
				}
			}
			res.Data = inspection
		case "messages_page":
			if finished && strings.Contains(mode, "refresh-gated") && !wait(".refresh") {
				return 9
			}
			res.Data = protocol.RPCMessagesPage{Messages: []protocol.Message{{ID: "user", Role: protocol.RoleUser, Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: "original user"}}}}, HistoryTools: map[string][]protocol.RPCHistoryTool{}}
		case "usage":
			res.Data = protocol.Usage{Input: 10, Output: 5, Total: 15}
			if finished {
				res.Data = protocol.Usage{Input: 30, Output: 10, Total: 40}
			}
		case "context":
			res.Data = map[string]any{"estimated_input_tokens": 100, "context_window": 1000, "PRIVATE": "must-not-retain"}
			if finished {
				res.Data = map[string]any{"estimated_input_tokens": 40, "context_window": 1000, "PRIVATE": "must-not-retain"}
			}
		case "prompt":
			promptDone = true
			emit(res)
			status := protocol.RPCPromptCompletedStatus
			if strings.HasSuffix(mode, "-failed") {
				status = protocol.RPCPromptFailedStatus
			}
			if strings.HasSuffix(mode, "-canceled") {
				status = protocol.RPCPromptCanceledStatus
			}
			emit(protocol.RPCPromptCompleted{Type: protocol.RPCTypePromptCompleted, RequestID: req.ID, Status: status})
			continue
		case "compaction_start":
			requestID = req.ID
			if mode == "lost-rpc-ack" {
				return 0
			}
			if mode == "rejected" {
				res.Success = false
				res.ErrorCode = protocol.RPCCompactionRejectedErrorCode
				break
			}
			var p protocol.RPCCompactionStartParams
			if json.Unmarshal(req.Params, &p) != nil || p.SessionID != "session" || p.BranchID != "main" || p.ExpectedTipID != "tip" {
				return 3
			}
			emit(compactionProgress().AgentEvent)
			emit(protocol.AgentEvent{Type: protocol.EvTurnDone, TurnID: "compact-one", TurnOrigin: "compact", RootEpoch: 1, TurnSequence: 1})
			if strings.HasPrefix(mode, "early-") {
				complete(strings.TrimPrefix(mode, "early-"))
			}
			if mode == "gated" && !wait(".ack") {
				return 4
			}
			res.Data = compactionAccepted()
			if strings.HasPrefix(mode, "late-") || mode == "refresh-gated" {
				emit(res)
				status := strings.TrimPrefix(mode, "late-")
				if mode == "refresh-gated" {
					status = "completed"
				}
				complete(status)
				continue
			}
		case "abort":
			if requestID != "" && !finished {
				complete("canceled")
				if mode == "gated" && !wait(".abort") {
					return 5
				}
			}
		default:
			return 6
		}
		emit(res)
	}
	return 0
}
