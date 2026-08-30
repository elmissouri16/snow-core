package snowsdk

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDebugStatusJSONOmitsZeroStart(t *testing.T) {
	data, err := json.Marshal(DebugStatus{})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(`"started_at"`)) {
		t.Fatalf("zero debug status unexpectedly includes started_at: %s", data)
	}
}

func TestSDKDebugControlsAndDump(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	s, err := Open(context.Background(), Options{Provider: "fake", NoSession: true, PermissionMode: "deny", CWD: t.TempDir(), NoPlugins: true, NoMCP: true, NoSkills: true, EnableDebug: true})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Prompt(context.Background(), "sdk diagnostic content"); err != nil {
		t.Fatal(err)
	}
	path, err := s.CreateDebugDump(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "sdk diagnostic content") {
		t.Fatalf("dump lacks session content: %s", data)
	}
	status, err := s.DebugStatus()
	if err != nil || !status.Enabled || status.EventCount == 0 {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	if err := s.SetDebugEnabled(false); err != nil {
		t.Fatal(err)
	}
	if status, err := s.DebugStatus(); err != nil || status.Enabled {
		t.Fatalf("disabled status=%+v err=%v", status, err)
	}
	if err := s.ClearDebugEvents(context.Background()); err != nil {
		t.Fatal(err)
	}
	if status, err := s.DebugStatus(); err != nil || status.EventCount != 0 {
		t.Fatalf("cleared status=%+v err=%v", status, err)
	}
}

func TestSDKAutomaticDebugDumpOnClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "close-dump.json")
	s, err := Open(context.Background(), Options{Provider: "fake", NoSession: true, PermissionMode: "deny", CWD: t.TempDir(), NoPlugins: true, NoMCP: true, NoSkills: true, DebugDumpPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Prompt(context.Background(), "content before close"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "content before close") {
		t.Fatalf("automatic dump lacks prompt: %s", data)
	}
}

func TestSDKDebugOptionsRejectConflict(t *testing.T) {
	if _, err := Open(context.Background(), Options{Provider: "fake", NoSession: true, PermissionMode: "deny", CWD: t.TempDir(), EnableDebug: true, DisableDebug: true}); err == nil {
		t.Fatal("conflicting debug options unexpectedly accepted")
	}
}
