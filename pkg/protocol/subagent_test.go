package protocol

import (
	"encoding/json"
	"testing"
)

func TestAgentPathValidationAndResolution(t *testing.T) {
	valid := []AgentPath{"/root", "/root/api_review", "/root/api_review/tests_2"}
	for _, p := range valid {
		if err := p.Validate(); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
	}
	invalid := []AgentPath{"", "root", "/root/", "/root/Root", "/root/a-b", "/root/../x", "/other/x", "/root/root"}
	for _, p := range invalid {
		if err := p.Validate(); err == nil {
			t.Fatalf("accepted %q", p)
		}
	}
	p, err := ResolveAgentPath("/root/reviews", "api")
	if err != nil || p != "/root/reviews/api" {
		t.Fatalf("resolve=%q %v", p, err)
	}
	if _, err := ResolveAgentPath("/root", "../x"); err == nil {
		t.Fatal("escape accepted")
	}
	if parent, ok := AgentPath("/root/reviews/api").Parent(); !ok || parent != "/root/reviews" {
		t.Fatalf("parent=%q %v", parent, ok)
	}
}

func TestSubagentEventCloneIsolationAndJSON(t *testing.T) {
	u := &Usage{Input: 1, Cost: &Cost{Total: 2}}
	ev := AgentEvent{Type: EvSubagentStatus, Agent: &AgentRef{ThreadID: "t", Path: "/root/a", ParentThreadID: "root", ParentPath: "/root", Depth: 1}, Subagent: &SubagentState{Agent: AgentRef{ThreadID: "t", Path: "/root/a", ParentThreadID: "root", ParentPath: "/root", Depth: 1}, Status: AgentCompleted, Usage: u}, AgentMessage: &AgentMessage{ID: "m", Author: "/root/a", Recipient: "/root", Kind: AgentMessageFinal, Content: "done"}}
	clone := ev.Clone()
	clone.Subagent.Usage.Cost.Total = 9
	clone.AgentMessage.Content = "changed"
	clone.Agent.Path = "/root/b"
	if ev.Subagent.Usage.Cost.Total != 2 || ev.AgentMessage.Content != "done" || ev.Agent.Path != "/root/a" {
		t.Fatal("clone aliases source")
	}
	raw, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	var round AgentEvent
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Fatal(err)
	}
	if round.Agent.Path != "/root/a" || round.Subagent.Status != AgentCompleted {
		t.Fatalf("round=%+v", round)
	}
}
