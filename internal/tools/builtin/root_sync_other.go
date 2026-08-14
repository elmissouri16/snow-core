//go:build !unix

package builtin

import "os"

// Non-Unix platforms do not expose portable directory syncing through os.File.
// Atomic replacement still holds, but crash durability follows platform rename
// semantics until a native rooted directory-flush implementation is available.
func syncRootedDirectory(_ *os.Root, _ string) error { return nil }
