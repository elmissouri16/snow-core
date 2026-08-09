package chatgpt

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/snow-core/snow/pkg/protocol"
)

func TestResponsesAgentMessageSealedAttribution(t *testing.T) {
	env := protocol.AgentMessage{ID: "m", Author: "/root/a", Recipient: "/root", Kind: protocol.AgentMessageFinal, Content: "done"}
	msg := protocol.NewAgentMessage("m", "", env)
	raw, err := buildResponsesBody(protocol.ChatRequest{Model: protocol.Model{ID: "m"}, Messages: []protocol.Message{msg}})
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, "Sender: /root/a") || !strings.Contains(text, "FINAL_ANSWER") {
		t.Fatalf("body=%s", text)
	}
}
