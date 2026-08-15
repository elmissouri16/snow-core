//go:build darwin || linux

package builtin

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestGracefulManagedProcessSendsSIGINTBeforeKill(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "interrupted")
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	script := "trap 'printf int > " + strconv.Quote(marker) + "; exit 0' INT; while :; do sleep 1; done"
	cmd := exec.CommandContext(ctx, "sh", "-c", script)
	managed, err := startManagedProcess(cmd, true)
	if err != nil {
		t.Fatal(err)
	}
	err = cmd.Wait()
	managed.close()
	if err == nil && ctx.Err() == nil {
		t.Fatal("command exited without cancellation")
	}
	data, readErr := os.ReadFile(marker)
	if readErr != nil || string(data) != "int" {
		t.Fatalf("SIGINT marker = %q, err=%v, wait=%v", data, readErr, err)
	}
}
