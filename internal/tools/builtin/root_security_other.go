//go:build darwin || linux

package builtin

import "os"

func preserveRootedReplacementSecurity(_ *os.Root, _ string, _ *os.File) error {
	return nil
}
