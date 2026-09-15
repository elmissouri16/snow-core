package rpc

import (
	"bytes"
	json "encoding/json/v2"
	"slices"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestMessageRegenerateWirePrepareCommitAndSchemas(t *testing.T) {
	a := regenerateRPCApp(t)
	messages, err := a.Session.Messages()
	if err != nil {
		t.Fatal(err)
	}
	reply := messages[len(messages)-1]
	var output bytes.Buffer
	srv := New(t.Context(), a, strings.NewReader(""), &output)
	params, _ := json.Marshal(protocol.RPCMessageRegeneratePrepareParams{SessionID: a.Session.ID(), EntryID: reply.ID})
	if err := srv.handle(t.Context(), Request{ID: "prepare", Type: "message_regenerate_prepare", Params: params}); err != nil {
		t.Fatal(err)
	}
	var preparedResponse struct {
		Success bool                                  `json:"success"`
		Data    protocol.RPCMessageRegeneratePrepared `json:"data"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &preparedResponse); err != nil {
		t.Fatal(err)
	}
	prepared := preparedResponse.Data
	if !preparedResponse.Success || prepared.ReplyEntryID != reply.ID || prepared.EntryID != messages[0].ID || prepared.Text != "original" {
		t.Fatalf("prepared=%+v", preparedResponse)
	}
	if err := resolveWireSchema(t, "output.schema.json").Validate(decodedJSON(t, bytes.TrimSpace(output.Bytes()))); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	params, _ = json.Marshal(protocol.RPCMessageRegenerateCommitParams{SessionID: prepared.SessionID, EditToken: prepared.EditToken})
	if err := srv.handle(t.Context(), Request{ID: "regenerate", Type: "message_regenerate_commit", Params: params}); err != nil {
		t.Fatal(err)
	}
	srv.promptWG.Wait()
	frames := bytes.Split(bytes.TrimSpace(output.Bytes()), []byte{'\n'})
	if len(frames) != 2 {
		t.Fatalf("frames=%s", output.Bytes())
	}
	var response struct {
		Success bool                             `json:"success"`
		Data    protocol.RPCMessageEditCommitted `json:"data"`
	}
	if err := json.Unmarshal(frames[0], &response); err != nil {
		t.Fatal(err)
	}
	if !response.Success || response.Data.SessionID != prepared.SessionID || response.Data.EntryID != prepared.EntryID || response.Data.TurnID == prepared.TurnID || len(response.Data.History.Messages) != 1 || response.Data.History.Messages[0].Content[0].Text != "original" {
		t.Fatalf("regenerate ACK=%s", frames[0])
	}
	schema := resolveWireSchema(t, "output.schema.json")
	for _, frame := range frames {
		if err := schema.Validate(decodedJSON(t, frame)); err != nil {
			t.Fatalf("frame %s: %v", frame, err)
		}
	}
	var completed protocol.RPCPromptCompleted
	if err := json.Unmarshal(frames[1], &completed); err != nil {
		t.Fatal(err)
	}
	if completed.RequestID != "regenerate" || completed.Status != protocol.RPCPromptCompletedStatus {
		t.Fatalf("completion=%+v", completed)
	}
}

func TestMessageRegenerateACKPrecedesProviderAndExcludesOtherPrompts(t *testing.T) {
	a := regenerateRPCApp(t)
	messages, _ := a.Session.Messages()
	prepared, err := a.PrepareMessageRegenerate(t.Context(), protocol.RPCMessageRegeneratePrepareParams{SessionID: a.Session.ID(), EntryID: messages[len(messages)-1].ID})
	if err != nil {
		t.Fatal(err)
	}
	writer := &editACKWriter{ackStarted: make(chan struct{}), release: make(chan struct{})}
	srv := New(t.Context(), a, strings.NewReader(""), writer)
	params, _ := json.Marshal(protocol.RPCMessageRegenerateCommitParams{SessionID: prepared.SessionID, EditToken: prepared.EditToken})
	if err := srv.handle(t.Context(), Request{ID: "regenerate", Type: "message_regenerate_commit", Params: params}); err != nil {
		t.Fatal(err)
	}
	<-writer.ackStarted
	if err := srv.handlePrompt(t.Context(), Request{Type: "prompt", Message: "racing prompt"}); err == nil {
		t.Fatal("regeneration allowed concurrent prompt")
	}
	current, _ := a.Session.Messages()
	if len(current) != 1 || current[0].Content[0].Text != "original" || current[0].ID == prepared.EntryID {
		t.Fatalf("provider outran durable-user ACK: %+v", current)
	}
	close(writer.release)
	srv.promptWG.Wait()
}

func TestMessageRegenerateRejectsTextAndAdvertisesNarrowSchemas(t *testing.T) {
	a := regenerateRPCApp(t)
	var output bytes.Buffer
	srv := New(t.Context(), a, strings.NewReader(""), &output)
	schema := resolveWireSchema(t, "request.schema.json")
	for _, raw := range []string{
		`{"type":"message_regenerate_prepare","params":{"session_id":"s","entry_id":"reply"}}`,
		`{"type":"message_regenerate_prepare","params":{"session_id":"s","turn_id":"root-turn"}}`,
		`{"type":"message_regenerate_commit","params":{"session_id":"s","edit_token":"token"}}`,
	} {
		if err := schema.Validate(decodedJSON(t, []byte(raw))); err != nil {
			t.Fatalf("valid schema %s: %v", raw, err)
		}
	}
	for _, raw := range []string{
		`{"type":"message_regenerate_prepare","params":{"session_id":"s","entry_id":"reply","turn_id":"turn"}}`,
		`{"type":"message_regenerate_prepare","params":{"session_id":"s","entry_id":"reply","text":"override"}}`,
		`{"type":"message_regenerate_commit","params":{"session_id":"s","edit_token":"token","text":"override"}}`,
		`{"type":"message_regenerate_commit","params":{"session_id":"s","edit_token":"token","text":""}}`,
		`{"type":"message_regenerate_commit","params":{"session_id":"s","edit_token":"token","entry_id":"override"}}`,
		`{"type":"message_regenerate_commit","message":"override","params":{"session_id":"s","edit_token":"token"}}`,
	} {
		if err := schema.Validate(decodedJSON(t, []byte(raw))); err == nil {
			t.Fatalf("unsafe schema accepted %s", raw)
		}
		var req Request
		if err := json.Unmarshal([]byte(raw), &req); err != nil {
			t.Fatal(err)
		}
		if err := srv.handle(t.Context(), req); err == nil {
			t.Fatalf("unsafe params accepted %s", raw)
		}
	}
	if !slices.Contains(protocol.KnownRPCCapabilities(), "message_regenerate") {
		t.Fatal("capability missing")
	}
	catalog := resolveWireSchema(t, "catalog-request.schema.json")
	if err := catalog.Validate(decodedJSON(t, []byte(`{"type":"message_regenerate_prepare","params":{"session_id":"s","entry_id":"reply"}}`))); err == nil {
		t.Fatal("read-only catalog accepted regeneration")
	}
}

func TestMessageRegenerateWireActionSwapIsDefinitiveRejection(t *testing.T) {
	a := regenerateRPCApp(t)
	prepared := editRPCPrepared(t, a)
	before := a.Session.BranchTip()
	var output bytes.Buffer
	srv := New(t.Context(), a, strings.NewReader(""), &output)
	params, _ := json.Marshal(protocol.RPCMessageRegenerateCommitParams{SessionID: prepared.SessionID, EditToken: prepared.EditToken})
	if err := srv.handle(t.Context(), Request{ID: "regenerate", Type: "message_regenerate_commit", Params: params}); err != nil {
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
	if response.Success || response.ErrorCode != protocol.RPCMessageEditRejectedErrorCode || a.Session.BranchTip() != before {
		t.Fatalf("unsafe cross-action token result=%s", frames[0])
	}
}

func TestMessageRegeneratePostPersistenceFailureIsUnknown(t *testing.T) {
	a := regenerateRPCApp(t)
	store := &editHistoryFailureStore{MemoryStore: session.NewMemoryStore(session.Options{})}
	if err := a.SetSession(store); err != nil {
		t.Fatal(err)
	}
	if err := a.Agent.Prompt(t.Context(), "original"); err != nil {
		t.Fatal(err)
	}
	appendRegenerateRPCReply(t, a)
	messages, _ := store.Messages()
	prepared, err := a.PrepareMessageRegenerate(t.Context(), protocol.RPCMessageRegeneratePrepareParams{SessionID: store.ID(), EntryID: messages[len(messages)-1].ID})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	srv := New(t.Context(), a, strings.NewReader(""), &output)
	params, _ := json.Marshal(protocol.RPCMessageRegenerateCommitParams{SessionID: prepared.SessionID, EditToken: prepared.EditToken})
	if err := srv.handle(t.Context(), Request{ID: "regenerate", Type: "message_regenerate_commit", Params: params}); err != nil {
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
	if response.Success || response.ErrorCode != protocol.RPCMessageEditUnknownErrorCode || store.ActiveBranchID() == prepared.SourceBranchID {
		t.Fatalf("unsafe ambiguous result=%s", frames[0])
	}
}

func regenerateRPCApp(t *testing.T) *app.App {
	t.Helper()
	a := editRPCApp(t)
	appendRegenerateRPCReply(t, a)
	return a
}
func appendRegenerateRPCReply(t *testing.T, a *app.App) {
	t.Helper()
	// The default fake provider intentionally returns an empty assistant. Seed
	// the terminal visible reply explicitly, keeping its real persisted turn ID.
	reply := protocol.NewAssistantMessage("terminal-reply", "", "fake", "fake-1", []protocol.ContentBlock{protocol.NewTextBlock("visible reply")}, protocol.StopStop, nil)
	if err := a.Session.Append(session.Entry{Type: session.EntryMessage, ID: reply.ID, Message: &reply}); err != nil {
		t.Fatal(err)
	}
}
