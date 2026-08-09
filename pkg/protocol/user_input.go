package protocol

// UserInputOption is one mutually exclusive answer shown for a question.
// Interactive clients also append a free-form Other choice.
type UserInputOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// UserInputQuestion is one item in a bounded ask_user request. Questions with
// no options are answered as free-form text.
type UserInputQuestion struct {
	ID       string            `json:"id"`
	Header   string            `json:"header"`
	Question string            `json:"question"`
	Options  []UserInputOption `json:"options,omitempty"`
}

// UserInputRequest is emitted while an ask_user tool call is blocked waiting
// for an interactive client. ID is stable and currently matches ToolCallID.
type UserInputRequest struct {
	ID         string              `json:"id"`
	ToolCallID string              `json:"tool_call_id"`
	Questions  []UserInputQuestion `json:"questions"`
}

// UserInputAnswer resolves one question by its stable ID.
type UserInputAnswer struct {
	QuestionID string `json:"id"`
	Answer     string `json:"answer"`
}

// UserInputResponse resolves a complete request. Answers are normalized into
// request order before they are returned to the model.
type UserInputResponse struct {
	RequestID string            `json:"request_id"`
	Answers   []UserInputAnswer `json:"answers"`
}
