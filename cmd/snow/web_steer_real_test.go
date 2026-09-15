//go:build darwin || linux

package main

import (
	"encoding/json/v2"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/web"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// Reuse the private-HOME HTTP fixture and its real production manager worker
// arguments -> buildOptions -> App/RPC/Agent path. Only provider responses are
// fictional and gated; no steering events, admission results, or roots are faked.
func steerRealStart(t *testing.T) (*managerExecutionHTTP, web.RuntimeSnapshot) {
	t.Helper()
	f := newManagerExecutionHTTP(t, "")
	s := f.open("")
	f.runtime("prompt", url.Values{"instance_id": {s.InstanceID}, "text": {"fictional original request"}}, nil)
	s = f.wait(func(s web.RuntimeSnapshot) bool {
		return f.count() == 1 && s.Status == "running" && s.Steer != nil && s.Steer.CanSteer && s.Queue != nil && s.Queue.CanEnqueue
	})
	return f, s
}

func steerRealForm(s web.RuntimeSnapshot, request, text string) url.Values {
	return url.Values{"instance_id": {s.InstanceID}, "session_id": {s.SessionID}, "live_steer_token": {s.Steer.Token}, "steer_revision": {strconv.FormatUint(s.Steer.Revision, 10)}, "request_id": {request}, "text": {text}}
}
func steerRealQueueForm(s web.RuntimeSnapshot, text string) url.Values {
	return url.Values{"instance_id": {s.InstanceID}, "session_id": {s.SessionID}, "queue_token": {s.Queue.Token}, "queue_revision": {strconv.FormatUint(s.Queue.Revision, 10)}, "text": {text}}
}
func steerRealSubmit(f *managerExecutionHTTP, s web.RuntimeSnapshot, id, text string) web.RuntimeSnapshot {
	f.t.Helper()
	form := steerRealForm(s, id, text)
	beforeQueueRevision := s.Queue.Revision
	f.runtime("steer", form, &s)
	if s.SteerACK == nil || s.SteerACK.Status != "accepted" || s.SteerACK.RequestID != id || s.SteerACK.ItemID == "" || s.SteerACK.Token != form.Get("live_steer_token") {
		f.t.Fatalf("uncorrelated native acceptance: %+v", s.SteerACK)
	}
	// Native queue events and the HTTP ACK have independent waiters. Before
	// another explicit fixture action, observe the acceptance event's queue
	// revision, rather than racing a still-in-flight SSE projection. This is a
	// read-only barrier, never an admission retry or a fabricated ACK/event.
	f.wait(func(current web.RuntimeSnapshot) bool {
		return current.Queue != nil && current.Queue.Revision > beforeQueueRevision
	})
	return s
}
func steerRealReject(f *managerExecutionHTTP, action string, form url.Values) {
	f.t.Helper()
	before := f.count()
	code, _ := f.request("POST", "/projects/"+f.project+"/runtime/"+action, form)
	if code < 400 || f.count() != before {
		f.t.Fatalf("%s rejection reached provider or succeeded: HTTP %d", action, code)
	}
}
func steerRealUsers(s web.RuntimeSnapshot) []string {
	var users []string
	for _, m := range s.Messages {
		if m.Role == "user" {
			users = append(users, m.Text)
		}
	}
	return users
}
func steerRealContext(f *managerExecutionHTTP, call int, want []string) {
	f.t.Helper()
	var messages []protocol.Message
	// The reused provider increments its counter before writing context. Wait
	// for a complete JSON document rather than racing its in-progress write.
	f.wait(func(web.RuntimeSnapshot) bool {
		data, err := os.ReadFile(filepath.Join(f.directory, fmt.Sprintf("context-%d.json", call)))
		return err == nil && json.Unmarshal(data, &messages) == nil
	})
	var got []string
	for _, message := range messages {
		if message.Role == protocol.RoleUser {
			for _, block := range message.Content {
				if block.Type == protocol.BlockText {
					got = append(got, block.Text)
				}
			}
		}
	}
	if !slices.Equal(got, want) {
		f.t.Fatalf("provider context %d users=%q want=%q", call, got, want)
	}
}
func steerRealCompletedTurns(f *managerExecutionHTTP) []protocol.AgentEvent {
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
		if event.Type == protocol.EvTurnDone {
			turns = append(turns, event)
		}
	}
	return turns
}

func TestWebSteerRealWorkerLiteralSharedRootAndNoReplay(t *testing.T) {
	f, s := steerRealStart(t)
	literal := "/not-a-command $not-a-skill\r\n <fictional> exact literal steering"
	instance, session, cancel := s.InstanceID, s.SessionID, s.CancelToken
	s = steerRealSubmit(f, s, "literal-request", literal)
	item := s.SteerACK.ItemID
	if len(s.Steer.Items) != 1 || s.Steer.Items[0].Status != "accepted" || len(s.Queue.Items) != 0 || !slices.Equal(steerRealUsers(s), []string{"fictional original request"}) || f.count() != 1 {
		t.Fatal("acceptance fabricated delivery, a follow-up, or a provider request")
	}
	// Same request ID is never retried, even with refreshed admission authority.
	steerRealReject(f, "steer", steerRealForm(f.snapshot(), "literal-request", literal))
	for range 3 {
		f.snapshot()
	}
	if code, _ := f.request("GET", "/?project="+url.QueryEscape(f.project), nil); code != 200 {
		t.Fatal("page reload failed")
	}
	if f.count() != 1 {
		t.Fatal("read/reload replayed pending steering")
	}
	f.release(1, "first fictional answer")
	s = f.wait(func(s web.RuntimeSnapshot) bool {
		return f.count() == 2 && len(s.Steer.Items) == 1 && s.Steer.Items[0].Status == "delivered"
	})
	steerRealContext(f, 2, []string{"fictional original request", literal})
	if s.InstanceID != instance || s.SessionID != session || s.CancelToken != cancel || s.Steer.Items[0].ItemID != item || s.Steer.Items[0].Text != literal || len(s.Queue.Items) != 0 {
		t.Fatal("native steering changed root authority or lost exact durable identity")
	}
	f.release(2, "second fictional answer")
	s = f.wait(func(s web.RuntimeSnapshot) bool { return s.Status == "idle" })
	turns := steerRealCompletedTurns(f)
	if len(turns) != 1 || turns[0].TurnID == "" || turns[0].TurnOrigin != "user" || turns[0].RootEpoch == 0 {
		t.Fatalf("steering invented another root: %+v", turns)
	}
	public, err := json.Marshal(s)
	if err != nil || strings.Contains(string(public), turns[0].TurnID) {
		t.Fatal("private native root leaked into the browser projection")
	}
	// Transcript previews intentionally normalize carriage returns. Native
	// provider context and editable durable source must still remain exact.
	preview := strings.ReplaceAll(literal, "\r", "")
	if !slices.Equal(steerRealUsers(s), []string{"fictional original request", preview}) {
		t.Fatal("durable steering preview missing or duplicated")
	}
	f.runtime("close", url.Values{"instance_id": {s.InstanceID}}, nil)
	s = f.open(session)
	if s.InstanceID == instance || f.count() != 2 || !slices.Equal(steerRealUsers(s), []string{"fictional original request", preview}) {
		t.Fatal("session reopen replayed steering or lost durable history")
	}
	var prepared struct {
		Text string `json:"text"`
	}
	for _, message := range s.Messages {
		if message.Role == "user" && message.Text == preview {
			f.runtime("message-edit-prepare", url.Values{"instance_id": {s.InstanceID}, "message_id": {message.ID}}, &prepared)
		}
	}
	if prepared.Text != literal || f.count() != 2 {
		t.Fatal("durable steering source transformed or source inspection replayed it")
	}
}

func TestWebSteerRealWorkerDeliveredAfterProviderFailure(t *testing.T) {
	f, s := steerRealStart(t)
	f.runtime("queue-enqueue", steerRealQueueForm(s, "held follow-up, never retry"), &s)
	s = steerRealSubmit(f, s, "failure-steer", "native recovery steering")
	item := s.SteerACK.ItemID
	// Opening the gate without its response file makes this fake provider return
	// a real Chat error after native acceptance, not a fabricated terminal event.
	f.write("release-1", "release")
	s = f.wait(func(s web.RuntimeSnapshot) bool {
		return f.count() == 2 && s.Steer != nil && len(s.Steer.Items) == 1 && s.Steer.Items[0].Status == "delivered"
	})
	steerRealContext(f, 2, []string{"fictional original request", "native recovery steering"})
	if s.Steer.Items[0].ItemID != item || s.Steer.CanSteer || s.Queue == nil || len(s.Queue.Items) != 1 || s.Queue.Items[0].State != "held" {
		t.Fatalf("provider-failure boundary confused steering with Queue next: steer=%+v queue=%+v", s.Steer, s.Queue)
	}
	f.release(2, "fictional recovery completed")
	s = f.wait(func(s web.RuntimeSnapshot) bool { return s.Status == "idle" })
	for range 3 {
		f.snapshot()
	}
	if f.count() != 2 || s.Steer.Items[0].Status != "delivered" || len(steerRealCompletedTurns(f)) != 1 {
		t.Fatal("terminal projection regressed delivery or retried held work")
	}
}

func TestWebSteerRealWorkerSharedCapacityBothDirections(t *testing.T) {
	for _, steerFirst := range []bool{false, true} {
		for _, byteLimit := range []bool{false, true} {
			t.Run(fmt.Sprintf("steer-first=%t/bytes=%t", steerFirst, byteLimit), func(t *testing.T) {
				f, s := steerRealStart(t)
				count, text := protocol.RPCQueueMaxItems, "fictional bounded input"
				if byteLimit {
					count, text = protocol.RPCQueueMaxTotalBytes/protocol.RPCQueueMaxTextBytes, strings.Repeat("x", protocol.RPCQueueMaxTextBytes)
				}
				for i := range count {
					s = f.snapshot()
					if steerFirst {
						s = steerRealSubmit(f, s, fmt.Sprintf("capacity-%d", i), text)
					} else {
						f.runtime("queue-enqueue", steerRealQueueForm(s, text), &s)
					}
				}
				if s.Queue.CanEnqueue || s.Steer.CanSteer {
					t.Errorf("full native capacity still grants projected admission: queue=%t steer=%t", s.Queue.CanEnqueue, s.Steer.CanSteer)
				}
				if steerFirst {
					steerRealReject(f, "queue-enqueue", steerRealQueueForm(s, "x"))
				} else {
					steerRealReject(f, "steer", steerRealForm(s, "beyond-capacity", "x"))
				}
				if f.count() != 1 || len(steerRealUsers(s)) != 1 {
					t.Fatal("capacity rejection delivered or replayed work")
				}
				f.stopGoal(s) // Same owned cancel endpoint for ordinary prompt roots.
			})
		}
	}
}

func TestWebSteerRealWorkerStopNextRootAndGoalGuard(t *testing.T) {
	f, s := steerRealStart(t)
	s = steerRealSubmit(f, s, "stopped-request", "never replay after Stop")
	old := s
	s = f.stopGoal(s)
	s = f.wait(func(s web.RuntimeSnapshot) bool { return s.Steer != nil && s.Steer.Items[0].Status == "discarded" })
	if s.Steer.CanSteer || f.count() != 1 || len(steerRealUsers(s)) != 1 {
		t.Fatal("Stop fabricated delivery or retained steering authority")
	}
	f.runtime("prompt", url.Values{"instance_id": {s.InstanceID}, "text": {"explicit next root"}}, nil)
	s = f.wait(func(s web.RuntimeSnapshot) bool { return f.count() == 2 && s.Steer.CanSteer })
	if s.Steer.Token == old.Steer.Token || s.CancelToken == old.CancelToken {
		t.Fatal("new root reused retired authority")
	}
	steerRealReject(f, "steer", steerRealForm(old, "stale-root", "do not inject old steering"))
	steerRealContext(f, 2, []string{"fictional original request", "explicit next root"})
	s = f.stopGoal(s)
	s = f.inspect(s)
	s = f.startGoal(s)
	s = f.wait(func(s web.RuntimeSnapshot) bool { return f.count() == 3 && s.Goal != nil && s.Goal.Running })
	if s.Steer != nil && s.Steer.CanSteer {
		t.Fatal("explicit goal grants ordinary-root steering")
	}
	steerRealReject(f, "steer", steerRealForm(old, "no-goal-steering", "not a goal prompt"))
	if len(steerRealUsers(s)) != 2 {
		t.Fatal("goal/steer guard invented user input")
	}
	f.stopGoal(s)
}
