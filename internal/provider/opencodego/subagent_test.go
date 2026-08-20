package opencodego

import (
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestMapAgentMessageSealedAttribution(t *testing.T) {
	env := protocol.AgentMessage{ID: "m", Author: "/root/a", Recipient: "/root", Kind: protocol.AgentMessageFinal, Content: "done"}
	msg := protocol.NewAgentMessage("m", "", env)
	wire, ok := mapMessage(msg)
	if !ok || wire.Role != "user" {
		t.Fatalf("wire=%+v %v", wire, ok)
	}
	text, _ := wire.Content.(string)
	if !strings.Contains(text, "Sender: /root/a") || !strings.Contains(text, "FINAL_ANSWER") {
		t.Fatalf("content=%q", text)
	}
}
