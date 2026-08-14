//go:build !darwin && !linux

package auth

import (
	"errors"
	"os"
)

func lockFile(*os.File) error {
	return errors.New("snow requires macOS or Linux")
}

func unlockFile(*os.File) {}
