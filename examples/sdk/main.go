package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/snow-core/snow/pkg/protocol"
	"github.com/snow-core/snow/pkg/snowsdk"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "snow SDK example:", err)
		os.Exit(1)
	}
}

func run() (runErr error) {
	provider := flag.String("provider", "fake", "provider id (fake, opencode-go, or chatgpt)")
	prompt := flag.String("prompt", "Summarize this repository.", "prompt to send")
	timeout := flag.Duration("timeout", 2*time.Minute, "overall prompt timeout")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	session, err := snowsdk.Open(ctx, snowsdk.Options{
		Provider:         *provider,
		NoSession:        true,
		PermissionMode:   "deny",
		NoPlugins:        true,
		NoMCP:            true,
		NoSkills:         true,
		DisableSubagents: true,
	})
	if err != nil {
		return err
	}
	defer func() {
		runErr = errors.Join(runErr, session.Close())
	}()

	var sawText atomic.Bool
	var sawTurnDone atomic.Bool
	unsubscribe := session.Subscribe(func(event protocol.AgentEvent) {
		if event.Agent != nil {
			return
		}
		switch event.Type {
		case protocol.EvTextDelta:
			sawText.Store(true)
			fmt.Print(event.Text)
		case protocol.EvTurnDone:
			sawTurnDone.Store(true)
		case protocol.EvError:
			fmt.Fprintln(os.Stderr, "agent event:", event.Message)
		}
	})
	defer unsubscribe()

	// Constructors do not publish restored goal/subagent state until the host has
	// installed observers. Calling both readiness methods is safe for new sessions.
	if err := session.ReadyGoals(); err != nil {
		return err
	}
	if err := session.ReadySubagents(); err != nil {
		return err
	}
	if err := session.Prompt(ctx, *prompt); err != nil {
		return err
	}
	if !sawTurnDone.Load() {
		return errors.New("prompt completed without a turn_done event")
	}
	if sawText.Load() {
		fmt.Println()
	} else {
		fmt.Printf("prompt completed with %s/%s (the fake provider emits no text)\n", session.Model().Provider, session.Model().ID)
	}
	return nil
}
