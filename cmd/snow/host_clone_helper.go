package main

import (
	"context"
	"os"

	"github.com/elmissouri16/snow-core/internal/hostops"
)

// runHostCloneHelperEarly must be called with os.Args[1:] before Cobra, config,
// app initialization, or ordinary CLI error rendering. The caller exits with
// code when handled. This private discriminant is not an RPC command endpoint.
func runHostCloneHelperEarly(args []string) (handled bool, code int) {
	if len(args) == 0 || args[0] != hostops.HelperArgument {
		return false, 0
	}
	if len(args) != 1 {
		return true, 1
	}
	// Package initialization can already have assigned fd3/fd4 to the Go
	// runtime. Pass raw numbers: validation must precede os.File ownership.
	return true, hostops.HelperMain(context.Background(), os.Stdin, os.Stdout, 3, 4)
}
