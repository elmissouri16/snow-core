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
			TimeoutMS int    `json:"timeout_ms,omitzero"`
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
			Truncated       bool   `json:"truncated,omitzero"`
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
	"spawn_agent":          {Name: "spawn_agent", Description: "Start a bounded child-agent task with separate conversation context but shared working directory, filesystem, OS privileges, and process side effects. Give parallel mutators disjoint ownership. The name becomes a canonical /root/... path; hyphens normalize to underscores. Use list_subagent_models for exact provider/model IDs and supported reasoning levels before explicit overrides. Built-in role defaults are general, explorer, and implementer, but configuration may change available roles and tools. Role names are configured capability profiles, not task labels; put task-specific and output-style instructions in task.", Parameters: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"One lowercase agent name. It becomes a canonical /root/... agent path; hyphens normalize to underscores.","minLength":1,"maxLength":64,"pattern":"^[a-z][a-z0-9_-]{0,63}$"},"task":{"type":"string","description":"The task for the new agent."},"role":{"type":"string","description":"Optional configured capability role; omit to use the configured default. Task-specific behavior and output-style instructions belong in task, not role, unless that exact role is configured. Built-in defaults are general for permission-gated shell investigation, explorer for read-only investigation or review, and implementer for implementation with mutation only when globally and per-role enabled, but configuration may change roles and tools."},"fork_turns":{"type":"string","description":"Sanitized parent conversation to copy. Omit or use all for every sanitized turn, none for no parent conversation, or a positive integer string for the last N user turns and following messages. System/agent messages, thinking, and incomplete tool pairs are excluded.","pattern":"^(none|all|[1-9][0-9]*)$"},"provider":{"type":"string","description":"Optional provider override. Use list_subagent_models for exact IDs."},"model":{"type":"string","description":"Optional model override. Omit to use the role model, configured subagent default model, or provider/parent default."},"reasoning_effort":{"type":"string","description":"Optional explicit model reasoning level. Set only to a schema-accepted level advertised for the exact provider/model by list_subagent_models; otherwise omit it so Snow selects a supported fallback.","enum":["off","minimal","low","medium","high"]}},"required":["name","task"],"additionalProperties":false}`)},
	"list_subagent_models": {Name: "list_subagent_models", Description: "List exact provider/model pairs and their supported reasoning levels for spawning subagents. Call this before selecting a different provider/model, when the user names an inexact model family, or before setting an explicit reasoning_effort.", Parameters: json.RawMessage(`{"type":"object","properties":{"provider":{"type":"string","description":"Optional exact provider ID filter."}},"additionalProperties":false}`)},
	"send_message":         {Name: "send_message", Description: "Append an attributed mailbox message to an existing non-closed agent without starting a turn. Use followup_task when the message must trigger work.", Parameters: json.RawMessage(`{"type":"object","properties":{"target":{"type":"string","description":"Canonical agent path or unambiguous agent name."},"message":{"type":"string","description":"Message to append to the target mailbox."}},"required":["target","message"],"additionalProperties":false}`)},
	"followup_task":        {Name: "followup_task", Description: "Append an attributed task, automatically reopening a closed target. An idle target runs a mailbox turn; an active target may consume the task during its current turn, otherwise a follow-up turn runs when it becomes idle.", Parameters: json.RawMessage(`{"type":"object","properties":{"target":{"type":"string","description":"Canonical agent path or unambiguous agent name."},"message":{"type":"string","description":"Task to append and execute in the target agent."}},"required":["target","message"],"additionalProperties":false}`)},
	"wait_agent":           {Name: "wait_agent", Description: "Wait for subagents. Use until=all whenever the answer depends on outstanding child work; activity returns after the next mailbox or lifecycle event. Result content arrives through attributed mailbox messages.", Parameters: json.RawMessage(`{"type":"object","properties":{"timeout_ms":{"type":"integer","minimum":0,"description":"Bounded wait in milliseconds. Omit or use 0 for the configured default; positive values below the configured minimum are clamped upward, and values above the configured maximum fail."},"until":{"type":"string","enum":["activity","all"],"description":"activity waits for one event; all waits until every descendant is terminal or the timeout expires"}},"additionalProperties":false}`)},
	"interrupt_agent":      {Name: "interrupt_agent", Description: "Cancel a target's queued or running work while preserving its reusable identity. If no work is active or queued, no work is changed.", Parameters: json.RawMessage(`{"type":"object","properties":{"target":{"type":"string"}},"required":["target"],"additionalProperties":false}`)},
	"close_agent":          {Name: "close_agent", Description: "Close an idle terminal or not_loaded agent to release its open-agent slot. Active, queued, running, or finalizing work cannot be closed; stable identity and history remain available for later resume.", Parameters: json.RawMessage(`{"type":"object","properties":{"target":{"type":"string"}},"required":["target"],"additionalProperties":false}`)},
	"resume_agent":         {Name: "resume_agent", Description: "Reopen a closed agent without starting a turn. This consumes an open-agent slot; use followup_task to reopen and run it in one operation.", Parameters: json.RawMessage(`{"type":"object","properties":{"target":{"type":"string"}},"required":["target"],"additionalProperties":false}`)},
	"list_agents":          {Name: "list_agents", Description: "List stable agent paths and lifecycle states, including closed agents, without exposing private task content. Optionally restrict results to one canonical path prefix.", Parameters: json.RawMessage(`{"type":"object","properties":{"path_prefix":{"type":"string","description":"Optional canonical agent path prefix such as /root/review."}},"additionalProperties":false}`)},
}

func ToolDescriptor(t tools.Tool) tools.ToolDescriptor {
	risk := permission.RiskRead
	if t.Schema().Name == "spawn_agent" || t.Schema().Name == "followup_task" || t.Schema().Name == "resume_agent" {
		risk = permission.RiskDelegate
	}
	return tools.ToolDescriptor{Schema: t.Schema(), Tool: t, Source: tools.SourceBuiltin, Owner: "subagents", Risk: risk}
}
