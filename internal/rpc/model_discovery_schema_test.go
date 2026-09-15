package rpc

import (
	"bytes"
	json "encoding/json/v2"
	"slices"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestModelsDiscoverRequestAndLegacyModelsListSchemas(t *testing.T) {
	requests := resolveWireSchema(t, "request.schema.json")
	for _, frame := range []string{
		`{"id":"discover","type":"models_discover"}`,
		`{"type":"models_discover"}`,
		`{"id":"legacy","type":"models_list"}`,
	} {
		if err := requests.Validate(decodedJSON(t, []byte(frame))); err != nil {
			t.Fatalf("valid request rejected: %s: %v", frame, err)
		}
	}
	if err := requests.Validate(decodedJSON(t, []byte(`{"type":"models_discover","params":{"timeout":60}}`))); err == nil {
		t.Fatal("discovery request schema allowed caller timeout override")
	}

	a, err := app.New(t.Context(), app.Options{Provider: "fake", NoSession: true, NoPlugins: true, NoMCP: true, NoSkills: true, Permission: "deny", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	var output bytes.Buffer
	srv := New(t.Context(), a, strings.NewReader("{\"id\":\"discover\",\"type\":\"models_discover\"}\n{\"id\":\"legacy\",\"type\":\"models_list\"}\n"), &output)
	if err := srv.Serve(t.Context()); err != nil {
		t.Fatal(err)
	}
	frames := bytes.Split(bytes.TrimSpace(output.Bytes()), []byte{'\n'})
	if len(frames) != 3 {
		t.Fatalf("frames=%s", output.Bytes())
	}
	var ready protocol.RPCReady
	if err := json.Unmarshal(frames[0], &ready); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(ready.Capabilities, "model_discovery") || !slices.Contains(ready.Capabilities, "models_list") {
		t.Fatalf("capabilities=%v", ready.Capabilities)
	}
	result := discoveryResponse(t, frames[1])
	if len(result.Models) != 1 || result.Models[0].Provider != "fake" || result.Partial || result.Truncated {
		t.Fatalf("fake discovery=%+v", result)
	}
	var legacy struct {
		Data protocol.RPCModelList `json:"data"`
	}
	if err := json.Unmarshal(frames[2], &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.Data.Provider != "fake" || legacy.Data.Current != "fake-1" || len(legacy.Data.Models) != 1 {
		t.Fatalf("legacy models_list changed: %+v", legacy.Data)
	}
	if err := resolveWireSchema(t, "output.schema.json").Validate(decodedJSON(t, frames[2])); err != nil {
		t.Fatal(err)
	}
}

func TestModelsDiscoverIsRejectedByCatalogStartup(t *testing.T) {
	var output bytes.Buffer
	if err := ServeCatalog(t.Context(), strings.NewReader("{\"id\":\"discover\",\"type\":\"models_discover\"}\n"), &output, t.TempDir(), t.TempDir(), "test"); err != nil {
		t.Fatal(err)
	}
	frames := bytes.Split(bytes.TrimSpace(output.Bytes()), []byte{'\n'})
	if len(frames) != 2 {
		t.Fatalf("frames=%s", output.Bytes())
	}
	var ready protocol.RPCReady
	if err := json.Unmarshal(frames[0], &ready); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(ready.Capabilities, "model_discovery") {
		t.Fatal("runtime-free catalog advertised discovery")
	}
	var response protocol.RPCResponse
	if err := json.Unmarshal(frames[1], &response); err != nil {
		t.Fatal(err)
	}
	if response.Success || response.ErrorCode != "unsupported" || response.Data != nil {
		t.Fatalf("catalog accepted runtime discovery: %+v", response)
	}
	if err := resolveWireSchema(t, "catalog-request.schema.json").Validate(decodedJSON(t, []byte(`{"type":"models_discover"}`))); err == nil {
		t.Fatal("catalog request schema accepts model discovery")
	}
}

func TestModelsDiscoverSchemaRequiresFlagsAndBounds(t *testing.T) {
	schema := resolveWireSchema(t, "model-discovery.schema.json")
	for _, data := range []string{
		`{"models":[],"partial":false,"truncated":false}`,
		`{"models":[],"partial":true,"truncated":true}`,
	} {
		if err := schema.Validate(decodedJSON(t, []byte(data))); err != nil {
			t.Fatal(err)
		}
	}
	for _, data := range []string{
		`{"models":[],"partial":false}`,
		`{"models":[],"truncated":false}`,
		`{"models":null,"partial":false,"truncated":false}`,
		`{"models":[],"partial":true,"truncated":false,"error":"private"}`,
	} {
		if err := schema.Validate(decodedJSON(t, []byte(data))); err == nil {
			t.Fatalf("invalid discovery accepted: %s", data)
		}
	}
	model := protocol.Model{Provider: "p", ID: "m"}
	oversized := make([]protocol.Model, 513)
	for i := range oversized {
		oversized[i] = model
	}
	for _, result := range []protocol.RPCModelDiscovery{
		{Models: oversized},
		{Models: []protocol.Model{{Provider: strings.Repeat("p", 129), ID: "m"}}},
		{Models: []protocol.Model{{Provider: "p", ID: strings.Repeat("m", 513)}}},
		{Models: []protocol.Model{{Provider: "p", ID: "m", Description: strings.Repeat("d", 4097)}}},
	} {
		data, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		if err := schema.Validate(decodedJSON(t, data)); err == nil {
			t.Fatal("unbounded discovery accepted")
		}
	}
	data, err := json.Marshal(boundedModelDiscovery(nil))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"models":[],"partial":false,"truncated":false}` {
		t.Fatalf("empty discovery JSON=%s", data)
	}
}
