package opencodego

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func benchmarkChatRequest(messageCount int, toolHeavy bool) protocol.ChatRequest {
	messages := make([]protocol.Message, 0, messageCount)
	text := strings.Repeat("request context ", 32)
	for i := 0; i < messageCount; i++ {
		id := fmt.Sprintf("message-%d", i)
		if toolHeavy && i%2 == 0 && i+1 < messageCount {
			callID := fmt.Sprintf("call-%d", i)
			messages = append(messages, protocol.NewAssistantMessage(id, "", "benchmark", "model", []protocol.ContentBlock{{Type: protocol.BlockToolCall, ToolCallID: callID, Name: "read", Arguments: json.RawMessage(`{"path":"README.md"}`)}}, protocol.StopToolUse, nil))
			messages = append(messages, protocol.NewToolResultMessage(fmt.Sprintf("result-%d", i), id, callID, "read", []protocol.ContentBlock{protocol.NewTextBlock(text)}, false))
			i++
			continue
		}
		messages = append(messages, protocol.NewUserMessage(id, "", text))
	}
	tools := make([]protocol.ToolSchema, 20)
	for i := range tools {
		tools[i] = protocol.ToolSchema{Name: fmt.Sprintf("tool_%d", i), Description: "benchmark tool", Parameters: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)}
	}
	return protocol.ChatRequest{Model: protocol.Model{Provider: "benchmark", ID: "model"}, Messages: messages, Tools: tools, System: "benchmark system"}
}

func BenchmarkBuildChatRequest1500(b *testing.B) {
	provider := &Provider{providerID: "benchmark", defaultModel: "model"}
	for _, toolHeavy := range []bool{false, true} {
		name := "plain"
		if toolHeavy {
			name = "tool-heavy"
		}
		request := benchmarkChatRequest(1500, toolHeavy)
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				body, err := provider.buildBody(request)
				if err != nil {
					b.Fatal(err)
				}
				benchmarkChatBodySink = body
			}
		})
	}
}

var benchmarkChatBodySink []byte
