package rpc

import (
	"bufio"
	"bytes"
	"context"
	json "encoding/json/v2"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func catalogFrames(t *testing.T, output string) []map[string]any {
	t.Helper()
	var frames []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		var frame map[string]any
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			t.Fatalf("invalid frame: %v", err)
		}
		frames = append(frames, frame)
	}
	return frames
}

func TestServeCatalogRuntimeFreeReadyAndNoInitialization(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Deliberately unusable runtime configuration/auth; catalog never reads it.
	if err := os.MkdirAll(filepath.Join(home, ".snow"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"config.json", "auth.json"} {
		if err := os.WriteFile(filepath.Join(home, ".snow", name), []byte("not valid JSON"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	root := filepath.Join(home, "sessions-not-created")
	var output bytes.Buffer
	err := ServeCatalog(t.Context(), strings.NewReader("{\"id\":\"list\",\"type\":\"catalog_sessions\"}\n"), &output, t.TempDir(), root, "catalog-test-version")
	if err != nil {
		t.Fatal(err)
	}
	frames := catalogFrames(t, output.String())
	if len(frames) != 2 {
		t.Fatalf("frames=%v", frames)
	}
	ready := frames[0]
	if ready["type"] != protocol.RPCTypeReady || ready["snow_version"] != "catalog-test-version" || ready["protocol_version"] != protocol.RPCProtocolVersion {
		t.Fatalf("ready=%v", ready)
	}
	if !reflect.DeepEqual(ready["capabilities"], []any{"runtime_free_catalog", "catalog_sessions", "catalog_messages", "catalog_public_tools", "catalog_image", "history_images"}) {
		t.Fatalf("capabilities=%v", ready["capabilities"])
	}
	if ready["max_input_bytes"] != float64(protocol.RPCMaxInputBytes) || frames[1]["success"] != true {
		t.Fatalf("frames=%v", frames)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("session root created: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(home, ".snow"))
	if err != nil || len(entries) != 2 {
		t.Fatalf("runtime files created: %v %v", entries, err)
	}
}

func TestServeCatalogRejectsExecutionMutationsAndInvalidRequests(t *testing.T) {
	requests := []string{
		`{"id":"prompt","type":"prompt","message":"do not execute"}`,
		`{"id":"create","type":"session_create"}`,
		`{"id":"delete","type":"session_delete","params":{"session_id":"x"}}`,
		`{"id":"open","type":"session_open","params":{"session_id":"x"}}`,
		`{"id":"live","type":"sessions_list"}`,
		`{"id":"badlimit","type":"catalog_sessions","params":{"limit":51}}`,
		`{"id":"negative","type":"catalog_sessions","params":{"offset":-1}}`,
		`{"id":"unknown","type":"catalog_sessions","params":{"path":"secret-file"}}`,
		`{"id":"missing","type":"catalog_messages"}`,
		`{"id":"unknown-session","type":"catalog_messages","params":{"session_id":"unknown"}}`,
		`{"id":"wrong-type","type":"catalog_sessions","params":{"offset":"secret-input"}}`,
		`{"type":"catalog_sessions","type":"prompt"}`,
		"\xff",
	}
	var output bytes.Buffer
	if err := ServeCatalog(t.Context(), strings.NewReader(strings.Join(requests, "\n")+"\n"), &output, t.TempDir(), t.TempDir(), ""); err != nil {
		t.Fatal(err)
	}
	frames := catalogFrames(t, output.String())
	if len(frames) != len(requests)+1 {
		t.Fatalf("frame count=%d", len(frames))
	}
	for i, frame := range frames[1:] {
		if frame["success"] != false {
			t.Fatalf("accepted request %d: %v", i, frame)
		}
		want := "invalid"
		if i < 5 {
			want = "unsupported"
		}
		if i == 9 {
			want = "not_found"
		}
		if frame["error_code"] != want {
			t.Fatalf("request %d error=%v want=%s", i, frame, want)
		}
	}
	if strings.Contains(output.String(), "secret-input") || strings.Contains(output.String(), "secret-file") || strings.Contains(output.String(), "do not execute") {
		t.Fatal("error reflected request content")
	}
}

func TestServeCatalogWireHistoryIsPlainTextOnly(t *testing.T) {
	root, cwd := t.TempDir(), t.TempDir()
	store, err := session.NewFileIndex(root).Create(cwd)
	if err != nil {
		t.Fatal(err)
	}
	message := protocol.Message{Role: protocol.RoleAssistant, Content: []protocol.ContentBlock{
		{Type: protocol.BlockText, Text: "public answer"},
		{Type: protocol.BlockThinking, Text: "THINKING_SECRET"},
		{Type: protocol.BlockProviderData, Text: "CONTINUITY_SECRET", Data: []byte("STATE_SECRET")},
		{Type: protocol.BlockToolCall, Name: "SECRET_TOOL", Arguments: []byte(`{"value":"ARGS_SECRET"}`)},
	}}
	if err := store.Append(session.Entry{ID: "assistant", Type: session.EntryMessage, Message: &message}); err != nil {
		t.Fatal(err)
	}
	id, path := store.ID(), store.Path()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	params, err := json.Marshal(protocol.RPCCatalogMessagesParams{SessionID: id, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	input := `{"id":"history","type":"catalog_messages","params":` + string(params) + "}\n"
	var output bytes.Buffer
	if err := ServeCatalog(t.Context(), strings.NewReader(input), &output, cwd, root, "test"); err != nil {
		t.Fatal(err)
	}
	frames := catalogFrames(t, output.String())
	if frames[1]["success"] != true {
		t.Fatalf("response=%v", frames[1])
	}
	data := frames[1]["data"].(map[string]any)
	messages := data["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("messages=%v", messages)
	}
	entry := messages[0].(map[string]any)
	if len(entry) != 5 || entry["role"] != "assistant" || entry["text"] != "public answer" {
		t.Fatalf("entry=%v", entry)
	}
	for _, hidden := range []string{"SECRET", "provider_data", "tool_call", path, root} {
		if strings.Contains(output.String(), hidden) {
			t.Fatalf("wire leaked %q", hidden)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("session mutated: %v", err)
	}
}

func TestServeCatalogTransportBoundsAndCancellation(t *testing.T) {
	t.Run("cancel pipe", func(t *testing.T) {
		in, writer := io.Pipe()
		defer writer.Close()
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { done <- ServeCatalog(ctx, in, io.Discard, t.TempDir(), t.TempDir(), "test") }()
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancel=%v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("catalog cancellation blocked")
		}
	})
	t.Run("deadline input", func(t *testing.T) {
		in := &deadlineOnlyReader{readStarted: make(chan struct{}), interrupted: make(chan struct{})}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- ServeCatalog(ctx, in, io.Discard, t.TempDir(), t.TempDir(), "test") }()
		<-in.readStarted
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancel=%v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("deadline cancellation blocked")
		}
	})
	t.Run("unavailable input deadline", func(t *testing.T) {
		if err := ServeCatalog(t.Context(), &unavailableDeadlineReader{}, io.Discard, "", "", ""); err == nil {
			t.Fatal("accepted unavailable deadline")
		}
	})
	t.Run("unavailable output deadline", func(t *testing.T) {
		out := &unavailableDeadlineWriter{}
		if err := ServeCatalog(t.Context(), strings.NewReader(""), out, "", "", ""); err == nil || out.wrote {
			t.Fatalf("err=%v wrote=%v", err, out.wrote)
		}
	})
	t.Run("write failure", func(t *testing.T) {
		failure := errors.New("output failure")
		out := &failAfterWriter{err: failure}
		if err := ServeCatalog(t.Context(), strings.NewReader("{\"type\":\"prompt\"}\n"), out, "", "", ""); !errors.Is(err, failure) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("frame bound", func(t *testing.T) {
		in := strings.NewReader(strings.Repeat("x", protocol.RPCMaxInputBytes+1) + "\n")
		if err := ServeCatalog(t.Context(), in, io.Discard, "", "", ""); !errors.Is(err, bufio.ErrTooLong) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("unbounded input", func(t *testing.T) {
		in := io.NopCloser(strings.NewReader(""))
		if err := ServeCatalog(t.Context(), in, io.Discard, "", "", ""); err == nil {
			t.Fatal("accepted uninterruptible transport")
		}
	})
}

func TestCatalogEncodedPageBoundIncludesJSONEscaping(t *testing.T) {
	response := Response{ID: strings.Repeat("\x00", 1024), Type: "response", Command: "catalog_messages", Success: true, Data: protocol.RPCCatalogMessagesPage{
		Messages: []protocol.RPCCatalogMessage{{ID: "id", Role: "user", Text: strings.Repeat("\x00", protocol.RPCCatalogMaxTextBytes)}}, NextOffset: 1,
	}}
	bounded, err := boundCatalogResponse(response)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(bounded)
	if err != nil || len(encoded)+1 > protocol.RPCCatalogMaxOutputBytes {
		t.Fatalf("bytes=%d err=%v", len(encoded)+1, err)
	}
	page := bounded.Data.(protocol.RPCCatalogMessagesPage)
	if len(page.Messages) != 1 || !page.Messages[0].Truncated || page.NextOffset != 1 || page.HasMore {
		t.Fatalf("page=%+v", page)
	}

	sessions := protocol.RPCCatalogSessionsPage{NextOffset: 50}
	for range 50 {
		sessions.Sessions = append(sessions.Sessions, protocol.RPCSessionSummary{SessionID: strings.Repeat("\x00", 4096), Name: strings.Repeat("\x00", 4096)})
	}
	bounded, err = boundCatalogResponse(Response{Type: "response", Success: true, Data: sessions})
	if err != nil {
		t.Fatal(err)
	}
	sp := bounded.Data.(protocol.RPCCatalogSessionsPage)
	if len(sp.Sessions) >= 50 || sp.NextOffset != len(sp.Sessions) || !sp.HasMore {
		t.Fatalf("session paging length=%d next=%d more=%v", len(sp.Sessions), sp.NextOffset, sp.HasMore)
	}
}
