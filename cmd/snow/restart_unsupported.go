//go:build !unix

package main

import (
	"errors"

	"github.com/elmissouri16/snow-core/internal/tui"
)

func restartAfterUpdate(tui.RunResult, string, bool) error {
	return errors.New("restart: automatic restart is unavailable on this platform; launch Snow again manually")
}
