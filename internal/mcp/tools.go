package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/snow-core/snow/internal/tools"
	"github.com/snow-core/snow/pkg/protocol"
)

type remoteTool struct {
	runtime    *serverRuntime
	schema     tools.ToolSchema
	remoteName string
}

func (t *remoteTool) Schema() tools.ToolSchema { return t.schema }

func (t *remoteTool) Run(ctx context.Context, raw json.RawMessage, _ tools.ToolHost) (tools.ToolResult, error) {
	var args map[string]any
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &args); err != nil {
			return tools.ErrorResult(fmt.Errorf("mcp %s %s: invalid arguments: %w", t.runtime.spec.ID, t.remoteName, err)), nil
		}
	}
	if args == nil {
		args = map[string]any{}
	}
	result, err := t.runtime.session.CallTool(ctx, &sdkmcp.CallToolParams{Name: t.remoteName, Arguments: args})
	if err != nil {
		return tools.ErrorResult(fmt.Errorf("mcp %s %s: %w", t.runtime.spec.ID, t.remoteName, err)), nil
	}
	blocks := convertContents(result.Content)
	if result.StructuredContent != nil {
		encoded, err := json.Marshal(result.StructuredContent)
		if err == nil {
			blocks = append(blocks, protocol.NewTextBlock("structuredContent:\n"+string(encoded)))
		}
	}
	if len(blocks) == 0 {
		blocks = []protocol.ContentBlock{protocol.NewTextBlock("MCP tool returned no content.")}
	}
	return tools.ToolResult{Content: boundBlocks(blocks, t.runtime.manager.opts.MaxOutputBytes), IsError: result.IsError}, nil
}

type bridgeTool struct {
	runtime *serverRuntime
	schema  tools.ToolSchema
	kind    string
}

func (t *bridgeTool) Schema() tools.ToolSchema          { return t.schema }
func (t *bridgeTool) setSchema(schema tools.ToolSchema) { t.schema = schema }

func setSchema(tool tools.Tool, schema tools.ToolSchema) {
	if setter, ok := tool.(interface{ setSchema(tools.ToolSchema) }); ok {
		setter.setSchema(schema)
	}
}

func newListResourcesTool(rt *serverRuntime) tools.Tool {
	return &bridgeTool{runtime: rt, kind: "list_resources", schema: tools.ToolSchema{
		Name: "list_resources", Description: "List resources or resource templates exposed by MCP server " + rt.spec.ID + ".",
		Parameters: json.RawMessage(`{"type":"object","properties":{"kind":{"type":"string","enum":["resources","templates"],"default":"resources"},"cursor":{"type":"string"}}}`),
	}}
}

func newReadResourceTool(rt *serverRuntime) tools.Tool {
	return &bridgeTool{runtime: rt, kind: "read_resource", schema: tools.ToolSchema{
		Name: "read_resource", Description: "Read a resource URI from MCP server " + rt.spec.ID + ".",
		Parameters: json.RawMessage(`{"type":"object","required":["uri"],"properties":{"uri":{"type":"string","description":"Exact MCP resource URI."}}}`),
	}}
}

func newResourceSubscriptionTool(rt *serverRuntime) tools.Tool {
	return &bridgeTool{runtime: rt, kind: "resource_subscription", schema: tools.ToolSchema{
		Name: "resource_subscription", Description: "Subscribe to or unsubscribe from updates for an MCP resource URI on server " + rt.spec.ID + ".",
		Parameters: json.RawMessage(`{"type":"object","required":["action","uri"],"properties":{"action":{"type":"string","enum":["subscribe","unsubscribe"]},"uri":{"type":"string"}}}`),
	}}
}

func newListPromptsTool(rt *serverRuntime) tools.Tool {
	return &bridgeTool{runtime: rt, kind: "list_prompts", schema: tools.ToolSchema{
		Name: "list_prompts", Description: "List prompt templates exposed by MCP server " + rt.spec.ID + ".",
		Parameters: json.RawMessage(`{"type":"object","properties":{"cursor":{"type":"string"}}}`),
	}}
}

func newGetPromptTool(rt *serverRuntime) tools.Tool {
	return &bridgeTool{runtime: rt, kind: "get_prompt", schema: tools.ToolSchema{
		Name: "get_prompt", Description: "Expand a named prompt template from MCP server " + rt.spec.ID + ".",
		Parameters: json.RawMessage(`{"type":"object","required":["name"],"properties":{"name":{"type":"string"},"arguments":{"type":"object","additionalProperties":{"type":"string"}}}}`),
	}}
}

func (t *bridgeTool) Run(ctx context.Context, raw json.RawMessage, _ tools.ToolHost) (tools.ToolResult, error) {
	var args struct {
		Kind      string            `json:"kind"`
		Cursor    string            `json:"cursor"`
		URI       string            `json:"uri"`
		Action    string            `json:"action"`
		Name      string            `json:"name"`
		Arguments map[string]string `json:"arguments"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return tools.ErrorResult(fmt.Errorf("mcp %s %s: invalid arguments: %w", t.runtime.spec.ID, t.kind, err)), nil
		}
	}
	var value any
	switch t.kind {
	case "list_resources":
		if args.Kind == "templates" {
			result, err := t.runtime.session.ListResourceTemplates(ctx, &sdkmcp.ListResourceTemplatesParams{Cursor: args.Cursor})
			if err != nil {
				return t.failure(err), nil
			}
			value = result
		} else {
			result, err := t.runtime.session.ListResources(ctx, &sdkmcp.ListResourcesParams{Cursor: args.Cursor})
			if err != nil {
				return t.failure(err), nil
			}
			value = result
		}
	case "read_resource":
		if strings.TrimSpace(args.URI) == "" {
			return tools.ErrorResult(fmt.Errorf("mcp %s read_resource: uri is required", t.runtime.spec.ID)), nil
		}
		result, err := t.runtime.session.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: args.URI})
		if err != nil {
			return t.failure(err), nil
		}
		var blocks []protocol.ContentBlock
		for _, content := range result.Contents {
			blocks = append(blocks, convertResource(content)...)
		}
		if len(blocks) == 0 {
			blocks = append(blocks, protocol.NewTextBlock("MCP resource returned no content."))
		}
		return tools.ToolResult{Content: boundBlocks(blocks, t.runtime.manager.opts.MaxOutputBytes)}, nil
	case "resource_subscription":
		if strings.TrimSpace(args.URI) == "" {
			return tools.ErrorResult(fmt.Errorf("mcp %s resource_subscription: uri is required", t.runtime.spec.ID)), nil
		}
		var err error
		switch args.Action {
		case "subscribe":
			err = t.runtime.session.Subscribe(ctx, &sdkmcp.SubscribeParams{URI: args.URI})
		case "unsubscribe":
			err = t.runtime.session.Unsubscribe(ctx, &sdkmcp.UnsubscribeParams{URI: args.URI})
		default:
			return tools.ErrorResult(fmt.Errorf("mcp %s resource_subscription: action must be subscribe or unsubscribe", t.runtime.spec.ID)), nil
		}
		if err != nil {
			return t.failure(err), nil
		}
		return tools.TextResult(args.Action + "d " + args.URI), nil
	case "list_prompts":
		result, err := t.runtime.session.ListPrompts(ctx, &sdkmcp.ListPromptsParams{Cursor: args.Cursor})
		if err != nil {
			return t.failure(err), nil
		}
		value = result
	case "get_prompt":
		if strings.TrimSpace(args.Name) == "" {
			return tools.ErrorResult(fmt.Errorf("mcp %s get_prompt: name is required", t.runtime.spec.ID)), nil
		}
		result, err := t.runtime.session.GetPrompt(ctx, &sdkmcp.GetPromptParams{Name: args.Name, Arguments: args.Arguments})
		if err != nil {
			return t.failure(err), nil
		}
		blocks := []protocol.ContentBlock{}
		if result.Description != "" {
			blocks = append(blocks, protocol.NewTextBlock("Prompt description: "+result.Description))
		}
		for _, message := range result.Messages {
			converted := convertContents([]sdkmcp.Content{message.Content})
			for i := range converted {
				if converted[i].Type == protocol.BlockText {
					converted[i].Text = string(message.Role) + ": " + converted[i].Text
				}
			}
			blocks = append(blocks, converted...)
		}
		return tools.ToolResult{Content: boundBlocks(blocks, t.runtime.manager.opts.MaxOutputBytes)}, nil
	default:
		return tools.ErrorResult(fmt.Errorf("unknown MCP bridge operation %q", t.kind)), nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return t.failure(err), nil
	}
	return tools.TextResult(boundText(string(encoded), t.runtime.manager.opts.MaxOutputBytes)), nil
}

func (t *bridgeTool) failure(err error) tools.ToolResult {
	return tools.ErrorResult(fmt.Errorf("mcp %s %s: %w", t.runtime.spec.ID, t.kind, err))
}

func marshalSchema(schema any) (json.RawMessage, error) {
	if schema == nil {
		return json.RawMessage(`{"type":"object"}`), nil
	}
	data, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil || object == nil {
		return nil, fmt.Errorf("inputSchema is not a JSON object")
	}
	return data, nil
}

func convertContents(contents []sdkmcp.Content) []protocol.ContentBlock {
	var blocks []protocol.ContentBlock
	for _, content := range contents {
		switch value := content.(type) {
		case *sdkmcp.TextContent:
			blocks = append(blocks, protocol.NewTextBlock(value.Text))
		case *sdkmcp.ImageContent:
			blocks = append(blocks, protocol.ContentBlock{Type: protocol.BlockImage, MIMEType: value.MIMEType, Data: append([]byte(nil), value.Data...)})
		case *sdkmcp.AudioContent:
			blocks = append(blocks, protocol.NewTextBlock(fmt.Sprintf("audio content (%s, base64):\n%s", value.MIMEType, base64.StdEncoding.EncodeToString(value.Data))))
		case *sdkmcp.ResourceLink:
			encoded, _ := json.Marshal(map[string]any{"type": "resource_link", "uri": value.URI, "name": value.Name, "title": value.Title, "description": value.Description, "mimeType": value.MIMEType, "size": value.Size})
			blocks = append(blocks, protocol.NewTextBlock(string(encoded)))
		case *sdkmcp.EmbeddedResource:
			blocks = append(blocks, convertResource(value.Resource)...)
		default:
			if encoded, err := content.MarshalJSON(); err == nil {
				blocks = append(blocks, protocol.NewTextBlock(string(encoded)))
			}
		}
	}
	return blocks
}

func convertResource(content *sdkmcp.ResourceContents) []protocol.ContentBlock {
	if content == nil {
		return nil
	}
	if content.Blob == nil {
		return []protocol.ContentBlock{protocol.NewTextBlock(fmt.Sprintf("resource %s (%s):\n%s", content.URI, content.MIMEType, content.Text))}
	}
	if strings.HasPrefix(content.MIMEType, "image/") {
		return []protocol.ContentBlock{{Type: protocol.BlockImage, MIMEType: content.MIMEType, Data: append([]byte(nil), content.Blob...)}}
	}
	return []protocol.ContentBlock{protocol.NewTextBlock(fmt.Sprintf("resource %s (%s, %d bytes, base64):\n%s", content.URI, content.MIMEType, len(content.Blob), base64.StdEncoding.EncodeToString(content.Blob)))}
}

func boundBlocks(blocks []protocol.ContentBlock, max int) []protocol.ContentBlock {
	if max <= 0 {
		return blocks
	}
	remaining := max
	out := make([]protocol.ContentBlock, 0, len(blocks))
	truncated := false
	for _, block := range blocks {
		size := len(block.Text) + len(block.Data)
		if size <= remaining {
			out = append(out, block)
			remaining -= size
			continue
		}
		truncated = true
		if remaining > 0 && block.Type == protocol.BlockText {
			block.Text = boundText(block.Text, remaining)
			out = append(out, block)
		}
		break
	}
	if truncated {
		out = append(out, protocol.NewTextBlock("… [MCP output truncated]"))
	}
	return out
}

func boundText(value string, max int) string {
	if max <= 0 || len(value) <= max {
		return value
	}
	if max <= len("… [truncated]") {
		return strings.ToValidUTF8(value[:max], "")
	}
	return strings.ToValidUTF8(value[:max-len("… [truncated]")], "") + "… [truncated]"
}
