package subagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type managerTool struct {
	name    string
	manager *Manager
	caller  Caller
}

func Tools(m *Manager, caller Caller) []tools.Tool {
	names := []string{"spawn_agent", "list_subagent_models", "send_message", "followup_task", "wait_agent", "interrupt_agent", "close_agent", "resume_agent", "list_agents"}
	out := make([]tools.Tool, 0, len(names))
	for _, name := range names {
		out = append(out, &managerTool{name: name, manager: m, caller: caller})
	}
	return out
}

func (t *managerTool) Schema() protocol.ToolSchema { return toolSchemas[t.name] }
func (t *managerTool) Run(ctx context.Context, raw json.RawMessage, _ tools.ToolHost) (tools.ToolResult, error) {
	if t.manager == nil {
		return tools.ErrorResult(ErrNotReady), nil
	}
	switch t.name {
	case "spawn_agent":
		var in protocol.SpawnSubagentRequest
		if err := decodeStrict(raw, &in); err != nil {
			return tools.ErrorResult(err), nil
		}
		// Model providers sometimes emit readable names with hyphens. Keep
		// persisted identities canonical without relaxing the manager/SDK API.
		in.Name = strings.ReplaceAll(in.Name, "-", "_")
		state, err := t.manager.Spawn(ctx, t.caller, in)
		if err != nil {
			return tools.ErrorResult(err), nil
		}
		return jsonResult(map[string]any{"name": state.Agent.Path, "status": state.Status})
	case "list_subagent_models":
		var in struct {
			Provider string `json:"provider,omitempty"`
		}
		if err := decodeStrict(raw, &in); err != nil {
			return tools.ErrorResult(err), nil
		}
		type item struct {
			Provider        string                   `json:"provider"`
			Model           string                   `json:"model"`
			DisplayName     string                   `json:"display_name,omitempty"`
			Tools           bool                     `json:"supports_tools"`
			ThinkingLevels  []protocol.ThinkingLevel `json:"thinking_levels"`
			DefaultThinking protocol.ThinkingLevel   `json:"default_thinking,omitempty"`
		}
		models, catalogErr := t.manager.Models(ctx)
		out := make([]item, 0, len(models))
		providers := make([]string, 0)
		seenProviders := make(map[string]bool)
		for _, model := range models {
			if !seenProviders[model.Provider] {
				seenProviders[model.Provider] = true
				providers = append(providers, model.Provider)
			}
			if in.Provider != "" && model.Provider != in.Provider {
				continue
			}
			out = append(out, item{Provider: model.Provider, Model: model.ID, DisplayName: model.DisplayName, Tools: model.SupportsTools, ThinkingLevels: model.SupportedThinkingLevels(), DefaultThinking: model.DefaultThinking})
		}
		result := map[string]any{"models": out, "available_providers": providers}
		if catalogErr != nil {
			result["warning"] = catalogErr.Error()
		}
		if in.Provider != "" && len(out) == 0 {
			result["message"] = fmt.Sprintf("no models found for exact provider %q; use one of available_providers", in.Provider)
		}
		return jsonResult(result)
	case "send_message":
		var in struct {
			Target  string `json:"target"`
			Message string `json:"message"`
		}
		if err := decodeStrict(raw, &in); err != nil {
			return tools.ErrorResult(err), nil
		}
		if err := t.manager.SendMessage(ctx, t.caller, in.Target, in.Message); err != nil {
			return tools.ErrorResult(err), nil
		}
		return jsonResult(map[string]any{})
	case "followup_task":
		var in struct {
			Target  string `json:"target"`
			Message string `json:"message"`
		}
		if err := decodeStrict(raw, &in); err != nil {
			return tools.ErrorResult(err), nil
		}
		if err := t.manager.Followup(ctx, t.caller, in.Target, in.Message); err != nil {
			return tools.ErrorResult(err), nil
		}
		return jsonResult(map[string]any{})
	case "wait_agent":
		var in struct {
			TimeoutMS int    `json:"timeout_ms,omitempty"`
			Until     string `json:"until,omitempty"`
		}
		if err := decodeStrict(raw, &in); err != nil {
			return tools.ErrorResult(err), nil
		}
		timeout, err := ParseWaitTimeoutMS(in.TimeoutMS)
		if err != nil {
			return tools.ErrorResult(err), nil
		}
		var res protocol.WaitSubagentsResult
		switch in.Until {
		case "", "activity":
			res, err = t.manager.Wait(ctx, t.caller, timeout)
		case "all":
			res, err = t.manager.WaitUntilAll(ctx, t.caller, timeout)
		default:
			return tools.ErrorResult(fmt.Errorf("invalid wait mode %q (use activity or all)", in.Until)), nil
		}
		if err != nil {
			return tools.ErrorResult(err), nil
		}
		return jsonResult(res)
	case "interrupt_agent":
		var in struct {
			Target string `json:"target"`
		}
		if err := decodeStrict(raw, &in); err != nil {
			return tools.ErrorResult(err), nil
		}
		previous, err := t.manager.Interrupt(ctx, t.caller, in.Target)
		if err != nil {
			return tools.ErrorResult(err), nil
		}
		return jsonResult(map[string]any{"previous_status": previous})
	case "close_agent":
		var in struct {
			Target string `json:"target"`
		}
		if err := decodeStrict(raw, &in); err != nil {
			return tools.ErrorResult(err), nil
		}
		previous, err := t.manager.CloseAgent(ctx, t.caller, in.Target)
		if err != nil {
			return tools.ErrorResult(err), nil
		}
		return jsonResult(map[string]any{"previous_status": previous, "status": protocol.AgentClosed})
	case "resume_agent":
		var in struct {
			Target string `json:"target"`
		}
		if err := decodeStrict(raw, &in); err != nil {
			return tools.ErrorResult(err), nil
		}
		state, err := t.manager.ResumeAgent(ctx, t.caller, in.Target)
		if err != nil {
			return tools.ErrorResult(err), nil
		}
		return jsonResult(map[string]any{"status": state.Status})
	case "list_agents":
		var in struct {
			PathPrefix string `json:"path_prefix,omitempty"`
		}
		if err := decodeStrict(raw, &in); err != nil {
			return tools.ErrorResult(err), nil
		}
		list, err := t.manager.List(ctx, t.caller, in.PathPrefix)
		if err != nil {
			return tools.ErrorResult(err), nil
		}
		type item struct {
			AgentName   protocol.AgentPath   `json:"agent_name"`
			AgentStatus protocol.AgentStatus `json:"agent_status"`
			Role        string               `json:"role,omitempty"`
		}
		out := struct {
			Agents          []item `json:"agents"`
			Running         int    `json:"running"`
			Queued          int    `json:"queued"`
			Terminal        int    `json:"terminal"`
			Open            int    `json:"open"`
			Closed          int    `json:"closed"`
			ConcurrentLimit int    `json:"concurrent_limit"`
			AgentLimit      int    `json:"agent_limit"`
			Truncated       bool   `json:"truncated,omitempty"`
		}{Running: list.Running, Queued: list.Queued, Terminal: list.Terminal, Open: list.Open, Closed: list.Closed, ConcurrentLimit: list.ConcurrentLimit, AgentLimit: list.AgentLimit, Truncated: list.Truncated}
		for _, s := range list.Agents {
			out.Agents = append(out.Agents, item{AgentName: s.Agent.Path, AgentStatus: s.Status, Role: s.Agent.Role})
		}
		return jsonResult(out)
	}
	return tools.ErrorResult(errors.New("unknown subagent tool")), nil
}

func decodeStrict(raw json.RawMessage, out any) error {
	if len(raw) == 0 {
		raw = []byte(`{}`)
	}
	var err error
	raw, err = unwrapRawArguments(raw)
	if err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return errors.New("invalid arguments: expected a JSON object")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("invalid trailing arguments")
		}
		return fmt.Errorf("invalid trailing arguments: %w", err)
	}
	return nil
}

// unwrapRawArguments accepts the sole-field compatibility envelope used by
// some provider tool-call parsers. The contained value must still be one valid
// JSON object and is subsequently decoded with the ordinary strict schema.
func unwrapRawArguments(raw json.RawMessage) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		// Let the strict decoder report ordinary malformed or trailing JSON.
		return raw, nil
	}
	encoded, ok := fields["_raw"]
	if !ok {
		return raw, nil
	}
	if len(fields) != 1 {
		return nil, errors.New(`compatibility field "_raw" must be the only argument`)
	}
	var inner string
	if err := json.Unmarshal(encoded, &inner); err != nil {
		return nil, errors.New(`compatibility field "_raw" must contain a JSON string`)
	}
	inner = strings.TrimSpace(inner)
	if !json.Valid([]byte(inner)) {
		return nil, errors.New(`compatibility field "_raw" contains malformed JSON`)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(inner), &object); err != nil || object == nil || !strings.HasPrefix(inner, "{") {
		return nil, errors.New(`compatibility field "_raw" must contain a JSON object`)
	}
	return json.RawMessage(inner), nil
}

func jsonResult(v any) (tools.ToolResult, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return tools.ToolResult{}, err
	}
	return tools.TextResult(string(raw)), nil
}

var toolSchemas = map[string]protocol.ToolSchema{
	"spawn_agent":          {Name: "spawn_agent", Description: "Start an independent bounded subagent task. The name becomes a canonical /root/... path; hyphens normalize to underscores. Use list_subagent_models for exact provider/model IDs and fork_turns=none for self-contained exploration. The general role can run permission-gated bash for investigation, explorer is read-only, and implementer is shell-capable but write/edit require explicit global and role mutation opt-in.", Parameters: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"One lowercase agent name. It becomes a canonical /root/... agent path; hyphens normalize to underscores.","minLength":1,"maxLength":64,"pattern":"^[a-z][a-z0-9_-]{0,63}$"},"task":{"type":"string","description":"The task for the new agent."},"role":{"type":"string","description":"Optional role: general is shell-capable, explorer is read-only, and implementer is shell-capable with file mutation only when explicitly enabled. Omit to use the configured default role."},"fork_turns":{"type":"string","description":"Parent context to copy: none, all, or a positive integer string.","pattern":"^(none|all|[1-9][0-9]*)$"},"provider":{"type":"string","description":"Optional provider override. Use list_subagent_models for exact IDs."},"model":{"type":"string","description":"Optional model override. Omit to use the role model, configured subagent default model, or provider/parent default."},"reasoning_effort":{"type":"string","enum":["off","minimal","low","medium","high"]}},"required":["name","task"],"additionalProperties":false}`)},
	"list_subagent_models": {Name: "list_subagent_models", Description: "List exact provider/model pairs available for spawning subagents. Call this before selecting a different provider or when the user names a model family rather than an exact ID.", Parameters: json.RawMessage(`{"type":"object","properties":{"provider":{"type":"string","description":"Optional exact provider ID filter."}},"additionalProperties":false}`)},
	"send_message":         {Name: "send_message", Description: "Queue an attributed message to an existing agent without starting a turn.", Parameters: json.RawMessage(`{"type":"object","properties":{"target":{"type":"string"},"message":{"type":"string"}},"required":["target","message"],"additionalProperties":false}`)},
	"followup_task":        {Name: "followup_task", Description: "Queue an attributed task and run or reuse the target when it is idle.", Parameters: json.RawMessage(`{"type":"object","properties":{"target":{"type":"string"},"message":{"type":"string"}},"required":["target","message"],"additionalProperties":false}`)},
	"wait_agent":           {Name: "wait_agent", Description: "Wait for subagents. Use until=all whenever the answer depends on outstanding child work; activity returns after the next mailbox or lifecycle event. Result content arrives through attributed mailbox messages.", Parameters: json.RawMessage(`{"type":"object","properties":{"timeout_ms":{"type":"integer","minimum":0},"until":{"type":"string","enum":["activity","all"],"description":"activity waits for one event; all waits until every descendant is terminal or the timeout expires"}},"additionalProperties":false}`)},
	"interrupt_agent":      {Name: "interrupt_agent", Description: "Interrupt a target's current turn while keeping its identity reusable.", Parameters: json.RawMessage(`{"type":"object","properties":{"target":{"type":"string"}},"required":["target"],"additionalProperties":false}`)},
	"close_agent":          {Name: "close_agent", Description: "Close a terminal agent to release its open-agent slot while preserving its stable identity and history for later resume.", Parameters: json.RawMessage(`{"type":"object","properties":{"target":{"type":"string"}},"required":["target"],"additionalProperties":false}`)},
	"resume_agent":         {Name: "resume_agent", Description: "Reopen a closed agent without starting a turn. This consumes an open-agent slot; use followup_task to reopen and run it in one operation.", Parameters: json.RawMessage(`{"type":"object","properties":{"target":{"type":"string"}},"required":["target"],"additionalProperties":false}`)},
	"list_agents":          {Name: "list_agents", Description: "List stable agent paths and lifecycle states without private task content, including closed agents.", Parameters: json.RawMessage(`{"type":"object","properties":{"path_prefix":{"type":"string"}},"additionalProperties":false}`)},
}

func ToolDescriptor(t tools.Tool) tools.ToolDescriptor {
	risk := permission.RiskRead
	if t.Schema().Name == "spawn_agent" || t.Schema().Name == "followup_task" || t.Schema().Name == "resume_agent" {
		risk = permission.RiskDelegate
	}
	return tools.ToolDescriptor{Schema: t.Schema(), Tool: t, Source: tools.SourceBuiltin, Owner: "subagents", Risk: risk}
}
