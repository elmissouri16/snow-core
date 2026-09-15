// Package web provides the optional browser surface. It does not construct
// agents, open session stores, or import runtime implementations.
package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

// Options configures the local-only manager. Live agents require explicit
// project activation; remote access remains deliberately unavailable.
type Options struct {
	Listen       string
	Version      string
	ManagerDir   string // Empty keeps the shell runtime-free and non-persistent.
	Executable   string // Absolute, operator-selected Snow binary; never project PATH.
	SessionsRoot string // Resolve once before a worker changes CWD.
	TLSCertFile  string // Optional absolute operator-supplied PEM certificate path.
	TLSKeyFile   string // Required with TLSCertFile; never reloaded while serving.
}

// Validate checks configuration without starting a listener or a runtime.
func (o Options) Validate() error {
	address := o.Listen
	if address == "" {
		address = "127.0.0.1:7331"
	}
	endpoint, err := netip.ParseAddrPort(address)
	if err != nil || !endpoint.Addr().IsLoopback() || endpoint.Addr().Zone() != "" {
		return errors.New("web: --web-listen must be a numeric loopback address and port (for example 127.0.0.1:7331); remote access is not enabled")
	}
	return o.validateTLSPaths()
}

// Run serves until canceled. Starting this shell performs no agent, provider,
// project configuration, session, plugin, or MCP initialization.
func Run(ctx context.Context, opts Options, output io.Writer) error {
	if err := opts.Validate(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	tlsConfig, err := loadTLSConfig(ctx, opts)
	if err != nil {
		return err
	}
	address := opts.Listen
	if address == "" {
		address = "127.0.0.1:7331"
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("web: listen: %w", err)
	}
	defer listener.Close()
	scheme := "http"
	if tlsConfig != nil {
		scheme = "https"
	}
	origin := scheme + "://" + listener.Addr().String()
	shell, err := newShell(origin, opts.Version)
	if err != nil {
		return err
	}
	if opts.ManagerDir != "" {
		registry, err := OpenRegistry(ctx, opts.ManagerDir)
		if err != nil {
			return err
		}
		defer registry.Close()
		shell.registry = registry
		if err := shell.restoreAccess(ctx, registry); err != nil {
			return err
		}
		shell.hostSettings = NewWorkerControl(opts.Executable, registry.path, registry)
		shell.catalog = newWorkerCatalog(opts.Executable, opts.SessionsRoot)
		shell.runtimes = NewRuntimeManager(ctx, opts.Executable, opts.SessionsRoot, registry)
		defer shell.runtimes.Close()
		operations, err := NewProjectOperations(ctx, registry, NewWorkerProjectOperations(opts.Executable, registry.path))
		if err != nil {
			return err
		}
		shell.operations = operations
		defer operations.Close()
	}
	// The pairing secret is a deliberate local operator credential, never a URL
	// parameter, access log entry, diagnostic field, or provider event.
	if _, err := fmt.Fprintf(output, "Snow Manager %s — local preview\n%s\nPairing code (reusable until %s or rotated; survives restart): %s\nNo agent starts until explicit activation. Remote access is disabled. Snow has no process sandbox.\n", safeVersion(opts.Version), shell.origin, shell.access.pairExpires.UTC().Format(time.RFC3339), shell.initialCode); err != nil {
		return err
	}
	shell.initialCode = ""
	server := &http.Server{
		TLSConfig: tlsConfig,
		Handler:   shell.handler(), BaseContext: func(net.Listener) context.Context { return ctx }, ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second,
		IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10,
		// Do not log attacker-controlled URLs, request fields or credentials.
		ErrorLog: log.New(io.Discard, "", 0),
	}
	finished := make(chan struct{})
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := server.Shutdown(shutdownCtx); err != nil {
				_ = server.Close()
			}
		case <-finished:
		}
	}()
	if tlsConfig != nil {
		// Empty paths deliberately prevent ServeTLS from reading certificate files.
		// The bounded, identity-checked PEM pair is already loaded in TLSConfig.
		err = server.ServeTLS(listener, "", "")
	} else {
		err = server.Serve(listener)
	}
	close(finished)
	<-shutdownDone
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func safeVersion(version string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r >= 0x7f && r <= 0x9f {
			return -1
		}
		return r
	}, version)
}
