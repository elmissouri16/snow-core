package rpc

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/elmissouri16/snow-core/internal/app"
)

// Main is the RPC entry point used by cmd/snow --mode rpc and embedders.
func Main(ctx context.Context, opts app.Options) (err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	a, err := app.New(ctx, opts)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, a.Close()) }()
	for _, diagnostic := range a.Diagnostics {
		fmt.Fprintf(os.Stderr, "config warning: %s: %s\n", diagnostic.Path, diagnostic.Message)
	}

	// Stream agent events to stdout as JSONL through the server's locked
	// writer so responses and events can never interleave corruptly.
	srv := New(ctx, a, newProcessInputReader(os.Stdin), newProcessOutputWriter(os.Stdout))
	if err := srv.announceReady(); err != nil {
		return err
	}
	stopEvents := srv.forwardAgentEvents()
	defer func() { err = errors.Join(err, stopEvents()) }()
	srv.write(a.Agent.StateEvent())
	if err := a.ReadyGoal(); err != nil {
		return err
	}
	if err := a.ReadySubagents(); err != nil {
		return err
	}
	return srv.Serve(ctx)
}

// MainWithVersion preserves the explicit-version entry point for embedders.
func MainWithVersion(ctx context.Context, opts app.Options, snowVersion string) error {
	opts.BuildVersion = snowVersion
	return Main(ctx, opts)
}
