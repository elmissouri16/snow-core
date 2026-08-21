//go:build darwin || linux

package mcp

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

func TestBoundedCommandTransportUsesDedicatedProcessGroup(t *testing.T) {
	cmd := exec.Command("/bin/cat")
	transport := &boundedCommandTransport{command: cmd, maxMessageBytes: 1024}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	connection, err := transport.Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Fatal("MCP subprocess was not configured as a process-group leader")
	}
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
}
