package supervisor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/snow-core/snow/pkg/protocol"
)

const maxWorkerStderr = 64 * 1024

var errWorkerExited = errors.New("supervisor: worker process exited")

type responseFrame struct {
	ID        string          `json:"id,omitempty"`
	Type      string          `json:"type"`
	Command   string          `json:"command,omitempty"`
	Success   bool            `json:"success"`
	Error     string          `json:"error,omitempty"`
	ErrorCode string          `json:"error_code,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
}

type rpcClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	ready  protocol.RPCReady
	info   protocol.RPCSessionInfo
	events chan protocol.AgentEvent
	done   chan struct{}

	writeMu  sync.Mutex
	mu       sync.Mutex
	pending  map[string]chan responseFrame
	prompts  map[string]chan protocol.RPCPromptCompleted
	failure  error
	closing  bool
	maxInput int

	stderrMu sync.Mutex
	stderr   []byte

	requestSeq atomic.Uint64
	failOnce   sync.Once
	closeOnce  sync.Once
}

func startRPCClient(ctx context.Context, opts Options, req StartRequest) (*rpcClient, []protocol.Message, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	commandFactory := opts.CommandFactory
	if commandFactory == nil {
		commandFactory = func(commandCtx context.Context, start StartRequest) *exec.Cmd {
			args := []string{"resume", start.SessionPath, "--mode", "rpc", "--permission", "ask", "--no-subagents"}
			if start.Provider != "" {
				args = append(args, "--provider", start.Provider)
			}
			if start.Model != "" {
				args = append(args, "--model", start.Model)
			}
			if start.Thinking != "" {
				args = append(args, "--thinking", string(start.Thinking))
			}
			if start.ConfigPath != "" {
				args = append(args, "--config", start.ConfigPath)
			}
			if start.AuthPath != "" {
				args = append(args, "--auth", start.AuthPath)
			}
			if start.RequireSandbox {
				args = append(args, "--require-sandbox")
			}
			if start.DisableSandbox {
				args = append(args, "--no-sandbox")
			}
			return exec.CommandContext(commandCtx, opts.Executable, args...)
		}
	}
	cmd := commandFactory(ctx, req)
	if cmd == nil {
		return nil, nil, errors.New("supervisor: command factory returned nil")
	}
	cmd.Dir = req.WorktreePath
	if len(cmd.Env) == 0 {
		cmd.Env = os.Environ()
	}
	configureProcess(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("supervisor: worker stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("supervisor: worker stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("supervisor: worker stderr: %w", err)
	}
	client := &rpcClient{
		cmd: cmd, stdin: stdin, events: make(chan protocol.AgentEvent, 256), done: make(chan struct{}),
		pending: make(map[string]chan responseFrame), prompts: make(map[string]chan protocol.RPCPromptCompleted), maxInput: protocol.RPCMaxInputBytes,
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("supervisor: start worker: %w", err)
	}
	go client.captureStderr(stderr)
	readyResult := make(chan error, 1)
	go client.readLoop(stdout, readyResult)
	go func() {
		err := cmd.Wait()
		if err == nil {
			err = errWorkerExited
		}
		client.fail(err)
	}()

	startupTimeout := opts.StartupTimeout
	if startupTimeout <= 0 {
		startupTimeout = 15 * time.Second
	}
	startupCtx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()
	select {
	case err := <-readyResult:
		if err != nil {
			_ = client.forceClose(opts.ShutdownTimeout)
			return nil, nil, fmt.Errorf("supervisor: worker handshake: %w", err)
		}
	case <-startupCtx.Done():
		_ = client.forceClose(opts.ShutdownTimeout)
		return nil, nil, fmt.Errorf("supervisor: worker handshake: %w", startupCtx.Err())
	case <-client.done:
		return nil, nil, fmt.Errorf("supervisor: worker exited before handshake: %w", client.failureSnapshot())
	}

	required := []string{"permission_input", "prompt_completion", "session_info", "session_messages"}
	for _, capability := range required {
		if !containsString(client.ready.Capabilities, capability) {
			_ = client.forceClose(opts.ShutdownTimeout)
			return nil, nil, fmt.Errorf("supervisor: worker lacks required RPC capability %q", capability)
		}
	}
	if client.ready.ProtocolVersion != protocol.RPCProtocolVersion {
		_ = client.forceClose(opts.ShutdownTimeout)
		return nil, nil, fmt.Errorf("supervisor: worker RPC protocol %q, want %q", client.ready.ProtocolVersion, protocol.RPCProtocolVersion)
	}
	if client.ready.MaxInputBytes > 0 && client.ready.MaxInputBytes < client.maxInput {
		client.maxInput = client.ready.MaxInputBytes
	}

	var info protocol.RPCSessionInfo
	if err := client.requestData(startupCtx, "session_info", nil, &info); err != nil {
		_ = client.forceClose(opts.ShutdownTimeout)
		return nil, nil, fmt.Errorf("supervisor: verify worker session: %w", err)
	}
	if err := verifySessionIdentity(info, req); err != nil {
		_ = client.forceClose(opts.ShutdownTimeout)
		return nil, nil, err
	}
	client.info = info
	var transcript protocol.RPCSessionMessages
	params := protocol.RPCSessionMessagesRequest{Limit: protocol.RPCSessionMessagesMax}
	if err := client.requestData(startupCtx, "session_messages", params, &transcript); err != nil {
		_ = client.forceClose(opts.ShutdownTimeout)
		return nil, nil, fmt.Errorf("supervisor: hydrate worker session: %w", err)
	}
	return client, transcript.Messages, nil
}

func (c *rpcClient) readLoop(reader io.Reader, readyResult chan<- error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), protocol.RPCMaxInputBytes)
	first := true
	for scanner.Scan() {
		frame := append([]byte(nil), scanner.Bytes()...)
		if !utf8.Valid(frame) {
			err := errors.New("invalid UTF-8 frame")
			if first {
				readyResult <- err
			}
			c.fail(err)
			return
		}
		if first {
			first = false
			var ready protocol.RPCReady
			if err := json.Unmarshal(frame, &ready); err != nil || ready.Type != protocol.RPCTypeReady {
				if err == nil {
					err = fmt.Errorf("first frame type %q, want %q", ready.Type, protocol.RPCTypeReady)
				}
				readyResult <- err
				c.fail(err)
				return
			}
			c.ready = ready
			readyResult <- nil
			continue
		}
		if err := c.routeFrame(frame); err != nil {
			c.fail(err)
			return
		}
	}
	if first {
		err := scanner.Err()
		if err == nil {
			err = io.ErrUnexpectedEOF
		}
		readyResult <- err
	}
	if err := scanner.Err(); err != nil {
		c.fail(fmt.Errorf("supervisor: read worker RPC: %w", err))
	}
}

func (c *rpcClient) routeFrame(frame []byte) error {
	var header struct {
		Type string `json:"type"`
		ID   string `json:"id,omitempty"`
	}
	if err := json.Unmarshal(frame, &header); err != nil {
		return fmt.Errorf("supervisor: decode worker frame: %w", err)
	}
	switch header.Type {
	case "response":
		var response responseFrame
		if err := json.Unmarshal(frame, &response); err != nil {
			return err
		}
		c.mu.Lock()
		waiter := c.pending[response.ID]
		delete(c.pending, response.ID)
		c.mu.Unlock()
		if waiter != nil {
			waiter <- response
		}
		return nil
	case protocol.RPCTypePromptCompleted:
		var completed protocol.RPCPromptCompleted
		if err := json.Unmarshal(frame, &completed); err != nil {
			return err
		}
		c.mu.Lock()
		waiter := c.prompts[completed.RequestID]
		delete(c.prompts, completed.RequestID)
		c.mu.Unlock()
		if waiter != nil {
			waiter <- completed
		}
		return nil
	case protocol.RPCTypeReady:
		return errors.New("supervisor: duplicate rpc_ready frame")
	default:
		var event protocol.AgentEvent
		if err := json.Unmarshal(frame, &event); err != nil {
			return err
		}
		cloned := event.Clone()
		select {
		case c.events <- cloned:
		default:
			// Streaming deltas are lossy at this internal observation boundary;
			// durable conversation hydration remains available after completion.
			if event.Type == protocol.EvTextDelta || event.Type == protocol.EvThinkingDelta || event.Type == protocol.EvToolProgress {
				return nil
			}
			select {
			case <-c.events:
			default:
			}
			select {
			case c.events <- cloned:
			default:
			}
		}
		return nil
	}
}

func (c *rpcClient) requestData(ctx context.Context, command string, params any, target any) error {
	request := protocol.RPCRequest{ID: c.nextID(command), Type: command}
	if params != nil {
		encoded, err := json.Marshal(params)
		if err != nil {
			return err
		}
		request.Params = encoded
	}
	response, err := c.request(ctx, request)
	if err != nil {
		return err
	}
	if target != nil && len(response.Data) > 0 {
		if err := json.Unmarshal(response.Data, target); err != nil {
			return fmt.Errorf("decode %s response: %w", command, err)
		}
	}
	return nil
}

func (c *rpcClient) request(ctx context.Context, request protocol.RPCRequest) (responseFrame, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	waiter := make(chan responseFrame, 1)
	c.mu.Lock()
	if c.failure != nil || c.closing {
		err := c.failure
		if err == nil {
			err = errWorkerExited
		}
		c.mu.Unlock()
		return responseFrame{}, err
	}
	if _, exists := c.pending[request.ID]; exists {
		c.mu.Unlock()
		return responseFrame{}, fmt.Errorf("supervisor: duplicate request ID %q", request.ID)
	}
	c.pending[request.ID] = waiter
	c.mu.Unlock()
	if err := c.write(request); err != nil {
		c.mu.Lock()
		delete(c.pending, request.ID)
		c.mu.Unlock()
		return responseFrame{}, err
	}
	select {
	case response := <-waiter:
		if !response.Success {
			return response, fmt.Errorf("worker %s: %s", request.Type, response.Error)
		}
		return response, nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, request.ID)
		c.mu.Unlock()
		return responseFrame{}, ctx.Err()
	case <-c.done:
		return responseFrame{}, c.failureSnapshot()
	}
}

func (c *rpcClient) write(value any) error {
	frame, err := json.Marshal(value)
	if err != nil {
		return err
	}
	frame = append(frame, '\n')
	if len(frame) > c.maxInput {
		return fmt.Errorf("supervisor: RPC frame is %d bytes, maximum %d", len(frame), c.maxInput)
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err = c.stdin.Write(frame)
	return err
}

func (c *rpcClient) prompt(ctx context.Context, message string) error {
	requestID := c.nextID("prompt")
	terminal := make(chan protocol.RPCPromptCompleted, 1)
	c.mu.Lock()
	if c.failure != nil || c.closing {
		err := c.failure
		if err == nil {
			err = errWorkerExited
		}
		c.mu.Unlock()
		return err
	}
	c.prompts[requestID] = terminal
	c.mu.Unlock()
	_, err := c.request(ctx, protocol.RPCRequest{ID: requestID, Type: "prompt", Message: message})
	if err != nil {
		if ctx.Err() != nil {
			abortCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = c.command(abortCtx, "abort", "", nil)
			cancel()
			select {
			case <-terminal:
			case <-c.done:
			case <-time.After(5 * time.Second):
			}
		}
		c.mu.Lock()
		delete(c.prompts, requestID)
		c.mu.Unlock()
		return err
	}
	select {
	case completed := <-terminal:
		switch completed.Status {
		case protocol.RPCPromptCompletedStatus:
			return nil
		case protocol.RPCPromptCanceledStatus:
			return context.Canceled
		default:
			return errors.New(completed.Error)
		}
	case <-ctx.Done():
		abortCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = c.command(abortCtx, "abort", "", nil)
		cancel()
		select {
		case <-terminal:
		case <-c.done:
		case <-time.After(5 * time.Second):
		}
		return ctx.Err()
	case <-c.done:
		return c.failureSnapshot()
	}
}

func (c *rpcClient) command(ctx context.Context, command, message string, params any) error {
	request := protocol.RPCRequest{ID: c.nextID(command), Type: command, Message: message}
	if params != nil {
		encoded, err := json.Marshal(params)
		if err != nil {
			return err
		}
		request.Params = encoded
	}
	_, err := c.request(ctx, request)
	return err
}

func (c *rpcClient) sessionMessages(ctx context.Context) ([]protocol.Message, error) {
	var transcript protocol.RPCSessionMessages
	err := c.requestData(ctx, "session_messages", protocol.RPCSessionMessagesRequest{Limit: protocol.RPCSessionMessagesMax}, &transcript)
	return transcript.Messages, err
}

func (c *rpcClient) nextID(prefix string) string {
	return fmt.Sprintf("supervisor-%s-%d", prefix, c.requestSeq.Add(1))
}

func (c *rpcClient) fail(err error) {
	if err == nil {
		err = errWorkerExited
	}
	c.failOnce.Do(func() {
		c.mu.Lock()
		c.failure = err
		pending := c.pending
		prompts := c.prompts
		c.pending = make(map[string]chan responseFrame)
		c.prompts = make(map[string]chan protocol.RPCPromptCompleted)
		c.mu.Unlock()
		for id, waiter := range pending {
			waiter <- responseFrame{ID: id, Type: "response", Success: false, Error: err.Error()}
		}
		for id, waiter := range prompts {
			waiter <- protocol.RPCPromptCompleted{Type: protocol.RPCTypePromptCompleted, RequestID: id, Status: protocol.RPCPromptFailedStatus, Error: err.Error()}
		}
		close(c.done)
	})
}

func (c *rpcClient) failureSnapshot() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failure != nil {
		return c.failure
	}
	return errWorkerExited
}

func (c *rpcClient) captureStderr(reader io.Reader) {
	buffer := make([]byte, 16*1024)
	for {
		n, err := reader.Read(buffer)
		if n > 0 {
			c.stderrMu.Lock()
			c.stderr = append(c.stderr, buffer[:n]...)
			if len(c.stderr) > maxWorkerStderr {
				c.stderr = append([]byte(nil), c.stderr[len(c.stderr)-maxWorkerStderr:]...)
			}
			c.stderrMu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func (c *rpcClient) stderrSuffix() string {
	c.stderrMu.Lock()
	defer c.stderrMu.Unlock()
	trimmed := strings.TrimSpace(string(bytes.ToValidUTF8(c.stderr, []byte("?"))))
	if trimmed == "" {
		return ""
	}
	return ": " + trimmed
}

func (c *rpcClient) close(timeout time.Duration) error {
	var result error
	c.closeOnce.Do(func() {
		if timeout <= 0 {
			timeout = 5 * time.Second
		}
		c.mu.Lock()
		active := len(c.prompts) > 0
		c.mu.Unlock()
		if active {
			abortCtx, cancel := context.WithTimeout(context.Background(), timeout/2)
			_ = c.command(abortCtx, "abort", "", nil)
			cancel()
		}
		c.mu.Lock()
		c.closing = true
		c.mu.Unlock()
		_ = c.stdin.Close()
		select {
		case <-c.done:
			// The RPC parent may have exited while one of its descendants remains
			// in the supervisor-owned process group. Reap the group as well.
			_ = terminateProcess(c.cmd)
			return
		case <-time.After(timeout):
		}
		_ = terminateProcess(c.cmd)
		select {
		case <-c.done:
			// The RPC parent may have exited while one of its descendants remains
			// in the supervisor-owned process group. Reap the group as well.
			_ = terminateProcess(c.cmd)
			return
		case <-time.After(timeout):
		}
		result = killProcess(c.cmd)
		select {
		case <-c.done:
		case <-time.After(timeout):
			result = errors.Join(result, errors.New("supervisor: worker did not exit after kill"))
		}
	})
	return result
}

func (c *rpcClient) forceClose(timeout time.Duration) error {
	c.mu.Lock()
	c.closing = true
	c.mu.Unlock()
	_ = c.stdin.Close()
	_ = terminateProcess(c.cmd)
	select {
	case <-c.done:
		return nil
	case <-time.After(max(timeout, time.Second)):
	}
	return killProcess(c.cmd)
}

func verifySessionIdentity(info protocol.RPCSessionInfo, req StartRequest) error {
	if req.SessionID != "" && info.SessionID != req.SessionID {
		return fmt.Errorf("supervisor: worker session ID %q, want %q", info.SessionID, req.SessionID)
	}
	if !sameCanonicalPath(info.Path, req.SessionPath) {
		return fmt.Errorf("supervisor: worker session path %q does not match %q", info.Path, req.SessionPath)
	}
	if !sameCanonicalPath(info.CWD, req.WorktreePath) {
		return fmt.Errorf("supervisor: worker CWD %q does not match worktree %q", info.CWD, req.WorktreePath)
	}
	if info.PermissionMode != "ask" {
		return fmt.Errorf("supervisor: worker permission mode %q, want ask", info.PermissionMode)
	}
	return nil
}

func sameCanonicalPath(left, right string) bool {
	canonical := func(path string) string {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return filepath.Clean(path)
		}
		resolved, err := filepath.EvalSymlinks(absolute)
		if err == nil {
			return filepath.Clean(resolved)
		}
		return filepath.Clean(absolute)
	}
	return canonical(left) == canonical(right)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
