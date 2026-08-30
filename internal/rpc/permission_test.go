package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/pkg/protocol"
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
	for frame := range strings.SplitSeq(strings.TrimSpace(out.String()), "\n") {
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

	// Subscribe before starting authorization so a fast broker publication
	// cannot be missed and leave the test blocked forever.
	requestIDs := make(chan string, 1)
	unsub := a.Agent.Subscribe(func(ev protocol.AgentEvent) {
		if ev.Type == protocol.EvPermissionRequest && ev.Permission != nil {
			requestIDs <- ev.Permission.Request.ID
		}
	})
	defer unsub()

	requirePermission := make(chan error, 1)
	requireDecision := make(chan string, 1)
	go func() {
		d, err := a.Perm.Authorize(t.Context(), permissionRequest("perm-e2e"))
		requireDecision <- string(d)
		requirePermission <- err
	}()

	var reqID string
	select {
	case reqID = <-requestIDs:
	case <-time.After(2 * time.Second):
		t.Fatal("permission_request was not published")
	}

	var out bytes.Buffer
	srv := New(context.Background(), a, strings.NewReader(""), &out)
	line := `{"id":"rp1","type":"permission_reply","params":{"request_id":"` + reqID + `","decision":"allow"}}`
	if err := srv.handle(context.Background(), mustRequestPB(t, line)); err != nil {
		t.Fatalf("handle: %v", err)
	}
	select {
	case err := <-requirePermission:
		if err != nil {
			t.Fatalf("authorize err: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("authorization did not resolve")
	}
	select {
	case d := <-requireDecision:
		if d != "allow" {
			t.Fatalf("decision = %s, want allow", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("authorization decision was not returned")
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
	for frame := range strings.SplitSeq(strings.TrimSpace(out.String()), "\n") {
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
