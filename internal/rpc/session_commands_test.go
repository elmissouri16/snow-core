package rpc

import (
	"bytes"
	jsonv1 "encoding/json"
	json "encoding/json/v2"
	"path/filepath"
	"testing"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestRPCIndependentSessionManagementByID(t *testing.T) {
	cwd := t.TempDir()
	t.Setenv("SNOW_HOME", filepath.Join(t.TempDir(), "home"))
	t.Setenv("SNOW_SESSIONS_DIR", filepath.Join(t.TempDir(), "sessions"))
	a, err := app.New(t.Context(), app.Options{
		Provider: "fake", Permission: "allow", CWD: cwd,
		NoPlugins: true, NoMCP: true, NoSkills: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	var output bytes.Buffer
	srv := New(t.Context(), a, bytes.NewReader(nil), &output)

	originalID := a.Session.ID()
	handleSessionRequest(t, srv, &output, Request{
		ID: "rename-original", Type: "session_rename",
		Params: jsonv1.RawMessage(`{"name":"original"}`),
	})
	created := handleSessionRequest(t, srv, &output, Request{ID: "create", Type: "session_create"})
	createdID, _ := created["session_id"].(string)
	if createdID == "" || createdID == originalID || created["active"] != true {
		t.Fatalf("session_create data = %+v, original ID = %q", created, originalID)
	}
	if _, found := created["path"]; found {
		t.Fatalf("session_create exposed a database path: %+v", created)
	}
	handleSessionRequest(t, srv, &output, Request{
		ID: "rename-created", Type: "session_rename",
		Params: jsonv1.RawMessage(`{"name":"created"}`),
	})

	listed := handleSessionRequest(t, srv, &output, Request{ID: "list", Type: "sessions_list"})
	sessions, ok := listed["sessions"].([]any)
	if !ok || len(sessions) != 2 {
		t.Fatalf("sessions_list data = %+v", listed)
	}
	active := 0
	for _, raw := range sessions {
		summary := raw.(map[string]any)
		if summary["active"] == true {
			active++
		}
		if _, found := summary["path"]; found {
			t.Fatalf("sessions_list exposed a database path: %+v", summary)
		}
	}
	if active != 1 {
		t.Fatalf("sessions_list active count = %d, sessions = %+v", active, sessions)
	}

	opened := handleSessionRequest(t, srv, &output, Request{
		ID: "open", Type: "session_open",
		Params: jsonv1.RawMessage(`{"session_id":"` + originalID + `"}`),
	})
	if opened["session_id"] != originalID || opened["active"] != true || a.Session.ID() != originalID {
		t.Fatalf("session_open data = %+v, active = %q", opened, a.Session.ID())
	}
	renamed := handleSessionRequest(t, srv, &output, Request{
		ID: "rename-inactive", Type: "session_rename",
		Params: jsonv1.RawMessage(`{"session_id":"` + createdID + `","name":"inactive renamed"}`),
	})
	if renamed["session_id"] != createdID || renamed["name"] != "inactive renamed" || a.Session.ID() != originalID {
		t.Fatalf("session_rename data = %+v, active = %q", renamed, a.Session.ID())
	}
	deleted := handleSessionRequest(t, srv, &output, Request{
		ID: "delete", Type: "session_delete",
		Params: jsonv1.RawMessage(`{"session_id":"` + createdID + `"}`),
	})
	if deleted["session_id"] != createdID || deleted["deleted"] != true {
		t.Fatalf("session_delete data = %+v", deleted)
	}

	err = srv.handle(t.Context(), Request{
		ID: "delete-active", Type: "session_delete",
		Params: jsonv1.RawMessage(`{"session_id":"` + originalID + `"}`),
	})
	if err == nil || rpcErrorCode(err) != "session_busy" {
		t.Fatalf("session_delete(active) error = %v, code = %q", err, rpcErrorCode(err))
	}
	err = srv.handle(t.Context(), Request{
		ID: "open-deleted", Type: "session_open",
		Params: jsonv1.RawMessage(`{"session_id":"` + createdID + `"}`),
	})
	if err == nil || rpcErrorCode(err) != "not_found" {
		t.Fatalf("session_open(deleted) error = %v, code = %q", err, rpcErrorCode(err))
	}
}

func handleSessionRequest(t *testing.T, srv *Server, output *bytes.Buffer, request Request) map[string]any {
	t.Helper()
	output.Reset()
	if err := srv.handle(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	var response protocol.RPCResponse
	if err := json.Unmarshal(output.Bytes(), &response); err != nil {
		t.Fatalf("decode response %q: %v", output.String(), err)
	}
	if !response.Success || response.Command != request.Type {
		t.Fatalf("response = %+v", response)
	}
	data, err := json.Marshal(response.Data)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	return object
}
