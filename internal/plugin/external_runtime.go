package plugin

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/elmissouri16/snow-core/internal/procgroup"
	"github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// SpawnExternal starts an explicit argv-based plugin process. No shell is
// involved. Environment is spec.Env when provided and empty otherwise.
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
	cmd.Env = slices.Clone(spec.Env)
	if err := procgroup.Configure(cmd); err != nil {
		return nil, fmt.Errorf("plugin %s: configure process group: %w", spec.ID, err)
	}
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
		writeToken: make(chan struct{}, 1),
	}
	h.writeToken <- struct{}{}
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
		Tools []plugin.ExternalToolDefinition `json:"tools"`
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
	supported := make(map[plugin.EventType]struct{}, len(out.SupportedEvents))
	for _, eventType := range out.SupportedEvents {
		supported[eventType] = struct{}{}
	}
	manifest := cloneManifest(out.Manifest)
	h.mu.Lock()
	h.manifest, h.tools, h.supportedEvents = manifest, cloneExternalSchemas(listed.Tools), supported
	h.mu.Unlock()
	out.Manifest = cloneManifest(manifest)
	out.Tools = cloneExternalSchemas(listed.Tools)
	out.SupportedEvents = slices.Clone(out.SupportedEvents)
	return out, nil
}

// WorkingDir returns the effective child working directory.
func (h *ExternalHost) WorkingDir() string { return h.cwd }

// Manifest returns the negotiated manifest.
func (h *ExternalHost) Manifest() plugin.Manifest {
	h.mu.Lock()
	defer h.mu.Unlock()
	return cloneManifest(h.manifest)
}

// SupportsEvent reports whether the runtime subscribed to an event during
// initialization. An omitted or empty supported_events list subscribes to no
// events, preserving the agent loop from unnecessary external fanout.
func (h *ExternalHost) SupportsEvent(eventType plugin.EventType) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.supportedEvents[eventType]
	return ok
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
	id := strconv.FormatUint(h.nextID.Add(1), 10)
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
	if err := h.acquireWriter(ctx); err != nil {
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
		h.releaseWriter()
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
	resp, ok := receivePluginResponse(ctx, p.result, h.done)
	if !ok {
		if err := ctx.Err(); err != nil {
			h.removePending(id)
			_ = h.notify("notifications/cancelled", map[string]any{"call_id": callID, "request_id": id, "reason": err.Error()})
			return err
		}
		return h.failureError()
	}
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
}

func receivePluginResponse(ctx context.Context, result <-chan rpcResponseV2, done <-chan struct{}) (rpcResponseV2, bool) {
	select {
	case resp := <-result:
		return resp, true
	case <-ctx.Done():
		return rpcResponseV2{}, false
	case <-done:
		// A plugin may exit immediately after writing a valid shutdown response.
		// Prefer that already-buffered response over the subsequent EOF signal.
		select {
		case resp := <-result:
			return resp, true
		default:
			return rpcResponseV2{}, false
		}
	}
}

func (h *ExternalHost) acquireWriter(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-h.done:
		return h.failureError()
	case <-h.writeToken:
	}
	select {
	case <-ctx.Done():
		h.releaseWriter()
		return ctx.Err()
	case <-h.done:
		h.releaseWriter()
		return h.failureError()
	default:
		return nil
	}
}

func (h *ExternalHost) releaseWriter() {
	select {
	case h.writeToken <- struct{}{}:
	default:
		panic("plugin: writer token released twice")
	}
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
			return out[:len(out)-1], nil
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
		if json.Unmarshal(params, &n) != nil || n.CallID == "" || len(n.Message) > h.maxProgress {
			return
		}
		h.mu.Lock()
		callbacks := make([]func(ProgressNotification), 0)
		for _, p := range h.pending {
			if p.progress != nil && p.callID == n.CallID {
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
			if err := h.acquireWriter(context.Background()); err != nil {
				return
			}
			n, err := h.in.Write(frame)
			if err == nil && n != len(frame) {
				err = io.ErrShortWrite
			}
			h.releaseWriter()
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
		if err := h.in.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			errs = append(errs, err)
		}
		leaderDone := false
		select {
		case <-h.waitDone:
			leaderDone = true
		case <-shutdownCtx.Done():
		}
		const cleanupGrace = 2 * time.Second
		if err := procgroup.Shutdown(h.cmd.Process, cleanupGrace); err != nil {
			errs = append(errs, err)
		}
		if !leaderDone {
			select {
			case <-h.waitDone:
			case <-time.After(cleanupGrace):
				errs = append(errs, errors.New("plugin process leader did not exit after group kill"))
			}
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
	return string(slices.Clone(b.data))
}

func boundString(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

func redactDiagnostics(s string) string {
	// Diagnostics are still untrusted and must never intentionally contain
	// credentials. Redact the complete line when it contains a common credential
	// assignment/header. Whole-line replacement avoids leaking escaped quoted
	// values or auth schemes that a value-oriented regexp could parse partially.
	lines := strings.SplitAfter(s, "\n")
	for i, line := range lines {
		ending := ""
		content := line
		if before, ok := strings.CutSuffix(content, "\n"); ok {
			content = before
			ending = "\n"
		}
		if diagnosticCredentialRE.MatchString(content) {
			lines[i] = "[REDACTED]" + ending
		}
	}
	return strings.Join(lines, "")
}

func cloneManifest(in plugin.Manifest) plugin.Manifest {
	out := in
	out.Capabilities = slices.Clone(in.Capabilities)
	return out
}

func cloneExternalSchemas(in []plugin.ExternalToolDefinition) []plugin.ExternalToolDefinition {
	out := make([]plugin.ExternalToolDefinition, len(in))
	for i, schema := range in {
		out[i] = schema
		out[i].Parameters = append(json.RawMessage(nil), schema.Parameters...)
		out[i].Discovery = cloneDiscovery(schema.Discovery)
		out[i].Capabilities = slices.Clone(schema.Capabilities)
	}
	return out
}

func cloneDiscovery(in *protocol.ToolDiscovery) *protocol.ToolDiscovery {
	if in == nil {
		return nil
	}
	out := *in
	out.Keywords = slices.Clone(in.Keywords)
	return &out
}

func validateSchemas(schemas []plugin.ExternalToolDefinition) error {
	seen := make(map[string]bool, len(schemas))
	for _, schema := range schemas {
		if !externalToolNameRE.MatchString(schema.Name) {
			return fmt.Errorf("invalid plugin tool name %q", schema.Name)
		}
		if seen[schema.Name] {
			return fmt.Errorf("duplicate plugin tool %q", schema.Name)
		}
		seen[schema.Name] = true
		if !validSchema(schema.Parameters) {
			return fmt.Errorf("plugin tool %q has invalid parameters schema", schema.Name)
		}
		if _, err := declaredRisk(schema.Risk); err != nil {
			return fmt.Errorf("plugin tool %q: %w", schema.Name, err)
		}
	}
	return nil
}
