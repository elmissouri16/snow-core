package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func tlsWebCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "snow"}
	cmd.Flags().String("mode", "web", "")
	cmd.Flags().String("web-listen", "127.0.0.1:0", "")
	cmd.Flags().String("web-tls-cert", "", "")
	cmd.Flags().String("web-tls-key", "", "")
	return cmd
}

func TestWebTLSFlagValidation(t *testing.T) {
	for _, tc := range []struct {
		name       string
		args       []string
		subcommand bool
		valid      bool
	}{
		{name: "HTTP unchanged", valid: true},
		{name: "empty TLS pair", args: []string{"--web-tls-cert=", "--web-tls-key="}},
		{name: "empty TLS cert", args: []string{"--web-tls-cert="}},
		{name: "TLS pair", args: []string{"--web-tls-cert", "/operator/cert.pem", "--web-tls-key", "/operator/key.pem"}, valid: true},
		{name: "missing key", args: []string{"--web-tls-cert", "/operator/cert.pem"}},
		{name: "missing cert", args: []string{"--web-tls-key", "/operator/key.pem"}},
		{name: "relative cert", args: []string{"--web-tls-cert", "cert.pem", "--web-tls-key", "/operator/key.pem"}},
		{name: "relative key", args: []string{"--web-tls-cert", "/operator/cert.pem", "--web-tls-key", "key.pem"}},
		{name: "not web cert", args: []string{"--mode", "tui", "--web-tls-cert", "/operator/cert.pem"}},
		{name: "not web key", args: []string{"--mode", "tui", "--web-tls-key", "/operator/key.pem"}},
		{name: "empty flag not web", args: []string{"--mode", "tui", "--web-tls-key="}},
		{name: "subcommand", subcommand: true, args: []string{"--web-tls-cert", "/operator/cert.pem", "--web-tls-key", "/operator/key.pem"}},
		{name: "LAN remains forbidden", args: []string{"--web-listen", "0.0.0.0:7331", "--web-tls-cert", "/operator/cert.pem", "--web-tls-key", "/operator/key.pem"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := tlsWebCommand()
			if tc.subcommand {
				cmd.Use = "resume"
			}
			if err := cmd.ParseFlags(tc.args); err != nil {
				t.Fatal(err)
			}
			err := validateWebFlags(cmd, nil)
			if (err == nil) != tc.valid {
				t.Fatalf("validation = %v, valid=%v", err, tc.valid)
			}
		})
	}
}

func TestWebTLSDispatchPropagatesPathsWithoutRuntimeStartup(t *testing.T) {
	home, sessions := t.TempDir(), t.TempDir()
	t.Setenv("SNOW_HOME", home)
	t.Setenv("SNOW_SESSIONS_DIR", sessions)
	// If runWeb fails to propagate TLS files, it would initialize the manager
	// and print pairing output instead of rejecting these missing fixtures.
	cmd := tlsWebCommand()
	cert, key := filepath.Join(home, "missing-cert.pem"), filepath.Join(home, "missing-key.pem")
	if err := cmd.ParseFlags([]string{"--web-tls-cert", cert, "--web-tls-key", key}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	cmd.SetOut(&output)
	err := runWeb(t.Context(), cmd)
	if err == nil || !strings.Contains(err.Error(), "TLS certificate/key unavailable") || strings.Contains(err.Error(), home) {
		t.Fatalf("unsanitized or missing TLS rejection: %v", err)
	}
	if output.Len() != 0 {
		t.Fatal("TLS failure printed pairing output")
	}
	for _, path := range []string{home, sessions} {
		entries, err := os.ReadDir(path)
		if err != nil || len(entries) != 0 {
			t.Fatal("TLS failure initialized host storage")
		}
	}
}
