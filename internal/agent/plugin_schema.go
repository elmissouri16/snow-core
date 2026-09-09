package agent

import (
	"crypto/sha256"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
)

type resolvedPluginSchema struct {
	schema *jsonschema.Resolved
	err    error
}

func (a *Agent) validatePluginArguments(raw json.RawMessage, args map[string]any) error {
	key := sha256.Sum256(raw)
	cached, ok := a.pluginSchemas.Load(key)
	if !ok {
		var schema jsonschema.Schema
		entry := resolvedPluginSchema{}
		entry.err = jsonv2.Unmarshal(raw, &schema)
		if entry.err == nil {
			entry.schema, entry.err = schema.Resolve(nil)
		} // Remote references fail closed without a loader.
		cached, _ = a.pluginSchemas.LoadOrStore(key, entry)
	}
	entry := cached.(resolvedPluginSchema)
	if entry.err != nil {
		return fmt.Errorf("invalid tool schema: %w", entry.err)
	}
	if err := entry.schema.Validate(args); err != nil {
		return fmt.Errorf("invalid tool arguments: %w", err)
	}
	return nil
}
