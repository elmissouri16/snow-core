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

// Separate opt-in child avoids changing the shared worker fixture dispatcher.
func init() {
	if os.Getenv("SNOW_WEB_STEER_TEST_CHILD") == "1" {
		os.Exit(runtimeSteerFixture())
	}
}

func runtimeSteerFixture() int {
	mode := os.Getenv("SNOW_WEB_RUNTIME_TEST_MODE")
	emit := func(value any) { data, _ := json.Marshal(value); fmt.Println(string(data)) }
	ready := protocol.NewRPCReady("steer-fixture")
	ready.Capabilities = slices.DeleteFunc(ready.Capabilities, func(c string) bool { return c == "goal_run" || c == "model_discovery" })
	if !slices.Contains(ready.Capabilities, "managed_steer") {
		ready.Capabilities = append(ready.Capabilities, "managed_steer")
	}
	emit(ready)
	log, _ := os.OpenFile(os.Getenv("SNOW_WEB_RUNTIME_TEST_LOG"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	defer log.Close()
	cwd, _ := os.Getwd()
	q := protocol.QueueControl{SessionID: "steer-session"}
	pending := make(map[string]string)
	root, serial := 0, 0
	promptID, userID := "", ""
	event := func(e protocol.AgentEvent) {
		e.RootEpoch = 1
		e.TurnSequence = uint64(root)
		e.TurnID = q.TurnID
		emit(e)
	}
	queueEvent := func() { event(protocol.AgentEvent{Type: protocol.EvQueueUpdated, QueueControl: q.Clone()}) }
	deliver := func(id, text string) {
		q.Revision++
		q.Change = protocol.QueueControlChange{Kind: "delivered", ItemID: id, UserEntryID: "steer-user-" + id, PreviousUserEntryID: userID, SpanID: "span-" + id, Text: text}
		userID = q.Change.UserEntryID
		queueEvent()
		delete(pending, id)
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
			res.Data = protocol.RPCSessionSummary{SessionID: q.SessionID}
		case "session_info":
			res.Data = protocol.RPCSessionInfo{SessionID: q.SessionID, Name: "Steer fixture", CWD: cwd, Path: "/durable/steer", Provider: "fake", Model: "fake", PermissionMode: "deny"}
		case "usage":
			res.Data = protocol.Usage{}
		case "context":
			res.Data = map[string]int{}
		case "messages_page":
			res.Data = protocol.RPCMessagesPage{Messages: []protocol.Message{}, HistoryTools: map[string][]protocol.RPCHistoryTool{}}
		case "prompt":
			root++
			promptID = req.ID
			userID = fmt.Sprintf("user-%d", root)
			q.TurnID = fmt.Sprintf("root-%d", root)
			q.Revision++
			q.Accepting = true
			q.Change = protocol.QueueControlChange{Kind: "root_admitted", UserEntryID: userID}
			queueEvent()
		case "managed_steer":
			var p protocol.RPCManagedSteerParams
			if json.Unmarshal(req.Params, &p) != nil {
				return 3
			}
			if p.SessionID != q.SessionID || p.TurnID != q.TurnID || p.RootEpoch != 1 || p.RequestID == "" || !q.Accepting {
				res.Success = false
				res.ErrorCode = protocol.RPCManagedSteerStaleErrorCode
				break
			}
			if mode == "steer-reject" {
				res.Success = false
				res.ErrorCode = protocol.RPCManagedSteerRejectedErrorCode
				break
			}
			if mode == "steer-exit" {
				return 0
			}
			serial++
			id := fmt.Sprintf("native-%d", serial)
			pending[id] = p.Text
			q.Revision++
			q.Change = protocol.QueueControlChange{Kind: "steer_accepted", ItemID: id}
			queueEvent()
			res.Data = protocol.RPCManagedSteerResult{SessionID: p.SessionID, TurnID: p.TurnID, RootEpoch: p.RootEpoch, RequestID: p.RequestID, ItemID: id, Status: "accepted"}
			if mode == "steer-event-first" {
				deliver(id, p.Text)
			}
			if mode == "steer-holdack" {
				deadline := time.Now().Add(5 * time.Second)
				for {
					if _, err := os.Stat(os.Getenv("SNOW_WEB_RUNTIME_TEST_LOG") + ".release"); err == nil {
						break
					}
					if time.Now().After(deadline) {
						return 4
					}
					time.Sleep(time.Millisecond)
				}
			}
			if mode == "steer-ack-first" {
				emit(res)
				deliver(id, p.Text)
				continue
			}
		case "queue_enqueue":
			var p protocol.RPCQueueEnqueueParams
			if json.Unmarshal(req.Params, &p) != nil {
				return 5
			}
			count, bytes := len(pending)+len(q.Items)+len(q.ReviewItems), 0
			for _, text := range pending {
				bytes += len(text)
			}
			for _, items := range [][]protocol.QueueControlItem{q.Items, q.ReviewItems} {
				for _, item := range items {
					bytes += len(item.Text)
				}
			}
			if count >= protocol.RPCQueueMaxItems || bytes+len(p.Text) > protocol.RPCQueueMaxTotalBytes {
				res.Success = false
				res.ErrorCode = protocol.RPCQueueRejectedErrorCode
				break
			}
			if p.Revision != q.Revision || p.TurnID != q.TurnID || p.SessionID != q.SessionID {
				res.Success = false
				res.ErrorCode = protocol.RPCQueueStaleErrorCode
				break
			}
			serial++
			id := fmt.Sprintf("followup-%d", serial)
			q.Items = append(q.Items, protocol.QueueControlItem{ID: id, Text: p.Text, State: "pending"})
			q.Revision++
			q.Change = protocol.QueueControlChange{Kind: "enqueued", ItemID: id}
			queueEvent()
			res.Data = q.Clone()
		case "abort":
			if promptID != "" {
				for id := range pending {
					q.Revision++
					q.Change = protocol.QueueControlChange{Kind: "discarded", ItemID: id}
					queueEvent()
					delete(pending, id)
				}
				for _, item := range q.Items {
					item.State = "held"
					q.ReviewItems = append(q.ReviewItems, item)
				}
				q.Items = nil
				q.Accepting = false
				q.Revision++
				q.Change = protocol.QueueControlChange{Kind: "closed"}
				queueEvent()
				emit(protocol.RPCPromptCompleted{Type: protocol.RPCTypePromptCompleted, RequestID: promptID, Status: protocol.RPCPromptCanceledStatus})
				promptID = ""
			}
		default:
			if strings.HasPrefix(req.Type, "shutdown") {
				emit(res)
				return 0
			}
		}
		emit(res)
	}
	return 0
}
