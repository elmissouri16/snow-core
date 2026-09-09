package app

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"strings"
	"uuid"

	"github.com/elmissouri16/snow-core/internal/plugin/javascript"
	"github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func (h *appExtensionHost) callUI(ctx context.Context, inv plugin.Invocation, operation string, raw json.RawMessage) (json.RawMessage, error) {
	if operation == "ui.input" || operation == "ui.select" || operation == "ui.confirm" || operation == "ui.form" {
		if !h.agent.PluginInputAllowed() {
			return nil, errors.New("interactive user input is unavailable during automatic goal turns")
		}
		return h.askPluginInput(ctx, operation, raw)
	}
	var arg struct {
		Name    string              `json:"name"`
		Content protocol.PluginNode `json:"content"`
	}
	if err := jsonv2.Unmarshal(raw, &arg); err != nil {
		return nil, err
	}
	s := h.services
	if operation == "ui.update" || operation == "ui.open" {
		id := inv.PluginID + ":" + arg.Name
		s.mu.Lock()
		view, exists := s.views[id]
		s.mu.Unlock()
		if !exists {
			return nil, errors.New("view is not registered by this plugin")
		}
		if operation == "ui.update" {
			if err := plugin.ValidateNode(arg.Content); err != nil {
				return nil, err
			}
			view.Content = &arg.Content
			s.mu.Lock()
			s.views[id] = view
			s.mu.Unlock()
		}
	}
	s.mu.Lock()
	handler := s.ui
	s.mu.Unlock()
	if handler == nil {
		if operation == "ui.update" || operation == "ui.notify" {
			return []byte("null"), nil
		}
		return nil, plugin.ErrUnavailable
	}
	return handler(ctx, protocol.PluginUIEvent{PluginID: inv.PluginID, Operation: operation, Arguments: raw, Generation: s.generation.Load()})
}

func (h *appExtensionHost) askPluginInput(ctx context.Context, operation string, raw json.RawMessage) (json.RawMessage, error) {
	var arg struct {
		Title   string                   `json:"title"`
		Options []string                 `json:"options"`
		Fields  []protocol.PluginSetting `json:"fields"`
	}
	if err := jsonv2.Unmarshal(raw, &arg); err != nil {
		return nil, err
	}
	if len(arg.Title) > 4096 || len(arg.Options) > 16 || len(arg.Fields) > 3 {
		return nil, errors.New("plugin dialog exceeds limits")
	}
	if h.app.userInput == nil {
		return nil, plugin.ErrUnavailable
	}
	if strings.TrimSpace(arg.Title) == "" {
		return nil, errors.New("dialog title is required")
	}
	if operation == "ui.select" {
		if len(arg.Options) == 0 {
			return nil, errors.New("selection requires 1..16 options")
		}
		if err := validateDialogChoices(arg.Options); err != nil {
			return nil, err
		}
	}
	id := "plugin-" + uuid.New().String()
	request := protocol.UserInputRequest{ID: id, ToolCallID: id}
	if operation == "ui.form" {
		if len(arg.Fields) == 0 {
			return nil, errors.New("form requires 1..3 fields")
		}
		seen := map[string]bool{}
		for _, field := range arg.Fields {
			if seen[field.Name] || !slices.Contains([]string{"string", "number", "boolean", "enum"}, field.Type) || len(field.Choices) > 64 || len(field.Title) > 4096 {
				return nil, errors.New("invalid form field")
			}
			seen[field.Name] = true
			if err := plugin.ValidateIdentifier("field", field.Name); err != nil {
				return nil, err
			}
			q := protocol.UserInputQuestion{ID: field.Name, Header: field.Name, Question: h.info.Name + ": " + field.Title}
			if field.Type == "enum" {
				if len(field.Choices) == 0 {
					return nil, errors.New("enum field requires choices")
				}
				if err := validateDialogChoices(field.Choices); err != nil {
					return nil, err
				}
				for _, choice := range field.Choices {
					q.Options = append(q.Options, protocol.UserInputOption{Label: choice})
				}
			}
			if field.Type == "boolean" {
				q.Options = []protocol.UserInputOption{{Label: "true"}, {Label: "false"}}
			}
			q.ChoicesOnly = len(q.Options) > 0
			request.Questions = append(request.Questions, q)
		}
	} else {
		q := protocol.UserInputQuestion{ID: "answer", Header: "Plugin", Question: h.info.Name + ": " + arg.Title}
		if operation == "ui.confirm" {
			arg.Options = []string{"No", "Yes"}
		}
		q.ChoicesOnly = operation == "ui.confirm" || operation == "ui.select"
		for _, option := range arg.Options {
			q.Options = append(q.Options, protocol.UserInputOption{Label: option})
		}
		request.Questions = []protocol.UserInputQuestion{q}
	}
	response, err := h.app.userInput.Ask(ctx, request, func(req protocol.UserInputRequest) {
		h.agent.Publish(protocol.AgentEvent{Type: protocol.EvUserInputRequest, UserInput: &req})
	})
	if err != nil {
		return nil, err
	}
	if operation == "ui.form" {
		values := map[string]any{}
		for _, answer := range response.Answers {
			var value any = answer.Answer
			for _, field := range arg.Fields {
				if field.Name == answer.QuestionID && (field.Type == "number" || field.Type == "boolean") {
					var parsed any
					if err := jsonv2.Unmarshal([]byte(answer.Answer), &parsed); err != nil {
						return nil, fmt.Errorf("invalid %s", field.Name)
					}
					value = parsed
				}
			}
			values[answer.QuestionID] = value
		}
		if len(values) != len(arg.Fields) {
			return nil, errors.New("form received incomplete answers")
		}
		if err := javascript.ValidateSettings(arg.Fields, values); err != nil {
			return nil, err
		}
		return jsonv2.Marshal(values)
	}
	if len(response.Answers) != 1 {
		return nil, errors.New("dialog received no answer")
	}
	if operation == "ui.confirm" {
		return jsonv2.Marshal(strings.EqualFold(response.Answers[0].Answer, "yes"))
	}
	if operation == "ui.select" && !slices.Contains(arg.Options, response.Answers[0].Answer) {
		return nil, errors.New("selection is not one of the options")
	}
	return jsonv2.Marshal(response.Answers[0].Answer)
}

func validateDialogChoices(choices []string) error {
	seen := make(map[string]bool, len(choices))
	for _, choice := range choices {
		if strings.TrimSpace(choice) != choice || choice == "" || len(choice) > 512 || seen[choice] {
			return errors.New("dialog choices must be unique, non-empty labels of at most 512 bytes without surrounding whitespace")
		}
		seen[choice] = true
	}
	return nil
}
