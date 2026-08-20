package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const (
	maxAgentInspectionMessages  = 24
	maxAgentMessagePreviewRunes = 2000
)

func historicalChildMessageCount(messages []protocol.Message) int {
	count := 0
	for _, message := range messages {
		if message.Role == protocol.RoleAgent {
			count++
		}
	}
	return count
}

func subagentInfoTitle(list protocol.SubagentList) string {
	spawned := list.Running + list.Queued + list.Terminal
	return fmt.Sprintf("Agents · %d running · %d queued · %d finished · concurrency %d/%d · spawned %d/%d",
		list.Running, list.Queued, list.Terminal, list.Running, list.ConcurrentLimit, spawned, list.AgentLimit)
}

func subagentInfoItems(list protocol.SubagentList, durable bool, now time.Time) ([]statusInfoItem, []string) {
	items := make([]statusInfoItem, 0, len(list.Agents))
	targets := make([]string, 0, len(list.Agents))
	for _, state := range list.Agents {
		label := fmt.Sprintf("%s  ·  %s  ·  %s", state.Agent.Path, state.Status, state.Agent.Role)
		details := []string{agentModelLabel(state)}
		if state.Thinking != "" {
			details = append(details, "thinking "+string(state.Thinking))
		}
		if elapsed := agentElapsed(state, now); elapsed > 0 {
			details = append(details, elapsed.Round(time.Millisecond).String())
		} else if state.CreatedAt > 0 && (state.Status == protocol.AgentQueued || state.Status == protocol.AgentPendingInit) {
			details = append(details, "queued "+now.Sub(time.UnixMilli(state.CreatedAt)).Round(time.Millisecond).String())
		}
		if state.Usage != nil {
			details = append(details, fmt.Sprintf("%d tokens", state.Usage.Total))
		}
		if state.Error != "" {
			details = append(details, "error: "+compactAgentText(state.Error, 180))
		} else if state.Result != "" {
			details = append(details, "result: "+compactAgentText(state.Result, 180))
		}
		if !durable && state.Agent.Path != protocol.RootAgentPath {
			details = append(details, "history is memory-only")
		}
		items = append(items, statusInfoItem{Label: label, Detail: strings.Join(nonEmptyStrings(details), " · ")})
		targets = append(targets, string(state.Agent.Path))
	}
	return items, targets
}

func renderSubagentInspection(state protocol.SubagentState, messages []protocol.Message, messageErr error, durable bool, now time.Time) string {
	lines := []string{
		fmt.Sprintf("agent %s", state.Agent.Path),
		fmt.Sprintf("status: %s", state.Status),
		fmt.Sprintf("role: %s", state.Agent.Role),
		fmt.Sprintf("thread: %s", state.Agent.ThreadID),
	}
	if state.Agent.ParentPath != "" {
		lines = append(lines, fmt.Sprintf("parent: %s (%s)", state.Agent.ParentPath, state.Agent.ParentThreadID))
	}
	lines = append(lines, "model: "+agentModelLabel(state))
	if state.Thinking != "" {
		lines = append(lines, "thinking: "+string(state.Thinking))
	}
	if state.CreatedAt > 0 {
		lines = append(lines, "created: "+formatAgentTime(state.CreatedAt))
	}
	if state.StartedAt > 0 {
		lines = append(lines, "started: "+formatAgentTime(state.StartedAt))
	}
	if state.FinishedAt > 0 {
		lines = append(lines, "finished: "+formatAgentTime(state.FinishedAt))
	}
	if elapsed := agentElapsed(state, now); elapsed > 0 {
		lines = append(lines, "elapsed: "+elapsed.Round(time.Millisecond).String())
	}
	if state.Usage != nil {
		lines = append(lines, fmt.Sprintf("usage: %d total · %d in · %d out · %d cached", state.Usage.Total, state.Usage.Input, state.Usage.Output, state.Usage.CacheRead))
	}
	lines = append(lines, fmt.Sprintf("generation: %d", state.Generation))
	if state.Agent.Path != protocol.RootAgentPath {
		if durable {
			lines = append(lines, "history: durable (available after restart)")
		} else {
			lines = append(lines, "history: memory-only (enable durable subagents to inspect after restart)")
		}
	}
	if state.Result != "" {
		lines = append(lines, "result: "+compactAgentText(state.Result, maxAgentMessagePreviewRunes))
	}
	if state.Error != "" {
		lines = append(lines, "error: "+compactAgentText(state.Error, maxAgentMessagePreviewRunes))
	}
	if messageErr != nil {
		lines = append(lines, "transcript: unavailable: "+messageErr.Error())
		return strings.Join(lines, "\n")
	}
	lines = append(lines, fmt.Sprintf("transcript: %d messages", len(messages)))
	start := 0
	if len(messages) > maxAgentInspectionMessages {
		start = len(messages) - maxAgentInspectionMessages
		lines = append(lines, fmt.Sprintf("… %d older messages omitted", start))
	}
	for _, message := range messages[start:] {
		lines = append(lines, agentMessageSummary(message))
	}
	return strings.Join(lines, "\n")
}

func agentMessageSummary(message protocol.Message) string {
	label := string(message.Role)
	if message.ToolName != "" {
		label += " " + message.ToolName
	}
	if message.StopReason != "" {
		label += " [" + string(message.StopReason) + "]"
	}
	if message.IsError {
		label += " [error]"
	}
	var parts []string
	if text := sessionMessageText(message); text != "" {
		parts = append(parts, text)
	}
	for _, block := range message.Content {
		if block.Type != protocol.BlockToolCall {
			continue
		}
		args := strings.TrimSpace(string(block.Arguments))
		if args != "" {
			var compact any
			if json.Unmarshal(block.Arguments, &compact) == nil {
				if encoded, err := json.Marshal(compact); err == nil {
					args = string(encoded)
				}
			}
			parts = append(parts, fmt.Sprintf("call %s %s", block.Name, args))
		} else {
			parts = append(parts, "call "+block.Name)
		}
	}
	if len(parts) == 0 && message.Error != "" {
		parts = append(parts, message.Error)
	}
	if len(parts) == 0 {
		parts = append(parts, "(no text content)")
	}
	return label + ": " + compactAgentText(strings.Join(parts, " · "), maxAgentMessagePreviewRunes)
}

func agentElapsed(state protocol.SubagentState, now time.Time) time.Duration {
	if state.StartedAt <= 0 {
		return 0
	}
	end := now.UnixMilli()
	if state.FinishedAt > 0 {
		end = state.FinishedAt
	}
	if end <= state.StartedAt {
		return 0
	}
	return time.Duration(end-state.StartedAt) * time.Millisecond
}

func agentModelLabel(state protocol.SubagentState) string {
	if state.Provider == "" {
		return state.Model
	}
	if state.Model == "" {
		return state.Provider
	}
	return state.Provider + "/" + state.Model
}

func formatAgentTime(milliseconds int64) string {
	return time.UnixMilli(milliseconds).Local().Format("2006-01-02 15:04:05")
}

func compactAgentText(value string, maxRunes int) string {
	value = strings.Join(strings.Fields(value), " ")
	return truncateRunes(value, maxRunes)
}

func nonEmptyStrings(values []string) []string {
	out := values[:0]
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	return out
}
