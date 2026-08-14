//go:build darwin || linux

package builtin

import "os"

func renameReplace(from, to string) error { return os.Rename(from, to) }
