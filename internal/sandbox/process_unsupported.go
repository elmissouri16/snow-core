//go:build !darwin && !linux

package sandbox

import (
	"context"
	"errors"
)

func boundedCombinedOutput(context.Context, string, ...string) ([]byte, error) {
	return nil, errors.New("sandbox: Snow requires macOS or Linux")
}

func boundedCombinedOutputEnv(context.Context, []string, string, ...string) ([]byte, error) {
	return nil, errors.New("sandbox: Snow requires macOS or Linux")
}
