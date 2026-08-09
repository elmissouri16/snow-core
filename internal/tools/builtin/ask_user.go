package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/snow-core/snow/internal/tools"
	"github.com/snow-core/snow/internal/userinput"
	"github.com/snow-core/snow/pkg/protocol"
)

const (
	maxUserQuestions          = 3
	maxQuestionHeaderRunes    = 30
	maxQuestionTextRunes      = 1000
	maxOptionLabelRunes       = 80
	maxOptionDescriptionRunes = 300
)

var questionIDRE = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// AskUser pauses the current tool call until an interactive surface answers.
type AskUser struct{ name string }

func NewAskUser() *AskUser { return &AskUser{name: "ask_user"} }

// NewRequestUserInput exposes the Plan-mode alias backed by the same broker.
func NewRequestUserInput() *AskUser { return &AskUser{name: "request_user_input"} }

func (a *AskUser) Schema() tools.ToolSchema {
	name := a.name
	if name == "" {
		name = "ask_user"
	}
	return tools.ToolSchema{
		Name:        name,
		Description: "Ask the user up to three short questions when their preference, clarification, or decision is required. Omit options for free-form input; choice questions automatically allow an Other answer.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["questions"],
  "properties": {
    "questions": {
      "type": "array",
      "minItems": 1,
      "maxItems": 3,
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["id", "header", "question"],
        "properties": {
          "id": {"type": "string", "pattern": "^[a-z][a-z0-9_]{0,63}$", "description": "Stable snake_case identifier used to map the answer."},
          "header": {"type": "string", "maxLength": 30, "description": "Very short label."},
          "question": {"type": "string", "maxLength": 1000, "description": "Complete question shown to the user."},
          "options": {
            "type": "array",
            "minItems": 2,
            "maxItems": 3,
            "description": "Mutually exclusive choices. Omit for free-form input. An Other choice is added automatically.",
            "items": {
              "type": "object",
              "additionalProperties": false,
              "required": ["label", "description"],
              "properties": {
                "label": {"type": "string", "maxLength": 80},
                "description": {"type": "string", "maxLength": 300}
              }
            }
          }
        }
      }
    }
  }
}`),
	}
}

func (a *AskUser) Run(ctx context.Context, raw json.RawMessage, host tools.ToolHost) (tools.ToolResult, error) {
	ctx = nonNilContext(ctx)
	name := a.Schema().Name
	var args struct {
		Questions []protocol.UserInputQuestion `json:"questions"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return tools.ErrorResult(fmt.Errorf("%s: invalid arguments: %w", name, err)), nil
	}
	if err := validateQuestions(args.Questions); err != nil {
		return tools.ErrorResult(fmt.Errorf("%s: %w", name, err)), nil
	}
	interactive, ok := host.(tools.UserInputHost)
	if !ok {
		return tools.ErrorResult(fmt.Errorf("%s: %w", name, userinput.ErrUnavailable)), nil
	}
	response, err := interactive.RequestUserInput(ctx, protocol.UserInputRequest{Questions: args.Questions})
	if err != nil {
		if errors.Is(err, userinput.ErrRejected) {
			return tools.ErrorResult(userinput.ErrRejected), nil
		}
		return tools.ErrorResult(fmt.Errorf("%s: %w", name, err)), nil
	}
	encoded, err := json.Marshal(struct {
		Answers []protocol.UserInputAnswer `json:"answers"`
	}{Answers: response.Answers})
	if err != nil {
		return tools.ErrorResult(fmt.Errorf("%s: encode answers: %w", name, err)), nil
	}
	return tools.TextResult(string(encoded)), nil
}

func validateQuestions(questions []protocol.UserInputQuestion) error {
	if len(questions) < 1 || len(questions) > maxUserQuestions {
		return fmt.Errorf("questions must contain between 1 and %d items", maxUserQuestions)
	}
	ids := make(map[string]bool, len(questions))
	for i := range questions {
		question := &questions[i]
		question.ID = strings.TrimSpace(question.ID)
		question.Header = strings.TrimSpace(question.Header)
		question.Question = strings.TrimSpace(question.Question)
		if !questionIDRE.MatchString(question.ID) {
			return fmt.Errorf("question %d has invalid id %q", i+1, question.ID)
		}
		if ids[question.ID] {
			return fmt.Errorf("duplicate question id %q", question.ID)
		}
		ids[question.ID] = true
		if question.Header == "" || utf8.RuneCountInString(question.Header) > maxQuestionHeaderRunes {
			return fmt.Errorf("question %q header must contain 1-%d characters", question.ID, maxQuestionHeaderRunes)
		}
		if question.Question == "" || utf8.RuneCountInString(question.Question) > maxQuestionTextRunes {
			return fmt.Errorf("question %q text must contain 1-%d characters", question.ID, maxQuestionTextRunes)
		}
		if len(question.Options) != 0 && (len(question.Options) < 2 || len(question.Options) > 3) {
			return fmt.Errorf("question %q options must contain 2-3 items or be omitted", question.ID)
		}
		labels := make(map[string]bool, len(question.Options))
		for j := range question.Options {
			option := &question.Options[j]
			option.Label = strings.TrimSpace(option.Label)
			option.Description = strings.TrimSpace(option.Description)
			if option.Label == "" || utf8.RuneCountInString(option.Label) > maxOptionLabelRunes {
				return fmt.Errorf("question %q option %d label must contain 1-%d characters", question.ID, j+1, maxOptionLabelRunes)
			}
			if option.Description == "" || utf8.RuneCountInString(option.Description) > maxOptionDescriptionRunes {
				return fmt.Errorf("question %q option %d description must contain 1-%d characters", question.ID, j+1, maxOptionDescriptionRunes)
			}
			key := strings.ToLower(option.Label)
			if labels[key] || key == "other" {
				return fmt.Errorf("question %q has duplicate or reserved option label %q", question.ID, option.Label)
			}
			labels[key] = true
		}
	}
	return nil
}
