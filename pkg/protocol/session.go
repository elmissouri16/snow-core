package protocol

// SessionBranch describes a durable branch reference within a session tree.
// TipID is the entry at which the branch currently ends; entries are shared
// between branches and are never copied when a branch is forked.
type SessionBranch struct {
	ID           string `json:"id"`
	Name         string `json:"name,omitempty"`
	ParentID     string `json:"parent_branch_id,omitempty"`
	ForkedFromID string `json:"forked_from_id,omitempty"`
	TipID        string `json:"tip_id"`
	Messages     int    `json:"messages"`
	Preview      string `json:"preview,omitempty"`
	CreatedAt    int64  `json:"created_at"`
	UpdatedAt    int64  `json:"updated_at"`
	Active       bool   `json:"active"`
}

// BranchForkOptions requests a named fork from an explicit source branch.
type BranchForkOptions struct {
	SourceBranchID string `json:"source_branch_id,omitempty"`
	FromEntryID    string `json:"from_entry_id"`
	Name           string `json:"name,omitempty"`
}

// CompactionResult describes a completed manual context compaction.
type CompactionResult struct {
	SummarizedMessages int    `json:"summarized_messages"`
	RetainedMessages   int    `json:"retained_messages"`
	Summary            string `json:"summary,omitempty"`
	UsedFallback       bool   `json:"used_fallback,omitempty"`
}
