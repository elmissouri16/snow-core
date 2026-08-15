package builtin

import (
	"context"
	"os/exec"
	"time"

	"github.com/snow-core/snow/internal/config"
	"github.com/snow-core/snow/internal/tools"
)

// Options configure the builtin tool set.
type Options struct {
	// MaxOutputBytes caps tool output (read, bash, grep, glob, webfetch). 0 means default.
	MaxOutputBytes int
	// SearchMaxMatches caps grep matches. 0 means the grep default.
	SearchMaxMatches int
	// GlobMaxResults caps glob paths. 0 means the glob default.
	GlobMaxResults int
	// BashTimeout caps bash execution. 0 means default.
	BashTimeout time.Duration
	// BashCommandFactory optionally routes Bash through a fail-closed backend.
	BashCommandFactory func(context.Context, string, []string, time.Duration) (*exec.Cmd, bool, bool, error)
	BashSandboxActive  func() bool
	SearchPolicy       config.EffectiveSearchPolicy
	// Roots are the allowed path roots for file tools. If empty, file tools
	// are created without a guard and use host roots at call time.
	Roots []string
	// CWD anchors relative paths for a pinned root set.
	CWD string
	// Guard reuses a host-owned pinned root set. When set it takes precedence
	// over Roots/CWD and must outlive all registered tools.
	Guard *PathGuard
}

// RegisterBuiltins registers the default file, search, shell, and deferred web tools into reg.
func RegisterBuiltins(reg tools.Registry, opts Options) error {
	guard := opts.Guard
	if guard == nil && len(opts.Roots) > 0 {
		cwd := opts.CWD
		if cwd == "" {
			cwd = opts.Roots[0]
		}
		guard = NewPathGuard(opts.Roots, cwd)
	}

	read := NewRead(guard)
	write := NewWrite(guard)
	edit := NewEdit(guard)
	bash := NewBash()
	bash.CommandFactory = opts.BashCommandFactory
	bash.SandboxActive = opts.BashSandboxActive
	grep := NewGrep(guard)
	grep.Policy = opts.SearchPolicy
	glob := NewGlob(guard)
	glob.Policy = opts.SearchPolicy
	webfetch := NewWebFetch()
	askUser := NewAskUser()
	requestUserInput := NewRequestUserInput()
	updatePlan := NewUpdatePlan()

	if opts.MaxOutputBytes > 0 {
		read.MaxOutputBytes = opts.MaxOutputBytes
		bash.MaxOutputBytes = opts.MaxOutputBytes
		grep.MaxOutputBytes = opts.MaxOutputBytes
		glob.MaxOutputBytes = opts.MaxOutputBytes
		webfetch.MaxOutputBytes = opts.MaxOutputBytes
	}
	if opts.SearchMaxMatches > 0 {
		grep.MaxMatches = opts.SearchMaxMatches
	}
	if opts.GlobMaxResults > 0 {
		glob.MaxResults = opts.GlobMaxResults
	}
	if opts.BashTimeout > 0 {
		bash.Timeout = opts.BashTimeout
	}

	for _, t := range []tools.Tool{read, write, edit, bash, grep, glob, askUser, requestUserInput, updatePlan, webfetch} {
		if err := reg.Register(t); err != nil {
			return err
		}
	}
	return nil
}
