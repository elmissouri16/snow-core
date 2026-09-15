package rpc

import (
	"bytes"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestBranchVersionsRPCPublicBoundedPreviewAndIdleRestore(t *testing.T) {
	a := editRPCApp(t)
	if err := a.Agent.SetMode(protocol.ModePlan); err != nil {
		t.Fatal(err)
	}
	for _, message := range publicPageHistory() {
		if err := a.Session.Append(session.Entry{ID: message.ID, Type: session.EntryMessage, Message: &message}); err != nil {
			t.Fatal(err)
		}
	}
	originalTip := a.Session.BranchTip()
	fork, err := a.ForkBranch("")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Agent.SetMode(protocol.ModeDefault); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	srv := New(t.Context(), a, strings.NewReader(""), &output)
	call := func(command string, params any, out any) {
		t.Helper()
		output.Reset()
		wire, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		request := Request{ID: "versions", Type: command, Params: wire}
		requestWire, _ := json.Marshal(request)
		if err := resolveWireSchema(t, "request.schema.json").Validate(decodedJSON(t, requestWire)); err != nil {
			t.Fatal(err)
		}
		if err := srv.handle(t.Context(), request); err != nil {
			t.Fatal(err)
		}
		frames := bytes.Split(bytes.TrimSpace(output.Bytes()), []byte{'\n'})
		if len(frames) != 1 {
			t.Fatalf("restore created a prompt lifecycle: %s", output.Bytes())
		}
		if err := resolveWireSchema(t, "output.schema.json").Validate(decodedJSON(t, frames[0])); err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(frames[0], []byte("PRIVATE-")) {
			t.Fatalf("private history leaked: %s", frames[0])
		}
		var envelope struct {
			Data jsontext.Value `json:"data"`
		}
		if err := json.Unmarshal(frames[0], &envelope); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(envelope.Data, out); err != nil {
			t.Fatal(err)
		}
	}
	var listed protocol.RPCBranchesPage
	call("branches_page", protocol.RPCBranchesPageParams{SessionID: a.Session.ID(), Limit: 1}, &listed)
	if len(listed.Branches) != 1 || listed.NextCursor == "" || listed.ActiveBranchID != fork.ID {
		t.Fatal("list is not bounded/exact")
	}
	var more protocol.RPCBranchesPage
	call("branches_page", protocol.RPCBranchesPageParams{SessionID: a.Session.ID(), Limit: 1, Cursor: listed.NextCursor}, &more)
	if len(more.Branches) != 1 || more.Branches[0].ID == listed.Branches[0].ID {
		t.Fatal("cursor duplicated branch")
	}
	var preview protocol.RPCBranchMessagesPage
	call("branch_messages_page", protocol.RPCBranchMessagesPageParams{SessionID: a.Session.ID(), BranchID: "main", TipID: originalTip, Limit: 3}, &preview)
	if preview.BranchID != "main" || preview.Total != 5 || len(preview.Messages) != 3 || preview.HistoryTools["owner"][0].ResultID != "result" || a.Session.(session.ActiveBranchStore).ActiveBranchID() != fork.ID || len(preview.HistoryTools["owner"]) != 1 {
		t.Fatalf("preview selected branch or lost public tools: %+v", preview)
	}
	var prepared protocol.RPCBranchRestorePrepared
	call("branch_restore_prepare", protocol.RPCBranchRestorePrepareParams{SessionID: a.Session.ID(), SourceBranchID: fork.ID, SourceTipID: originalTip, TargetBranchID: "main", TargetTipID: originalTip}, &prepared)
	var restored protocol.RPCBranchRestoreCommitted
	call("branch_restore_commit", protocol.RPCBranchRestoreCommitParams{SessionID: a.Session.ID(), RestoreToken: prepared.RestoreToken}, &restored)
	if restored.Mode != protocol.ModePlan || a.Agent.Mode() != protocol.ModePlan || restored.Settings.Thinking != a.Agent.Thinking() || a.Agent.IsRunning() || restored.BranchID != "main" || restored.History.Total != 5 || restored.Settings.Provider != "fake" || restored.Settings.PermissionMode != "deny" {
		t.Fatalf("restore not idle/exact: %+v", restored)
	}
	wire, _ := json.Marshal(protocol.RPCBranchRestoreCommitParams{SessionID: a.Session.ID(), RestoreToken: prepared.RestoreToken})
	if err := srv.handle(t.Context(), Request{Type: "branch_restore_commit", Params: wire}); rpcErrorCode(err) != protocol.RPCBranchRestoreRejectedErrorCode {
		t.Fatalf("token replay not rejected: %v", err)
	}
}
func TestBranchVersionsStrictSchemasAndWire(t *testing.T) {
	a := editRPCApp(t)
	for _, frame := range []string{
		`{"type":"branches_page","params":{"session_id":"s","limit":101}}`,
		`{"type":"branches_page","params":{"session_id":"s","limit":0}}`,
		`{"type":"branches_page","params":{"session_id":"s","limit":null}}`,
		`{"type":"branches_page","params":{"session_id":"s","cursor":null}}`,
		`{"type":"branches_page","message":"","params":{"session_id":"s"}}`,
		`{"type":"branch_messages_page","params":{"session_id":"s","branch_id":"b","tip_id":"t","limit":65}}`,
		`{"type":"branch_messages_page","params":{"session_id":"s","branch_id":"b"}}`,
		`{"type":"branch_restore_prepare","params":{"session_id":"s","target_branch_id":"b"}}`,
		`{"type":"branch_restore_prepare","params":{"session_id":"s","source_branch_id":"a","source_tip_id":"x","target_branch_id":"b","target_tip_id":"y","entry_id":"old"}}`,
		`{"type":"branch_restore_commit","params":{"session_id":"s","restore_token":"x","text":"do work"}}`,
		`{"type":"branch_restore_commit","params":{"session_id":"s","restore_token":null}}`,
	} {
		t.Run(frame, func(t *testing.T) {
			if err := resolveWireSchema(t, "request.schema.json").Validate(decodedJSON(t, []byte(frame))); err == nil {
				t.Fatal("invalid schema accepted")
			}
			var output bytes.Buffer
			srv := New(t.Context(), a, strings.NewReader(frame+"\n"), &output)
			if err := srv.Serve(t.Context()); err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(output.Bytes(), []byte(`"success":false`)) {
				t.Fatalf("invalid frame accepted: %s", output.Bytes())
			}
		})
	}
}
func TestBranchVersionPublicFramesBoundedWithoutTruncatingIdentity(t *testing.T) {
	messages := []protocol.Message{}
	for i := range 4 {
		messages = append(messages, protocol.NewUserMessage(strings.Repeat("i", i+1), "", strings.Repeat("x", 700<<10)))
	}
	page, err := boundVersionHistory(protocol.RPCBranchMessagesPage{SessionID: "s", BranchID: "b", TipID: "t", Messages: messages, Total: 4}, false)
	if err != nil || len(page.Messages) >= 4 || page.NextCursor == "" {
		t.Fatalf("page not bounded: %+v %v", page, err)
	}
	if err := checkVersionFrame(page); err != nil {
		t.Fatal(err)
	}
	_, err = boundVersionHistory(protocol.RPCBranchMessagesPage{SessionID: "s", BranchID: "b", TipID: "t", Messages: []protocol.Message{protocol.NewUserMessage(strings.Repeat("id", branchVersionFrameBytes), "", "small")}}, false)
	if err == nil {
		t.Fatal("oversized identity was truncated")
	}
}
