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

func TestSessionSetModelRequestAndResponseSchemas(t *testing.T) {
	requestSchema := resolveWireSchema(t, "request.schema.json")
	for _, frame := range []string{
		`{"id":"select","type":"session_set_model","provider":"fake","model":"fake-1"}`,
		`{"type":"session_set_model","provider":"fake","model":"fake-1"}`,
		// The legacy operator/project settings request remains supported.
		`{"type":"set_model","provider":"fake","model":"fake-1","thinking":"high"}`,
	} {
		if err := requestSchema.Validate(decodedJSON(t, []byte(frame))); err != nil {
			t.Fatalf("valid request %s: %v", frame, err)
		}
	}
	for _, frame := range []string{
		`{"type":"session_set_model"}`,
		`{"type":"session_set_model","model":"fake-1"}`,
		`{"type":"session_set_model","provider":"fake"}`,
		`{"type":"session_set_model","provider":"","model":"fake-1"}`,
		`{"type":"session_set_model","provider":"fake","model":""}`,
		`{"type":"session_set_model","provider":"fake","model":"fake-1","thinking":"high"}`,
		`{"type":"session_set_model","provider":"fake","model":"fake-1","params":{"supports_tools":true}}`,
	} {
		if err := requestSchema.Validate(decodedJSON(t, []byte(frame))); err == nil {
			t.Fatalf("invalid request accepted: %s", frame)
		}
	}
	outputSchema := resolveWireSchema(t, "output.schema.json")
	if err := outputSchema.Validate(decodedJSON(t, []byte(`{"type":"response","command":"session_set_model","success":true}`))); err != nil {
		t.Fatal(err)
	}
	if err := outputSchema.Validate(decodedJSON(t, []byte(`{"type":"response","command":"session_set_model","success":true,"data":{"secret":"private"}}`))); err == nil {
		t.Fatal("selection acknowledgement accepted unexpected payload")
	}
}

func TestSessionSetModelWireAndCatalogRejection(t *testing.T) {
	a, err := app.New(t.Context(), app.Options{Provider: "fake", NoSession: true, NoPlugins: true, NoMCP: true, NoSkills: true, Permission: "deny", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	request := "{\"id\":\"select\",\"type\":\"session_set_model\",\"provider\":\"fake\",\"model\":\"fake-1\"}\n"
	var output bytes.Buffer
	srv := New(t.Context(), a, strings.NewReader(request), &output)
	if err := srv.Serve(t.Context()); err != nil {
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
	if !slices.Contains(ready.Capabilities, "session_model_selection") {
		t.Fatal("runtime did not advertise session model selection")
	}
	var response protocol.RPCResponse
	if err := json.Unmarshal(frames[1], &response); err != nil {
		t.Fatal(err)
	}
	if !response.Success || response.Command != "session_set_model" || response.ID != "select" {
		t.Fatalf("response=%+v", response)
	}
	output.Reset()
	if err := ServeCatalog(t.Context(), strings.NewReader(request), &output, t.TempDir(), t.TempDir(), "test"); err != nil {
		t.Fatal(err)
	}
	frames = bytes.Split(bytes.TrimSpace(output.Bytes()), []byte{'\n'})
	if len(frames) != 2 {
		t.Fatalf("catalog frames=%s", output.Bytes())
	}
	ready = protocol.RPCReady{}
	if err := json.Unmarshal(frames[0], &ready); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(ready.Capabilities, "session_model_selection") {
		t.Fatal("catalog advertised session model selection")
	}
	response = protocol.RPCResponse{}
	if err := json.Unmarshal(frames[1], &response); err != nil {
		t.Fatal(err)
	}
	if response.Success || response.ErrorCode != "unsupported" || response.Data != nil {
		t.Fatalf("catalog accepted session selection: %+v", response)
	}
	if err := resolveWireSchema(t, "catalog-request.schema.json").Validate(decodedJSON(t, []byte(request))); err == nil {
		t.Fatal("catalog request schema accepted session selection")
	}
}
