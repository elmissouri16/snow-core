package plugin

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/snow-core/snow/pkg/plugin"
	"github.com/snow-core/snow/pkg/protocol"
)

const (
	defaultFrameBytes    = 4 * 1024 * 1024
	defaultOutputBytes   = 256 * 1024
	defaultProgressBytes = 16 * 1024
)

// ExternalInitResult is the version-2 initialize result.
type ExternalInitResult struct {
	Manifest        plugin.Manifest       `json:"manifest"`
	Capabilities    []string              `json:"capabilities,omitempty"`
	Tools           []protocol.ToolSchema `json:"tools,omitempty"`
	SupportedEvents []plugin.EventType    `json:"supported_events,omitempty"`
	Limits          map[string]int        `json:"limits,omitempty"`

	// Name and Version are accepted as a convenience for small runtimes. New
	// runtimes should return Manifest.
	Name            string `json:"name,omitempty"`
	Version         string `json:"version,omitempty"`
	ProtocolVersion int    `json:"protocol_version,omitempty"`
}

type rpcResponseV2 struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type rpcRequestV2 struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type pendingV2 struct {
	callID   string
	progress func(ProgressNotification)
	result   chan rpcResponseV2
}

// ProgressNotification is a bounded progress notification from a child.
type ProgressNotification struct {
	CallID  string `json:"call_id"`
	Message string `json:"message,omitempty"`
	Done    bool   `json:"done"`
	IsError bool   `json:"is_error,omitempty"`
}

type eventNotification struct {
	Version int                 `json:"version,omitempty"`
	Type    plugin.EventType    `json:"type"`
	Payload protocol.AgentEvent `json:"payload"`
}

type logNotification struct {
	Severity string `json:"severity,omitempty"`
	Message  string `json:"message,omitempty"`
}

// ExternalHost manages one JSON-RPC 2.0-over-stdio plugin process. It has one
// reader multiplexer and correlates all responses by string request IDs.
type ExternalHost struct {
	spec plugin.PluginSpec
	cwd  string
	cmd  *exec.Cmd
	in   io.WriteCloser

	writeMu  sync.Mutex
	mu       sync.Mutex
	pending  map[string]*pendingV2
	failed   error
	done     chan struct{}
	failOnce sync.Once
	nextID   uint64

	maxFrame    int
	maxOutput   int
	maxProgress int
	maxInput    int
	semaphore   chan struct{}
	stderr      *boundedBuffer
	logs        *boundedBuffer
	waitDone    chan struct{}
	waitErr     error
	notifyQueue chan []byte
	closeOnce   sync.Once
	closeErr    error
	manifest    plugin.Manifest
	tools       []protocol.ToolSchema
}

// SpawnExternal starts an explicit argv-based plugin process. No shell is
// involved. Environment is exactly spec.Env when provided; an empty
// environment is used otherwise.
func SpawnExternal(ctx context.Context, spec plugin.PluginSpec, cwd string) (*ExternalHost, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := plugin.ValidateSpec(spec); err != nil {
		return nil, err
	}
	if spec.CWD != "" {
		cwd = spec.CWD
	}
	cmd := exec.CommandContext(ctx, spec.Command[0], spec.Command[1:]...)
	cmd.Dir = cwd
	cmd.Env = append([]string(nil), spec.Env...)
	if spec.Env == nil {
		cmd.Env = []string{}
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("plugin %s: stdin: %w", spec.ID, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("plugin %s: stdout: %w", spec.ID, err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("plugin %s: stderr: %w", spec.ID, err)
	}

	diagnosticLimit := defaultOutputBytes
	if spec.MaxOutputBytes > 0 && spec.MaxOutputBytes < diagnosticLimit {
		diagnosticLimit = spec.MaxOutputBytes
	}
	h := &ExternalHost{
		spec: spec, cwd: cwd, cmd: cmd, in: stdin,
		pending: make(map[string]*pendingV2), done: make(chan struct{}),
		maxFrame: spec.MaxFrameBytes, maxOutput: spec.MaxOutputBytes,
		maxProgress: spec.MaxProgressBytes, maxInput: spec.MaxInputBytes,
		stderr: newBoundedBuffer(diagnosticLimit), logs: newBoundedBuffer(diagnosticLimit),
		waitDone: make(chan struct{}), notifyQueue: make(chan []byte, 64),
	}
	if h.maxFrame <= 0 {
		h.maxFrame = defaultFrameBytes
	}
	if h.maxOutput <= 0 {
		h.maxOutput = defaultOutputBytes
	}
	if h.maxProgress <= 0 {
		h.maxProgress = defaultProgressBytes
	}
	if h.maxInput <= 0 {
		h.maxInput = h.maxFrame
	}
	concurrency := spec.MaxConcurrent
	if concurrency <= 0 {
		concurrency = 8
	}
	h.semaphore = make(chan struct{}, concurrency)
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderr.Close()
		return nil, fmt.Errorf("plugin %s: start: %w", spec.ID, err)
	}
	go h.readLoop(stdout)
	go h.writeNotificationLoop()
	go func() {
		// boundedBuffer bounds retained memory while draining the pipe so a
		// noisy child cannot deadlock on stderr.
		_, _ = io.Copy(h.stderr, stderr)
		_ = stderr.Close()
	}()
	go func() {
		err := cmd.Wait()
		h.mu.Lock()
		h.waitErr = err
		h.mu.Unlock()
		h.fail(err)
		close(h.waitDone)
	}()
	return h, nil
}

// SpawnV2 is an explicit alias for SpawnExternal.
func SpawnV2(ctx context.Context, spec plugin.PluginSpec, cwd string) (*ExternalHost, error) {
	return SpawnExternal(ctx, spec, cwd)
}

// Initialize performs the v2 handshake and refreshes tools/list.
func (h *ExternalHost) Initialize(ctx context.Context, hostVersion, cwd, sessionID string, capabilities []string) (ExternalInitResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	initCtx := ctx
	cancel := func() {}
	if h.spec.TimeoutMS > 0 {
		initCtx, cancel = context.WithTimeout(ctx, time.Duration(h.spec.TimeoutMS)*time.Millisecond)
	}
	defer cancel()
	var out ExternalInitResult
	params := map[string]any{
		"protocol_version":  plugin.ProtocolVersion,
		"host_version":      hostVersion,
		"cwd":               cwd,
		"session_id":        sessionID,
		"host_capabilities": capabilities,
		"config":            json.RawMessage(h.spec.Config),
	}
	b, err := json.Marshal(params)
	if err != nil {
		return out, fmt.Errorf("plugin %s: marshal initialize: %w", h.spec.ID, err)
	}
	if err := h.call(initCtx, "initialize", b, "", nil, &out); err != nil {
		return out, err
	}
	if out.Manifest.ID == "" {
		out.Manifest.ID = h.spec.ID
	}
	if out.Manifest.Name == "" {
		out.Manifest.Name = out.Name
	}
	if out.Manifest.Version == "" {
		out.Manifest.Version = out.Version
	}
	if out.Manifest.ProtocolVersion == 0 {
		out.Manifest.ProtocolVersion = out.ProtocolVersion
	}
	if out.Manifest.ProtocolVersion == 0 {
		out.Manifest.ProtocolVersion = plugin.ProtocolVersion
	}
	if err := plugin.ValidateManifest(out.Manifest); err != nil {
		return out, err
	}
	if out.Manifest.ID != h.spec.ID {
		return out, fmt.Errorf("plugin %s: manifest id %q does not match spec", h.spec.ID, out.Manifest.ID)
	}
	var listed struct {
		Tools []protocol.ToolSchema `json:"tools"`
	}
	if err := h.call(initCtx, "tools/list", []byte(`{}`), "", nil, &listed); err != nil {
		return out, err
	}
	if listed.Tools == nil {
		listed.Tools = out.Tools
	}
	if err := validateSchemas(listed.Tools); err != nil {
		return out, err
	}
	h.mu.Lock()
	h.manifest, h.tools = out.Manifest, cloneSchemas(listed.Tools)
	h.mu.Unlock()
	out.Tools = cloneSchemas(listed.Tools)
	return out, nil
}

// WorkingDir returns the effective child working directory.
func (h *ExternalHost) WorkingDir() string { return h.cwd }

// Manifest returns the negotiated manifest.
func (h *ExternalHost) Manifest() plugin.Manifest {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.manifest
}

// ToolSchemas returns the latest negotiated schemas.
func (h *ExternalHost) ToolSchemas() []protocol.ToolSchema {
	h.mu.Lock()
	defer h.mu.Unlock()
	return cloneSchemas(h.tools)
}

// Call invokes a child tool and routes progress notifications to onProgress.
func (h *ExternalHost) Call(ctx context.Context, name, callID string, args json.RawMessage, onProgress func(ProgressNotification)) (toolsCallResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(args) > h.maxInput {
		return toolsCallResult{IsError: true, Content: []protocol.ContentBlock{protocol.NewTextBlock("Error: plugin input exceeded limit")}}, nil
	}
	callCtx := ctx
	cancel := func() {}
	if h.spec.TimeoutMS > 0 {
		callCtx, cancel = context.WithTimeout(ctx, time.Duration(h.spec.TimeoutMS)*time.Millisecond)
	}
	defer cancel()
	select {
	case h.semaphore <- struct{}{}:
		defer func() { <-h.semaphore }()
	case <-callCtx.Done():
		return toolsCallResult{}, callCtx.Err()
	}
	params, err := json.Marshal(map[string]any{
		"name": name, "call_id": callID, "arguments": json.RawMessage(args),
		"timeout_ms":   timeoutMillis(callCtx),
		"cancellation": map[string]any{"supported": true},
	})
	if err != nil {
		return toolsCallResult{}, err
	}
	var out toolsCallResult
	if err := h.call(callCtx, "tools/call", params, callID, onProgress, &out); err != nil {
		return out, err
	}
	if encoded, _ := json.Marshal(out); len(encoded) > h.maxOutput {
		out.Content = []protocol.ContentBlock{protocol.NewTextBlock("Error: plugin output exceeded limit")}
		out.Details = nil
		out.IsError = true
	}
	return out, nil
}

type toolsCallResult struct {
	Content []protocol.ContentBlock `json:"content"`
	Details json.RawMessage         `json:"details,omitempty"`
	IsError bool                    `json:"is_error,omitempty"`
}

func timeoutMillis(ctx context.Context) int64 {
	if deadline, ok := ctx.Deadline(); ok {
		return time.Until(deadline).Milliseconds()
	}
	return 0
}

func (h *ExternalHost) call(ctx context.Context, method string, params []byte, callID string, progress func(ProgressNotification), out any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	id := strconv.FormatUint(atomic.AddUint64(&h.nextID, 1), 10)
	req, err := json.Marshal(rpcRequestV2{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return fmt.Errorf("plugin %s: marshal request: %w", h.spec.ID, err)
	}
	p := &pendingV2{callID: callID, progress: progress, result: make(chan rpcResponseV2, 1)}
	h.mu.Lock()
	if h.failed != nil {
		err := h.failed
		h.mu.Unlock()
		return fmt.Errorf("plugin %s: %w", h.spec.ID, err)
	}
	h.pending[id] = p
	h.mu.Unlock()
	if len(req)+1 > h.maxFrame {
		h.removePending(id)
		return fmt.Errorf("plugin %s: request exceeds frame limit", h.spec.ID)
	}
	if err := h.lockWriter(ctx); err != nil {
		h.removePending(id)
		return err
	}
	writeDone := make(chan error, 1)
	go func() {
		payload := append(req, '\n')
		n, writeErr := h.in.Write(payload)
		if writeErr == nil && n != len(payload) {
			writeErr = io.ErrShortWrite
		}
		h.writeMu.Unlock()
		writeDone <- writeErr
	}()
	select {
	case err := <-writeDone:
		if err != nil {
			h.removePending(id)
			return fmt.Errorf("plugin %s: write: %w", h.spec.ID, err)
		}
	case <-ctx.Done():
		// Pipe writes have no context-aware API. Closing stdin unblocks a
		// blocked writer and isolates the unresponsive child process.
		_ = h.in.Close()
		h.removePending(id)
		return ctx.Err()
	}
	select {
	case resp := <-p.result:
		if resp.Error != nil {
			return fmt.Errorf("plugin %s %s: %s", h.spec.ID, method, resp.Error.Message)
		}
		if out == nil || len(resp.Result) == 0 {
			return nil
		}
		if err := json.Unmarshal(resp.Result, out); err != nil {
			return fmt.Errorf("plugin %s: invalid %s result: %w", h.spec.ID, method, err)
		}
		return nil
	case <-ctx.Done():
		h.removePending(id)
		_ = h.notify("notifications/cancelled", map[string]any{"call_id": callID, "request_id": id, "reason": ctx.Err().Error()})
		return ctx.Err()
	case <-h.done:
		return h.failureError()
	}
}

func (h *ExternalHost) lockWriter(ctx context.Context) error {
	for !h.writeMu.TryLock() {
		timer := time.NewTimer(time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return nil
}

func (h *ExternalHost) removePending(id string) {
	h.mu.Lock()
	delete(h.pending, id)
	h.mu.Unlock()
}

func (h *ExternalHost) readLoop(stdout io.Reader) {
	reader := bufio.NewReader(stdout)
	for {
		line, err := readFrame(reader, h.maxFrame)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				h.fail(err)
			} else {
				h.fail(errors.New("EOF before response (plugin exited)"))
			}
			return
		}
		var msg struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id,omitempty"`
			Method  string          `json:"method,omitempty"`
			Params  json.RawMessage `json:"params,omitempty"`
			Result  json.RawMessage `json:"result,omitempty"`
			Error   *RPCError       `json:"error,omitempty"`
		}
		if err := json.Unmarshal(line, &msg); err != nil {
			h.fail(fmt.Errorf("malformed JSON frame: %w", err))
			return
		}
		if msg.JSONRPC != "2.0" {
			h.fail(errors.New("invalid JSON-RPC version"))
			return
		}
		if msg.Method != "" {
			h.notification(msg.Method, msg.Params)
			continue
		}
		var id string
		if err := json.Unmarshal(msg.ID, &id); err != nil || id == "" {
			h.fail(errors.New("response id must be a string"))
			return
		}
		h.mu.Lock()
		p := h.pending[id]
		if p != nil {
			delete(h.pending, id)
		}
		h.mu.Unlock()
		if p != nil {
			p.result <- rpcResponseV2{JSONRPC: msg.JSONRPC, ID: msg.ID, Result: msg.Result, Error: msg.Error}
		}
	}
}

func readFrame(r *bufio.Reader, max int) ([]byte, error) {
	var out []byte
	for {
		part, err := r.ReadSlice('\n')
		out = append(out, part...)
		if len(out) > max {
			return nil, errors.New("frame exceeds limit")
		}
		if err == nil {
			return []byte(strings.TrimSuffix(string(out), "\n")), nil
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			return nil, err
		}
	}
}

func (h *ExternalHost) notification(method string, params json.RawMessage) {
	switch method {
	case "notifications/progress":
		var n ProgressNotification
		if json.Unmarshal(params, &n) != nil || len(n.Message) > h.maxProgress {
			return
		}
		h.mu.Lock()
		callbacks := make([]func(ProgressNotification), 0)
		for _, p := range h.pending {
			if p.progress != nil && (n.CallID == "" || p.callID == n.CallID) {
				callbacks = append(callbacks, p.progress)
			}
		}
		h.mu.Unlock()
		for _, callback := range callbacks {
			callback(n)
		}
	case "notifications/log":
		var n logNotification
		if json.Unmarshal(params, &n) == nil {
			msg := boundString(n.Message, h.maxOutput)
			_, _ = h.logs.Write([]byte(n.Severity + ": " + msg + "\n"))
		}
	}
}

func (h *ExternalHost) fail(err error) {
	if err == nil {
		err = errors.New("plugin process exited")
	}
	h.failOnce.Do(func() {
		h.mu.Lock()
		h.failed = err
		pending := h.pending
		h.pending = make(map[string]*pendingV2)
		h.mu.Unlock()
		close(h.done)
		for _, p := range pending {
			p.result <- rpcResponseV2{Error: &RPCError{Code: -32000, Message: err.Error()}}
		}
	})
}

func (h *ExternalHost) failureError() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.failed != nil {
		return fmt.Errorf("plugin %s: %w", h.spec.ID, h.failed)
	}
	return fmt.Errorf("plugin %s: closed", h.spec.ID)
}

// NotifyEvent delivers a sanitized host observation as a JSON-RPC
// notification. The bounded queue makes event delivery best effort and keeps
// a slow child from blocking the agent event bus.
func (h *ExternalHost) NotifyEvent(e plugin.Event) error {
	params, err := json.Marshal(e)
	if err != nil {
		return err
	}
	req, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "notifications/event", "params": json.RawMessage(params)})
	if err != nil || len(req)+1 > h.maxFrame {
		return fmt.Errorf("plugin %s: notification exceeds frame limit", h.spec.ID)
	}
	select {
	case h.notifyQueue <- append(req, '\n'):
		return nil
	case <-h.done:
		return h.failureError()
	default:
		return errors.New("plugin event notification queue full")
	}
}

func (h *ExternalHost) writeNotificationLoop() {
	for {
		select {
		case frame := <-h.notifyQueue:
			h.writeMu.Lock()
			n, err := h.in.Write(frame)
			if err == nil && n != len(frame) {
				err = io.ErrShortWrite
			}
			h.writeMu.Unlock()
			if err != nil {
				h.fail(fmt.Errorf("plugin %s: notification write: %w", h.spec.ID, err))
				return
			}
		case <-h.done:
			return
		}
	}
}

func (h *ExternalHost) notify(method string, value any) error {
	params, err := json.Marshal(value)
	if err != nil {
		return err
	}
	req, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": json.RawMessage(params)})
	if err != nil || len(req)+1 > h.maxFrame {
		return fmt.Errorf("plugin %s: notification exceeds frame limit", h.spec.ID)
	}
	select {
	case h.notifyQueue <- append(req, '\n'):
		return nil
	case <-h.done:
		return h.failureError()
	default:
		return errors.New("plugin notification queue full")
	}
}

// Diagnostics returns bounded child stderr and protocol logs with obvious
// credential fields redacted.
func (h *ExternalHost) Diagnostics() string {
	return redactDiagnostics(h.stderr.String() + h.logs.String())
}

// Close requests a graceful shutdown once, then closes/kills the child within
// the caller's context. It is safe to call repeatedly.
func (h *ExternalHost) Close(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	var result error
	h.closeOnce.Do(func() {
		shutdownCtx := ctx
		if _, ok := ctx.Deadline(); !ok {
			var cancel context.CancelFunc
			shutdownCtx, cancel = context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
		}
		var errs []error
		shutdownOK := false
		select {
		case <-h.done:
		default:
			if err := h.call(shutdownCtx, "shutdown", []byte(`{}`), "", nil, nil); err != nil {
				errs = append(errs, err)
			} else {
				shutdownOK = true
			}
		}
		if err := h.in.Close(); err != nil {
			errs = append(errs, err)
		}
		select {
		case <-h.waitDone:
		case <-shutdownCtx.Done():
			if h.cmd.Process != nil {
				if err := h.cmd.Process.Kill(); err != nil {
					errs = append(errs, err)
				}
			}
			<-h.waitDone
		}
		h.mu.Lock()
		if h.waitErr != nil {
			errs = append(errs, h.waitErr)
		}
		if h.failed != nil && !shutdownOK && h.waitErr == nil {
			errs = append(errs, h.failed)
		}
		h.mu.Unlock()
		result = errors.Join(errs...)
		h.closeErr = result
	})
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closeErr != nil {
		return h.closeErr
	}
	return result
}

// boundedBuffer stores only the last bounded amount of diagnostics.
type boundedBuffer struct {
	mu   sync.Mutex
	max  int
	data []byte
}

func newBoundedBuffer(max int) *boundedBuffer { return &boundedBuffer{max: max} }
func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, p...)
	if len(b.data) > b.max {
		b.data = b.data[len(b.data)-b.max:]
	}
	return len(p), nil
}
func (b *boundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(append([]byte(nil), b.data...))
}

func boundString(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

func redactDiagnostics(s string) string {
	for _, key := range []string{"key", "token", "secret", "password", "authorization"} {
		for _, sep := range []string{"=", ":"} {
			// Keep this deliberately conservative: it only redacts one-line
			// key/value diagnostics and never logs the value.
			for i := 0; i < len(s); {
				lower := strings.ToLower(s[i:])
				idx := strings.Index(lower, key+sep)
				if idx < 0 {
					break
				}
				start := i + idx + len(key) + len(sep)
				end := start
				for end < len(s) && s[end] != '\n' && s[end] != ' ' && s[end] != '\t' {
					end++
				}
				s = s[:start] + "[REDACTED]" + s[end:]
				i = start + len("[REDACTED]")
			}
		}
	}
	return s
}

func cloneSchemas(in []protocol.ToolSchema) []protocol.ToolSchema {
	out := make([]protocol.ToolSchema, len(in))
	for i, s := range in {
		out[i] = s
		out[i].Parameters = append(json.RawMessage(nil), s.Parameters...)
		out[i].Discovery = cloneDiscovery(s.Discovery)
	}
	return out
}

func cloneDiscovery(in *protocol.ToolDiscovery) *protocol.ToolDiscovery {
	if in == nil {
		return nil
	}
	out := *in
	out.Keywords = append([]string(nil), in.Keywords...)
	return &out
}

var externalToolNameRE = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,127}$`)

func validateSchemas(schemas []protocol.ToolSchema) error {
	seen := make(map[string]bool, len(schemas))
	for _, s := range schemas {
		if !externalToolNameRE.MatchString(s.Name) {
			return fmt.Errorf("invalid plugin tool name %q", s.Name)
		}
		if seen[s.Name] {
			return fmt.Errorf("duplicate plugin tool %q", s.Name)
		}
		seen[s.Name] = true
		if !validSchema(s.Parameters) {
			return fmt.Errorf("plugin tool %q has invalid parameters schema", s.Name)
		}
	}
	return nil
}
