//go:build darwin || linux

package plugin

import (
	"context"
	"testing"
	"time"

	publicplugin "github.com/elmissouri16/snow-core/pkg/plugin"
)

func TestExternalHostUsesDedicatedProcessGroup(t *testing.T) {
	host, err := SpawnExternal(context.Background(), publicplugin.PluginSpec{
		ID: "process-group", Command: []string{"/bin/cat"}, Enabled: true,
	}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if host.cmd.SysProcAttr == nil || !host.cmd.SysProcAttr.Setpgid {
		t.Fatal("external plugin was not configured as a process-group leader")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	_ = host.Close(ctx)
	select {
	case <-host.waitDone:
	case <-time.After(time.Second):
		t.Fatal("external plugin leader was not reaped")
	}
}
