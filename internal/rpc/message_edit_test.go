package rpc

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func editRPCApp(t *testing.T) *app.App {
	t.Helper()
	a, err := app.New(t.Context(), app.Options{CWD: t.TempDir(), Provider: "fake", NoSession: true, NoPlugins: true, NoMCP: true, NoSkills: true, Permission: "deny"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	if err := a.Agent.Prompt(t.Context(), "original"); err != nil {
		t.Fatal(err)
	}
	return a
}

func editRPCPrepared(t *testing.T, a *app.App) protocol.RPCMessageEditPrepared {
	t.Helper()
	messages, err := a.Session.Messages()
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := a.PrepareMessageEdit(t.Context(), protocol.RPCMessageEditPrepareParams{SessionID: a.Session.ID(), EntryID: messages[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	return prepared
}

func TestMessageEditWireAdmissionHistoryAndCompletion(t *testing.T) {
	a := editRPCApp(t)
	prepared := editRPCPrepared(t, a)
	var output bytes.Buffer
	srv := New(t.Context(), a, strings.NewReader(""), &output)
	payload, err := json.Marshal(protocol.RPCMessageEditCommitParams{SessionID: prepared.SessionID, EditToken: prepared.EditToken, Text: "replacement"})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.handle(t.Context(), Request{ID: "edit-1", Type: "message_edit_commit", Params: payload}); err != nil {
		t.Fatal(err)
	}
	srv.promptWG.Wait()
	frames := bytes.Split(bytes.TrimSpace(output.Bytes()), []byte{'\n'})
	if len(frames) != 2 {
		t.Fatalf("frames=%s", output.Bytes())
	}
	var response struct {
		Type    string                           `json:"type"`
		Command string                           `json:"command"`
		Success bool                             `json:"success"`
		Data    protocol.RPCMessageEditCommitted `json:"data"`
	}
	if err := json.Unmarshal(frames[0], &response); err != nil {
		t.Fatal(err)
	}
	if !response.Success || response.Command != "message_edit_commit" || response.Data.SessionID != prepared.SessionID || response.Data.SourceTipID != prepared.SourceTipID || response.Data.EntryID != prepared.EntryID || response.Data.BranchID == prepared.SourceBranchID || response.Data.TurnID == prepared.TurnID {
		t.Fatalf("ACK=%s", frames[0])
	}
	history := response.Data.History
	if len(history.Messages) != 1 || history.Messages[0].ID != response.Data.UserEntryID || history.Messages[0].Content[0].Text != "replacement" || history.Total != 1 || history.HasMore {
		t.Fatalf("history=%+v", history)
	}
	var complete protocol.RPCPromptCompleted
	if err := json.Unmarshal(frames[1], &complete); err != nil {
		t.Fatal(err)
	}
	if complete.RequestID != "edit-1" || complete.Status != protocol.RPCPromptCompletedStatus {
		t.Fatalf("completion=%+v", complete)
	}
	schema := resolveWireSchema(t, "output.schema.json")
	for _, frame := range frames {
		if err := schema.Validate(decodedJSON(t, frame)); err != nil {
			t.Fatalf("frame=%s: %v", frame, err)
		}
	}
}

func TestMessageEditStrictParameters(t *testing.T) {
	a := editRPCApp(t)
	var output bytes.Buffer
	srv := New(t.Context(), a, strings.NewReader(""), &output)
	for _, raw := range []string{
		`{"type":"message_edit_prepare","params":null}`,
		`{"type":"message_edit_prepare","params":{"session_id":"s","entry_id":"u","turn_id":"t"}}`,
		`{"type":"message_edit_prepare","params":{"session_id":"s","entry_id":"u","parent_id":"root"}}`,
		`{"type":"message_edit_commit","params":{"session_id":"s","edit_token":"t","text":"new","from_entry_id":"root"}}`,
		`{"type":"message_edit_commit","params":{"session_id":"s","edit_token":"t","text":"new","text":"again"}}`,
		`{"type":"message_edit_commit","message":"other","params":{"session_id":"s","edit_token":"t","text":"new"}}`,
		`{"type":"message_edit_commit","params":{"session_id":"s","edit_token":"t","text":" "}}`,
	} {
		var req Request
		if err := json.Unmarshal([]byte(raw), &req); err != nil {
			continue
		}
		if err := srv.handle(t.Context(), req); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestMessageEditHistoryBoundAndPrivacy(t *testing.T) {
	messages := make([]protocol.Message, 150)
	for i := range messages {
		messages[i] = protocol.NewAssistantMessage(fmt.Sprint(i), "", "private-provider", "private-model", []protocol.ContentBlock{{Type: protocol.BlockText, Text: "public", Data: []byte("private-data")}, {Type: protocol.BlockProviderData, Data: []byte("private-state")}}, protocol.StopStop, nil)
		messages[i].PluginDetails = []byte(`{"secret":"private-plugin"}`)
	}
	messages[149] = protocol.NewUserMessage("replacement", "", "new text")
	page, err := messageEditHistory("edit", messages)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) > 128 || page.Start == 0 || page.Total != 150 || page.Messages[len(page.Messages)-1].ID != "replacement" || bytes.Contains(raw, []byte("private")) {
		t.Fatalf("unsafe history=%s", raw)
	}
	// A huge earlier message is dropped rather than blocking the replacement ACK.
	messages[148].Content = []protocol.ContentBlock{protocol.NewTextBlock(strings.Repeat("large", defaultMessagesPageBytes))}
	page, err = messageEditHistory("edit", messages)
	if err != nil || len(page.Messages) != 1 || page.Start != 149 {
		t.Fatalf("bounded suffix=%+v err=%v", page, err)
	}
}

type editACKWriter struct {
	mu                  sync.Mutex
	frames              [][]byte
	ackStarted, release chan struct{}
	once                sync.Once
}

func (w *editACKWriter) RPCWriteBounded() bool { return true }
func (w *editACKWriter) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte(`"command":"message_edit_commit","success":true`)) || bytes.Contains(p, []byte(`"command":"message_regenerate_commit","success":true`)) {
		w.once.Do(func() { close(w.ackStarted) })
		<-w.release
	}
	w.mu.Lock()
	w.frames = append(w.frames, bytes.Clone(p))
	w.mu.Unlock()
	return len(p), nil
}

func TestMessageEditReservesRPCLifecycleAndAgentAdmission(t *testing.T) {
	a := editRPCApp(t)
	prepared := editRPCPrepared(t, a)
	writer := &editACKWriter{ackStarted: make(chan struct{}), release: make(chan struct{})}
	srv := New(t.Context(), a, strings.NewReader(""), writer)
	params, _ := json.Marshal(protocol.RPCMessageEditCommitParams{SessionID: prepared.SessionID, EditToken: prepared.EditToken, Text: "replacement"})
	if err := srv.handle(t.Context(), Request{ID: "edit", Type: "message_edit_commit", Params: params}); err != nil {
		t.Fatal(err)
	}
	<-writer.ackStarted
	if err := srv.handlePrompt(t.Context(), Request{Type: "prompt", Message: "race"}); err == nil {
		t.Fatal("RPC allowed concurrent prompt")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := a.Agent.Prompt(ctx, "direct race"); !errors.Is(err, context.Canceled) {
		t.Fatalf("direct admission not held: %v", err)
	}
	// The user and turn are durable, but no provider response exists while the
	// acknowledgement write is held. Thus web can atomically replace its view.
	messages, _ := a.Session.Messages()
	if len(messages) != 1 || messages[0].Content[0].Text != "replacement" {
		t.Fatalf("provider outran ACK: %+v", messages)
	}
	close(writer.release)
	srv.promptWG.Wait()
}

type editHistoryFailureStore struct{ *session.MemoryStore }

func (s *editHistoryFailureStore) Messages() ([]protocol.Message, error) {
	if s.ActiveBranchID() != "main" {
		return nil, errors.New("history projection failed")
	}
	return s.MemoryStore.Messages()
}
func TestMessageEditPostPersistenceFailureIsNotRejection(t *testing.T) {
	a := editRPCApp(t)
	store := &editHistoryFailureStore{MemoryStore: session.NewMemoryStore(session.Options{})}
	if err := a.SetSession(store); err != nil {
		t.Fatal(err)
	}
	if err := a.Agent.Prompt(t.Context(), "original"); err != nil {
		t.Fatal(err)
	}
	prepared := editRPCPrepared(t, a)
	var output bytes.Buffer
	srv := New(t.Context(), a, strings.NewReader(""), &output)
	params, _ := json.Marshal(protocol.RPCMessageEditCommitParams{SessionID: prepared.SessionID, EditToken: prepared.EditToken, Text: "replacement"})
	if err := srv.handle(t.Context(), Request{ID: "edit", Type: "message_edit_commit", Params: params}); err != nil {
		t.Fatal(err)
	}
	srv.promptWG.Wait()
	frames := bytes.Split(bytes.TrimSpace(output.Bytes()), []byte{'\n'})
	var response Response
	if len(frames) != 2 {
		t.Fatalf("frames=%s", output.Bytes())
	}
	if err := json.Unmarshal(frames[0], &response); err != nil {
		t.Fatal(err)
	}
	if response.Success || response.ErrorCode != protocol.RPCMessageEditUnknownErrorCode || store.ActiveBranchID() == "main" {
		t.Fatalf("unsafe rejection=%s", frames[0])
	}
	if err := resolveWireSchema(t, "output.schema.json").Validate(decodedJSON(t, frames[0])); err != nil {
		t.Fatal(err)
	}
}

func TestMessageEditDefinitiveRejectionCode(t *testing.T) {
	a := editRPCApp(t)
	prepared := editRPCPrepared(t, a)
	oldTip := a.Session.BranchTip()
	var output bytes.Buffer
	srv := New(t.Context(), a, strings.NewReader(""), &output)
	params, _ := json.Marshal(protocol.RPCMessageEditCommitParams{SessionID: prepared.SessionID, EditToken: "unknown", Text: "replacement"})
	if err := srv.handle(t.Context(), Request{ID: "edit", Type: "message_edit_commit", Params: params}); err != nil {
		t.Fatal(err)
	}
	srv.promptWG.Wait()
	frames := bytes.Split(bytes.TrimSpace(output.Bytes()), []byte{'\n'})
	var response Response
	if len(frames) != 2 {
		t.Fatalf("frames=%s", output.Bytes())
	}
	if err := json.Unmarshal(frames[0], &response); err != nil {
		t.Fatal(err)
	}
	if response.Success || response.ErrorCode != protocol.RPCMessageEditRejectedErrorCode || a.Session.BranchTip() != oldTip {
		t.Fatalf("rejection=%s", frames[0])
	}
}
