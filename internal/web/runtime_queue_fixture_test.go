package web

import (
	"bufio"
	"encoding/json/v2"
	"fmt"
	"os"
	"slices"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func runtimeQueueFixture() int {
	mode := os.Getenv("SNOW_WEB_RUNTIME_TEST_MODE")
	emit := func(value any) { data, _ := json.Marshal(value); fmt.Println(string(data)) }
	ready := protocol.NewRPCReady("queue-fixture")
	ready.Capabilities = slices.DeleteFunc(ready.Capabilities, func(c string) bool { return c == "goal_run" })
	emit(ready)
	log, _ := os.OpenFile(os.Getenv("SNOW_WEB_RUNTIME_TEST_LOG"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	defer log.Close()
	cwd, _ := os.Getwd()
	q := protocol.QueueControl{SessionID: "queue-session"}
	promptID := ""
	root := 0
	serial := 0
	event := func(e protocol.AgentEvent) {
		e.RootEpoch = 1
		e.TurnID = q.TurnID
		e.TurnSequence = uint64(root)
		emit(e)
	}
	queueEvent := func() { event(protocol.AgentEvent{Type: protocol.EvQueueUpdated, QueueControl: q.Clone()}) }
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
			res.Data = protocol.RPCSessionSummary{SessionID: "queue-session"}
		case "session_info":
			res.Data = protocol.RPCSessionInfo{SessionID: "queue-session", Name: "Same queue chat", CWD: cwd, Path: "/durable/queue", Provider: "fake", Model: "fake", PermissionMode: "deny"}
		case "usage":
			res.Data = protocol.Usage{}
		case "context":
			res.Data = map[string]int{}
		case "messages_page":
			res.Data = protocol.RPCMessagesPage{Messages: []protocol.Message{}, HistoryTools: map[string][]protocol.RPCHistoryTool{}}
		case "prompt":
			if mode == "queue-reject-held" && len(q.ReviewItems) != 0 {
				res.Success = false
				break
			}
			root++
			promptID = req.ID
			q.TurnID = fmt.Sprintf("root-%d", root)
			q.Revision++
			q.Accepting = true
			q.Items = nil
			q.Change = protocol.QueueControlChange{Kind: "root_admitted", UserEntryID: fmt.Sprintf("user-%d", root)}
			queueEvent()
			event(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: "first answer"})
		case "queue_enqueue", "queue_update", "queue_remove":
			var p protocol.RPCQueueUpdateParams
			if json.Unmarshal(req.Params, &p) != nil {
				return 3
			}
			if mode == "queue-exit" {
				return 0
			}
			if mode == "queue-reject" || mode == "queue-unknown" {
				res.Success = false
				if mode == "queue-reject" {
					res.ErrorCode = protocol.RPCQueueRejectedErrorCode
				}
				break
			}
			if p.SessionID != q.SessionID || p.TurnID != q.TurnID || p.Revision != q.Revision {
				res.Success = false
				res.ErrorCode = protocol.RPCQueueStaleErrorCode
				break
			}
			if mode == "queue-holdack" {
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
			switch req.Type {
			case "queue_enqueue":
				serial++
				p.ItemID = fmt.Sprintf("item-%d", serial)
				q.Items = append(q.Items, protocol.QueueControlItem{ID: p.ItemID, Text: p.Text, State: "pending"})
			case "queue_update":
				index := slices.IndexFunc(q.Items, func(i protocol.QueueControlItem) bool { return i.ID == p.ItemID })
				if index < 0 {
					return 5
				}
				q.Items[index].Text = p.Text
			case "queue_remove":
				q.Items = slices.DeleteFunc(q.Items, func(i protocol.QueueControlItem) bool { return i.ID == p.ItemID })
				q.ReviewItems = slices.DeleteFunc(q.ReviewItems, func(i protocol.QueueControlItem) bool { return i.ID == p.ItemID })
			}
			q.Revision++
			kind := map[string]string{"queue_enqueue": "enqueued", "queue_update": "updated", "queue_remove": "removed"}[req.Type]
			q.Change = protocol.QueueControlChange{Kind: kind, ItemID: p.ItemID}
			queueEvent()
			res.Data = q.Clone()
			if mode == "queue-unknown-after-event" {
				res.Success = false
				break
			}
			if mode == "queue-deliver" && req.Type == "queue_enqueue" {
				oldUser := fmt.Sprintf("user-%d", root)
				q.Revision++
				q.Change = protocol.QueueControlChange{Kind: "reply", UserEntryID: oldUser, ReplyEntryID: "first-answer-id"}
				queueEvent()
				q.Revision++
				q.Items[0].State = "delivering"
				q.Change = protocol.QueueControlChange{Kind: "selected", ItemID: p.ItemID}
				queueEvent()
				q.Revision++
				q.Items = nil
				q.Change = protocol.QueueControlChange{Kind: "delivered", ItemID: p.ItemID, UserEntryID: "queued-user-id", PreviousUserEntryID: oldUser, PrecedingReplyEntryID: "first-answer-id", SpanID: "queued-span", Text: p.Text}
				queueEvent()
				event(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: "queued answer"})
				q.Revision++
				q.Change = protocol.QueueControlChange{Kind: "reply", UserEntryID: "queued-user-id", ReplyEntryID: "queued-answer-id"}
				queueEvent()
				q.Revision++
				q.Accepting = false
				q.Change = protocol.QueueControlChange{Kind: "closed"}
				queueEvent()
				emit(protocol.RPCPromptCompleted{Type: protocol.RPCTypePromptCompleted, RequestID: promptID, Status: protocol.RPCPromptCompletedStatus})
				time.Sleep(15 * time.Millisecond)
			}
		case "abort":
			if promptID == "" {
				break
			}
			q.Accepting = false
			for _, item := range q.Items {
				item.State = "held"
				q.ReviewItems = append(q.ReviewItems, item)
			}
			q.Items = nil
			q.Revision++
			q.Change = protocol.QueueControlChange{Kind: "retained"}
			queueEvent()
			emit(protocol.RPCPromptCompleted{Type: protocol.RPCTypePromptCompleted, RequestID: promptID, Status: protocol.RPCPromptCanceledStatus})
		case "queue_list":
			res.Data = q.Clone()
		default:
			res.Success = false
		}
		emit(res)
	}
	return 0
}
