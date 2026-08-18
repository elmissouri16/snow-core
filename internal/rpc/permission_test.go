package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/snow-core/snow/internal/app"
	"github.com/snow-core/snow/internal/permission"
	"github.com/snow-core/snow/pkg/protocol"
)

func permissionRequest(id string) permission.Request {
	return permission.Request{Tool: "bash", Risk: permission.RiskExec, Reason: id}
}

func TestRPCPermissionReplyInvalidAndNoPending(t *testing.T) {
	a, err := app.New(context.Background(), app.Options{
		Provider: "fake", NoSession: true, CWD: t.TempDir(), Permission: "ask",
		NoPlugins: true, NoMCP: true, NoSkills: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.EnablePermissionReplies()

	input := strings.Join([]string{
		`{"id":"r1","type":"permission_reply","params":{"request_id":"perm-1","decision":"allow"}}`,
		`{"id":"r2","type":"permission_reject","params":{"request_id":"perm-1"}}`,
		`{"id":"r3","type":"permission_reply","params":{"request_id":"perm-1","decision":"bogus"}}`,
		`{"id":"r4","type":"permission_reply"}`,
		"",
	}, "\n")

	var out bytes.Buffer
	srv := New(context.Background(), a, strings.NewReader(input), &out)
	if err := srv.Serve(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"r1", "r2", "r3", "r4"} {
		assertResponseFailed(t, &out, id)
	}
}

func assertResponseFailed(t *testing.T, out *bytes.Buffer, id string) {
	t.Helper()
	for _, frame := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if frame == "" {
			continue
		}
		var resp Response
		if err := json.Unmarshal([]byte(frame), &resp); err != nil {
			continue
		}
		if resp.ID == id {
			if resp.Success {
				t.Fatalf("response %s unexpectedly succeeded", id)
			}
			return
		}
	}
	t.Fatalf("no failed response with id %s", id)
}

func TestRPCPermissionReplyEndToEnd(t *testing.T) {
	a, err := app.New(context.Background(), app.Options{
		Provider: "fake", NoSession: true, CWD: t.TempDir(), Permission: "ask",
		NoPlugins: true, NoMCP: true, NoSkills: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.EnablePermissionReplies()

	// Drive an ask-mode authorization in a goroutine; the broker blocks until
	// the RPC reply resolves it.
	requirePermission := make(chan error, 1)
	requireDecision := make(chan string, 1)
	go func() {
		d, err := a.Perm.Authorize(context.Background(), permissionRequest("perm-e2e"))
		requireDecision <- string(d)
		requirePermission <- err
	}()

	// Capture the published permission_request id.
	var reqID string
	done := make(chan struct{})
	unsub := a.Agent.Subscribe(func(ev protocol.AgentEvent) {
		if ev.Type == protocol.EvPermissionRequest && ev.Permission != nil {
			reqID = ev.Permission.Request.ID
			close(done)
		}
	})
	<-done
	unsub()

	var out bytes.Buffer
	srv := New(context.Background(), a, strings.NewReader(""), &out)
	line := `{"id":"rp1","type":"permission_reply","params":{"request_id":"` + reqID + `","decision":"allow"}}`
	if err := srv.handle(context.Background(), mustRequestPB(t, line)); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if err := <-requirePermission; err != nil {
		t.Fatalf("authorize err: %v", err)
	}
	if d := <-requireDecision; d != "allow" {
		t.Fatalf("decision = %s, want allow", d)
	}
	assertResponseSuccess(t, &out, "rp1")
}

func mustRequestPB(t *testing.T, line string) Request {
	t.Helper()
	var req Request
	if err := json.Unmarshal([]byte(line), &req); err != nil {
		t.Fatal(err)
	}
	return req
}

func assertResponseSuccess(t *testing.T, out *bytes.Buffer, id string) {
	t.Helper()
	for _, frame := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if frame == "" {
			continue
		}
		var resp Response
		if err := json.Unmarshal([]byte(frame), &resp); err != nil {
			continue
		}
		if resp.ID == id && resp.Success {
			return
		}
	}
	t.Fatalf("no successful response with id %s", id)
}
