package rpc

import (
	"context"
	"encoding/json/v2"
	"testing"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestCatalogPublicSchemas(t *testing.T) {
	requests := resolveWireSchema(t, "catalog-request.schema.json")
	outputs := resolveWireSchema(t, "catalog-output.schema.json")
	catalog := session.NewCatalog(t.TempDir(), t.TempDir())
	for _, req := range []protocol.RPCRequest{
		{ID: "sessions", Type: "catalog_sessions"},
		{ID: "image", Type: "catalog_image", Params: []byte(`{"session_id":"missing","message_id":"message","index":1}`)},
		{ID: "messages", Type: "catalog_messages", Params: []byte(`{"session_id":"missing","offset":0,"limit":25}`)},
	} {
		data, err := json.Marshal(req)
		if err != nil {
			t.Fatal(err)
		}
		if err := requests.Validate(decodedJSON(t, data)); err != nil {
			t.Fatalf("catalog request schema: %v", err)
		}
		response := catalogResponse(context.Background(), catalog, req)
		data, err = json.Marshal(response)
		if err != nil {
			t.Fatal(err)
		}
		if err := outputs.Validate(decodedJSON(t, data)); err != nil {
			t.Fatalf("catalog output schema: %v", err)
		}
	}
	for _, value := range []any{
		protocol.NewRPCCatalogReady("test"),
		protocol.RPCResponse{Type: "response", Command: "catalog_image", Success: true, Data: protocol.RPCCatalogImage{SessionID: "s", MessageID: "m", Index: 1, MIMEType: "image/png", Data: []byte{1}}},
		protocol.RPCResponse{Type: "response", Command: "catalog_messages", Success: true, Data: protocol.RPCCatalogMessagesPage{Messages: []protocol.RPCCatalogMessage{{ID: "message", Role: "user", Text: "saved"}}}},
	} {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := outputs.Validate(decodedJSON(t, data)); err != nil {
			t.Fatalf("catalog schema: %v", err)
		}
	}
}
