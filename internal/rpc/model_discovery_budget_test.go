package rpc

import (
	"bytes"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"fmt"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestModelsDiscoverEncodedBudgetKeepsRPCUsable(t *testing.T) {
	for _, escaped := range []bool{false, true} {
		t.Run(fmt.Sprint(escaped), func(t *testing.T) {
			text, upgrade := "d", "u"
			if escaped {
				text, upgrade = "\x01", "<"
			}
			models := make([]protocol.Model, 512)
			for i := range models {
				models[i] = protocol.Model{
					Provider: strings.Repeat("p", 128), ID: strings.Repeat("m", 508) + fmt.Sprintf("%04d", i),
					DisplayName: strings.Repeat("n", 512), Description: strings.Repeat(text, 4096),
					Upgrade: &protocol.ModelUpgrade{Model: strings.Repeat("u", 512), Message: strings.Repeat(upgrade, 4096)},
				}
			}
			result := boundedModelDiscovery(models)
			encoded, err := json.Marshal(result, jsontext.EscapeForHTML(true), jsontext.EscapeForJS(true))
			if err != nil {
				t.Fatal(err)
			}
			if len(encoded) > modelDiscoveryMaxResultBytes {
				t.Fatalf("encoded result exceeds budget: got %d, limit %d", len(encoded), modelDiscoveryMaxResultBytes)
			}
			var output bytes.Buffer
			srv := &Server{out: &output, outputBounded: true, outputIndependentBound: true, writeFailed: make(chan struct{})}
			if err := srv.write(Response{ID: "discover", Type: "response", Command: "models_discover", Success: true, Data: result}); err != nil {
				t.Fatal(err)
			}
			const clientFrameLimit = 4 * 1024 * 1024
			if output.Len() >= clientFrameLimit {
				t.Fatalf("discovery response exceeds client frame limit: got %d bytes, limit %d", output.Len(), clientFrameLimit)
			}
			if !result.Truncated || len(result.Models) == 0 || len(result.Models) >= len(models) || result.Partial {
				t.Fatalf("encoded budget did not retain a truncated catalog: count=%d partial=%v truncated=%v", len(result.Models), result.Partial, result.Truncated)
			}
			// The budget retains complete provider/model pairs rather than slicing
			// serialized JSON or modifying identities to make a frame fit.
			for i, model := range result.Models {
				if model.Provider != models[i].Provider || model.ID != models[i].ID {
					t.Fatal("encoded budget changed a retained model identity")
				}
			}
			if err := srv.write(Response{ID: "next", Type: "response", Command: "session_info", Success: true}); err != nil {
				t.Fatalf("RPC writer unusable after discovery: %v", err)
			}
			frames := bytes.Split(bytes.TrimSpace(output.Bytes()), []byte{'\n'})
			if len(frames) != 2 {
				t.Fatalf("frames=%d, want discovery and next response", len(frames))
			}
			var response protocol.RPCResponse
			if err := json.Unmarshal(frames[1], &response); err != nil {
				t.Fatal(err)
			}
			if !response.Success || response.ID != "next" {
				t.Fatalf("next response=%+v", response)
			}
			t.Logf("bounded discovery response: %d bytes, %d retained models", len(frames[0]), len(result.Models))
		})
	}
}
