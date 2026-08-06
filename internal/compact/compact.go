// Package compact implements context compaction: summarizing older turns and
// replacing them with a single summary entry to free context window.
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
	// NOTE: v1 compaction appends a summary entry but does not rewrite the
	// tree, so these messages remain loadable; full span replacement is a
	// phase-4 refinement (see package doc).
	SummarizedMessages int
	Summary            string
	BeforeEntries      int
	AfterEntries       int
}

// Plan describes what compaction would do, without applying it.
type Plan struct {
	// CompactionCandidates are the messages that would be summarized.
	CompactionCandidates []protocol.Message
	// KeepFrom is the index in the linearized messages where the retained
	// tail begins.
	KeepFrom int
	// EstimatedTokens is a rough estimate (chars/4).
	EstimatedTokens int
}

// Planner finds the prefix of messages to compact, keeping the most recent
// maxTokens worth (roughly). It is deterministic and pure.
func Planner(msgs []protocol.Message, maxTokens int) Plan {
	// Always keep at least the last 4 messages (system-adjacent tail).
	keepMin := 4
	if len(msgs) <= keepMin {
		return Plan{KeepFrom: 0, EstimatedTokens: estimateTokens(msgs)}
	}
	// Walk backwards accumulating "tokens" (chars/4 heuristic) until we reach
	// maxTokens or the keepMin boundary.
	tokens := 0
	keep := len(msgs)
	for i := len(msgs) - 1; i >= keepMin; i-- {
		t := estimateTokens(msgs[i : i+1])
		if tokens+t > maxTokens && tokens > 0 {
			break
		}
		tokens += t
		keep = i
	}
	plan := Plan{KeepFrom: keep}
	plan.CompactionCandidates = msgs[:keep]
	plan.EstimatedTokens = estimateTokens(msgs[:keep])
	return plan
}

// Apply compacts the session: candidates are summarized and replaced in place.
// Because sessions are append-only, compaction writes a compaction entry
// carrying the summary and marks the branch tip at the last retained message.
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

	// Determine the id of the message just before KeepFrom so we can branch
	// the summary entry there.
	// Note: entries are appended at the tip, so we need the retained tail.
	// This simple implementation appends the summary at the current tip and
	// reports counts; full tree rewriting is a phase-4 refinement.
	before := len(msgs)
	_ = before

	entry := session.Entry{
		Type:    session.EntryCompaction,
		Summary: summary,
	}
	if err := st.Append(entry); err != nil {
		return Result{}, err
	}

	afterMsgs, _ := st.Messages()
	return Result{
		SummarizedMessages: plan.KeepFrom,
		Summary:            summary,
		BeforeEntries:      len(msgs),
		AfterEntries:       len(afterMsgs),
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

// DefaultSummarizer produces a structured summary using the first N messages'
// text content (used when no model-backed summarizer is configured).
func DefaultSummarizer(ctx context.Context, msgs []protocol.Message) (string, error) {
	var b strings.Builder
	b.WriteString("Compacted conversation summary:\n")
	for i, m := range msgs {
		text := ""
		for _, c := range m.Content {
			if c.Type == protocol.BlockText {
				text = c.Text
				break
			}
		}
		if text == "" {
			continue
		}
		if len(text) > 200 {
			text = text[:200] + "…"
		}
		fmt.Fprintf(&b, "- %s: %s\n", m.Role, text)
		if i >= 20 {
			b.WriteString("…\n")
			break
		}
	}
	return b.String(), nil
}
