// Package plugin implements the out-of-process JSON-RPC tool plugin protocol
// (IMPLEMENTATION.md §10.3): JSONL on stdin/stdout, handshake via initialize,
// then tools/list and tools/call. Plugins enable crash isolation and
// non-Go implementations.
package plugin

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"

	"github.com/snow-core/snow/internal/tools"
	"github.com/snow-core/snow/pkg/protocol"
)

// ProtocolVersion is the plugin protocol version.
const ProtocolVersion = 1

// Request is one JSON-RPC request line sent to the plugin.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is one JSON-RPC response line read from the plugin.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError mirrors the JSON-RPC error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// InitResult is the initialize response.
type InitResult struct {
	Name    string             `json:"name"`
	Version string             `json:"version"`
	Tools   []tools.ToolSchema `json:"tools"`
}

// Host manages a plugin subprocess.
type Host struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Scanner
	mu     sync.Mutex
	nextID int
	// toolCache caches schemas returned by tools/list.
	toolCache []tools.ToolSchema
}

// Spawn starts a plugin binary (or "cmd args..." style command via sh -c).
func Spawn(ctx context.Context, binPath string, cwd string) (*Host, error) {
	cmd := exec.CommandContext(ctx, binPath)
	if cwd != "" {
		cmd.Dir = cwd
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("plugin: stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("plugin: stdout: %w", err)
	}
	cmd.Stderr = io.Discard // plugin logs go to stderr; discard by default
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("plugin: start: %w", err)
	}
	h := &Host{cmd: cmd, stdin: stdin, stdout: bufio.NewScanner(stdout)}
	h.stdout.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	return h, nil
}

// Initialize negotiates the protocol and fetches the tool catalog.
func (h *Host) Initialize(ctx context.Context) (InitResult, error) {
	var res InitResult
	params, _ := json.Marshal(map[string]any{"protocol_version": ProtocolVersion})
	if err := h.call(ctx, "initialize", params, &res); err != nil {
		return res, err
	}
	h.toolCache = res.Tools
	return res, nil
}

// ToolSchemas returns the tools the plugin exposes (from initialize).
func (h *Host) ToolSchemas() []tools.ToolSchema {
	return h.toolCache
}

// Call invokes a tool.
func (h *Host) Call(ctx context.Context, name string, args json.RawMessage) (tools.ToolResult, error) {
	var res struct {
		Content []json.RawMessage `json:"content"`
		IsError bool              `json:"is_error"`
	}
	params, _ := json.Marshal(map[string]any{"name": name, "arguments": json.RawMessage(args)})
	if err := h.call(ctx, "tools/call", params, &res); err != nil {
		return tools.ErrorResult(err), err
	}
	var blocks []protocolBlock
	for _, c := range res.Content {
		var b protocolBlock
		if err := json.Unmarshal(c, &b); err != nil {
			return tools.ErrorResult(fmt.Errorf("plugin: bad content block: %w", err)), nil
		}
		blocks = append(blocks, b)
	}
	return tools.ToolResult{Content: toProtocol(blocks), IsError: res.IsError}, nil
}

// Close terminates the plugin.
func (h *Host) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cmd != nil && h.cmd.Process != nil {
		_ = h.stdin.Close()
		return h.cmd.Wait()
	}
	return nil
}

func (h *Host) call(ctx context.Context, method string, params json.RawMessage, out any) error {
	h.mu.Lock()
	h.nextID++
	id := h.nextID
	h.mu.Unlock()

	req := Request{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	line, err := json.Marshal(req)
	if err != nil {
		return err
	}
	h.mu.Lock()
	_, err = h.stdin.Write(append(line, '\n'))
	h.mu.Unlock()
	if err != nil {
		return fmt.Errorf("plugin: write: %w", err)
	}

	if !h.stdout.Scan() {
		if err := h.stdout.Err(); err != nil {
			return fmt.Errorf("plugin: read: %w", err)
		}
		return errors.New("plugin: EOF before response (did it crash?)")
	}
	var resp Response
	if err := json.Unmarshal(h.stdout.Bytes(), &resp); err != nil {
		return fmt.Errorf("plugin: bad response JSON: %w", err)
	}
	if resp.ID != id {
		return fmt.Errorf("plugin: response id %d != %d", resp.ID, id)
	}
	if resp.Error != nil {
		return fmt.Errorf("plugin %s: %s", method, resp.Error.Message)
	}
	if out == nil || len(resp.Result) == 0 {
		return nil
	}
	return json.Unmarshal(resp.Result, out)
}

// protocolBlock is the wire shape of a content block.
type protocolBlock struct {
	Type       string          `json:"type"`
	Text       string          `json:"text,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	Name       string          `json:"name,omitempty"`
	Arguments  json.RawMessage `json:"arguments,omitempty"`
}

func toProtocol(blocks []protocolBlock) []protocol.ContentBlock {
	out := make([]protocol.ContentBlock, 0, len(blocks))
	for _, b := range blocks {
		out = append(out, protocol.ContentBlock{
			Type:       protocol.ContentBlockType(b.Type),
			Text:       b.Text,
			ToolCallID: b.ToolCallID,
			Name:       b.Name,
			Arguments:  b.Arguments,
		})
	}
	return out
}
