//go:build !darwin && !linux

package auth

import (
	"context"
	"os"
)

func hostKeyRevision(os.FileInfo) (string, error)        { return "", ErrHostKeyUnavailable }
func openHostKeyLock(*os.Root, string) (*os.File, error) { return nil, ErrHostKeyUnavailable }
func lockHostKeyFile(context.Context, *os.File) error    { return ErrHostKeyUnavailable }
