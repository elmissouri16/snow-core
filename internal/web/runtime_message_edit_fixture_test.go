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

func runtimeMessageEditFixture() int {
	mode := os.Getenv("SNOW_WEB_RUNTIME_TEST_MODE")
	regenerate := strings.HasPrefix(mode, "regenerate-")
	mode = strings.Replace(mode, "regenerate-", "edit-", 1)
	emit := func(value any) { data, _ := json.Marshal(value); fmt.Println(string(data)) }
	ready := protocol.NewRPCReady("edit-fixture")
	ready.Capabilities = slices.DeleteFunc(ready.Capabilities, func(c string) bool { return c == "goal_run" })
	emit(ready)
	log, _ := os.OpenFile(os.Getenv("SNOW_WEB_RUNTIME_TEST_LOG"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	defer log.Close()
	cwd, _ := os.Getwd()
	messages := []protocol.Message{}
	for i := range 3 {
		messages = append(messages, protocol.Message{ID: fmt.Sprintf("user-%d", i), Role: protocol.RoleUser, Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: "identical"}}}, protocol.Message{ID: fmt.Sprintf("answer-%d", i), Role: protocol.RoleAssistant, Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: fmt.Sprintf("answer %d", i)}, {Type: protocol.BlockPlan, Text: fmt.Sprintf("plan %d", i)}}})
	}
	if regenerate {
		for i := range messages {
			if messages[i].Role == protocol.RoleAssistant {
				messages[i].StopReason = protocol.StopStop
				messages[i].Content = messages[i].Content[:1]
			}
		}
	}
	var preparedAction string
	var prepared protocol.RPCMessageEditPrepared
	branch, serial := "main", 0
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
			res.Data = protocol.RPCSessionSummary{SessionID: "edit-session"}
		case "abort":
		case "session_info":
			res.Data = protocol.RPCSessionInfo{SessionID: "edit-session", Name: "Same title", CWD: cwd, Path: "/durable/session", Provider: "fake", Model: "fake", PermissionMode: "deny"}
		case "usage":
			res.Data = protocol.Usage{}
		case "context":
			res.Data = map[string]int{}
		case "messages_page":
			res.Data = protocol.RPCMessagesPage{Messages: messages, HistoryTools: map[string][]protocol.RPCHistoryTool{}}
		case "message_edit_prepare", "message_regenerate_prepare":
			var p protocol.RPCMessageEditPrepareParams
			_ = json.Unmarshal(req.Params, &p)
			fmt.Fprintf(log, "source:%s:%s\n", p.EntryID, p.TurnID)
			id := p.EntryID
			if p.TurnID == "live-root-turn" {
				id = "live-persisted-user"
				if req.Type == "message_regenerate_prepare" {
					id = "live-persisted-reply"
				}
			}
			index := -1
			for i, message := range messages {
				if message.ID == id {
					if req.Type == "message_edit_prepare" && message.Role == protocol.RoleUser {
						index = i
					}
					if req.Type == "message_regenerate_prepare" && message.Role == protocol.RoleAssistant && message.StopReason == protocol.StopStop {
						index = i - 1
					}
				}
			}
			if index < 0 || p.SessionID != "edit-session" {
				res.Success = false
				break
			}
			serial++
			selected := id
			if req.Type == "message_regenerate_prepare" {
				id = messages[index].ID
			}
			preparedAction = req.Type
			prepared = protocol.RPCMessageEditPrepared{EditToken: fmt.Sprintf("token-%d", serial), SessionID: "edit-session", EntryID: id, TurnID: p.TurnID, SourceBranchID: branch, SourceTipID: messages[len(messages)-1].ID, Text: messages[index].Content[0].Text, ExpiresAt: time.Now().Add(time.Minute).Unix()}
			res.Data = prepared
			if req.Type == "message_regenerate_prepare" {
				res.Data = protocol.RPCMessageRegeneratePrepared{RPCMessageEditPrepared: prepared, ReplyEntryID: selected}
			}
		case "message_edit_commit", "message_regenerate_commit":
			if mode == "edit-exit" {
				return 0
			}
			if mode == "edit-reject" || mode == "edit-ambiguous" {
				res.Success = false
				if mode == "edit-reject" {
					res.ErrorCode = "message_edit_rejected"
				}
				break
			}
			var p protocol.RPCMessageEditCommitParams
			_ = json.Unmarshal(req.Params, &p)
			if req.Type == "message_regenerate_commit" {
				var strict protocol.RPCMessageRegenerateCommitParams
				if json.Unmarshal(req.Params, &strict, json.RejectUnknownMembers(true)) != nil {
					return 6
				}
				p.Text = prepared.Text
			}
			if strings.TrimSuffix(req.Type, "_commit") != strings.TrimSuffix(preparedAction, "_prepare") {
				res.Success = false
				res.ErrorCode = protocol.RPCMessageEditRejectedErrorCode
				break
			}
			if prepared.EditToken == "" || p.EditToken != prepared.EditToken {
				res.Success = false
				break
			}
			if mode == "edit-hold" {
				deadline := time.Now().Add(5 * time.Second)
				for {
					if _, err := os.Stat(os.Getenv("SNOW_WEB_RUNTIME_TEST_LOG") + ".release"); err == nil {
						break
					}
					if time.Now().After(deadline) {
						return 3
					}
					time.Sleep(time.Millisecond)
				}
			}
			index := 0
			for i, message := range messages {
				if message.ID == prepared.EntryID {
					index = i
				}
			}
			messages = append(messages[:index:index], protocol.Message{ID: "replacement-user", Role: protocol.RoleUser, Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: p.Text}}})
			branch = fmt.Sprintf("branch-%d", serial)
			res.Data = protocol.RPCMessageEditCommitted{SessionID: "edit-session", SourceBranchID: prepared.SourceBranchID, SourceTipID: prepared.SourceTipID, EntryID: prepared.EntryID, BranchID: branch, TurnID: "replacement-turn", UserEntryID: "replacement-user", History: protocol.RPCMessagesPage{Messages: messages, HistoryTools: map[string][]protocol.RPCHistoryTool{}}}
			epoch := uint64(2)
			if regenerate {
				epoch = 4
			}
			// Deliberately race the call waiter: every projection event precedes ACK.
			emit(protocol.AgentEvent{Type: protocol.EvTextDelta, TurnID: "old-root", RootEpoch: 99, TurnSequence: 99, Text: "STALE OLD ROOT"})
			emit(protocol.AgentEvent{Type: protocol.EvTextDelta, TurnID: "replacement-turn", RootEpoch: epoch, TurnSequence: 1, Text: "regenerated", ToolOutput: "PRIVATE-TOOL", Message: "PRIVATE-MESSAGE"})
			emit(protocol.AgentEvent{Type: protocol.EvThinkingDelta, TurnID: "replacement-turn", RootEpoch: epoch, TurnSequence: 1, Text: "PRIVATE-THINKING"})
			status := protocol.RPCPromptCompletedStatus
			if mode == "edit-fail" {
				status = protocol.RPCPromptFailedStatus
			}
			emit(protocol.RPCPromptCompleted{Type: protocol.RPCTypePromptCompleted, RequestID: req.ID, Status: status, Error: "PRIVATE-ERROR"})
			time.Sleep(15 * time.Millisecond)
		case "prompt":
			messages = append(messages, protocol.Message{ID: "live-persisted-user", Role: protocol.RoleUser, Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: req.Message}}})
			if regenerate {
				for i := range 2 {
					callID := fmt.Sprintf("step-%d", i)
					emit(protocol.AgentEvent{Type: protocol.EvTextDelta, TurnID: "live-root-turn", RootEpoch: 3, TurnSequence: 2, Text: "same answer"})
					emit(protocol.AgentEvent{Type: protocol.EvToolStart, TurnID: "live-root-turn", RootEpoch: 3, TurnSequence: 2, ToolName: "read", ToolCallID: callID})
					emit(protocol.AgentEvent{Type: protocol.EvToolEnd, TurnID: "live-root-turn", RootEpoch: 3, TurnSequence: 2, ToolName: "read", ToolCallID: callID})
				}
			}
			emit(protocol.AgentEvent{Type: protocol.EvTextDelta, TurnID: "live-root-turn", RootEpoch: 3, TurnSequence: 2, Text: "live answer"})
			if regenerate {
				messages = append(messages, protocol.Message{ID: "live-persisted-reply", Role: protocol.RoleAssistant, StopReason: protocol.StopStop, Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: "live answer"}}})
			}
			if mode == "edit-hold-live" {
				emit(res)
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
				emit(protocol.RPCPromptCompleted{Type: protocol.RPCTypePromptCompleted, RequestID: req.ID, Status: protocol.RPCPromptCompletedStatus})
				continue
			}
			emit(protocol.RPCPromptCompleted{Type: protocol.RPCTypePromptCompleted, RequestID: req.ID, Status: protocol.RPCPromptCompletedStatus})
		default:
			res.Success = false
		}
		emit(res)
	}
	return 0
}
