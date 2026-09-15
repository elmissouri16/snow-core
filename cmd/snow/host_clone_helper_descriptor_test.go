//go:build darwin || linux

package main

import (
	"bytes"
	json "encoding/json/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/hostops"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// runPrivateHostHelper bypasses only the worker transport for descriptor/payload
// tests; the actual early CLI entrypoint and Git supervisor remain unchanged.
func runPrivateHostHelper(t *testing.T, child *os.File, payload []byte) []byte {
	t.Helper()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	defer write.Close()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, hostops.HelperArgument)
	cmd.Env = []string{"PATH=/usr/bin:/bin"}
	cmd.ExtraFiles = []*os.File{child, read}
	cmd.Stdin = bytes.NewReader(payload)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	_ = read.Close()
	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()
	select {
	case err := <-wait:
		if err != nil {
			t.Fatal("private helper failed", err)
		}
	case <-time.After(12 * time.Second):
		_ = write.Close()
		_ = cmd.Process.Kill()
		t.Fatal("private helper timeout")
	}
	if output.Len() > 256 || bytes.Contains(output.Bytes(), []byte("FICTIONAL_SECRET")) {
		t.Fatal("helper output not fixed and bounded")
	}
	return output.Bytes()
}

func privateHostPayload(t *testing.T, git string, child protocol.HostDirectoryIdentity) []byte {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"git_executable": git,
		"start":          protocol.RPCProjectCloneStartParams{OperationID: "fictional-descriptor", Child: child, URL: "https://example.invalid/fictional"},
		"deadline":       time.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func TestHostCloneHelperUsesOpenedDirectoryNotReboundPath(t *testing.T) {
	_, git := newHostCloneFixture(t, "success")
	prepared := prepareHostFixture(t, git)
	identity := prepared.Result().Child
	child, err := os.Open(identity.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close()
	original := identity.Path + "-original"
	if err := os.Rename(identity.Path, original); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(identity.Path, 0700); err != nil {
		t.Fatal(err)
	}
	output := runPrivateHostHelper(t, child, privateHostPayload(t, git, identity))
	if !bytes.Contains(output, []byte(`"status":"succeeded"`)) {
		t.Fatal(string(output))
	}
	if _, err := os.Stat(filepath.Join(original, "FICTIONAL_PARTIAL")); err != nil {
		t.Fatal("clone missed opened directory", err)
	}
	if entries, err := os.ReadDir(identity.Path); err != nil || len(entries) != 0 {
		t.Fatal("clone mutated replacement", entries, err)
	}
}

func TestHostCloneHelperRejectsReboundParentDescriptor(t *testing.T) {
	fixture, git := newHostCloneFixture(t, "success")
	prepared := prepareHostFixture(t, git)
	identity := prepared.Result().Child
	wrong, err := os.Open(prepared.Result().Parent.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer wrong.Close()
	output := runPrivateHostHelper(t, wrong, privateHostPayload(t, git, identity))
	if !bytes.Contains(output, []byte(`"status":"failed"`)) {
		t.Fatal(string(output))
	}
	if _, err := os.Stat(filepath.Join(fixture, "record")); !os.IsNotExist(err) {
		t.Fatal("Git ran on mismatched descriptor", err)
	}
}

func TestHostCloneHelperRejectsPopulatedDirectoryWithoutDeleting(t *testing.T) {
	fixture, git := newHostCloneFixture(t, "success")
	prepared := prepareHostFixture(t, git)
	identity := prepared.Result().Child
	if err := os.WriteFile(filepath.Join(identity.Path, "keep"), []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	child, err := os.Open(identity.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close()
	output := runPrivateHostHelper(t, child, privateHostPayload(t, git, identity))
	if !bytes.Contains(output, []byte(`"status":"failed"`)) {
		t.Fatal(string(output))
	}
	if _, err := os.Stat(filepath.Join(fixture, "record")); !os.IsNotExist(err) {
		t.Fatal("Git ran in populated directory", err)
	}
	if data, err := os.ReadFile(filepath.Join(identity.Path, "keep")); err != nil || string(data) != "preserve" {
		t.Fatal("existing content changed", err)
	}
}

func TestHostCloneHelperStrictBoundedPayloadAndPrivateFDs(t *testing.T) {
	for _, kind := range []string{"unknown", "duplicate", "oversized", "trailing", "expired"} {
		t.Run(kind, func(t *testing.T) {
			fixture, git := newHostCloneFixture(t, "success")
			prepared := prepareHostFixture(t, git)
			identity := prepared.Result().Child
			child, err := os.Open(identity.Path)
			if err != nil {
				t.Fatal(err)
			}
			defer child.Close()
			payload := privateHostPayload(t, git, identity)
			switch kind {
			case "unknown":
				payload = append([]byte(`{"unexpected":true,`), payload[1:]...)
			case "duplicate":
				payload = append([]byte(`{"git_executable":"/not-used",`), payload[1:]...)
			case "oversized":
				payload = []byte(strings.Repeat(" ", 17<<10))
			case "trailing":
				payload = append(payload, []byte(` {}`)...)
			case "expired":
				var data map[string]any
				if err := json.Unmarshal(payload, &data); err != nil {
					t.Fatal(err)
				}
				data["deadline"] = time.Now().Add(-time.Minute)
				payload, err = json.Marshal(data)
				if err != nil {
					t.Fatal(err)
				}
			}
			output := runPrivateHostHelper(t, child, payload)
			if bytes.Contains(output, []byte(`"status":"succeeded"`)) {
				t.Fatal(string(output))
			}
			if _, err := os.Stat(filepath.Join(fixture, "record")); !os.IsNotExist(err) {
				t.Fatal("Git ran for invalid payload", err)
			}
		})
	}
	t.Run("missing-descriptors", func(t *testing.T) {
		binary, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(binary, hostops.HelperArgument)
		cmd.Env = []string{"PATH=/usr/bin:/bin"}
		cmd.Stdin = strings.NewReader(`{}`)
		output, err := cmd.CombinedOutput()
		if err != nil || !bytes.Contains(output, []byte(`"status":"failed"`)) {
			t.Fatal(string(output), err)
		}
	})
}
