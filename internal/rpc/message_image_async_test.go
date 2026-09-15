package rpc

import (
	"context"
	json "encoding/json/v2"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// This regression exercises the real scanner/dispatcher and real app admission,
// not a fake worker. The image cannot complete until the test releases admission.
func TestMessageImageServeDoesNotBlockAbort(t *testing.T) {
	a := editRPCApp(t)
	message := rpcImageMessage(t)
	if err := a.Session.Append(session.Entry{Type: session.EntryMessage, ID: message.ID, Message: &message}); err != nil {
		t.Fatal(err)
	}
	release := sync.OnceFunc(a.Agent.LockAdmission())
	defer release()
	input, send := io.Pipe()
	defer send.Close()
	writer := &controlFrameWriter{frames: make(chan []byte, 32)}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	srv := New(ctx, a, input, writer)
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		release()
		_ = send.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("image server did not join during cleanup")
		}
	})
	if frame := readControlTestFrame(t, writer); frame["type"] != protocol.RPCTypeReady {
		t.Fatalf("ready=%v", frame)
	}
	params, err := json.Marshal(protocol.RPCMessageImageParams{SessionID: a.Session.ID(), MessageID: message.ID, Index: 1})
	if err != nil {
		t.Fatal(err)
	}
	request, err := json.Marshal(Request{ID: "blocked-image", Type: "message_image", Params: params})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(send, string(request)+"\n"+`{"id":"stop-now","type":"abort"}`+"\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case raw := <-writer.frames:
		var frame map[string]any
		if err := json.Unmarshal(raw, &frame); err != nil {
			t.Fatal(err)
		}
		if frame["id"] != "stop-now" || frame["success"] != true {
			t.Fatalf("abort did not respond before image admission released: %s", raw)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("image read blocked Serve from dispatching abort")
	}
	if len(srv.imageReadSlots) != 1 {
		t.Fatal("image was not outstanding behind held admission")
	}
	release()
	frame := readControlTestFrame(t, writer)
	if frame["id"] != "blocked-image" || frame["success"] != true {
		t.Fatalf("image did not complete after release: %v", frame)
	}
}

func TestMessageImageServeBoundsPoolAndJoinsCanceledReads(t *testing.T) {
	a := editRPCApp(t)
	release := sync.OnceFunc(a.Agent.LockAdmission())
	defer release()
	input, send := io.Pipe()
	defer send.Close()
	writer := &controlFrameWriter{frames: make(chan []byte, 32)}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	srv := New(ctx, a, input, writer)
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx) }()
	joined := false
	t.Cleanup(func() {
		cancel()
		release()
		_ = send.Close()
		if !joined {
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Error("image server did not join during cleanup")
			}
		}
	})
	if frame := readControlTestFrame(t, writer); frame["type"] != protocol.RPCTypeReady {
		t.Fatalf("ready=%v", frame)
	}
	params, err := json.Marshal(protocol.RPCMessageImageParams{SessionID: a.Session.ID(), MessageID: "user", Index: 1})
	if err != nil {
		t.Fatal(err)
	}
	request, err := json.Marshal(Request{ID: "image", Type: "message_image", Params: params})
	if err != nil {
		t.Fatal(err)
	}
	for range maxConcurrentImageReads + 1 {
		if _, err := io.WriteString(send, string(request)+"\n"); err != nil {
			t.Fatal(err)
		}
	}
	frame := readControlTestFrame(t, writer)
	if frame["success"] != false || !strings.Contains(frame["error"].(string), "too many outstanding message_image reads") {
		t.Fatalf("unbounded image pool or wrong rejection: %v", frame)
	}
	if len(srv.imageReadSlots) != maxConcurrentImageReads {
		t.Fatalf("outstanding image reads=%d", len(srv.imageReadSlots))
	}
	// EOF must cancel admission waiters and join them while admission stays held.
	if err := send.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		joined = true
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("EOF failed to cancel and join image admission waiters")
	}
	if len(srv.imageReadSlots) != 0 {
		t.Fatal("Serve returned with outstanding image workers")
	}
	for range maxConcurrentImageReads {
		frame := readControlTestFrame(t, writer)
		if frame["id"] != "image" || frame["success"] != false || frame["error_code"] != "canceled" {
			t.Fatalf("expected canceled image without bytes: %v", frame)
		}
		if _, ok := frame["data"]; ok {
			t.Fatal("canceled image returned data")
		}
	}
}
