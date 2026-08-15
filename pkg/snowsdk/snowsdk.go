// Package snowsdk is the public, embeddable Go API for snow-core. It exposes
// the same agent loop as the CLI — no TUI, no duplicated logic.
package snowsdk

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/snow-core/snow/internal/agent"
	"github.com/snow-core/snow/internal/app"
	"github.com/snow-core/snow/internal/config"
	publicmcp "github.com/snow-core/snow/pkg/mcp"
	publicplugin "github.com/snow-core/snow/pkg/plugin"
	"github.com/snow-core/snow/pkg/protocol"
	publicsandbox "github.com/snow-core/snow/pkg/sandbox"
)

// Options configures a Session.
type Options struct {
	// CWD is the working directory. Empty means the caller's cwd.
	CWD string
	// Provider is the provider id (opencode-go | openai-compatible | chatgpt | fake). Empty uses config default.
	Provider string
	// Model is the model id. Empty resolves the provider default.
	Model string
	// SessionPath opens or creates a SQLite .db session. Empty creates an indexed one.
	SessionPath string
	// NoSession uses an ephemeral in-memory conversation. Provider credentials
	// and model caches still use AuthPath/SNOW_HOME.
	NoSession bool
	// AuthPath overrides the default auth file path.
	AuthPath string
	// ConfigPath overrides the default config file path.
	ConfigPath string
	// DisableSandbox explicitly keeps Bash on the host even when the canonical
	// project has a smolvm association. The association is inherited by default.
	DisableSandbox bool
	// RequireSandbox makes Open fail unless the project has a smolvm association.
	RequireSandbox bool
	// PermissionMode is ask|allow|deny. Headless default: deny for mutating tools.
	PermissionMode string
	// AutoApprove allows all tool calls without asking. Dangerous; CI/trusted only.
	AutoApprove bool
	// Tools is a subset allowlist of tool names. Empty = all builtins.
	Tools []string
	// SystemPrompt overrides the built-in preamble.
	SystemPrompt string
	// Thinking is a thinking level (off|minimal|low|medium|high|xhigh|max|ultra).
	Thinking string
	// ReasoningSummary is off|auto|concise|detailed.
	ReasoningSummary string
	// TextVerbosity is low|medium|high.
	TextVerbosity string
	// CollaborationMode is default|plan. Empty restores persisted state or Default.
	CollaborationMode string
	// PlanModeReasoningEffort optionally overrides Plan's Medium preset.
	PlanModeReasoningEffort string
	// APIKey provides an explicit credential (overrides auth.json and env).
	APIKey string
	// BaseURL overrides the active provider base URL. OpenAI-compatible requires
	// either this value or a globally configured endpoint.
	BaseURL string
	// Plugins are explicit argv-based external runtimes.
	Plugins []publicplugin.PluginSpec
	// NoPlugins disables all external and statically supplied plugins.
	NoPlugins bool
	// GoPlugins are statically linked extensions supplied by the embedding app.
	GoPlugins []publicplugin.Plugin
	// MCPServers are explicit stdio or Streamable HTTP MCP servers.
	MCPServers []publicmcp.ServerSpec
	// NoMCP disables configured and explicit MCP servers.
	NoMCP bool
	// SkillDirs adds trusted Agent Skills discovery roots.
	SkillDirs []string
	// NoSkills disables Agent Skills discovery and activation tools.
	NoSkills bool
	// EnableSubagents opts into independent role-scoped child agents. Mutation
	// and recursion remain controlled only by config.
	EnableSubagents  bool
	DisableSubagents bool
	// SubagentProvider and SubagentModel override the configured defaults for children.
	SubagentProvider       string
	SubagentModel          string
	SubagentMaxConcurrency int
	SubagentMaxAgents      int
	SubagentMaxDepth       int
	// UserInputHandler answers direct ask_user tool calls. When nil, calls fail
	// fast with an unavailable-input tool result instead of blocking.
	UserInputHandler func(context.Context, protocol.UserInputRequest) (protocol.UserInputResponse, error)
}

// Session is an opened agent session.
type Session struct {
	mu     sync.RWMutex
	app    *app.App
	ctx    context.Context
	closed bool
}

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
	if opts.DisableSandbox && opts.RequireSandbox {
		return nil, errors.New("snowsdk: DisableSandbox and RequireSandbox conflict")
	}
	a, err := app.New(ctx, app.Options{
		CWD:                     opts.CWD,
		Provider:                opts.Provider,
		Model:                   opts.Model,
		SessionPath:             opts.SessionPath,
		NoSession:               opts.NoSession,
		AuthPath:                opts.AuthPath,
		ConfigPath:              opts.ConfigPath,
		DisableSandbox:          opts.DisableSandbox,
		RequireSandbox:          opts.RequireSandbox,
		Permission:              effectivePermission(opts),
		Tools:                   opts.Tools,
		SystemPrompt:            opts.SystemPrompt,
		Thinking:                opts.Thinking,
		ReasoningSummary:        opts.ReasoningSummary,
		TextVerbosity:           opts.TextVerbosity,
		CollaborationMode:       opts.CollaborationMode,
		PlanModeReasoningEffort: opts.PlanModeReasoningEffort,
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

// PromptWithMode atomically switches mode and starts the prompt.
func (s *Session) PromptWithMode(ctx context.Context, text string, mode protocol.CollaborationMode) error {
	a, err := s.activeApp()
	if err != nil {
		return err
	}
	return a.Agent.PromptWithMode(ctx, text, mode)
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
	all := append([]config.Diagnostic(nil), a.Diagnostics...)
	themes, themeDiagnostics := config.LoadThemes(config.GlobalDir(), a.ProjectInputRoot, a.ProjectAllowed)
	_, keyDiagnostics := config.LoadKeybindings(config.GlobalDir(), a.ProjectInputRoot, a.ProjectAllowed)
	selected := a.Cfg.TUI.Theme
	if selected != "default" && selected != "dark" && selected != "light" && selected != "high-contrast" {
		if _, ok := themes[selected]; !ok {
			themeDiagnostics = append(themeDiagnostics, config.Diagnostic{Path: "tui.theme", Message: "selected custom theme is missing or invalid: " + selected})
		}
	}
	all = append(all, themeDiagnostics...)
	all = append(all, keyDiagnostics...)
	out := make([]protocol.ConfigDiagnostic, 0, len(all))
	for _, d := range all {
		out = append(out, protocol.ConfigDiagnostic{Path: d.Path, Message: d.Message})
	}
	return out, nil
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

// Fork creates and activates a durable branch at an existing entry ID. The
// next Prompt appends the first divergent child to that branch.
func (s *Session) Fork(fromEntryID string) (protocol.SessionBranch, error) {
	a, err := s.activeApp()
	if err != nil {
		return protocol.SessionBranch{}, err
	}
	return a.ForkBranch(fromEntryID)
}

// ForkNamed creates a durable branch from an explicit source with an optional display name.
func (s *Session) ForkNamed(sourceBranchID, fromEntryID, name string) (protocol.SessionBranch, error) {
	a, err := s.activeApp()
	if err != nil {
		return protocol.SessionBranch{}, err
	}
	return a.ForkBranchWithOptions(protocol.BranchForkOptions{SourceBranchID: sourceBranchID, FromEntryID: fromEntryID, Name: name})
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

// Subscribe registers an event listener; returns an unsubscribe func.
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

// SkillInfo is the dependency-light Agent Skills catalog exposed to SDK users.
type SkillInfo struct {
	Name          string            `json:"name"`
	Description   string            `json:"description"`
	License       string            `json:"license,omitempty"`
	Compatibility string            `json:"compatibility,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	AllowedTools  string            `json:"allowed_tools,omitempty"`
	Location      string            `json:"location"`
	Scope         string            `json:"scope"`
	Source        string            `json:"source"`
	Enabled       bool              `json:"enabled"`
	DisabledBy    string            `json:"disabled_by,omitempty"`
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
	return a.Agent.Messages()
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

// SandboxStatus returns the fixed, secret-free Bash execution boundary for this session.
func (s *Session) SandboxStatus() publicsandbox.Status {
	a, err := s.activeApp()
	if err != nil {
		return publicsandbox.Status{Backend: "host"}
	}
	return a.SandboxStatus()
}

// CWD returns the session working directory.
func (s *Session) CWD() string {
	a, err := s.activeApp()
	if err != nil {
		return ""
	}
	return a.CWD()
}

// Close releases resources. Subsequent calls return ErrStopped.
func (s *Session) Close() error {
	if s == nil {
		return ErrStopped
	}
	s.mu.Lock()
	if s.closed || s.app == nil {
		s.mu.Unlock()
		return ErrStopped
	}
	s.closed = true
	a := s.app
	s.mu.Unlock()
	return a.Close()
}

// Convenience helpers

// MustOpen panics on error; for tests and tiny scripts.
func MustOpen(ctx context.Context, opts Options) *Session {
	s, err := Open(ctx, opts)
	if err != nil {
		panic(err)
	}
	return s
}

// RunPrompt is a one-shot helper: open, prompt, collect text deltas, close.
// Returns the accumulated assistant text.
func RunPrompt(ctx context.Context, opts Options, prompt string) (result string, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	s, err := Open(ctx, opts)
	if err != nil {
		return "", err
	}
	defer func() { err = errors.Join(err, s.Close()) }()

	var out []byte
	s.Subscribe(func(ev protocol.AgentEvent) {
		// One-shot output is the root assistant response. Child streams are
		// attributed and remain available through Subscribe, but must not be
		// concatenated into the convenience result.
		if ev.Agent == nil && ev.Type == protocol.EvTextDelta {
			out = append(out, ev.Text...)
		}
	})
	if err := s.ReadySubagents(); err != nil {
		return "", err
	}
	if err := s.Prompt(ctx, prompt); err != nil {
		return "", err
	}
	if a, err := s.activeApp(); err == nil {
		if err := a.Agent.WaitGoal(ctx); err != nil {
			return "", err
		}
		if err := a.WaitSubagentsIdle(ctx); err != nil {
			return "", err
		}
		if err := a.Agent.DrainEvents(ctx); err != nil {
			return "", err
		}
	}
	return string(out), nil
}

var (
	// ErrNotRunning is returned when an operation needs a running turn.
	ErrNotRunning = errors.New("snowsdk: no running turn")
	// ErrStopped is returned after Close.
	ErrStopped = errors.New("snowsdk: session closed")
)
