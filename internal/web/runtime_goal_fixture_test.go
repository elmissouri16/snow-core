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
	if os.Getenv("SNOW_WEB_GOAL_TEST_CHILD") == "1" {
		os.Exit(runtimeGoalFixture())
	}
}

func runtimeGoalFixture() int {
	mode := os.Getenv("SNOW_WEB_RUNTIME_TEST_MODE")
	logPath := os.Getenv("SNOW_WEB_RUNTIME_TEST_LOG")
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return 1
	}
	defer log.Close()
	emit := func(value any) { data, _ := json.Marshal(value); fmt.Println(string(data)) }
	ready := protocol.NewRPCReady("goal-fixture")
	ready.Capabilities = slices.DeleteFunc(ready.Capabilities, func(c string) bool { return c == "usage" || c == "context_report" || c == "model_discovery" })
	if !slices.Contains(ready.Capabilities, "goal_run") {
		ready.Capabilities = append(ready.Capabilities, "goal_run")
	}
	emit(ready)
	cwd, _ := os.Getwd()
	g := &protocol.ThreadGoal{SessionID: "session", BranchID: "main", GoalID: "goal", Objective: "saved objective", Status: protocol.GoalActive, TokenBudget: new(int64(1000))}
	deferred := true
	runID, requestID := "", ""
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
		case "messages_page":
			res.Data = protocol.RPCMessagesPage{Messages: []protocol.Message{}}
		case "goal_inspect":
			inspection := protocol.RPCGoalInspection{SessionID: "session", BranchID: "main", TipID: "tip", Goal: g.Clone(), Deferred: deferred, GoalRunID: runID}
			if runID != "" && strings.HasPrefix(mode, "goal-inspect-") {
				inspection.TipID = "live-tip"
				switch mode {
				case "goal-inspect-wrong-run":
					inspection.GoalRunID = "foreign-run"
				case "goal-inspect-wrong-branch":
					inspection.BranchID, inspection.Goal.BranchID = "foreign-branch", "foreign-branch"
				case "goal-inspect-wrong-session":
					inspection.SessionID, inspection.Goal.SessionID = "foreign-session", "foreign-session"
				case "goal-inspect-wrong-goal":
					inspection.Goal.GoalID = "foreign-goal"
				case "goal-inspect-delayed-update", "goal-inspect-delayed-completion":
					// Freeze the read result before delivering a newer update.
					// The parent releases this reply only after observing that
					// event, making the revision-fencing race deterministic.
					inspection.TipID = "stale-read-tip"
					g = g.Clone()
					g.TokensUsed = 80
					g.Objective = "newer authoritative goal"
					emit(protocol.AgentEvent{Type: protocol.EvThreadGoalUpdated, GoalRunID: runID, RootEpoch: 1, ThreadGoal: &protocol.ThreadGoalUpdate{Goal: g}})
					if mode == "goal-inspect-delayed-completion" {
						g.Status = protocol.GoalComplete
						emit(protocol.RPCGoalRunCompleted{Type: protocol.RPCTypeGoalRunCompleted, RequestID: requestID, GoalRunID: runID, GoalID: g.GoalID, Status: "finished", GoalStatus: g.Status})
						runID = ""
					}
					if !wait(".inspect") {
						return 6
					}
				}
			}
			res.Data = inspection
		case "goal_run":
			if mode == "goal-lost-rpc-ack" {
				return 0
			}
			var p protocol.RPCGoalRunParams
			if json.Unmarshal(req.Params, &p) != nil || p.SessionID != "session" || p.BranchID != "main" || p.ExpectedTipID != "tip" || p.ExpectedGoalID != "goal" {
				res.Success = false
				res.ErrorCode = "goal_run_rejected"
				break
			}
			runID, requestID = "run", req.ID
			deferred = false
			emit(protocol.AgentEvent{Type: protocol.EvThreadGoalUpdated, GoalRunID: runID, RootEpoch: 1, ThreadGoal: &protocol.ThreadGoalUpdate{Goal: g}})
			emit(protocol.AgentEvent{Type: protocol.EvTextDelta, GoalRunID: runID, RootEpoch: 1, TurnSequence: 1, TurnID: "turn-one", Text: "first answer"})
			emit(protocol.AgentEvent{Type: protocol.EvTurnDone, GoalRunID: runID, RootEpoch: 1, TurnSequence: 1, TurnID: "turn-one"})
			if mode == "goal-completed-before-ack" {
				g = g.Clone()
				g.Status = protocol.GoalComplete
				emit(protocol.RPCGoalRunCompleted{Type: protocol.RPCTypeGoalRunCompleted, RequestID: requestID, GoalRunID: runID, GoalID: g.GoalID, Status: "finished", GoalStatus: g.Status})
				runID = ""
			}
			if strings.Contains(mode, "gated") && !wait(".ack") {
				return 3
			}
			res.Data = protocol.RPCGoalRunAccepted{GoalRunID: "run", GoalID: g.GoalID, SessionID: g.SessionID, BranchID: g.BranchID}
		case "abort":
			deferred = true
			if runID != "" {
				emit(protocol.RPCGoalRunCompleted{Type: protocol.RPCTypeGoalRunCompleted, RequestID: requestID, GoalRunID: runID, GoalID: g.GoalID, Status: "canceled", GoalStatus: g.Status})
				runID = ""
				if strings.Contains(mode, "gated") && !wait(".abort") {
					return 4
				}
			}
		default:
			return 5
		}
		emit(res)
	}
	return 0
}
