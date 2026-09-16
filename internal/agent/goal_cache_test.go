package agent

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/elmissouri16/snow-core/internal/provider/responsesapi"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestGoalInternalContextPreservesGrowingProviderPrefix(t *testing.T) {
	provider := &scriptedProvider{}
	agent, controller, store := goalAgent(t, provider)
	goal, err := controller.Create("finish the cache-sensitive objective", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	provider.scripts = [][]protocol.StreamEvent{
		{
			{Type: protocol.EvStreamToolCallDone, ToolCallID: "goal-1", ToolName: "get_goal", Arguments: []byte(`{}`)},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse},
		},
		{
			{Type: protocol.EvStreamToolCallDone, ToolCallID: "goal-2", ToolName: "get_goal", Arguments: []byte(`{}`)},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse},
		},
		{
			{Type: protocol.EvStreamToolCallDone, ToolCallID: "goal-3", ToolName: "update_goal", Arguments: []byte(`{"goal_id":"` + goal.GoalID + `","status":"complete"}`)},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse},
		},
		{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
	}

	if err := agent.internalTurn(t.Context(), false); err != nil {
		t.Fatal(err)
	}
	if len(provider.requests) != 4 {
		t.Fatalf("provider requests=%d, want 4", len(provider.requests))
	}
	if len(provider.requests[0].InternalContext) != 1 || provider.requests[0].InternalContext[0].Source != "goal" {
		t.Fatalf("first request goal context=%+v", provider.requests[0].InternalContext)
	}
	for i := 1; i < 3; i++ {
		if len(provider.requests[i].InternalContext) != 0 {
			t.Fatalf("request %d repeated unchanged goal steering: %+v", i+1, provider.requests[i].InternalContext)
		}
	}
	inputs := make([][]json.RawMessage, 3)
	for i := range inputs {
		inputs[i] = responseInputItems(t, provider.requests[i])
	}
	for i := 1; i < len(inputs); i++ {
		prior := inputs[i-1]
		current := inputs[i]
		if len(current) <= len(prior) {
			t.Fatalf("request %d input items=%d, prior=%d", i+1, len(current), len(prior))
		}
		for item := range prior {
			if !bytes.Equal(prior[item], current[item]) {
				t.Fatalf("request %d stopped extending the prior exact prefix at item %d\nprior: %s\nnext:  %s", i+1, item, prior[item], current[item])
			}
		}
	}

	public, err := store.Messages()
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range public {
		if message.Role == protocol.RoleInternal {
			t.Fatalf("provider-only steering reached ordinary history: %+v", message)
		}
	}
	context, err := store.ContextMessages()
	if err != nil {
		t.Fatal(err)
	}
	internal := 0
	for _, message := range context {
		if message.Role == protocol.RoleInternal {
			internal++
		}
	}
	if internal != 1 {
		t.Fatalf("durable internal context messages=%d, want one per high-level goal turn", internal)
	}
}

func TestInternalContextPersistenceKeyKeepsGoalBudgetTransition(t *testing.T) {
	activeOne := protocol.InternalContextFragment{Source: "goal", Text: "Continue working on the thread goal below.\nToken budget remaining: 100."}
	activeTwo := protocol.InternalContextFragment{Source: "goal", Text: "Continue working on the thread goal below.\nToken budget remaining: 50."}
	budget := protocol.InternalContextFragment{Source: "goal", Text: "The goal token budget has been reached. Do not perform further substantive work."}
	if internalContextPersistenceKey(activeOne) != internalContextPersistenceKey(activeTwo) {
		t.Fatal("changing remaining-token text defeated within-turn goal steering reuse")
	}
	if internalContextPersistenceKey(activeOne) == internalContextPersistenceKey(budget) {
		t.Fatal("budget-wrap steering was suppressed with ordinary goal context")
	}
	pluginOne := protocol.InternalContextFragment{Source: "plugin-example", Text: "one"}
	pluginTwo := protocol.InternalContextFragment{Source: "plugin-example", Text: "two"}
	if !reusableInternalContext(pluginOne) || internalContextPersistenceKey(pluginOne) == internalContextPersistenceKey(pluginTwo) {
		t.Fatal("changed plugin steering was treated as an unchanged recurring fragment")
	}
	if reusableInternalContext(protocol.InternalContextFragment{Source: "loop-guard", Text: "repeat the reminder"}) || reusableInternalContext(protocol.InternalContextFragment{Source: "provider-recovery", Text: "recover"}) {
		t.Fatal("event-local recovery steering was incorrectly made reusable")
	}
}

func responseInputItems(t *testing.T, request protocol.ChatRequest) []json.RawMessage {
	t.Helper()
	wire, err := responsesapi.BuildRequest(request, responsesapi.RequestOptions{ProviderID: request.Model.Provider})
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Input []json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(wire, &body); err != nil {
		t.Fatal(err)
	}
	return body.Input
}
