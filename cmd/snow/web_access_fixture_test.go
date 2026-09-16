//go:build darwin || linux

package main

import (
	"bufio"
	"context"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/web"
)

const accessFixtureEnv = "SNOW_WEB_ACCESS_FIXTURE_DIR"

// Opt-in production HTTP fixture for native browser pairing/revocation. Unlike
// conversation fixtures this has no registered project, provider or worker: the
// manager-only browser-access surface must never activate any of them. Reusing
// the private root across fresh test processes exercises actual durable resume.
func TestWebAccessFixture(t *testing.T) {
	directory := os.Getenv(accessFixtureEnv)
	if directory == "" {
		t.Skip("opt-in native browser access fixture")
	}
	if !filepath.IsAbs(directory) {
		t.Fatal("fixture root must be absolute")
	}
	info, err := os.Stat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatal("fixture root must exist and be private")
	}
	directory, err = filepath.EvalSymlinks(directory)
	if err != nil {
		t.Fatal("fixture root unavailable")
	}
	home := filepath.Join(directory, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal("private fixture home unavailable")
	}
	t.Setenv("HOME", home)
	t.Setenv("SNOW_HOME", filepath.Join(home, ".snow"))
	t.Setenv("SNOW_SESSIONS_DIR", filepath.Join(directory, "sessions"))
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Second)
	defer cancel()
	startup := streamFixtureStartup{output: make(chan string, 1)}
	stopped := make(chan error, 1)
	go func() {
		stopped <- web.Run(ctx, web.Options{
			Listen: "127.0.0.1:0", ManagerDir: filepath.Join(directory, "manager"),
			Executable:   filepath.Join(directory, "worker-must-not-start"),
			SessionsRoot: filepath.Join(directory, "sessions"), Version: "browser-access-fixture",
		}, startup)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-stopped:
			if err != nil {
				t.Error("fixture manager shutdown failed")
			}
		case <-time.After(8 * time.Second):
			t.Error("fixture manager shutdown deadline")
		}
	})
	var output string
	select {
	case output = <-startup.output:
	case <-ctx.Done():
		t.Fatal("fixture startup deadline")
	}
	origin, ok := fixtureOrigin(output)
	if !ok {
		t.Fatal("private fixture listener unavailable")
	}
	code, ok := fixturePairingCode(output)
	if !ok {
		t.Fatal("private pairing credential unavailable")
	}
	ready, err := json.Marshal(map[string]string{"origin": origin, "code": code})
	if err != nil {
		t.Fatal("private fixture frame unavailable")
	}
	// The runner consumes/suppresses this credential-bearing private IPC frame.
	// No pre-paired cookie: native browser form submission must obtain authority.
	fmt.Fprintln(os.Stdout, "SNOW_ACCESS_READY "+string(ready))
	inputDone := make(chan struct{})
	go func() { scanner := bufio.NewScanner(os.Stdin); scanner.Scan(); close(inputDone) }()
	select {
	case <-inputDone:
	case <-ctx.Done():
		t.Fatal("fixture lifetime deadline")
	}
}
