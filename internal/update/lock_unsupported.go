//go:build !darwin && !linux

package update

import (
	"context"
	"errors"
	"os"
)

func openUpdateLock(context.Context, *os.Root, string) (*os.File, error) {
	return nil, errors.New("update: self-update requires macOS or Linux")
}
