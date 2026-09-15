package rpc

import (
	"bytes"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"strings"

	"github.com/elmissouri16/snow-core/internal/app"
)

func isGoalControlCommand(command string) bool {
	return command == "goal_run" || command == "goal_inspect"
}

// validateGoalControlFrame runs on the original Serve input, before the legacy
// compatibility request decoder can erase explicitly empty extra fields.
func validateGoalControlFrame(frame []byte) error {
	var fields map[string]jsontext.Value
	if err := json.Unmarshal(frame, &fields); err != nil {
		return errors.Join(app.ErrGoalRunRejected, err)
	}
	for key := range fields {
		if key != "id" && key != "type" && key != "params" {
			return errors.Join(app.ErrGoalRunRejected, errors.New("goal control accepts only id, type and params"))
		}
	}
	for _, key := range []string{"id", "type"} {
		raw, ok := fields[key]
		if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return errors.Join(app.ErrGoalRunRejected, errors.New("goal control requires a correlated id and type"))
		}
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return errors.Join(app.ErrGoalRunRejected, err)
		}
		if text == "" || len(text) > 256 || strings.ContainsRune(text, 0) {
			return errors.Join(app.ErrGoalRunRejected, errors.New("goal control identity is empty or invalid"))
		}
	}
	if raw, ok := fields["params"]; !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return errors.Join(app.ErrGoalRunRejected, errors.New("goal control requires params"))
	}
	return nil
}
