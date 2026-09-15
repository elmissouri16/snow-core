//go:build !darwin && !linux

package auth

import "os"

func openHostStatusFile(*os.Root, string) (*os.File, error) { return nil, ErrHostStatusUnavailable }
