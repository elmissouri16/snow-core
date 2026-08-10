package builtin

import (
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
	BashTimeout  time.Duration
	SearchPolicy config.EffectiveSearchPolicy
	WindowsShell WindowsShellOptions
	// Roots are the allowed path roots for file tools. If empty, file tools
	// are created without a guard and deny all paths unless the host provides
	// roots at call time.
	Roots []string
}

// RegisterBuiltins registers the default file, search, shell, and deferred web tools into reg.
func RegisterBuiltins(reg tools.Registry, opts Options) error {
	cwd := ""
	guard := NewPathGuard(opts.Roots, cwd)

	read := NewRead(guard)
	write := NewWrite(guard)
	edit := NewEdit(guard)
	bash := NewBash()
	bash.WindowsShell = opts.WindowsShell
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
