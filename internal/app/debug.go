package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/diagnostics"
	"github.com/elmissouri16/snow-core/internal/session"
	publicmcp "github.com/elmissouri16/snow-core/pkg/mcp"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// DebugStatus describes shared runtime capture. LastDumpPath is populated only
// by surfaces that retain the result of CreateDebugDump themselves.
type DebugStatus struct {
	diagnostics.Status
}

type debugRuntimeSnapshot struct {
	SnowVersion       string                     `json:"snow_version"`
	GoVersion         string                     `json:"go_version"`
	OS                string                     `json:"os"`
	Architecture      string                     `json:"architecture"`
	WorkingDirectory  string                     `json:"working_directory"`
	Provider          string                     `json:"provider"`
	Model             protocol.Model             `json:"model"`
	Thinking          protocol.ThinkingLevel     `json:"thinking"`
	ReasoningSummary  protocol.ReasoningSummary  `json:"reasoning_summary"`
	TextVerbosity     protocol.TextVerbosity     `json:"text_verbosity"`
	CollaborationMode protocol.CollaborationMode `json:"collaboration_mode"`
	PermissionMode    string                     `json:"permission_mode"`
	ProjectAllowed    bool                       `json:"project_allowed"`
	Debug             config.DebugConfig         `json:"debug"`
	ConfigDiagnostics []config.Diagnostic        `json:"config_diagnostics,omitempty"`
	MCPStatuses       any                        `json:"mcp_statuses,omitempty"`
}

type debugSessionSnapshot struct {
	Header         session.Header           `json:"header"`
	Path           string                   `json:"path,omitempty"`
	BranchTip      string                   `json:"branch_tip,omitempty"`
	ActiveBranchID string                   `json:"active_branch_id,omitempty"`
	Branches       []protocol.SessionBranch `json:"branches,omitempty"`
	Entries        any                      `json:"entries,omitempty"`
	Messages       any                      `json:"messages"`
}

type debugDump struct {
	Format                    string                    `json:"format"`
	Warning                   string                    `json:"warning"`
	CreatedAt                 time.Time                 `json:"created_at"`
	Runtime                   debugRuntimeSnapshot      `json:"runtime"`
	Recorder                  diagnostics.Status        `json:"recorder"`
	Events                    []diagnostics.EventRecord `json:"events"`
	Session                   debugSessionSnapshot      `json:"session"`
	ProviderDataBlocksOmitted int                       `json:"provider_data_blocks_omitted"`
	RedactionNotes            []string                  `json:"redaction_notes"`
}

// DebugStatus returns current recorder counters.
func (a *App) DebugStatus() DebugStatus {
	if a == nil || a.Debugger == nil {
		return DebugStatus{MaxEvents: diagnostics.MaxEventRecords, MaxBytes: diagnostics.MaxEventBytes}
	}
	return DebugStatus{Status: a.Debugger.Status()}
}

// SetDebugEnabled changes capture for the current runtime. Persistence is a
// surface concern so SDK and RPC callers do not unexpectedly rewrite config.
func (a *App) SetDebugEnabled(enabled bool) {
	if a == nil || a.Debugger == nil {
		return
	}
	a.Debugger.SetEnabled(enabled)
}

// ClearDebugEvents discards retained event records while preserving enablement.
func (a *App) ClearDebugEvents(ctx context.Context) error {
	if a == nil || a.Debugger == nil {
		return nil
	}
	return a.Debugger.Clear(ctx)
}

// CreateDebugDump writes a private, bounded, self-diagnostic JSON snapshot.
// Dumps require an idle agent so session entries and messages have a stable
// turn boundary.
func (a *App) CreateDebugDump(ctx context.Context, path string) (string, error) {
	if a == nil || a.Debugger == nil || a.Agent == nil {
		return "", errors.New("diagnostics: runtime unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// Match the App transaction lock order used by session/provider controls,
	// then hold agent admission so no prompt or branch transition can begin
	// while the session projection is read.
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	unlockAdmission := a.Agent.LockAdmission()
	defer unlockAdmission()
	store, err := a.Agent.IdleSessionAdmitted("create a diagnostic dump")
	if err != nil {
		if a.Agent.IsRunning() {
			return "", errors.New("diagnostics: wait for the current turn to finish")
		}
		return "", fmt.Errorf("diagnostics: acquire stable session: %w", err)
	}
	if err := a.Agent.DrainEvents(ctx); err != nil {
		return "", fmt.Errorf("diagnostics: drain events: %w", err)
	}
	records, recorderStatus, err := a.Debugger.Snapshot(ctx)
	if err != nil {
		return "", fmt.Errorf("diagnostics: snapshot events: %w", err)
	}

	removed := 0
	for index := range records {
		sanitized, count, err := diagnostics.SanitizeJSONWithSecrets(records[index].Event, a.diagnosticSecrets)
		if err != nil {
			return "", fmt.Errorf("diagnostics: sanitize event %d: %w", index, err)
		}
		records[index].Event = sanitized
		removed += count
	}

	messages, err := store.Messages()
	if err != nil {
		return "", fmt.Errorf("diagnostics: read session messages: %w", err)
	}
	safeMessages, count, err := sanitizeDumpValue(messages, a.diagnosticSecrets)
	if err != nil {
		return "", fmt.Errorf("diagnostics: sanitize session messages: %w", err)
	}
	removed += count

	snapshot := debugSessionSnapshot{
		Header: store.Header(), Path: store.Path(), BranchTip: store.BranchTip(),
		Messages: safeMessages,
	}
	if active, ok := store.(session.ActiveBranchStore); ok {
		snapshot.ActiveBranchID = active.ActiveBranchID()
	}
	if branches, ok := store.(session.BranchStore); ok {
		snapshot.Branches, err = branches.Branches()
		if err != nil {
			return "", fmt.Errorf("diagnostics: read branches: %w", err)
		}
	}
	if entries, ok := store.(session.BranchEntryStore); ok {
		branchEntries, entryErr := entries.BranchEntries()
		if entryErr != nil {
			return "", fmt.Errorf("diagnostics: read branch entries: %w", entryErr)
		}
		snapshot.Entries, count, err = sanitizeDumpValue(branchEntries, a.diagnosticSecrets)
		if err != nil {
			return "", fmt.Errorf("diagnostics: sanitize branch entries: %w", err)
		}
		removed += count
	}

	providerID := a.ProviderID
	projectAllowed := a.ProjectAllowed
	configDiagnostics := slices.Clone(a.Diagnostics)
	statuses := slices.Clone(a.MCPStatuses)
	if a.MCPManager != nil {
		statuses = a.MCPManager.Statuses()
	}
	dump := debugDump{
		Format: diagnostics.FormatVersion, Warning: diagnostics.SharingWarning, CreatedAt: time.Now().UTC(),
		Runtime: debugRuntimeSnapshot{
			SnowVersion: a.BuildVersion, GoVersion: runtime.Version(), OS: runtime.GOOS, Architecture: runtime.GOARCH,
			WorkingDirectory: a.cwd, Provider: providerID, Model: a.Agent.Model(), Thinking: a.Agent.Thinking(),
			ReasoningSummary: a.Agent.ReasoningSummary(), TextVerbosity: a.Agent.TextVerbosity(), CollaborationMode: a.Agent.Mode(),
			PermissionMode: string(a.Perm.Mode()), ProjectAllowed: projectAllowed, Debug: config.DebugConfig{Enabled: recorderStatus.Enabled},
			ConfigDiagnostics: configDiagnostics, MCPStatuses: statuses,
		},
		Recorder: recorderStatus, Events: records, Session: snapshot, ProviderDataBlocksOmitted: removed,
		RedactionNotes: []string{
			"protocol provider_data blocks are omitted in full",
			"known authorization headers, API keys, tokens, client secrets, passwords, and private keys are replaced",
			"auth-store contents, configured environment values, explicit API credentials, and configured transport headers are excluded from runtime metadata",
		},
	}

	path, err = a.resolveDebugDumpPath(path, store.ID())
	if err != nil {
		return "", err
	}
	safeDump, _, err := sanitizeDumpValue(dump, a.diagnosticSecrets)
	if err != nil {
		return "", fmt.Errorf("diagnostics: sanitize dump metadata: %w", err)
	}
	if err := diagnostics.WriteJSONContext(ctx, path, safeDump); err != nil {
		return "", err
	}
	return path, nil
}

func sanitizeDumpValue(value any, secrets []string) (any, int, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, 0, err
	}
	safe, removed, err := diagnostics.SanitizeJSONWithSecrets(data, secrets)
	if err != nil {
		return nil, 0, err
	}
	var result any
	decoder := json.NewDecoder(strings.NewReader(string(safe)))
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil {
		return nil, 0, err
	}
	return result, removed, nil
}

func collectDiagnosticSecrets(opts Options, cfg config.Config, store auth.Store, service *auth.Service) []string {
	var secrets []string
	add := func(value string) {
		if strings.TrimSpace(value) != "" {
			secrets = append(secrets, value)
		}
	}
	add(opts.APIKey)
	if service != nil {
		for _, descriptor := range service.Providers() {
			if store != nil {
				if credential, ok := store.Get(descriptor.ProviderID); ok {
					add(credential.Key)
					add(credential.Access)
					add(credential.Refresh)
				}
			}
			for _, name := range descriptor.Environment {
				add(os.Getenv(name))
			}
		}
	}
	addMCP := func(spec publicmcp.ServerSpec) {
		for _, values := range []map[string]string{spec.Headers, spec.Env} {
			for _, value := range values {
				add(configuredSecretValue(value))
			}
		}
	}
	for _, spec := range cfg.MCPServers {
		addMCP(spec)
	}
	for _, spec := range opts.MCPServers {
		addMCP(spec)
	}
	addPluginEnv := func(values []string) {
		for _, value := range values {
			if _, secret, ok := strings.Cut(value, "="); ok {
				add(configuredSecretValue(secret))
			}
		}
	}
	for _, spec := range cfg.Plugins {
		addPluginEnv(spec.Env)
	}
	for _, spec := range opts.Plugins {
		addPluginEnv(spec.Env)
	}
	return secrets
}

func configuredSecretValue(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "${") && strings.HasSuffix(value, "}") {
		return os.Getenv(strings.TrimSuffix(strings.TrimPrefix(value, "${"), "}"))
	}
	return value
}

func debugFilenamePart(value string) string {
	var out strings.Builder
	out.Grow(len(value))
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			out.WriteRune(r)
		}
	}
	return out.String()
}

func (a *App) resolveDebugDumpPath(path, sessionID string) (string, error) {
	if strings.TrimSpace(path) == "" {
		id := debugFilenamePart(sessionID)
		if len(id) > 12 {
			id = id[:12]
		}
		if id == "" {
			id = "session"
		}
		name := fmt.Sprintf("snow-diagnostic-%s-%s.json", time.Now().UTC().Format("20060102T150405.000000000Z"), id)
		path = filepath.Join(config.GlobalDir(), "diagnostics", name)
	} else if !filepath.IsAbs(path) {
		path = filepath.Join(a.cwd, path)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("diagnostics: resolve dump path: %w", err)
	}
	if info, err := os.Lstat(filepath.Dir(absolute)); err == nil && !info.IsDir() {
		return "", fmt.Errorf("diagnostics: dump parent is not a directory: %s", filepath.Dir(absolute))
	}
	return absolute, nil
}
