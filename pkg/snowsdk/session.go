package snowsdk

import (
	"context"
	"errors"
	"time"

	"github.com/elmissouri16/snow-core/internal/agent"
	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/buildinfo"
	"github.com/elmissouri16/snow-core/internal/config"
	publicmcp "github.com/elmissouri16/snow-core/pkg/mcp"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func (s *Session) activeApp() (*app.App, error) {
	if s == nil {
		return nil, ErrStopped
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed || s.app == nil {
		return nil, ErrStopped
	}
	return s.app, nil
}

// Open creates a session.
func Open(ctx context.Context, opts Options) (*Session, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var subagents *bool
	if opts.EnableSubagents || opts.DisableSubagents {
		enabled := opts.EnableSubagents && !opts.DisableSubagents
		subagents = &enabled
	}
	if opts.EnableSubagents && opts.DisableSubagents {
		return nil, errors.New("snowsdk: EnableSubagents and DisableSubagents conflict")
	}
	var debug *bool
	if opts.EnableDebug || opts.DisableDebug {
		enabled := opts.EnableDebug && !opts.DisableDebug
		debug = &enabled
	}
	if opts.EnableDebug && opts.DisableDebug {
		return nil, errors.New("snowsdk: EnableDebug and DisableDebug conflict")
	}
	if opts.DisableDebug && opts.DebugDumpPath != "" {
		return nil, errors.New("snowsdk: DisableDebug and DebugDumpPath conflict")
	}
	var retry *config.RetryConfig
	if opts.Retry != nil {
		profile := func(value RetryProfile) config.RetryProfileConfig {
			return config.RetryProfileConfig{MaxAttempts: value.MaxAttempts, MaxElapsedMS: value.MaxElapsedMS, InitialDelayMS: value.InitialDelayMS, MaxDelayMS: value.MaxDelayMS, JitterPercent: value.JitterPercent}
		}
		value := config.RetryConfig{Normal: profile(opts.Retry.Normal), Goal: profile(opts.Retry.Goal)}
		retry = &value
	}
	a, err := app.New(ctx, app.Options{
		CWD:                     opts.CWD,
		BuildVersion:            buildinfo.Version,
		Provider:                opts.Provider,
		Model:                   opts.Model,
		SessionPath:             opts.SessionPath,
		NoSession:               opts.NoSession,
		AuthPath:                opts.AuthPath,
		ConfigPath:              opts.ConfigPath,
		Permission:              effectivePermission(opts),
		Tools:                   opts.Tools,
		SystemPrompt:            opts.SystemPrompt,
		Thinking:                opts.Thinking,
		ReasoningSummary:        opts.ReasoningSummary,
		TextVerbosity:           opts.TextVerbosity,
		CollaborationMode:       opts.CollaborationMode,
		PlanModeReasoningEffort: opts.PlanModeReasoningEffort,
		Retry:                   retry,
		Debug:                   debug,
		DebugDumpPath:           opts.DebugDumpPath,
		APIKey:                  opts.APIKey,
		BaseURL:                 opts.BaseURL,
		Plugins:                 opts.Plugins,
		NoPlugins:               opts.NoPlugins,
		GoPlugins:               opts.GoPlugins,
		MCPServers:              opts.MCPServers,
		NoMCP:                   opts.NoMCP,
		SkillDirs:               opts.SkillDirs,
		NoSkills:                opts.NoSkills,
		UserInputHandler:        opts.UserInputHandler,
		PermissionHandler:       opts.PermissionHandler,
		Subagents:               subagents,
		SubagentProvider:        opts.SubagentProvider,
		SubagentModel:           opts.SubagentModel,
		SubagentMaxConcurrency:  opts.SubagentMaxConcurrency,
		SubagentMaxAgents:       opts.SubagentMaxAgents,
		SubagentMaxDepth:        opts.SubagentMaxDepth,
	})
	if err != nil {
		return nil, err
	}
	return &Session{app: a, ctx: ctx}, nil
}

func effectivePermission(opts Options) string {
	if opts.AutoApprove {
		return "allow"
	}
	if opts.PermissionMode != "" {
		return opts.PermissionMode
	}
	return "deny"
}

// Prompt runs a full user turn to completion.
func (s *Session) Prompt(ctx context.Context, text string) error {
	a, err := s.activeApp()
	if err != nil {
		return err
	}
	return a.Agent.Prompt(ctx, text)
}

// PromptContent runs a full user turn with text and image content blocks.
func (s *Session) PromptContent(ctx context.Context, text string, attachments []protocol.ContentBlock) error {
	a, err := s.activeApp()
	if err != nil {
		return err
	}
	return a.Agent.PromptContent(ctx, text, attachments)
}

// PromptWithMode atomically switches mode and starts the prompt.
func (s *Session) PromptWithMode(ctx context.Context, text string, mode protocol.CollaborationMode) error {
	a, err := s.activeApp()
	if err != nil {
		return err
	}
	return a.Agent.PromptWithMode(ctx, text, mode)
}

// PromptContentWithMode atomically switches mode and starts a turn with text
// and image content blocks.
func (s *Session) PromptContentWithMode(ctx context.Context, text string, attachments []protocol.ContentBlock, mode protocol.CollaborationMode) error {
	a, err := s.activeApp()
	if err != nil {
		return err
	}
	return a.Agent.PromptContentWithMode(ctx, text, attachments, mode)
}

// Mode returns the active collaboration mode.
func (s *Session) Mode() protocol.CollaborationMode {
	a, err := s.activeApp()
	if err != nil {
		return protocol.ModeDefault
	}
	return a.Agent.Mode()
}

// SetMode switches collaboration mode while idle.
func (s *Session) SetMode(mode protocol.CollaborationMode) error {
	a, err := s.activeApp()
	if err != nil {
		return err
	}
	return a.Agent.SetMode(mode)
}

// Steer queues text for the next safe boundary of an active root run.
func (s *Session) Steer(ctx context.Context, text string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	a, err := s.activeApp()
	if err != nil {
		return err
	}
	if err := a.Agent.Steer(text); err != nil {
		if errors.Is(err, agent.ErrNotRunning) {
			return ErrNotRunning
		}
		return err
	}
	return nil
}

// FollowUp queues text after the active root run naturally stops and all
// steering input has been handled.
func (s *Session) FollowUp(ctx context.Context, text string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	a, err := s.activeApp()
	if err != nil {
		return err
	}
	if err := a.Agent.FollowUp(text); err != nil {
		if errors.Is(err, agent.ErrNotRunning) {
			return ErrNotRunning
		}
		return err
	}
	return nil
}

// PendingInputs returns an independent root input queue snapshot.
func (s *Session) PendingInputs() (protocol.InputQueue, error) {
	a, err := s.activeApp()
	if err != nil {
		return protocol.InputQueue{}, err
	}
	return a.Agent.PendingInputs(), nil
}

// ClearPendingInputs closes queue admission and returns every accepted input
// that has not reached a provider boundary. It is also used to recover text
// retained after an operational turn failure.
func (s *Session) ClearPendingInputs() (protocol.InputQueue, error) {
	a, err := s.activeApp()
	if err != nil {
		return protocol.InputQueue{}, err
	}
	return a.Agent.ClearPendingInputs(), nil
}

// Abort cancels any in-flight turn and clears undelivered queued input.
func (s *Session) Abort(ctx context.Context) error {
	a, e := s.activeApp()
	if e != nil {
		return e
	}
	return a.Agent.AbortContext(ctx)
}

// ReadyGoals publishes the initial goal snapshot and permits a restored,
// active, non-deferred Default-mode goal to continue. Call it only after the
// embedding host installs event subscriptions and interaction handlers.
func (s *Session) ReadyGoals() error {
	a, e := s.activeApp()
	if e != nil {
		return e
	}
	reentrantEventCallback := a.Agent.InEventCallback()
	if err := a.ReadyGoal(); err != nil {
		return err
	}
	if !reentrantEventCallback {
		return a.Agent.DrainEvents(context.Background())
	}
	return nil
}

// ReadySubagents publishes restored topology after subscriptions are installed.
func (s *Session) ReadySubagents() error {
	a, e := s.activeApp()
	if e != nil {
		return e
	}
	if err := a.ReadySubagents(); err != nil {
		return err
	}
	if !a.Agent.InEventCallback() {
		return a.Agent.DrainEvents(context.Background())
	}
	return nil
}

// SubagentModels returns exact provider/model pairs available for child agents.
func (s *Session) SubagentModels() []protocol.Model {
	a, err := s.activeApp()
	if err != nil {
		return nil
	}
	return a.SubagentModels()
}

// SpawnSubagent creates a role-scoped child at a canonical path.
func (s *Session) SpawnSubagent(ctx context.Context, req protocol.SpawnSubagentRequest) (protocol.SubagentState, error) {
	a, e := s.activeApp()
	if e != nil {
		return protocol.SubagentState{}, e
	}
	return a.SpawnSubagent(ctx, req)
}

// SendSubagentMessage queues attributed mail without starting a child turn.
func (s *Session) SendSubagentMessage(ctx context.Context, target, message string) error {
	a, e := s.activeApp()
	if e != nil {
		return e
	}
	return a.SendSubagentMessage(ctx, target, message)
}

// FollowupSubagent queues work and starts or reuses an idle child.
func (s *Session) FollowupSubagent(ctx context.Context, target, message string) error {
	a, e := s.activeApp()
	if e != nil {
		return e
	}
	return a.FollowupSubagent(ctx, target, message)
}

// WaitSubagents waits for one child activity or lifecycle change, or timeout.
func (s *Session) WaitSubagents(ctx context.Context, timeout time.Duration) (protocol.WaitSubagentsResult, error) {
	a, e := s.activeApp()
	if e != nil {
		return protocol.WaitSubagentsResult{}, e
	}
	return a.WaitSubagents(ctx, timeout)
}

// WaitSubagentsUntilAll waits until every root child is terminal or the
// bounded timeout expires. Child results still arrive through attributed
// AgentEvent/mailbox messages rather than this aggregate status result.
func (s *Session) WaitSubagentsUntilAll(ctx context.Context, timeout time.Duration) (protocol.WaitSubagentsResult, error) {
	a, e := s.activeApp()
	if e != nil {
		return protocol.WaitSubagentsResult{}, e
	}
	return a.WaitSubagentsUntilAll(ctx, timeout)
}

// InterruptSubagent cancels only the target's current turn and returns its previous status.
func (s *Session) InterruptSubagent(ctx context.Context, target string) (protocol.AgentStatus, error) {
	a, e := s.activeApp()
	if e != nil {
		return protocol.AgentNotFound, e
	}
	return a.InterruptSubagent(ctx, target)
}

// CloseSubagent releases a terminal child's open-agent slot while preserving
// its stable identity and durable history. It returns the previous status.
func (s *Session) CloseSubagent(ctx context.Context, target string) (protocol.AgentStatus, error) {
	a, e := s.activeApp()
	if e != nil {
		return protocol.AgentNotFound, e
	}
	return a.CloseSubagent(ctx, target)
}

// ResumeSubagent reopens a closed identity without starting a turn.
func (s *Session) ResumeSubagent(ctx context.Context, target string) (protocol.SubagentState, error) {
	a, e := s.activeApp()
	if e != nil {
		return protocol.SubagentState{}, e
	}
	return a.ResumeSubagent(ctx, target)
}

// Subagents returns bounded snapshots for the root and its visible descendants.
func (s *Session) Subagents() []protocol.SubagentState {
	a, e := s.activeApp()
	if e != nil {
		return nil
	}
	list, err := a.ListSubagents(context.Background(), "")
	if err != nil {
		return nil
	}
	return list.Agents
}

// Subagent returns one child snapshot by canonical path or supported identifier.
func (s *Session) Subagent(target string) (protocol.SubagentState, error) {
	a, e := s.activeApp()
	if e != nil {
		return protocol.SubagentState{}, e
	}
	return a.Subagent(context.Background(), target)
}

// SubagentUsage returns aggregate usage across visible child agents.
func (s *Session) SubagentUsage() (protocol.Usage, error) {
	a, e := s.activeApp()
	if e != nil {
		return protocol.Usage{}, e
	}
	return a.SubagentUsage()
}

// Goal returns the active branch goal, or nil when no goal exists.
func (s *Session) Goal() (*protocol.ThreadGoal, error) {
	a, e := s.activeApp()
	if e != nil {
		return nil, e
	}
	return a.GoalState()
}

// CreateGoal creates a persistent Thread Goal on the active branch.
func (s *Session) CreateGoal(objective string, budget *int64, replace bool) (*protocol.ThreadGoal, error) {
	a, e := s.activeApp()
	if e != nil {
		return nil, e
	}
	return a.CreateGoal(objective, budget, replace)
}

// SetGoal is an alias for CreateGoal.
func (s *Session) SetGoal(objective string, budget *int64, replace bool) (*protocol.ThreadGoal, error) {
	return s.CreateGoal(objective, budget, replace)
}

// EditGoal rotates the goal objective and identity while preserving its budget and usage.
func (s *Session) EditGoal(objective string) (*protocol.ThreadGoal, error) {
	a, e := s.activeApp()
	if e != nil {
		return nil, e
	}
	return a.EditGoal(objective)
}

// PauseGoal pauses eligible automatic continuation.
func (s *Session) PauseGoal() (*protocol.ThreadGoal, error) {
	a, e := s.activeApp()
	if e != nil {
		return nil, e
	}
	return a.PauseGoal()
}

// ResumeGoal resumes eligible automatic continuation, including abort-deferred work.
func (s *Session) ResumeGoal() (*protocol.ThreadGoal, error) {
	a, e := s.activeApp()
	if e != nil {
		return nil, e
	}
	return a.ResumeGoal()
}

// ClearGoal removes the active branch goal.
func (s *Session) ClearGoal() error {
	a, e := s.activeApp()
	if e != nil {
		return e
	}
	return a.ClearGoal()
}

// ContinueGoal clears continuation deferral and starts eligible idle goal work.
func (s *Session) ContinueGoal() error {
	a, e := s.activeApp()
	if e != nil {
		return e
	}
	return a.ContinueGoal()
}

// Diagnostics returns an immutable snapshot of non-fatal auxiliary config warnings.
func (s *Session) Diagnostics() ([]protocol.ConfigDiagnostic, error) {
	a, err := s.activeApp()
	if err != nil {
		return nil, err
	}
	return a.ConfigDiagnostics(), nil
}

// Compact manually compacts the active branch. Goal continuation may also
// compact automatically according to the host configuration.
func (s *Session) Compact(ctx context.Context) (protocol.CompactionResult, error) {
	a, err := s.activeApp()
	if err != nil {
		return protocol.CompactionResult{}, err
	}
	return a.Agent.Compact(ctx)
}

// Branches lists durable branches in the active session.
func (s *Session) Branches() ([]protocol.SessionBranch, error) {
	a, err := s.activeApp()
	if err != nil {
		return nil, err
	}
	return a.Agent.Branches()
}

// SelectBranch switches the active branch and affects subsequent Messages,
// Usage, and Prompt calls.
func (s *Session) SelectBranch(branchID string) error {
	a, err := s.activeApp()
	if err != nil {
		return err
	}
	return a.SelectBranch(branchID)
}

// Branch creates and activates a durable branch in the current session.
func (s *Session) Branch(opts protocol.BranchForkOptions) (protocol.SessionBranch, error) {
	a, err := s.activeApp()
	if err != nil {
		return protocol.SessionBranch{}, err
	}
	return a.ForkBranchWithOptions(opts)
}

// Fork creates and activates a durable branch at an existing entry ID. The
// next Prompt appends the first divergent child to that branch. It is retained
// as a compatibility alias for Branch.
func (s *Session) Fork(fromEntryID string) (protocol.SessionBranch, error) {
	a, err := s.activeApp()
	if err != nil {
		return protocol.SessionBranch{}, err
	}
	return a.ForkBranch(fromEntryID)
}

// ForkNamed creates a durable branch from an explicit source with an optional display name.
func (s *Session) ForkNamed(sourceBranchID, fromEntryID, name string) (protocol.SessionBranch, error) {
	return s.Branch(protocol.BranchForkOptions{SourceBranchID: sourceBranchID, FromEntryID: fromEntryID, Name: name})
}

// ForkSession creates an independent durable session in the current workspace.
// The receiver remains bound to the source; open result.SessionPath explicitly
// to continue in the child.
func (s *Session) ForkSession(ctx context.Context, opts protocol.SessionForkOptions) (protocol.SessionForkResult, error) {
	a, err := s.activeApp()
	if err != nil {
		return protocol.SessionForkResult{}, err
	}
	return a.ForkSession(ctx, opts)
}

// ForkWorktree creates a clean Git worktree and an independent durable session
// rooted there. The receiver remains bound to the source so project-specific
// trust and tool roots are never retargeted in place.
func (s *Session) ForkWorktree(ctx context.Context, opts protocol.SessionWorktreeForkOptions) (protocol.SessionForkResult, error) {
	a, err := s.activeApp()
	if err != nil {
		return protocol.SessionForkResult{}, err
	}
	return a.ForkWorktree(ctx, opts)
}

// RenameBranch updates a branch display name without changing its stable ID.
func (s *Session) RenameBranch(branchID, name string) (protocol.SessionBranch, error) {
	a, err := s.activeApp()
	if err != nil {
		return protocol.SessionBranch{}, err
	}
	return a.RenameBranch(branchID, name)
}

// DeleteBranch removes an eligible inactive leaf branch reference.
func (s *Session) DeleteBranch(branchID string) error {
	a, err := s.activeApp()
	if err != nil {
		return err
	}
	return a.DeleteBranch(branchID)
}

// Subscribe registers an ordered event listener and returns an unsubscribe
// function. Callbacks must return promptly; callbacks exceeding the runtime's
// bounded delivery timeout are evicted so they cannot strand later events.
func (s *Session) Subscribe(fn func(protocol.AgentEvent)) func() {
	if fn == nil {
		return func() {}
	}
	a, err := s.activeApp()
	if err != nil {
		return func() {}
	}
	return a.Agent.Subscribe(fn)
}

// StateEvent returns the current collaboration-mode snapshot. Emit it after
// subscribing when the host needs initial state before the first prompt.
func (s *Session) StateEvent() protocol.AgentEvent {
	a, err := s.activeApp()
	if err != nil {
		return protocol.AgentEvent{}
	}
	return a.Agent.StateEvent()
}

// Model returns the current model.
func (s *Session) Model() protocol.Model {
	a, err := s.activeApp()
	if err != nil {
		return protocol.Model{}
	}
	return a.Agent.Model()
}

// Models returns a deep defensive copy of the active provider's current catalog.
func (s *Session) Models() []protocol.Model {
	a, err := s.activeApp()
	if err != nil {
		return nil
	}
	models := make([]protocol.Model, len(a.Models))
	for i, model := range a.Models {
		models[i] = model.Clone()
	}
	return models
}

// MCPServers returns negotiated server status without credentials or headers.
func (s *Session) MCPServers() []publicmcp.Status {
	a, err := s.activeApp()
	if err != nil {
		return nil
	}
	statuses := a.MCPStatuses
	if a.MCPManager != nil {
		statuses = a.MCPManager.Statuses()
	}
	out := make([]publicmcp.Status, len(statuses))
	copy(out, statuses)
	for i := range out {
		out[i].Capabilities = append([]string(nil), out[i].Capabilities...)
	}
	return out
}

// Skills returns discovered metadata; full instructions remain progressively
// disclosed through activate_skill.
func (s *Session) Skills() []SkillInfo {
	a, err := s.activeApp()
	if err != nil || a.Skills == nil {
		return nil
	}
	list := a.Skills.List()
	out := make([]SkillInfo, 0, len(list))
	for _, skill := range list {
		metadata := make(map[string]string, len(skill.Metadata))
		for key, value := range skill.Metadata {
			metadata[key] = value
		}
		out = append(out, SkillInfo{Name: skill.Name, Description: skill.Description, License: skill.License, Compatibility: skill.Compatibility, Metadata: metadata, AllowedTools: skill.AllowedTools, Location: skill.Location, Scope: skill.Scope, Source: skill.Source, Enabled: skill.Enabled, DisabledBy: skill.DisabledBy})
	}
	return out
}

// SkillInventory returns all discovered skills, including policy-disabled
// entries that are excluded from provider context and activation tools.
func (s *Session) SkillInventory() []SkillInfo {
	a, err := s.activeApp()
	if err != nil || a.Skills == nil {
		return nil
	}
	list := a.Skills.Inventory()
	out := make([]SkillInfo, 0, len(list))
	for _, skill := range list {
		metadata := make(map[string]string, len(skill.Metadata))
		for key, value := range skill.Metadata {
			metadata[key] = value
		}
		out = append(out, SkillInfo{Name: skill.Name, Description: skill.Description, License: skill.License, Compatibility: skill.Compatibility, Metadata: metadata, AllowedTools: skill.AllowedTools, Location: skill.Location, Scope: skill.Scope, Source: skill.Source, Enabled: skill.Enabled, DisabledBy: skill.DisabledBy})
	}
	return out
}

// SetModel switches the active model.
func (s *Session) SetModel(m protocol.Model) error {
	a, err := s.activeApp()
	if err != nil {
		return err
	}
	return a.SetModel(m)
}

// SetThinking changes the effort for subsequent turns.
func (s *Session) SetThinking(level protocol.ThinkingLevel) error {
	a, err := s.activeApp()
	if err != nil {
		return err
	}
	return a.Agent.SetThinking(level)
}

// Thinking returns the current normalized effort.
func (s *Session) Thinking() protocol.ThinkingLevel {
	a, err := s.activeApp()
	if err != nil {
		return protocol.ThinkingOff
	}
	return a.Agent.Thinking()
}

// SetReasoningSummary changes the provider reasoning-summary preference.
func (s *Session) SetReasoningSummary(summary protocol.ReasoningSummary) error {
	a, err := s.activeApp()
	if err != nil {
		return err
	}
	return a.Agent.SetReasoningSummary(summary)
}

// ReasoningSummary returns the current normalized summary preference.
func (s *Session) ReasoningSummary() protocol.ReasoningSummary {
	a, err := s.activeApp()
	if err != nil {
		return protocol.ReasoningSummaryAuto
	}
	return a.Agent.ReasoningSummary()
}

// SetTextVerbosity changes the provider text-verbosity preference.
func (s *Session) SetTextVerbosity(verbosity protocol.TextVerbosity) error {
	a, err := s.activeApp()
	if err != nil {
		return err
	}
	return a.Agent.SetTextVerbosity(verbosity)
}

// TextVerbosity returns the current normalized text-verbosity preference.
func (s *Session) TextVerbosity() protocol.TextVerbosity {
	a, err := s.activeApp()
	if err != nil {
		return protocol.TextVerbosityLow
	}
	return a.Agent.TextVerbosity()
}

// Messages returns the linearized session messages.
func (s *Session) Messages() ([]protocol.Message, error) {
	a, err := s.activeApp()
	if err != nil {
		return nil, err
	}
	messages, err := a.Agent.Messages()
	if err != nil {
		return nil, err
	}
	for i := range messages {
		content := make([]protocol.ContentBlock, 0, len(messages[i].Content))
		for _, block := range messages[i].Content {
			if block.Type != protocol.BlockProviderData {
				content = append(content, block)
			}
		}
		messages[i].Content = content
	}
	return messages, nil
}

// Usage returns aggregate token/cache usage for the active session branch.
func (s *Session) Usage() (protocol.Usage, error) {
	a, err := s.activeApp()
	if err != nil {
		return protocol.Usage{}, err
	}
	return a.Agent.Usage()
}

// IsRunning reports whether a turn is in flight.
func (s *Session) IsRunning() bool {
	a, err := s.activeApp()
	return err == nil && a.Agent.IsRunning()
}

// SessionID returns the session identifier.
func (s *Session) SessionID() string {
	a, err := s.activeApp()
	if err != nil {
		return ""
	}
	id, _, err := a.Agent.SessionIdentity()
	if err != nil {
		return ""
	}
	return id
}

// SessionName returns the optional session display title.
func (s *Session) SessionName() string {
	a, err := s.activeApp()
	if err != nil {
		return ""
	}
	title, err := a.Agent.SessionTitle()
	if err != nil {
		return ""
	}
	return title
}

// RenameSession changes the display title without changing the stable ID or path.
func (s *Session) RenameSession(title string) error {
	a, err := s.activeApp()
	if err != nil {
		return err
	}
	return a.RenameSession(title)
}

// SessionPath returns the session file path ("" for in-memory).
func (s *Session) SessionPath() string {
	a, err := s.activeApp()
	if err != nil {
		return ""
	}
	_, path, err := a.Agent.SessionIdentity()
	if err != nil {
		return ""
	}
	return path
}

// EnablePermissionReplies permits manual ReplyPermission/RejectPermission
// resolution of ask-mode permission requests from this SDK session.
func (s *Session) EnablePermissionReplies() {
	a, err := s.activeApp()
	if err != nil {
		return
	}
	a.EnablePermissionReplies()
}

// ReplyPermission resolves the pending ask-mode permission request.
func (s *Session) ReplyPermission(response protocol.PermissionResponse) error {
	a, err := s.activeApp()
	if err != nil {
		return err
	}
	return a.ReplyPermission(response)
}

// RejectPermission declines the pending ask-mode permission request.
func (s *Session) RejectPermission(requestID string) error {
	a, err := s.activeApp()
	if err != nil {
		return err
	}
	return a.RejectPermission(requestID)
}

// CWD returns the session working directory.
func (s *Session) CWD() string {
	a, err := s.activeApp()
	if err != nil {
		return ""
	}
	return a.CWD()
}
