//go:build !darwin && !linux

package sandbox

import (
	"context"
	"errors"
)

func lockStoreFileContext(context.Context, string) (func(), error) {
	return nil, errors.New("sandbox: Snow requires macOS or Linux")
}
