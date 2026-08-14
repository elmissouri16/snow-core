package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/snow-core/snow/internal/app"
	"github.com/snow-core/snow/pkg/protocol"
)

func resolveWireSchema(t *testing.T, name string) *jsonschema.Resolved {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve source path")
	}
	dir := filepath.Join(filepath.Dir(source), "..", "..", "pkg", "protocol", "schema", "rpc", "v1")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	schemas := make(map[string]*jsonschema.Schema, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".schema.json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var schema jsonschema.Schema
		if err := json.Unmarshal(data, &schema); err != nil {
			t.Fatalf("parse %s: %v", entry.Name(), err)
		}
		schemas[schema.ID] = &schema
	}
	rootID := "https://snow-core.dev/schemas/rpc/v1/" + name
	root := schemas[rootID]
	if root == nil {
		t.Fatalf("missing schema %s", rootID)
	}
	resolved, err := root.Resolve(&jsonschema.ResolveOptions{Loader: func(uri *url.URL) (*jsonschema.Schema, error) {
		if schema := schemas[uri.String()]; schema != nil {
			return schema, nil
		}
		return nil, fmt.Errorf("unexpected non-local schema reference %s", uri)
	}})
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func decodedJSON(t *testing.T, data []byte) any {
	t.Helper()
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestLiveRPCFramesConformToVersionOneSchemas(t *testing.T) {
	requests := []string{
		`{"id":"i1","type":"session_info"}`,
		`{"id":"m1","type":"models_list"}`,
		`{"id":"sm1","type":"subagent_models"}`,
		`{"id":"p1","type":"prompt","message":"schema smoke"}`,
	}
	requestSchema := resolveWireSchema(t, "request.schema.json")
	for _, frame := range requests {
		if err := requestSchema.Validate(decodedJSON(t, []byte(frame))); err != nil {
			t.Fatalf("request does not conform: %s: %v", frame, err)
		}
	}

	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "deny", NoPlugins: true, NoMCP: true, NoSkills: true, CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	var input bytes.Buffer
	for _, frame := range requests {
		input.WriteString(frame)
		input.WriteByte('\n')
	}
	var output bytes.Buffer
	server := NewWithOptions(context.Background(), a, &input, &output, ServerOptions{SnowVersion: "schema-test"})
	a.Agent.Subscribe(func(event protocol.AgentEvent) { _ = server.write(event) })
	if err := server.Serve(context.Background()); err != nil {
		t.Fatal(err)
	}
	wire := output.Bytes()
	if len(wire) == 0 || wire[len(wire)-1] != '\n' || bytes.Contains(wire, []byte{'\r'}) {
		t.Fatalf("RPC output is not LF-only JSONL: %q", wire)
	}
	outputSchema := resolveWireSchema(t, "output.schema.json")
	lines := bytes.Split(bytes.TrimSuffix(wire, []byte{'\n'}), []byte{'\n'})
	readyCount := 0
	completionCount := 0
	for i, line := range lines {
		if len(line) > protocol.RPCMaxInputBytes {
			t.Fatalf("frame %d exceeds maximum: %d", i, len(line))
		}
		value := decodedJSON(t, line)
		if err := outputSchema.Validate(value); err != nil {
			t.Fatalf("output frame %d does not conform: %s: %v", i, line, err)
		}
		object := value.(map[string]any)
		switch object["type"] {
		case protocol.RPCTypeReady:
			readyCount++
			if i != 0 {
				t.Fatalf("rpc_ready is frame %d, want first", i)
			}
		case protocol.RPCTypePromptCompleted:
			completionCount++
		}
	}
	if readyCount != 1 || completionCount != 1 {
		t.Fatalf("ready=%d completion=%d frames=%q", readyCount, completionCount, lines)
	}
}
