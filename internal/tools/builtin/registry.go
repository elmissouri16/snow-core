package builtin

import (
	"time"

	"github.com/snow-core/snow/internal/tools"
)

// Options configure the builtin tool set.
type Options struct {
	// MaxOutputBytes caps tool output (read, bash). 0 means default.
	MaxOutputBytes int
	// BashTimeout caps bash execution. 0 means default.
	BashTimeout time.Duration
	// Roots are the allowed path roots for file tools. If empty, file tools
	// are created without a guard and deny all paths unless the host provides
	// roots at call time.
	Roots []string
}

// RegisterBuiltins registers read, write, edit, bash into reg.
func RegisterBuiltins(reg tools.Registry, opts Options) error {
	cwd := ""
	guard := NewPathGuard(opts.Roots, cwd)

	read := NewRead(guard)
	write := NewWrite(guard)
	edit := NewEdit(guard)
	bash := NewBash()

	if opts.MaxOutputBytes > 0 {
		read.MaxOutputBytes = opts.MaxOutputBytes
		bash.MaxOutputBytes = opts.MaxOutputBytes
	}
	if opts.BashTimeout > 0 {
		bash.Timeout = opts.BashTimeout
	}

	for _, t := range []tools.Tool{read, write, edit, bash} {
		if err := reg.Register(t); err != nil {
			return err
		}
	}
	return nil
}
