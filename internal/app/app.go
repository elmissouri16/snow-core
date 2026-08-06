// Package app wires configuration, auth, session, tools, providers, and the
// agent into ready-to-run surfaces (CLI, TUI, print, SDK, RPC).
package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/snow-core/snow/internal/agent"
	"github.com/snow-core/snow/internal/auth"
	"github.com/snow-core/snow/internal/config"
	ctxpkg "github.com/snow-core/snow/internal/context"
	"github.com/snow-core/snow/internal/permission"
	"github.com/snow-core/snow/internal/provider"
	"github.com/snow-core/snow/internal/provider/fake"
	"github.com/snow-core/snow/internal/provider/opencodego"
	"github.com/snow-core/snow/internal/session"
	"github.com/snow-core/snow/internal/tools"
	"github.com/snow-core/snow/internal/tools/builtin"
	"github.com/snow-core/snow/pkg/protocol"
)

// App is the assembled runtime.
type App struct {
	Cfg       config.Config
	ConfigPath string
	AuthPath  string

	Auth     auth.Store
	Registry *tools.SimpleRegistry
	Provider provider.Provider
	ProviderID string
	Model    protocol.Model
	Perm     *permission.SimpleService
	Session  session.Store
	Agent    *agent.Agent

	cwd string
}

// Options control app assembly.
type Options struct {
	CWD         string
	ConfigPath  string
	AuthPath    string
	Provider    string
	Model       string
	APIKey      string
	Permission  string // ask|allow|deny
	SessionPath string // empty → create new; or existing .jsonl to resume
	Tools       []string // subset allowlist; empty = all builtins
	SystemPrompt string
	Thinking    string
	NoSession   bool // in-memory session (SDK ephemeral)
	UseFake     bool // force fake provider (demo/tests)
	BaseURL     string // provider base URL override (opencode-go)
}

// DefaultPaths resolves config/auth paths from the environment.
func DefaultPaths() (configPath, authPath string) {
	c, a, _ := config.DefaultPaths()
	return c, a
}

// New assembles the app.
func New(ctx context.Context, opts Options) (*App, error) {
	cwd := opts.CWD
	if cwd == "" {
		var err error
		cwd, err = getwd()
		if err != nil {
			return nil, fmt.Errorf("app: cwd: %w", err)
		}
	}
	absCWD, err := filepath.Abs(cwd)
	if err != nil {
		return nil, fmt.Errorf("app: abs cwd: %w", err)
	}

	configPath := opts.ConfigPath
	if configPath == "" {
		c, _, _ := config.DefaultPaths()
		configPath = c
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}
	// Apply CLI overrides.
	if opts.Provider != "" {
		cfg.DefaultProvider = opts.Provider
	}
	if opts.Model != "" {
		cfg.DefaultModel = opts.Model
	}
	if opts.Permission != "" {
		cfg.PermissionMode = opts.Permission
	}
	if opts.Thinking != "" {
		cfg.Thinking = opts.Thinking
	}
	if opts.BaseURL != "" {
		if cfg.Providers == nil {
			cfg.Providers = map[string]config.ProviderConfig{}
		}
		pc := cfg.Providers[cfg.DefaultProvider]
		pc.BaseURL = opts.BaseURL
		cfg.Providers[cfg.DefaultProvider] = pc
	}

	authPath := opts.AuthPath
	if authPath == "" {
		_, a, _ := config.DefaultPaths()
		authPath = a
	}
	var authStore auth.Store
	if opts.NoSession {
		authStore = auth.NewMemoryStore()
	} else {
		fs, err := auth.NewFileStore(authPath)
		if err != nil {
			return nil, fmt.Errorf("app: auth store: %w", err)
		}
		authStore = fs
	}

	// Tools.
	reg := tools.NewRegistry()
	toolOpts := builtin.Options{
		MaxOutputBytes: cfg.ToolOutputLimit(),
		BashTimeout:    cfg.BashTimeout(),
		Roots:          []string{absCWD},
	}
	builtin.RegisterBuiltins(reg, toolOpts)

	// Provider.
	providerID := cfg.DefaultProvider
	if providerID == "" {
		providerID = "opencode-go"
	}
	var prov provider.Provider
	switch providerID {
	case "fake":
		prov = fake.NewWithModels(nil)
	case "opencode-go":
		ocCfg := opencodego.Config{APIKey: opts.APIKey}
		if pc, ok := cfg.Providers[providerID]; ok {
			ocCfg.BaseURL = pc.BaseURL
			ocCfg.DefaultModel = pc.DefaultModel
		}
		if opts.BaseURL != "" {
			ocCfg.BaseURL = opts.BaseURL
		}
		oc, err := opencodego.New(ocCfg)
		if err != nil {
			return nil, fmt.Errorf("app: opencode-go: %w", err)
		}
		prov = oc
	default:
		return nil, fmt.Errorf("app: unsupported provider %q", providerID)
	}

	// Session.
	var st session.Store
	if opts.NoSession {
		st = session.NewMemoryStore(session.Options{CWD: absCWD})
	} else if opts.SessionPath != "" {
		st, err = session.NewJSONLStore(opts.SessionPath, absCWD, session.Options{})
		if err != nil {
			return nil, fmt.Errorf("app: open session: %w", err)
		}
	} else {
		idx := session.NewFileIndex(session.DefaultSessionsRoot())
		st, err = idx.Create(absCWD)
		if err != nil {
			return nil, fmt.Errorf("app: create session: %w", err)
		}
	}

	// Permission service (deny-by-default headless; TUI replaces asker).
	permMode := permission.Mode(cfg.PermissionMode)
	if permMode != permission.ModeAsk && permMode != permission.ModeAllow && permMode != permission.ModeDeny {
		permMode = permission.ModeDeny
	}
	perm := permission.NewService(permMode, permission.DenyAll{})

	// Model resolution.
	model := protocol.Model{Provider: providerID, ID: cfg.DefaultModel, SupportsTools: true}
	if model.ID == "" {
		models, err := prov.ListModels(ctx)
		if err == nil && len(models) > 0 {
			model = models[0]
		} else if err == nil {
			model.ID = "default"
		} else {
			model.ID = "default"
		}
	}

	// Host (path roots + progress bridge).
	host := &toolHost{cwd: absCWD, roots: []string{absCWD}, perm: perm, reg: reg}

	// Context assembly.
	loader := ctxpkg.NewLoader(cfg.ContextCapBytes, false)
	assembly := loader.Assemble(absCWD, opts.SystemPrompt, "")
	systemPrompt := assembly.Render()

	ag, err := agent.New(agent.Options{
		Provider:     prov,
		Registry:     reg,
		Session:      st,
		Permission:   perm,
		ToolHost:     host,
		SystemPrompt: systemPrompt,
		Model:        model,
		Auth:         authStore,
		APIKey:       opts.APIKey,
	})
	if err != nil {
		return nil, fmt.Errorf("app: agent: %w", err)
	}

	a := &App{
		Cfg:        cfg,
		ConfigPath: configPath,
		AuthPath:   authPath,
		Auth:       authStore,
		Registry:   reg,
		Provider:   prov,
		ProviderID: providerID,
		Model:      model,
		Perm:       perm,
		Session:    st,
		Agent:      ag,
		cwd:        absCWD,
	}
	return a, nil
}

// CWD returns the app working directory.
func (a *App) CWD() string { return a.cwd }

func getwd() (string, error) { return os.Getwd() }

// Close releases the session.
func (a *App) Close() error {
	return a.Session.Close()
}

// ErrNoProvider is returned when a requested provider is not configured.
var ErrNoProvider = errors.New("app: no provider")

// toolHost adapts the tools.ToolHost contract to app state.
type toolHost struct {
	cwd   string
	roots []string
	perm  permission.Service
	reg   *tools.SimpleRegistry
}

func (h *toolHost) CWD() string             { return h.cwd }
func (h *toolHost) Roots() []string         { return h.roots }
func (h *toolHost) Permission() permission.Service { return h.perm }
func (h *toolHost) Environ() []string       { return nil }
func (h *toolHost) EmitProgress(ev tools.ToolProgressEvent) {}
