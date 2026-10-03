package cli

import (
	"bufio"
	"bytes"
	json "encoding/json/v2"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// Inherited stdin is a blocking OS pipe in the child, unlike an os.Pipe
// created inside a Go process. Keep its writer open throughout cancellation.
func TestRPCShutdownWithIdleInheritedStdin(t *testing.T) {
	for _, startup := range []string{"catalog", "control", "eager"} {
		t.Run(startup, func(t *testing.T) {
			f := newControlCLIFixture(t)
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			args := []string{"-test.run=^TestControlCLIHelperProcess$", "--", "--mode", "rpc", "--rpc-startup", startup}
			if startup == "eager" {
				args = append(args, "--provider", "fake", "--permission", "deny", "--no-session", "--no-plugins", "--no-mcp", "--no-skills", "--no-subagents")
			}
			cmd := exec.CommandContext(t.Context(), executable, args...)
			cmd.Dir = f.cwd
			cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + f.home, "SNOW_HOME=" + f.snow,
				"SNOW_SESSIONS_DIR=" + f.sessions, "SNOW_CONTROL_TEST_HELPER=1", "TMPDIR=" + f.root}
			stdin, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			defer stdin.Close()
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			var waitErr error
			go func() {
				waitErr = cmd.Wait()
				close(done)
			}()
			t.Cleanup(func() {
				_ = cmd.Process.Kill()
				<-done
			})
			if err := stdout.(*os.File).SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
				t.Fatal(err)
			}
			reader := bufio.NewReader(stdout)
			line, err := reader.ReadBytes('\n')
			if err != nil {
				t.Fatalf("RPC ready: %v", err)
			}
			var ready struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(line, &ready); err != nil || ready.Type != "rpc_ready" {
				t.Fatalf("ready=%q error=%v", line, err)
			}
			// A response proves the command loop is active, rather than racing
			// cancellation against startup immediately after rpc_ready.
			if _, err := stdin.Write([]byte("{\"id\":\"idle\",\"type\":\"get_state\"}\n")); err != nil {
				t.Fatal(err)
			}
			for {
				line, err := reader.ReadBytes('\n')
				if err != nil {
					t.Fatalf("RPC response: %v", err)
				}
				var frame struct {
					Type string `json:"type"`
					ID   string `json:"id"`
				}
				if err := json.Unmarshal(line, &frame); err != nil {
					t.Fatal(err)
				}
				if frame.Type == "response" && frame.ID == "idle" {
					break
				}
			}
			if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
				t.Fatal(err)
			}
			select {
			case <-done:
				if waitErr != nil && !bytes.Contains(stderr.Bytes(), []byte("context canceled")) {
					t.Fatalf("shutdown error=%v stderr=%q", waitErr, stderr.String())
				}
			case <-time.After(5 * time.Second):
				t.Fatal("SIGTERM did not terminate RPC while inherited stdin remained open")
			}
		})
	}
}
