package responsesapi

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

var (
	benchmarkRequestBodySink  responsesRequest
	benchmarkRequestBytesSink []byte
)

func BenchmarkBuildRequest(b *testing.B) {
	for _, kind := range []string{"plain", "tool_heavy", "continuity"} {
		b.Run(kind, func(b *testing.B) {
			for _, messages := range []int{100, 500, 1500} {
				b.Run(fmt.Sprintf("messages_%d", messages), func(b *testing.B) {
					for _, tools := range []int{0, 15, 20} {
						b.Run(fmt.Sprintf("tools_%d", tools), func(b *testing.B) {
							req, opts := benchmarkChatRequest(kind, messages, tools)
							body, err := BuildRequest(req, opts)
							if err != nil {
								b.Fatal(err)
							}
							b.ReportAllocs()
							b.SetBytes(int64(len(body)))
							for b.Loop() {
								benchmarkRequestBytesSink, err = BuildRequest(req, opts)
								if err != nil {
									b.Fatal(err)
								}
							}
						})
					}
				})
			}
		})
	}
}

func BenchmarkBuildRequestStages(b *testing.B) {
	for _, kind := range []string{"plain", "tool_heavy", "continuity"} {
		req, opts := benchmarkChatRequest(kind, 1500, 20)
		body, err := buildRequestBody(req, opts)
		if err != nil {
			b.Fatal(err)
		}
		wire, err := marshalRequestBody(body)
		if err != nil {
			b.Fatal(err)
		}
		b.Run(kind, func(b *testing.B) {
			b.Run("transform", func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(len(wire)))
				for b.Loop() {
					benchmarkRequestBodySink, err = buildRequestBody(req, opts)
					if err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("marshal", func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(len(wire)))
				for b.Loop() {
					benchmarkRequestBytesSink, err = marshalRequestBody(body)
					if err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("json_marshal", func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(len(wire)))
				for b.Loop() {
					benchmarkRequestBytesSink, err = json.Marshal(body)
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}

func BenchmarkBuildRequestImages(b *testing.B) {
	for _, size := range []int{4 << 10, 256 << 10, 2 << 20} {
		b.Run(fmt.Sprintf("bytes_%d", size), func(b *testing.B) {
			data := make([]byte, size)
			for i := range data {
				data[i] = byte(i)
			}
			req := benchmarkBaseRequest(nil, nil)
			req.Messages = []protocol.Message{{
				Role: protocol.RoleUser,
				Content: []protocol.ContentBlock{
					{Type: protocol.BlockText, Text: "inspect this benchmark image"},
					{Type: protocol.BlockImage, MIMEType: "image/png", Data: data},
				},
			}}
			opts := RequestOptions{ProviderID: "benchmark"}
			body, err := BuildRequest(req, opts)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(body)))
			for b.Loop() {
				benchmarkRequestBytesSink, err = BuildRequest(req, opts)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func benchmarkChatRequest(kind string, messageCount, toolCount int) (protocol.ChatRequest, RequestOptions) {
	messages := make([]protocol.Message, 0, messageCount)
	text := strings.Repeat("provider request benchmark text ", 4)
	for i := range messageCount {
		switch kind {
		case "plain":
			role := protocol.RoleUser
			if i%2 == 1 {
				role = protocol.RoleAssistant
			}
			messages = append(messages, protocol.Message{Role: role, Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: text}}})
		case "tool_heavy":
			callID := fmt.Sprintf("call-%d", i/2)
			if i%2 == 0 {
				messages = append(messages, protocol.Message{
					Role: protocol.RoleAssistant, StopReason: protocol.StopToolUse,
					Content: []protocol.ContentBlock{{Type: protocol.BlockToolCall, ToolCallID: callID, Name: "read", Arguments: json.RawMessage(`{"path":"README.md"}`)}},
				})
			} else {
				messages = append(messages, protocol.Message{
					Role: protocol.RoleTool, ToolCallID: callID, ToolName: "read",
					Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: text}},
				})
			}
		case "continuity":
			id := fmt.Sprintf("reasoning-%d", i)
			wire := json.RawMessage(fmt.Sprintf(`{"type":"reasoning","id":%q,"summary":[],"encrypted_content":"opaque-%d"}`, id, i))
			messages = append(messages, protocol.Message{
				Role: protocol.RoleAssistant, Provider: "benchmark", StopReason: protocol.StopStop,
				Content: []protocol.ContentBlock{
					{Type: protocol.BlockProviderData, Name: id, Data: wire},
					{Type: protocol.BlockText, Text: text},
				},
			})
		default:
			panic("unknown benchmark request kind: " + kind)
		}
	}
	tools := make([]protocol.ToolSchema, 0, toolCount)
	for i := range toolCount {
		tools = append(tools, protocol.ToolSchema{
			Name:        fmt.Sprintf("benchmark_tool_%02d", i),
			Description: "Inspect benchmark repository data without changing it.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
		})
	}
	return benchmarkBaseRequest(messages, tools), RequestOptions{ProviderID: "benchmark", IncludeEncryptedReasoning: true}
}

func benchmarkBaseRequest(messages []protocol.Message, tools []protocol.ToolSchema) protocol.ChatRequest {
	return protocol.ChatRequest{
		Model:     protocol.Model{Provider: "benchmark", ID: "benchmark-model", SupportsTools: true},
		Messages:  messages,
		Tools:     tools,
		System:    "You are a benchmark model. Preserve tool ordering and provider continuity.",
		MaxTokens: 4096,
	}
}
