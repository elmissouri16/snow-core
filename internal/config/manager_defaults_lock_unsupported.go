//go:build !darwin && !linux

package config

import (
	"context"
	"os"
)

func lockManagerFile(context.Context, *os.File) error { return ErrManagerUnavailable }

func openManagerReadFile(*os.Root, string) (*os.File, error) { return nil, ErrManagerUnavailable }
