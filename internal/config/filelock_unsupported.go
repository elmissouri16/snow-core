//go:build !darwin && !linux

package config

import (
	"errors"
	"os"
)

func lockConfigFile(*os.File) error {
	return errors.New("snow requires macOS or Linux")
}

func unlockConfigFile(*os.File) {}
