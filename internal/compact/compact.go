// Package compact implements manual context compaction: summarizing older
// turns and recording a logical boundary while preserving append-only history.
package compact

import (
	"context"
	"fmt"
	"strings"

	"github.com/snow-core/snow/internal/session"
	"github.com/snow-core/snow/pkg/protocol"
)

// Summarizer produces a summary of messages.
type Summarizer func(ctx context.Context, msgs []protocol.Message) (string, error)

// Result describes a completed compaction.
type Result struct {
	// SummarizedMessages counts the messages folded into the summary entry.
	// History remains physically available; ContextMessages projects the
	// summary plus retained tail to providers.
	SummarizedMessages int
	RetainedMessages   int
	Summary            string
	// BeforeEntries is the message count before compaction; AfterEntries is
	// the same count plus the appended marker entry (which is not a message).
	BeforeEntries int
	AfterEntries  int
}

// Plan describes what compaction would do, without applying it.
type Plan struct {
	// CompactionCandidates are the messages that would be summarized.
	CompactionCandidates []protocol.Message
	// KeepFrom is the index in the provider-facing messages where the retained
	// complete-turn tail begins.
	KeepFrom int
	// BoundaryID is the last real persisted message folded into the summary.
	BoundaryID string
	// EstimatedTokens is a rough estimate (chars/4).
	EstimatedTokens int
}

// PlannerOptions controls turn-aware tail retention.
type PlannerOptions struct {
	RetainTokens     int
	MinRetainedTurns int
}

// Planner preserves the legacy entry point while using turn-aware planning.
func Planner(msgs []protocol.Message, maxTokens int) Plan {
	return PlannerWithOptions(msgs, PlannerOptions{RetainTokens: maxTokens, MinRetainedTurns: 2})
}

// PlannerWithOptions finds a prefix to compact while retaining complete recent
// user turns. Tool calls and results remain in the same turn by construction.
func PlannerWithOptions(msgs []protocol.Message, opts PlannerOptions) Plan {
	if opts.MinRetainedTurns < 1 {
		opts.MinRetainedTurns = 2
	}
	if opts.RetainTokens < 1 {
		opts.RetainTokens = 8 * 1024
	}
	if len(msgs) <= 1 {
		return Plan{EstimatedTokens: estimateTokens(msgs)}
	}
	starts := completeTurnStarts(msgs)
	keep := 0
	if len(starts) > opts.MinRetainedTurns {
		keep = starts[len(starts)-opts.MinRetainedTurns]
		for turn := len(starts) - opts.MinRetainedTurns - 1; turn >= 1; turn-- {
			candidate := starts[turn]
			if estimateTokens(msgs[candidate:]) > opts.RetainTokens {
				break
			}
			keep = candidate
		}
	}
	if keep <= 0 {
		return Plan{EstimatedTokens: estimateTokens(msgs)}
	}
	plan := Plan{KeepFrom: keep, CompactionCandidates: msgs[:keep]}
	plan.EstimatedTokens = estimateTokens(plan.CompactionCandidates)
	for i := len(plan.CompactionCandidates) - 1; i >= 0; i-- {
		id := plan.CompactionCandidates[i].ID
		if id != "" && !strings.HasPrefix(id, "compaction-") {
			plan.BoundaryID = id
			break
		}
	}
	if plan.BoundaryID == "" {
		return Plan{EstimatedTokens: estimateTokens(msgs)}
	}
	return plan
}

// completeTurnStarts returns boundaries that can safely begin retained provider
// context. User and mailbox messages always begin a turn. Private goal turns do
// not append another user message, so an assistant message after a terminal
// assistant response also begins a turn. An assistant following a tool result
// is deliberately not a boundary: it belongs to the same tool-call/result group.
func completeTurnStarts(msgs []protocol.Message) []int {
	starts := make([]int, 0)
	for i, msg := range msgs {
		switch msg.Role {
		case protocol.RoleUser, protocol.RoleAgent:
			starts = append(starts, i)
		case protocol.RoleAssistant:
			if i > 0 && msgs[i-1].Role == protocol.RoleAssistant && msgs[i-1].StopReason != protocol.StopToolUse {
				starts = append(starts, i)
			}
		}
	}
	return starts
}

// Apply compacts the session by appending a marker that records the summary and
// the last compacted message. ContextMessages uses that marker to project the
// summary plus retained tail without deleting history.
func Apply(ctx context.Context, st session.Store, summarizer Summarizer, plan Plan) (Result, error) {
	if len(plan.CompactionCandidates) == 0 {
		return Result{}, nil
	}
	summary, err := summarizer(ctx, plan.CompactionCandidates)
	if err != nil {
		return Result{}, fmt.Errorf("compact: summarize: %w", err)
	}

	msgs, err := st.Messages()
	if err != nil {
		return Result{}, err
	}

	boundary := plan.BoundaryID
	if boundary == "" {
		boundary = plan.CompactionCandidates[len(plan.CompactionCandidates)-1].ID
	}
	entry := session.Entry{
		Type:             session.EntryCompaction,
		Summary:          summary,
		CompactedThrough: boundary,
	}
	if err := st.Append(entry); err != nil {
		return Result{}, err
	}

	return Result{
		SummarizedMessages: plan.KeepFrom,
		RetainedMessages:   len(msgs) - plan.KeepFrom,
		Summary:            summary,
		BeforeEntries:      len(msgs),
		// The marker entry is appended but is not a message.
		AfterEntries: len(msgs) + 1,
	}, nil
}

// estimateTokens is a rough chars/4 heuristic; provider-reported usage is
// authoritative at runtime.
func estimateTokens(msgs []protocol.Message) int {
	n := 0
	for _, m := range msgs {
		for _, c := range m.Content {
			n += len(c.Text) / 4
			n += len(c.Arguments) / 4
		}
	}
	return n
}

// DefaultSummarizer produces a bounded role/tool-aware continuation summary.
func DefaultSummarizer(ctx context.Context, msgs []protocol.Message) (string, error) {
	const maxRunes = 8000
	var users, assistants, toolsOut, failures []string
	for _, m := range msgs {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		for _, c := range m.Content {
			text := strings.TrimSpace(c.Text)
			if text == "" && len(c.Arguments) > 0 {
				text = strings.TrimSpace(string(c.Arguments))
			}
			if text == "" {
				continue
			}
			text = compactText(text, 500)
			switch m.Role {
			case protocol.RoleUser:
				users = append(users, text)
			case protocol.RoleTool:
				line := m.ToolName + ": " + text
				toolsOut = append(toolsOut, line)
				if m.IsError || strings.Contains(strings.ToLower(text), "error") || strings.Contains(strings.ToLower(text), "failed") {
					failures = append(failures, line)
				}
			case protocol.RoleAssistant:
				assistants = append(assistants, text)
			}
		}
	}
	var b strings.Builder
	b.WriteString("Compacted continuation context (local fallback):\n")
	writeRecent := func(title string, values []string, n int) {
		b.WriteString("\n## " + title + "\n")
		if len(values) == 0 {
			b.WriteString("- None recorded.\n")
			return
		}
		start := len(values) - n
		if start < 0 {
			start = 0
		}
		for _, value := range values[start:] {
			fmt.Fprintf(&b, "- %s\n", value)
			if len([]rune(b.String())) >= maxRunes {
				return
			}
		}
	}
	writeRecent("User objectives and constraints", users, 8)
	writeRecent("Decisions and current work", assistants, 12)
	writeRecent("Important tool results", toolsOut, 12)
	writeRecent("Errors and unresolved failures", failures, 8)
	out := []rune(b.String())
	if len(out) > maxRunes {
		out = out[:maxRunes]
	}
	return string(out), nil
}

func compactText(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	r := []rune(value)
	if len(r) > limit {
		return string(r[:limit]) + "…"
	}
	return value
}
