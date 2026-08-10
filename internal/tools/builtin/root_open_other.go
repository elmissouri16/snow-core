//go:build !unix

package builtin

import "os"

const rootedReadOnlyFlags = os.O_RDONLY
