package rpc

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestMessageEditSchemasAndCapability(t *testing.T) {
	request := resolveWireSchema(t, "request.schema.json")
	for _, frame := range []string{
		`{"id":"prepare","type":"message_edit_prepare","params":{"session_id":"session","entry_id":"user"}}`,
		`{"type":"message_edit_prepare","params":{"session_id":"session","turn_id":"turn"}}`,
		`{"id":"commit","type":"message_edit_commit","params":{"session_id":"session","edit_token":"opaque","text":"replacement"}}`,
	} {
		if err := request.Validate(decodedJSON(t, []byte(frame))); err != nil {
			t.Fatalf("valid frame %s: %v", frame, err)
		}
	}
	for _, frame := range []string{
		`{"type":"message_edit_prepare"}`,
		`{"type":"message_edit_prepare","params":{"session_id":"session"}}`,
		`{"type":"message_edit_prepare","params":{"session_id":"session","entry_id":"user","turn_id":"turn"}}`,
		`{"type":"message_edit_prepare","params":{"session_id":"session","entry_id":"","turn_id":"turn"}}`,
		`{"type":"message_edit_prepare","params":{"session_id":"session","entry_id":"user","parent_id":"root"}}`,
		`{"type":"message_edit_commit","params":{"session_id":"session","edit_token":"opaque","text":"replacement","branch_id":"main"}}`,
		`{"type":"message_edit_commit","params":{"session_id":"session","edit_token":"opaque","text":null}}`,
		`{"type":"message_edit_commit","message":"","params":{"session_id":"session","edit_token":"opaque","text":"replacement"}}`,
	} {
		if err := request.Validate(decodedJSON(t, []byte(frame))); err == nil {
			t.Fatalf("invalid frame accepted %s", frame)
		}
	}
	if !slices.Contains(protocol.KnownRPCCapabilities(), "message_edit") {
		t.Fatal("capability missing")
	}
	// Catalog-only workers cannot admit edits.
	catalog := resolveWireSchema(t, "catalog-request.schema.json")
	if err := catalog.Validate(decodedJSON(t, []byte(`{"type":"message_edit_prepare","params":{"session_id":"session","entry_id":"user"}}`))); err == nil {
		t.Fatal("catalog accepted edit")
	}
}

func TestMessageEditWireRejectsEmptyExtraAndDuplicateEnvelope(t *testing.T) {
	a := editRPCApp(t)
	for _, frame := range []string{
		`{"type":"message_edit_prepare","message":"","params":{"session_id":"s","entry_id":"u"}}`,
		`{"type":"message_edit_prepare","unknown":null,"params":{"session_id":"s","entry_id":"u"}}`,
		`{"type":"message_edit_prepare","params":{"session_id":"s","entry_id":"u"},"params":{"session_id":"s","entry_id":"u"}}`,
	} {
		var output bytes.Buffer
		srv := New(t.Context(), a, strings.NewReader(frame+"\n"), &output)
		if err := srv.Serve(t.Context()); err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(output.Bytes(), []byte(`"success":false`)) {
			t.Fatalf("accepted frame %s: %s", frame, output.Bytes())
		}
	}
}
