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
	"strings"
	"time"
)

// Options configures the manager network boundary. Live agents still require
// explicit project activation in every network mode.
type Options struct {
	Listen       string
	Version      string
	ManagerDir   string // Empty keeps the shell runtime-free and non-persistent.
	Executable   string // Absolute, operator-selected Snow binary; never project PATH.
	SessionsRoot string // Resolve once before a worker changes CWD.
}

// Validate checks configuration without starting a listener or a runtime.
func (o Options) Validate() error {
	_, err := o.deployment()
	return err
}

// Run serves until canceled. Starting this shell performs no agent, provider,
// project configuration, session, plugin, or MCP initialization.
func Run(ctx context.Context, opts Options, output io.Writer) error {
	if err := opts.Validate(); err != nil {
		return err
	}
	deployment, err := opts.deployment()
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", deployment.listen.String())
	if err != nil {
		return fmt.Errorf("web: listen: %w", err)
	}
	defer listener.Close()
	var loopbackListener net.Listener
	if deployment.mode == deploymentTrustedLANHTTP {
		loopbackAddress, err := loopbackAddressFor(listener.Addr())
		if err != nil {
			return err
		}
		loopbackListener, err = (&net.ListenConfig{}).Listen(ctx, "tcp", loopbackAddress)
		if err != nil {
			return fmt.Errorf("web: listen on localhost: %w", err)
		}
		defer loopbackListener.Close()
	}
	origin := deployment.publicOrigin
	if deployment.mode == deploymentTrustedLANHTTP {
		origin = "http://" + listener.Addr().String()
	}
	if origin == "" {
		origin = "http://" + listener.Addr().String()
	}
	shell, err := newShellWithNetwork(origin, opts.Version, deployment.policy())
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
	// The pairing secret is a deliberate operator credential, never a URL
	// parameter, access log entry, diagnostic field, or provider event.
	profile, listenerNotice, networkNotice := "local preview", "", "Remote access is disabled."
	switch deployment.mode {
	case deploymentTrustedLANHTTP:
		profile = "trusted-LAN HTTP"
		listenerNotice = "LAN URL: " + origin + "\nLocal URL: http://" + loopbackListener.Addr().String() + " (redirects to the LAN URL)\n"
		networkNotice = "Traffic is unencrypted; use only on a trusted LAN. Pairing is still required, and interface/firewall policy remains operator-managed."
	}
	privateAddresses, addressErr := hostPrivateAddresses()
	addressNotice := privateAddressNotice(deployment, privateAddresses, addressErr)
	if _, err := fmt.Fprintf(output, "Snow Manager %s — %s\n%s\n%s%sPairing code (reusable until %s or rotated; survives restart): %s\nNo agent starts until explicit activation. %s Snow has no process sandbox.\n", safeVersion(opts.Version), profile, shell.origin, listenerNotice, addressNotice, shell.access.pairExpires.UTC().Format(time.RFC3339), shell.initialCode, networkNotice); err != nil {
		return err
	}
	shell.initialCode = ""
	servers := []serverBinding{{
		server:   managerHTTPServer(ctx, shell.handler()),
		listener: listener,
	}}
	if loopbackListener != nil {
		localOrigin := "http://" + loopbackListener.Addr().String()
		servers = append(servers, serverBinding{
			server:   managerHTTPServer(ctx, localRedirectHandler(localOrigin, origin)),
			listener: loopbackListener,
		})
	}
	return serveHTTP(ctx, servers)
}

type serverBinding struct {
	server   *http.Server
	listener net.Listener
}

func managerHTTPServer(ctx context.Context, handler http.Handler) *http.Server {
	return &http.Server{
		Handler: handler,
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
		// Do not log attacker-controlled URLs, request fields or credentials.
		ErrorLog: log.New(io.Discard, "", 0),
	}
}

func serveHTTP(ctx context.Context, bindings []serverBinding) error {
	results := make(chan error, len(bindings))
	for _, binding := range bindings {
		go func() {
			results <- binding.server.Serve(binding.listener)
		}()
	}
	remaining := len(bindings)
	var firstErr error
	select {
	case firstErr = <-results:
		remaining--
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, binding := range bindings {
		if err := binding.server.Shutdown(shutdownCtx); err != nil {
			_ = binding.server.Close()
		}
	}
	for range remaining {
		err := <-results
		if firstErr == nil || errors.Is(firstErr, http.ErrServerClosed) {
			firstErr = err
		}
	}
	if firstErr == nil || errors.Is(firstErr, http.ErrServerClosed) {
		return nil
	}
	return firstErr
}

func loopbackAddressFor(address net.Addr) (string, error) {
	_, port, err := net.SplitHostPort(address.String())
	if err != nil {
		return "", fmt.Errorf("web: derive localhost listener: %w", err)
	}
	return net.JoinHostPort("127.0.0.1", port), nil
}

func localRedirectHandler(localOrigin, publicOrigin string) http.Handler {
	localHost := strings.TrimPrefix(localOrigin, "http://")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Host != localHost || r.URL.IsAbs() || r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "Unexpected localhost request", http.StatusForbidden)
			return
		}
		http.Redirect(w, r, publicOrigin+r.URL.RequestURI(), http.StatusTemporaryRedirect)
	})
}

func safeVersion(version string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r >= 0x7f && r <= 0x9f {
			return -1
		}
		return r
	}, version)
}
