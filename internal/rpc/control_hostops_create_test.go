//go:build darwin || linux

package rpc

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/hostops"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// Use the production lazy adapter and ServeControl, not a fake host service.
// These Git selections cannot execute or contact any network endpoint.
func TestControlHostCreateWithUnavailableConfiguredGit(t *testing.T) {
	for _, kind := range []string{"missing", "non-executable"} {
		t.Run(kind, func(t *testing.T) {
			cwd, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			helper, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			git := filepath.Join(t.TempDir(), "fictional-git")
			if kind == "non-executable" {
				if err := os.WriteFile(git, []byte("not executable"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			info, err := os.Stat(cwd)
			if err != nil {
				t.Fatal(err)
			}
			stat := info.Sys().(*syscall.Stat_t)
			parent := protocol.HostDirectoryIdentity{Path: cwd, Device: strconv.FormatUint(uint64(stat.Dev), 10), Inode: strconv.FormatUint(uint64(stat.Ino), 10)}
			operations := NewControlHostOperations(hostops.Options{GitExecutable: git, HelperExecutable: helper})
			input, writer := io.Pipe()
			ctx, cancel := context.WithCancel(t.Context())
			output := &controlFrameWriter{frames: make(chan []byte, 32)}
			done := make(chan error, 1)
			go func() {
				done <- ServeControl(ctx, input, output, cwd, "", &controlTestService{}, ControlOptions{HostOperations: operations})
			}()
			t.Cleanup(func() {
				defer cancel()
				_ = writer.Close()
				select {
				case err := <-done:
					if err != nil && !errors.Is(err, context.Canceled) {
						t.Errorf("ServeControl cleanup: %v", err)
					}
				case <-time.After(5 * time.Second):
					t.Error("ServeControl did not stop")
				}
			})
			_ = readControlTestFrame(t, output) // Ready precedes mutation admission.
			writeControlTestRequest(t, writer, "prepare", "project_prepare", protocol.RPCProjectPrepareParams{OperationID: "create-no-git", Parent: parent, Leaf: "project"})
			response := readControlTestFrame(t, output)
			if response["success"] != true {
				t.Fatalf("CREATE rejected solely because Git is unavailable: %v", response)
			}
			data, err := json.Marshal(response["data"])
			if err != nil {
				t.Fatal(err)
			}
			var prepared protocol.RPCProjectPrepared
			if err := json.Unmarshal(data, &prepared); err != nil {
				t.Fatal(err)
			}
			if prepared.Parent != parent || prepared.Child.Path != filepath.Join(cwd, "project") {
				t.Fatal(prepared)
			}
			writeControlTestRequest(t, writer, "clone", "project_clone_start", protocol.RPCProjectCloneStartParams{OperationID: prepared.OperationID, Child: prepared.Child, URL: "https://example.invalid/fictional"})
			response = readControlTestFrame(t, output)
			if response["success"] != false || response["error_code"] != "unavailable" {
				t.Fatalf("clone must fail before execution: %v", response)
			}
			writeControlTestRequest(t, writer, "close", "project_cancel", protocol.RPCProjectCancelParams{OperationID: prepared.OperationID})
			response = readControlTestFrame(t, output)
			if response["success"] != true {
				t.Fatal(response)
			}
			entries, err := os.ReadDir(prepared.Child.Path)
			if err != nil || len(entries) != 0 {
				t.Fatalf("created destination was not preserved: %v, %v", entries, err)
			}
		})
	}
}
