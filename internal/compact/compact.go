// Package compact implements manual context compaction: summarizing older
// turns and recording a logical boundary while preserving append-only history.
package compact

import (
	"context"

	"github.com/elmissouri16/snow-core/pkg/protocol"
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
	// TotalMessages is the provider-facing message count used to build the plan.
	TotalMessages int
}

// PlannerOptions controls turn-aware tail retention.
type PlannerOptions struct {
	RetainTokens          int
	MinRetainedTurns      int
	AllowActiveToolCycles bool
}

const WorkingStateTitle = "# Working State Checkpoint"

var WorkingStateSections = [...]string{
	"Objective and constraints",
	"Current working state",
	"Decisions and rationale",
	"Files and symbols",
	"Commands and verification",
	"Important tool results",
	"Errors and failed approaches",
	"Attributed agent updates",
	"Prior working-state checkpoints",
	"Retrieval references",
	"Unresolved next steps",
	"Active tool batch",
}

var providerToolMarkupMarkers = [...]string{
	"<｜dsml｜tool_calls>",
	"<｜dsml｜invoke",
	"<tool_call",
	"<function_call",
	"</tool_calls>",
}
