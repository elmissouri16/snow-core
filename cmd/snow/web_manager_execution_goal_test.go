//go:build darwin || linux

package main

import (
	"encoding/json/v2"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/web"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func (f *managerExecutionHTTP) startGoal(s web.RuntimeSnapshot) web.RuntimeSnapshot {
	f.t.Helper()
	form := managerExecutionGoalForm(s)
	form.Set("objective", "Fictional objective: inventory three imaginary moons")
	form.Set("token_budget", "100000")
	f.runtime("goal-start", form, &s)
	if s.Goal == nil || s.Goal.GoalID == "" || s.Goal.GoalRunID == "" || s.CancelToken == "" {
		f.t.Fatalf("accepted run lacks owned authority: %+v", s.Goal)
	}
	f.write("selected-goal", s.Goal.GoalID)
	return s
}
func (f *managerExecutionHTTP) stopGoal(s web.RuntimeSnapshot) web.RuntimeSnapshot {
	f.t.Helper()
	f.runtime("cancel", url.Values{"instance_id": {s.InstanceID}, "cancel_token": {s.CancelToken}}, nil)
	return f.wait(func(s web.RuntimeSnapshot) bool {
		return s.Status == "idle" && !s.CancelRequested && s.CancelToken == ""
	})
}
func (f *managerExecutionHTTP) rejectGoal(action string, values url.Values) {
	f.t.Helper()
	before := f.count()
	code, body := f.request("POST", "/projects/"+f.project+"/runtime/"+action, values)
	if code < 400 {
		f.t.Fatalf("%s accepted stale/malformed authority: %s", action, body)
	}
	if f.count() != before {
		f.t.Fatal("rejected goal reached provider")
	}
}
func (f *managerExecutionHTTP) nativeTurns() []protocol.AgentEvent {
	f.t.Helper()
	data, err := os.ReadFile(filepath.Join(f.directory, "native-events.jsonl"))
	if err != nil {
		f.t.Fatal(err)
	}
	var turns []protocol.AgentEvent
	for line := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
		var event protocol.AgentEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			f.t.Fatal(err)
		}
		if event.Type == protocol.EvTurnDone && event.TurnOrigin == "goal" {
			turns = append(turns, event)
		}
	}
	return turns
}

// Actual web HTTP -> production manager -> private test-binary RPC child ->
// production App/Agent native goal turns. No prompt-shaped RPC goal surrogate.
func TestWebManagerGoalRunRealWorker(t *testing.T) {
	f := newManagerExecutionHTTP(t, "")
	s := f.open("")
	s = f.inspect(s)
	if s.Goal == nil || s.Goal.GoalID != "" || s.Goal.Status != "none" || f.count() != 0 {
		t.Fatal("inspection created work instead of proving absence")
	}
	absence := managerExecutionGoalForm(s)
	bad := absence.Clone()
	bad.Set("objective", "fictional")
	bad.Set("text", "composer draft must not become goal input")
	f.rejectGoal("goal-start", bad)
	bad = absence.Clone()
	bad.Set("objective", "fictional")
	bad.Set("expected_goal_id", "stale-fictional-goal")
	f.rejectGoal("goal-start", bad)
	s = f.startGoal(s)
	run, goal, stop := s.Goal.GoalRunID, s.Goal.GoalID, s.CancelToken
	// Three completed native turns plus a fourth held provider call prove this is
	// a serial run, not the ordinary prompt's one-turn lifecycle.
	for call := 1; call <= 3; call++ {
		f.wait(func(s web.RuntimeSnapshot) bool { return f.count() == call && s.Status == "running" })
		f.release(call, fmt.Sprintf("distinct imaginary moon %d", call))
		s = f.wait(func(s web.RuntimeSnapshot) bool { return f.count() == call+1 && s.Status == "running" })
		if s.Goal == nil || s.Goal.GoalRunID != run || s.Goal.GoalID != goal || s.CancelToken != stop {
			t.Fatal("native turn rotated run/Stop authority")
		}
	}
	turns := f.nativeTurns()
	if len(turns) != 3 {
		t.Fatalf("native completions=%d want 3", len(turns))
	}
	ids := map[string]bool{}
	for _, event := range turns {
		if event.TurnID == "" || ids[event.TurnID] || event.GoalRunID != run {
			t.Fatalf("native serial identity: %+v", event)
		}
		ids[event.TurnID] = true
	}
	// A read and a page reload must not replay or synthesize provider input.
	s = f.inspect(s)
	code, _ := f.request("GET", "/?project="+url.QueryEscape(f.project), nil)
	if code != 200 {
		t.Fatalf("page reload: %d", code)
	}
	if f.count() != 4 {
		t.Fatal("read replayed execution")
	}
	for _, m := range s.Messages {
		if m.Role == "user" {
			t.Fatalf("explicit goal invented composer/user prompt: %q", m.Text)
		}
	}
	s = f.stopGoal(s)
	s = f.inspect(s)
	if s.Goal.GoalID != goal || s.Goal.Status == "complete" || !s.Goal.Deferred || s.Goal.Running {
		t.Fatalf("run cancellation confused with semantic completion: %+v", s.Goal)
	}
	if f.count() != 4 {
		t.Fatal("Stop admitted another turn")
	}
	// Close/reopen the same durable session. No automatically replayed goal.
	sessionID, instance := s.SessionID, s.InstanceID
	f.runtime("close", url.Values{"instance_id": {instance}}, nil)
	s = f.open(sessionID)
	s = f.inspect(s)
	if s.InstanceID == instance || s.Goal.GoalID != goal || !s.Goal.Deferred || f.count() != 4 {
		t.Fatalf("reload resumed or lost deferred goal: %+v", s.Goal)
	}
	stale := managerExecutionGoalForm(s)
	stale.Set("expected_tip_id", "fictional-stale-tip")
	f.rejectGoal("goal-resume", stale)
	stale = managerExecutionGoalForm(s)
	stale.Set("expected_revision", "1")
	f.rejectGoal("goal-resume", stale)
	resume := managerExecutionGoalForm(s)
	f.runtime("goal-resume", resume, &s)
	if s.Goal.GoalRunID == run || s.Goal.GoalID != goal {
		t.Fatal("resume did not own a new run for selected goal")
	}
	f.wait(func(s web.RuntimeSnapshot) bool { return f.count() == 5 })
	f.release(5, "complete")
	s = f.wait(func(s web.RuntimeSnapshot) bool { return f.count() == 6 || s.Status == "permission" })
	if s.Status == "permission" {
		f.runtime("permission", url.Values{"instance_id": {s.InstanceID}, "request_id": {s.Permission.ID}, "decision": {"allow"}}, nil)
		f.wait(func(s web.RuntimeSnapshot) bool { return f.count() == 6 })
	}
	f.release(6, "selected goal completed")
	s = f.wait(func(s web.RuntimeSnapshot) bool { return s.Status == "idle" })
	s = f.inspect(s)
	if s.Goal.GoalID != goal || s.Goal.Status != "complete" || s.Goal.Running || s.CancelToken != "" {
		t.Fatalf("exact update_goal completion: %+v", s.Goal)
	}
	if s.Goal.TokenBudget == nil || *s.Goal.TokenBudget != 100000 {
		t.Fatal("resume changed reviewed budget")
	}
}

func TestWebManagerGoalRunStopBeforeResponseAndPermission(t *testing.T) {
	for _, phase := range []string{"first-response", "permission", "turn-gap"} {
		t.Run(phase, func(t *testing.T) {
			f := newManagerExecutionHTTP(t, "")
			s := f.inspect(f.open(""))
			if phase == "first-response" {
				form := managerExecutionGoalForm(s)
				form.Set("objective", "Fictional optional-budget goal")
				f.runtime("goal-start", form, &s)
				if s.Goal == nil || s.Goal.TokenBudget != nil || s.CancelToken == "" {
					t.Fatal("optional budget or owned Stop contract")
				}
			} else {
				s = f.startGoal(s)
			}
			if phase == "permission" {
				f.release(1, "process")
				s = f.wait(func(s web.RuntimeSnapshot) bool { return s.Status == "permission" })
				if s.Permission.Tool != "process_start" {
					t.Fatal("not real process permission")
				}
			}
			if phase == "turn-gap" {
				f.release(1, "one imaginary moon")
				// Native terminal event precedes automaticTurnDelay. Cancel the run using
				// its stable token, not the just-completed inner turn ID.
				deadline := time.Now().Add(3 * time.Second)
				for {
					data, _ := os.ReadFile(filepath.Join(f.directory, "native-events.jsonl"))
					if strings.Contains(string(data), `"type":"turn_done"`) {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("native gap deadline")
					}
					time.Sleep(time.Millisecond)
				}
			}
			s = f.stopGoal(s)
			s = f.inspect(s)
			if s.Goal.Status == "complete" || !s.Goal.Deferred || s.Goal.Running || s.Permission != nil {
				t.Fatalf("stop %s: %+v", phase, s.Goal)
			}
			if phase == "permission" {
				var inventory struct {
					Result protocol.RPCProcessControlList `json:"result"`
				}
				f.process("list", s, "", nil, &inventory)
				if len(inventory.Result.Processes) != 0 {
					t.Fatal("canceled permission launched process")
				}
			}
		})
	}
}

func TestWebManagerGoalRunRejectsPlanMalformedAndOrdinaryCreation(t *testing.T) {
	f := newManagerExecutionHTTP(t, "")
	s := f.inspect(f.open(""))
	form := managerExecutionGoalForm(s)
	form.Set("objective", "Fictional objective")
	for _, budget := range []string{"0", "-1", "01", "+1", "9223372036854775808"} {
		bad := form.Clone()
		bad.Set("token_budget", budget)
		f.rejectGoal("goal-start", bad)
	}
	for _, key := range []string{"instance_id", "session_id", "branch_id", "expected_tip_id", "expected_goal_id", "expected_revision", "objective"} {
		bad := form.Clone()
		bad.Del(key)
		f.rejectGoal("goal-start", bad)
		bad = form.Clone()
		bad.Add(key, bad.Get(key))
		f.rejectGoal("goal-start", bad)
	}
	s = f.mode(s, "plan")
	s = f.inspect(s)
	planForm := managerExecutionGoalForm(s)
	planForm.Set("objective", "Fictional objective rejected by current Plan policy")
	f.rejectGoal("goal-start", planForm)
	s = f.inspect(s)
	if s.Goal.GoalID != "" {
		t.Fatal("Plan rejection created a goal")
	}
	s = f.mode(s, "default")
	// Even explicit Permission Allow is not a bypass of the managed goal policy.
	s = f.permissionMode(s, "allow")
	f.runtime("prompt", url.Values{"instance_id": {s.InstanceID}, "text": {"Fictional ordinary prompt; no authorization for autonomous goals"}}, nil)
	f.release(1, "create")
	f.wait(func(s web.RuntimeSnapshot) bool { return f.count() == 2 })
	f.release(2, "ordinary denied goal attempt acknowledged")
	s = f.wait(func(s web.RuntimeSnapshot) bool { return s.Status == "idle" })
	s = f.inspect(s)
	if s.Goal.GoalID != "" || f.count() != 2 {
		t.Fatal("ordinary provider create_goal spawned invisible goal work")
	}
	data, err := os.ReadFile(filepath.Join(f.directory, "context-2.json"))
	if err != nil {
		t.Fatal(err)
	}
	var messages []protocol.Message
	if err := json.Unmarshal(data, &messages); err != nil {
		t.Fatal(err)
	}
	denied := false
	for _, m := range messages {
		if m.Role == protocol.RoleTool && m.ToolCallID == "unsolicited-goal" && m.IsError {
			denied = true
		}
	}
	if !denied {
		t.Fatal("create_goal denial absent from actual provider continuation")
	}
}

func TestWebManagerGoalRunRejectsHeldQueueBeforeEffects(t *testing.T) {
	f := newManagerExecutionHTTP(t, "")
	s := f.inspect(f.open(""))
	f.runtime("prompt", url.Values{"instance_id": {s.InstanceID}, "text": {"Fictional ordinary held request"}}, nil)
	s = f.wait(func(s web.RuntimeSnapshot) bool { return f.count() == 1 && s.Queue != nil && s.Queue.CanEnqueue })
	enqueue := url.Values{"instance_id": {s.InstanceID}, "session_id": {s.SessionID}, "queue_token": {s.Queue.Token}, "queue_revision": {fmt.Sprint(s.Queue.Revision)}, "text": {"Fictional queued request requiring review"}}
	f.runtime("queue-enqueue", enqueue, &s)
	s = f.stopGoal(s)
	s = f.inspect(s)
	if s.Queue == nil || len(s.Queue.Items) != 1 {
		t.Fatal("Stop lost queued review")
	}
	before := f.count()
	form := managerExecutionGoalForm(s)
	form.Set("objective", "Fictional goal must not consume queued work")
	f.rejectGoal("goal-start", form)
	after := f.inspect(s)
	if after.Goal.GoalID != "" || f.count() != before || len(after.Queue.Items) != 1 {
		t.Fatal("queue rejection changed goal, provider, or pending work")
	}
}
