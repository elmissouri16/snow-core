package app

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func (h *appExtensionHost) callControl(ctx context.Context, inv plugin.Invocation, operation string, raw json.RawMessage) (any, error) {
	var arg struct {
		Text      string                 `json:"text"`
		ID        string                 `json:"id"`
		Name      string                 `json:"name"`
		Target    string                 `json:"target"`
		Provider  string                 `json:"provider"`
		Model     string                 `json:"model"`
		Thinking  protocol.ThinkingLevel `json:"thinking"`
		Objective string                 `json:"objective"`
		Budget    *int64                 `json:"budget"`
		Replace   bool                   `json:"replace"`
		Limit     int                    `json:"limit"`
		Offset    int                    `json:"offset"`
		TimeoutMS int64                  `json:"timeoutMS"`
		Until     string                 `json:"until"`
		Arguments json.RawMessage        `json:"arguments"`
	}
	if err := jsonv2.Unmarshal(raw, &arg); err != nil {
		return nil, err
	}
	a, ag := h.app, h.agent
	switch operation {
	case "agent.state":
		usage, err := ag.Usage()
		if err != nil {
			return nil, err
		}
		return map[string]any{"running": ag.IsRunning(), "sessionId": h.store.ID(), "model": ag.Model(), "mode": ag.Mode(), "thinking": ag.Thinking(), "usage": usage}, nil
	case "agent.pending":
		return ag.PendingInputs(), nil
	case "agent.prompt":
		return nil, ag.Prompt(ctx, arg.Text)
	case "agent.steer":
		return nil, ag.Steer(arg.Text)
	case "agent.followUp":
		return nil, ag.FollowUp(arg.Text)
	case "agent.abort":
		return nil, ag.AbortContext(ctx)
	case "models.list":
		provider, model, models := a.ActiveModelsSnapshot()
		return map[string]any{"provider": provider, "model": model, "models": models}, nil
	case "models.set":
		provider, current, models := a.ActiveModelsSnapshot()
		if arg.Provider != "" {
			provider = arg.Provider
			models = a.SubagentModels()
		}
		if arg.Model == "" {
			arg.Model = current.ID
		}
		for _, model := range models {
			if model.ID == arg.Model && (model.Provider == "" || model.Provider == provider) {
				thinking := arg.Thinking
				if thinking == "" {
					thinking = ag.Thinking()
				}
				return nil, a.SetProviderModelThinkingContext(ctx, provider, model, thinking)
			}
		}
		return nil, errors.New("model is not in the configured catalog")
	case "session.messages":
		messages, err := ag.Messages()
		if err != nil {
			return nil, err
		}
		return boundedPluginMessages(messages, arg.Offset, arg.Limit, a.Cfg.ToolOutputLimit()), nil
	case "session.branches":
		return ag.Branches()
	case "session.rename":
		return nil, a.RenameSession(arg.Name)
	case "session.renameBranch":
		return a.RenameBranch(arg.ID, arg.Name)
	case "session.deleteBranch":
		return nil, a.DeleteBranch(arg.ID)
	case "session.fork":
		var request protocol.BranchForkOptions
		if err := jsonv2.Unmarshal(raw, &request); err != nil {
			return nil, err
		}
		return h.schedulePluginTransition(inv, pluginTransition{fork: &request})
	case "session.selectBranch":
		return h.schedulePluginTransition(inv, pluginTransition{branch: arg.ID})
	case "session.compact":
		if ag.IsRunning() {
			return nil, errors.New("agent busy")
		}
		return ag.Compact(ctx)
	case "goals.get":
		return a.GoalState()
	case "goals.create":
		return a.CreateGoal(arg.Objective, arg.Budget, arg.Replace)
	case "goals.edit":
		return a.EditGoal(arg.Objective)
	case "goals.pause":
		return a.PauseGoal()
	case "goals.resume":
		return a.ResumeGoal()
	case "goals.clear":
		return nil, a.ClearGoal()
	case "subagents.models":
		return a.SubagentModels(), nil
	case "subagents.spawn":
		if err := a.ReadySubagents(); err != nil {
			return nil, err
		}
		var request protocol.SpawnSubagentRequest
		if err := jsonv2.Unmarshal(raw, &request); err != nil {
			return nil, err
		}
		child, err := a.SpawnSubagent(ctx, request)
		if err == nil && inv.Kind == "command" {
			h.services.mu.Lock()
			id := inv.PluginID + ":" + inv.Name
			h.services.children[id] = append(h.services.children[id], string(child.Agent.Path))
			h.services.mu.Unlock()
		}
		return child, err
	case "subagents.list":
		return a.ListSubagents(ctx, arg.Target)
	case "subagents.get":
		return a.Subagent(ctx, arg.Target)
	case "subagents.messages":
		messages, err := a.SubagentMessages(ctx, arg.Target)
		if err != nil {
			return nil, err
		}
		return boundedPluginMessages(messages, arg.Offset, arg.Limit, a.Cfg.ToolOutputLimit()), nil
	case "subagents.message":
		return nil, a.SendSubagentMessage(ctx, arg.Target, arg.Text)
	case "subagents.followUp":
		return nil, a.FollowupSubagent(ctx, arg.Target, arg.Text)
	case "subagents.interrupt":
		return a.InterruptSubagent(ctx, arg.Target)
	case "subagents.close":
		return a.CloseSubagent(ctx, arg.Target)
	case "subagents.resume":
		return a.ResumeSubagent(ctx, arg.Target)
	case "subagents.wait":
		if arg.TimeoutMS < 0 || arg.TimeoutMS > 60000 {
			return nil, errors.New("wait timeoutMS must be 0..60000")
		}
		timeout := time.Duration(arg.TimeoutMS) * time.Millisecond
		if timeout == 0 {
			timeout = time.Second
		}
		if arg.Until == "all" {
			return a.WaitSubagentsUntilAll(ctx, timeout)
		}
		return a.WaitSubagents(ctx, timeout)
	case "tools.call":
		if !slices.Contains(h.info.HostTools, arg.Name) || !slices.Contains(inv.Uses, arg.Name) {
			return nil, errors.New("host tool is not declared")
		}
		result, err := ag.InvokePluginTool(ctx, inv, arg.Name, arg.Arguments)
		if err != nil && len(result.Content) == 0 {
			return nil, err
		}
		if err != nil {
			result.Content = append(result.Content, protocol.NewTextBlock(err.Error()))
		}
		for _, block := range result.Content {
			if block.Type != protocol.BlockText {
				return nil, errors.New("JavaScript host results support text only")
			}
		}
		return struct {
			Content []protocol.ContentBlock `json:"content"`
			IsError bool                    `json:"isError"`
		}{result.Content, result.IsError}, nil
	}
	return nil, fmt.Errorf("unknown plugin host operation %s", operation)
}

func boundedPluginMessages(messages []protocol.Message, offset, limit, maxBytes int) []protocol.Message {
	if limit <= 0 {
		limit = 50
	}
	limit = min(limit, 100)
	offset = max(offset, 0)
	end := max(0, len(messages)-offset)
	start := max(0, end-limit)
	out := make([]protocol.Message, 0, end-start)
	size := 0
	for i := end - 1; i >= start; i-- {
		message := messages[i].Clone()
		message.Content = slices.DeleteFunc(message.Content, func(b protocol.ContentBlock) bool { return b.Type == protocol.BlockProviderData })
		raw, _ := jsonv2.Marshal(message)
		if size+len(raw) > maxBytes {
			break
		}
		size += len(raw)
		out = append(out, message)
	}
	slices.Reverse(out)
	return out
}

func (h *appExtensionHost) schedulePluginTransition(inv plugin.Invocation, transition pluginTransition) (any, error) {
	if inv.Kind != "command" {
		return nil, errors.New("branch transition requires an explicit command")
	}
	if h.agent.IsRunning() {
		return nil, errors.New("agent busy")
	}
	h.services.mu.Lock()
	defer h.services.mu.Unlock()
	id := inv.PluginID + ":" + inv.Name
	if h.services.commands[id] == nil {
		return nil, errors.New("command is no longer active")
	}
	h.services.transitions[id] = transition
	return map[string]bool{"scheduled": true}, nil
}
