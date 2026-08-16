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
	FromEntryID    string `json:"from_entry_id,omitempty"`
	Name           string `json:"name,omitempty"`
}

// SessionForkOptions requests an independent durable session snapshot. The
// source session remains unchanged; DestinationPath is optional and must not
// already exist when supplied.
type SessionForkOptions struct {
	SourceBranchID  string `json:"source_branch_id,omitempty"`
	FromEntryID     string `json:"from_entry_id,omitempty"`
	Name            string `json:"name,omitempty"`
	DestinationPath string `json:"destination_path,omitempty"`
}

// SessionWorktreeForkOptions requests an independent session rooted in a new
// Git worktree. WorktreePath may be empty for a collision-resistant generated
// sibling path. GitBranch may be empty for a generated snow/* branch.
type SessionWorktreeForkOptions struct {
	SourceBranchID  string `json:"source_branch_id,omitempty"`
	FromEntryID     string `json:"from_entry_id,omitempty"`
	Name            string `json:"name,omitempty"`
	DestinationPath string `json:"destination_path,omitempty"`
	WorktreePath    string `json:"worktree_path,omitempty"`
	GitBranch       string `json:"git_branch,omitempty"`
}

// WorktreeInfo describes the Git workspace created for a session fork.
type WorktreeInfo struct {
	Path   string `json:"path"`
	Branch string `json:"branch"`
	Commit string `json:"commit,omitempty"`
}

// SessionForkResult identifies an independent child session and its immutable
// provenance. The destination always starts on its local main branch.
type SessionForkResult struct {
	SourceSessionID string        `json:"source_session_id"`
	SourceBranchID  string        `json:"source_branch_id"`
	SourceEntryID   string        `json:"source_entry_id"`
	SessionID       string        `json:"session_id"`
	SessionPath     string        `json:"session_path"`
	CWD             string        `json:"cwd"`
	Name            string        `json:"name,omitempty"`
	Branch          SessionBranch `json:"branch"`
	Worktree        *WorktreeInfo `json:"worktree,omitempty"`
}

// CompactionResult describes a completed context compaction.
type CompactionResult struct {
	SummarizedMessages int    `json:"summarized_messages"`
	RetainedMessages   int    `json:"retained_messages"`
	Summary            string `json:"summary,omitempty"`
	UsedFallback       bool   `json:"used_fallback,omitempty"`
	Automatic          bool   `json:"automatic,omitempty"`
}
