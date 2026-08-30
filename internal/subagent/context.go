package subagent

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// ParseForkTurns accepts none, all, or a positive integer string.
func ParseForkTurns(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "all" {
		return -1, nil
	}
	if value == "none" {
		return 0, nil
	}
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		return 0, errors.New("fork_turns must be none, all, or a positive integer string")
	}
	return n, nil
}

// ForkContext creates an independent child store and projects sanitized parent
// context into it. Tool calls survive only with every ordered result present;
// copied parent ids are rebuilt as one verified linear chain.
func ForkContext(messages []protocol.Message, forkTurns string, cwd, id string) (*session.MemoryStore, error) {
	n, err := ParseForkTurns(forkTurns)
	if err != nil {
		return nil, err
	}
	store := session.NewMemoryStore(session.Options{CWD: cwd, ID: id})
	if n == 0 {
		return store, nil
	}
	clean := sanitizeContext(messages)
	if n > 0 {
		clean = lastUserTurns(clean, n)
	}
	parent := store.BranchTip()
	batch := make([]session.Entry, 0, len(clean))
	for i := range clean {
		// sanitizeContext already owns all mutable payloads. Rewrite only the
		// copied message header; AppendBatch performs the store's defensive copy.
		message := &clean[i]
		message.ID = fmt.Sprintf("fork-%d-%s", i+1, shortID(message.ID))
		message.ParentID = parent
		batch = append(batch, session.Entry{Type: session.EntryMessage, ID: message.ID, ParentID: parent, Message: message})
		parent = message.ID
	}
	if err := store.AppendBatch(batch); err != nil {
		_ = store.Close()
		return nil, err
	}
	return store, nil
}

func sanitizeContext(messages []protocol.Message) []protocol.Message {
	calls := map[string]bool{}
	results := map[string]bool{}
	for _, m := range messages {
		if m.Role == protocol.RoleAssistant {
			for _, b := range m.Content {
				if b.Type == protocol.BlockToolCall && b.ToolCallID != "" {
					calls[b.ToolCallID] = true
				}
			}
		}
		if m.Role == protocol.RoleTool && m.ToolCallID != "" {
			results[m.ToolCallID] = true
		}
	}
	complete := map[string]bool{}
	for id := range calls {
		complete[id] = results[id]
	}
	out := make([]protocol.Message, 0, len(messages))
	for _, original := range messages {
		if original.Role == protocol.RoleSystem || original.Role == protocol.RoleAgent {
			continue
		}
		if original.Role == protocol.RoleCustom && strings.Contains(messageText(original), "snow_agent") {
			continue
		}
		if original.Role == protocol.RoleTool && !complete[original.ToolCallID] {
			continue
		}
		m := original
		m.Content = make([]protocol.ContentBlock, 0, len(original.Content))
		m.Usage = original.Usage.Clone()
		m.ToolDisplay = original.ToolDisplay.Clone()
		for _, block := range original.Content {
			switch block.Type {
			case protocol.BlockThinking:
				continue
			case protocol.BlockPlan:
				if !block.PlanComplete {
					continue
				}
			case protocol.BlockToolCall:
				if !complete[block.ToolCallID] {
					continue
				}
			}
			block.Arguments = slices.Clone(block.Arguments)
			block.Data = slices.Clone(block.Data)
			m.Content = append(m.Content, block)
		}
		if len(m.Content) == 0 {
			continue
		}
		out = append(out, m)
	}
	return out
}

func lastUserTurns(messages []protocol.Message, n int) []protocol.Message {
	if n <= 0 {
		return nil
	}
	starts := []int{}
	for i, m := range messages {
		if m.Role == protocol.RoleUser {
			starts = append(starts, i)
		}
	}
	if len(starts) <= n {
		return messages
	}
	return messages[starts[len(starts)-n]:]
}

func shortID(id string) string {
	id = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, id)
	if len(id) > 24 {
		id = id[:24]
	}
	if id == "" {
		id = "entry"
	}
	return id
}
func messageText(m protocol.Message) string {
	var b strings.Builder
	for _, block := range m.Content {
		if block.Type == protocol.BlockText || block.Type == protocol.BlockPlan {
			b.WriteString(block.Text)
		}
	}
	return b.String()
}
