package goal

import (
	"context"
	_ "embed"
	"encoding/json"

	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func (t *createTool) Run(_ context.Context, raw json.RawMessage, _ tools.ToolHost) (tools.ToolResult, error) {
	var a struct {
		Objective   string `json:"objective"`
		TokenBudget *int64 `json:"token_budget"`
	}
	if e := json.Unmarshal(raw, &a); e != nil {
		return tools.ErrorResult(e), nil
	}
	g, e := t.c.Create(a.Objective, a.TokenBudget, false)
	if e != nil {
		return tools.ErrorResult(e), nil
	}
	b, _ := json.Marshal(g)
	return tools.ToolResult{Content: []protocol.ContentBlock{protocol.NewTextBlock(string(b))}, Details: tools.PrivateDetails{}}, nil
}

func (*updateTool) Schema() protocol.ToolSchema {
	return protocol.ToolSchema{Name: "update_goal", Description: "Set goal status only to complete or blocked. Complete requires a full evidence audit. Blocked requires the same true external blocker for at least 3 consecutive goal turns; never use blocked for ordinary unfinished work.", Parameters: json.RawMessage(`{"type":"object","required":["goal_id","status"],"properties":{"goal_id":{"type":"string"},"status":{"type":"string","enum":["complete","blocked"]}},"additionalProperties":false}`)}
}

func (t *updateTool) Run(_ context.Context, raw json.RawMessage, _ tools.ToolHost) (tools.ToolResult, error) {
	var a struct {
		GoalID string                    `json:"goal_id"`
		Status protocol.ThreadGoalStatus `json:"status"`
	}
	if e := json.Unmarshal(raw, &a); e != nil {
		return tools.ErrorResult(e), nil
	}
	g, e := t.c.SetStatus(a.GoalID, a.Status, true)
	if e != nil {
		return tools.ErrorResult(e), nil
	}
	b, _ := json.Marshal(g)
	return tools.ToolResult{Content: []protocol.ContentBlock{protocol.NewTextBlock(string(b))}, Details: tools.PrivateDetails{}}, nil
}
