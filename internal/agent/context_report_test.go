package agent

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestBuildContextReportCategorizesProviderRequest(t *testing.T) {
	req := protocol.ChatRequest{
		Model:  protocol.Model{Provider: "test", ContextWindow: 128000},
		System: "system instructions",
		Tools: []protocol.ToolSchema{{
			Name: "read", Description: "read a file", Parameters: json.RawMessage(`{"type":"object"}`),
		}},
		InternalContext: []protocol.InternalContextFragment{{Source: "goal", Text: "continue the objective"}},
		Messages: []protocol.Message{
			{Role: protocol.RoleUser, Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: "question"}, {Type: protocol.BlockImage, MIMEType: "image/png", Data: []byte("image bytes")}}},
			{Role: protocol.RoleAssistant, Provider: "test", StopReason: protocol.StopStop, Content: []protocol.ContentBlock{
				{Type: protocol.BlockThinking, Text: "reasoning"},
				{Type: protocol.BlockText, Text: "answer"},
				{Type: protocol.BlockToolCall, Name: "read", ToolCallID: "call-1", Arguments: json.RawMessage(`{"path":"x"}`)},
				{Type: protocol.BlockProviderData, Data: []byte("opaque")},
			}},
			{Role: protocol.RoleTool, ToolName: "read", ToolCallID: "call-1", Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: "file contents"}}},
			{Role: protocol.RoleAgent, Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: "child report"}}},
			{Role: protocol.RoleSystem, Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: "system message"}}},
			{Role: protocol.RoleCustom, Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: "compaction summary"}}},
		},
	}

	report := buildContextReport(req, true)
	if !report.LatestRequest || report.ContextWindow != 128000 || report.MessageCount != len(req.Messages) || report.ToolCount != 1 {
		t.Fatalf("report metadata = %+v", report)
	}
	categories := make(map[string]ContextCategory, len(report.Categories))
	totalBytes := 0
	for _, category := range report.Categories {
		categories[category.Name] = category
		totalBytes += category.Bytes
		if category.EstimatedTokens <= 0 {
			t.Errorf("category %q has no token estimate: %+v", category.Name, category)
		}
	}
	for _, name := range []string{
		"System prompt", "Tool schemas", "Internal steering", "User messages", "Assistant responses",
		"Tool calls", "Tool results", "Agent messages", "Images", "Provider state", "Other messages",
	} {
		if categories[name].Bytes == 0 {
			t.Errorf("missing category %q in %+v", name, report.Categories)
		}
	}
	if categories["System prompt"].Items != 1 {
		t.Fatalf("system category items = %d, want only assembled provider system prompt", categories["System prompt"].Items)
	}
	if _, ok := categories["Assistant reasoning"]; ok {
		t.Fatalf("raw thinking blocks must not be counted as provider input: %+v", categories["Assistant reasoning"])
	}
	if categories["Images"].Items != 1 {
		t.Fatalf("image category items = %d, want 1", categories["Images"].Items)
	}
	if want := estimatedTokensForBytes(totalBytes); report.EstimatedInputTokens != want {
		t.Fatalf("estimated total = %d, want %d", report.EstimatedInputTokens, want)
	}
}

func TestEstimateImageTokensUsesDimensionsNotCompressedFileSize(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 320, 160))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	small := encoded.Bytes()
	large := append(append([]byte(nil), small...), make([]byte, 2<<20)...)
	if got, want := estimateImageTokens(small), 85; got != want {
		t.Fatalf("320x160 image estimate = %d, want %d", got, want)
	}
	if smallTokens, largeTokens := estimateImageTokens(small), estimateImageTokens(large); smallTokens != largeTokens {
		t.Fatalf("same-dimension image estimates vary by file size: small=%d large=%d", smallTokens, largeTokens)
	}
	if got, want := estimateImageTokens([]byte("not an image")), 1024; got != want {
		t.Fatalf("unknown image estimate = %d, want %d", got, want)
	}
}

func TestContextReportRetainsLatestProviderUsage(t *testing.T) {
	provider := &scriptedProvider{scripts: [][]protocol.StreamEvent{{
		{Type: protocol.EvStreamUsage, Usage: &protocol.Usage{Input: 123, Output: 17, Total: 140}},
		{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
	}}}
	a, err := New(Options{
		Provider: provider, Registry: tools.NewRegistry(), Session: session.NewMemoryStore(session.Options{}),
		Permission:   permission.NewService(permission.ModeAllow, nil),
		SystemPrompt: "system", Model: protocol.Model{Provider: provider.ID(), ID: "m", ContextWindow: 1000},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	if err := a.Prompt(t.Context(), "hello"); err != nil {
		t.Fatal(err)
	}
	report, err := a.ContextReport()
	if err != nil {
		t.Fatal(err)
	}
	if !report.LatestRequest || report.Usage == nil || report.Usage.Input != 123 || report.Usage.Output != 17 {
		t.Fatalf("latest context report = %+v", report)
	}
	if report.EstimatedInputTokens == 0 || report.MessageCount != 1 {
		t.Fatalf("request estimate = %+v", report)
	}

	// Returned reports are isolated from the agent's retained snapshot.
	report.Usage.Input = 999
	report.Categories[0].Bytes = 0
	again, err := a.ContextReport()
	if err != nil {
		t.Fatal(err)
	}
	if again.Usage.Input != 123 || again.Categories[0].Bytes == 0 {
		t.Fatalf("retained report was mutated through caller copy: %+v", again)
	}
}
