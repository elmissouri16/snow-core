package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	publicplugin "github.com/snow-core/snow/pkg/plugin"
)

func TestJavaScriptExamplePlugin(t *testing.T) {
	runtimePath, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	testExternalExample(t, "example-js", []string{runtimePath, examplePath(t, "javascript", "plugin.mjs")}, "node")
}

func TestPythonExamplePlugin(t *testing.T) {
	runtimePath, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 unavailable")
	}
	testExternalExample(t, "example-python", []string{runtimePath, "-u", examplePath(t, "python", "plugin.py")}, "python")
}

func TestJavaScriptSDKExamplePlugin(t *testing.T) {
	runtimePath, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	testExternalExample(t, "example-js-sdk", []string{runtimePath, examplePath(t, "javascript-sdk", "plugin.mjs")}, "node-sdk")
}

func TestPythonSDKExamplePlugin(t *testing.T) {
	runtimePath, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 unavailable")
	}
	testExternalExample(t, "example-python-sdk", []string{runtimePath, "-u", examplePath(t, "python-sdk", "plugin.py")}, "python-sdk")
}

func examplePath(t *testing.T, language, name string) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "..", "examples", "plugins", language, name))
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func testExternalExample(t *testing.T, id string, command []string, runtimeName string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	host, err := SpawnExternal(ctx, publicplugin.PluginSpec{ID: id, Command: command, Enabled: true}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close(context.Background())
	initialized, err := host.Initialize(ctx, "test", host.WorkingDir(), "session", []string{"tools", "events"})
	if err != nil {
		t.Fatalf("initialize: %v; diagnostics: %s", err, host.Diagnostics())
	}
	if initialized.Manifest.ID != id || len(initialized.Tools) != 1 || initialized.Tools[0].Risk != "read" {
		t.Fatalf("initialize = %+v", initialized)
	}
	if !host.SupportsEvent(publicplugin.EventToolEnd) || host.SupportsEvent(publicplugin.EventTextDelta) {
		t.Fatalf("supported events = %+v", initialized.SupportedEvents)
	}
	progress := make(chan ProgressNotification, 4)
	result, err := host.Call(ctx, "echo", "example-call", json.RawMessage(`{"text":"hello"}`), func(update ProgressNotification) {
		progress <- update
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 1 || result.Content[0].Text != "hello" {
		t.Fatalf("content = %+v", result.Content)
	}
	if !strings.Contains(string(result.Details), `"runtime":"`+runtimeName+`"`) {
		t.Fatalf("details = %s", result.Details)
	}
	select {
	case update := <-progress:
		if update.CallID != "example-call" {
			t.Fatalf("progress = %+v", update)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("missing progress")
	}

	if _, err := host.Call(ctx, "echo", "", json.RawMessage(`{"text":"missing id"}`), nil); err == nil {
		t.Fatal("empty call id was accepted")
	}

	callCtx, cancelCall := context.WithCancel(context.Background())
	cancelProgress := make(chan ProgressNotification, 1)
	cancelled := make(chan error, 1)
	go func() {
		_, callErr := host.Call(callCtx, "echo", "cancel-call", json.RawMessage(`{"text":"late","delay_ms":5000}`), func(update ProgressNotification) {
			select {
			case cancelProgress <- update:
			default:
			}
		})
		cancelled <- callErr
	}()
	select {
	case update := <-cancelProgress:
		if update.CallID != "cancel-call" {
			t.Fatalf("cancel progress = %+v", update)
		}
		cancelCall()
	case <-time.After(2 * time.Second):
		cancelCall()
		t.Fatal("cancelled call did not start")
	}
	select {
	case err = <-cancelled:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled call error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled call did not return")
	}

	result, err = host.Call(ctx, "echo", "after-cancel", json.RawMessage(`{"text":"still alive"}`), nil)
	if err != nil || len(result.Content) != 1 || result.Content[0].Text != "still alive" {
		t.Fatalf("post-cancel result = %+v, error = %v", result, err)
	}
}
