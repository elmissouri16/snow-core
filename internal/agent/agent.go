// Package agent implements the streaming turn loop: prompt → provider →
// permission gate → tools → loop until the model stops.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/snow-core/snow/internal/auth"
	"github.com/snow-core/snow/internal/permission"
	"github.com/snow-core/snow/internal/provider"
	"github.com/snow-core/snow/internal/session"
	"github.com/snow-core/snow/internal/tools"
	"github.com/snow-core/snow/pkg/protocol"
)

// MaxToolRetries bounds retries for malformed tool arguments.
const MaxToolRetries = 1

// Options configures an Agent.
type Options struct {
	Provider      provider.Provider
	Registry      tools.Registry
	Session       session.Store
	Permission    permission.Service
	ToolHost      tools.ToolHost
	SystemPrompt  string
	Model         protocol.Model
	MaxTurns      int // 0 = unlimited
	CallLimit     int // max tool calls per turn (0 = unlimited)
	MaxToolOutput int
	// Auth resolves credentials (auth.json). Optional: env fallback is
	// implemented by providers for known env vars.
	Auth auth.Store
	// APIKey is an explicit credential override (CLI --api-key / SDK option).
	APIKey string
}

// Agent drives turns against a provider and tool registry.
type Agent struct {
	mu      sync.RWMutex
	opts    Options
	model   protocol.Model
	bus     *eventBus
	running bool
	// tool results retained between the tool_use assistant message and the
	// continuation provider call
	pending      map[string]protocol.ContentBlock
	pendingOrder []string
}

// New creates an agent.
func New(opts Options) (*Agent, error) {
	if opts.Provider == nil {
		return nil, errors.New("agent: provider required")
	}
	if opts.Registry == nil {
		return nil, errors.New("agent: tool registry required")
	}
	if opts.Session == nil {
		return nil, errors.New("agent: session required")
	}
	if opts.Permission == nil {
		opts.Permission = permission.NewService(permission.ModeDeny, nil)
	}
	if opts.Model.Provider == "" && opts.Provider != nil {
		opts.Model.Provider = opts.Provider.ID()
	}
	a := &Agent{opts: opts, model: opts.Model, bus: newEventBus()}
	a.pending = make(map[string]protocol.ContentBlock)
	return a, nil
}

// Model returns the current model.
func (a *Agent) Model() protocol.Model {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.model
}

// SetModel updates the active model.
func (a *Agent) SetModel(m protocol.Model) error {
	if m.Provider == "" {
		return errors.New("agent: model has no provider")
	}
	a.mu.Lock()
	a.model = m
	a.mu.Unlock()
	a.bus.Publish(protocol.AgentEvent{Type: protocol.EvModelChanged, Model: &m})
	return nil
}

// IsRunning reports whether a turn is in flight.
func (a *Agent) IsRunning() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.running
}

// Subscribe registers an event listener; returns an unsubscribe func.
func (a *Agent) Subscribe(fn func(protocol.AgentEvent)) func() {
	return a.bus.Subscribe(fn)
}

// Messages returns the linearized session messages.
func (a *Agent) Messages() ([]protocol.Message, error) {
	return a.opts.Session.Messages()
}

// Prompt runs a full user turn to completion.
func (a *Agent) Prompt(ctx context.Context, text string) error {
	if strings_trim(text) == "" {
		return errors.New("agent: empty prompt")
	}

	// Claim the running flag BEFORE appending so a concurrent second Prompt
	// cannot persist a ghost user message that never gets processed.
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return errors.New("agent: already running")
	}
	a.running = true
	a.pending = make(map[string]protocol.ContentBlock)
	a.pendingOrder = a.pendingOrder[:0]
	a.mu.Unlock()

	// Ensure we stop running on any exit.
	defer func() {
		a.mu.Lock()
		a.running = false
		a.mu.Unlock()
	}()

	userMsg := protocol.NewUserMessage(newID(), "", text)
	if err := a.opts.Session.Append(session.Entry{
		Type:     session.EntryMessage,
		ID:       userMsg.ID,
		ParentID: "",
		Message:  &userMsg,
	}); err != nil {
		return fmt.Errorf("agent: append user message: %w", err)
	}
	a.bus.Publish(protocol.AgentEvent{Type: protocol.EvSessionUpdated})

	return a.run(ctx)
}

func (a *Agent) run(ctx context.Context) error {
	turn := 0
	for {
		if a.opts.MaxTurns > 0 && turn >= a.opts.MaxTurns {
			a.bus.Publish(protocol.AgentEvent{Type: protocol.EvError, Message: "max turns reached"})
			a.bus.Publish(protocol.AgentEvent{Type: protocol.EvTurnDone})
			return errors.New("agent: max turns reached")
		}
		turn++

		msgs, err := a.opts.Session.Messages()
		if err != nil {
			a.bus.Publish(protocol.AgentEvent{Type: protocol.EvTurnDone})
			return fmt.Errorf("agent: load messages: %w", err)
		}

		req := protocol.ChatRequest{
			Model:    a.Model(),
			Messages: msgs,
			Tools:    a.opts.Registry.Schemas(),
			System:   a.opts.SystemPrompt,
		}

		// Call the provider (optionally with a merged retry on malformed args).
		stop, err := a.streamTurn(ctx, req)
		if err != nil {
			a.bus.Publish(protocol.AgentEvent{Type: protocol.EvTurnDone})
			return err
		}
		switch stop {
		case protocol.StopToolUse:
			if err := a.executeToolCalls(ctx); err != nil {
				a.bus.Publish(protocol.AgentEvent{Type: protocol.EvTurnDone})
				return err
			}
			continue
		case protocol.StopStop, protocol.StopLength, protocol.StopAborted, protocol.StopError:
			a.bus.Publish(protocol.AgentEvent{Type: protocol.EvTurnDone})
			return nil
		}
		a.bus.Publish(protocol.AgentEvent{Type: protocol.EvTurnDone})
		return nil
	}
}

// streamTurn calls the provider and persists the assistant message; returns stop reason.
func (a *Agent) streamTurn(ctx context.Context, req protocol.ChatRequest) (protocol.StopReason, error) {
	creds := a.resolveCreds(ctx)
	if err := a.opts.Provider.Resolve(ctx, creds); err != nil {
		a.bus.Publish(protocol.AgentEvent{Type: protocol.EvError, Message: err.Error()})
		return protocol.StopError, fmt.Errorf("agent: provider resolve: %w", err)
	}

	stream, err := a.opts.Provider.Chat(ctx, creds, req)
	if err != nil {
		a.bus.Publish(protocol.AgentEvent{Type: protocol.EvError, Message: err.Error()})
		return protocol.StopError, fmt.Errorf("agent: provider chat: %w", err)
	}
	defer stream.Close()

	asstID := newID()
	parent := a.opts.Session.BranchTip()
	var content []protocol.ContentBlock
	var usage *protocol.Usage
	var stop protocol.StopReason = protocol.StopPending
	textBuf := ""
	thinkingBuf := ""
	toolCalls := map[string]protocol.ContentBlock{} // id -> block

	for {
		ev, err := stream.Next(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				stop = protocol.StopAborted
				content = append(content, textBlock(textBuf))
				if perr := a.persistAssistant(asstID, parent, content, stop, usage, ""); perr != nil {
					return protocol.StopAborted, perr
				}
				a.bus.Publish(protocol.AgentEvent{Type: protocol.EvAborted})
				return protocol.StopAborted, nil
			}
			// Normal end of stream: io.EOF per the EventStream contract.
			if errors.Is(err, io.EOF) || errors.Is(err, ErrStreamEOF) {
				break
			}
			// Stream error event
			stop = protocol.StopError
			content = append(content, textBlock(textBuf))
			if perr := a.persistAssistant(asstID, parent, content, stop, usage, err.Error()); perr != nil {
				return protocol.StopError, perr
			}
			a.bus.Publish(protocol.AgentEvent{Type: protocol.EvError, Message: err.Error()})
			return protocol.StopError, err
		}

		switch ev.Type {
		case protocol.EvStreamTextDelta:
			textBuf += ev.Text
			a.bus.Publish(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: ev.Text})
		case protocol.EvStreamThinkingDelta:
			thinkingBuf += ev.Text
			a.bus.Publish(protocol.AgentEvent{Type: protocol.EvThinkingDelta, Text: ev.Text})
		case protocol.EvStreamToolCallDelta:
			cb, ok := toolCalls[ev.ToolCallID]
			if !ok {
				cb = protocol.ContentBlock{
					Type:       protocol.BlockToolCall,
					ToolCallID: ev.ToolCallID,
					Name:       ev.ToolName,
				}
			}
			if ev.Arguments != nil {
				cb.Arguments = append(cb.Arguments, ev.Arguments...)
			}
			toolCalls[ev.ToolCallID] = cb
		case protocol.EvStreamToolCallDone:
			cb, ok := toolCalls[ev.ToolCallID]
			if !ok {
				cb = protocol.ContentBlock{
					Type:       protocol.BlockToolCall,
					ToolCallID: ev.ToolCallID,
					Name:       ev.ToolName,
				}
			}
			if ev.Arguments != nil {
				cb.Arguments = ev.Arguments
			}
			if cb.Name == "" {
				cb.Name = ev.ToolName
			}
			toolCalls[ev.ToolCallID] = cb
		case protocol.EvStreamUsage:
			usage = ev.Usage
			a.bus.Publish(protocol.AgentEvent{Type: protocol.EvUsage, Usage: ev.Usage})
		case protocol.EvStreamDone:
			stop = ev.StopReason
			if stop == "" {
				stop = protocol.StopStop
			}
		case protocol.EvStreamError:
			stop = protocol.StopError
			errMsg := "provider error"
			if ev.Err != nil {
				errMsg = ev.Err.Error()
			}
			content = append(content, textBlock(textBuf))
			if perr := a.persistAssistant(asstID, parent, content, stop, usage, errMsg); perr != nil {
				return protocol.StopError, perr
			}
			a.bus.Publish(protocol.AgentEvent{Type: protocol.EvError, Message: errMsg})
			return protocol.StopError, fmt.Errorf("agent: %s", errMsg)
		}
	}

	// Assemble final content: thinking first, then text, then tool calls.
	if thinkingBuf != "" {
		content = append(content, protocol.ContentBlock{Type: protocol.BlockThinking, Text: thinkingBuf})
	}
	if textBuf != "" {
		content = append(content, textBlock(textBuf))
	}
	for _, cb := range toolCalls {
		content = append(content, cb)
	}
	if stop == protocol.StopPending {
		stop = protocol.StopStop
	}

	if err := a.persistAssistant(asstID, parent, content, stop, usage, ""); err != nil {
		return stop, err
	}

	// Stash tool calls for execution (ordered).
	if stop == protocol.StopToolUse {
		a.mu.Lock()
		a.pending = make(map[string]protocol.ContentBlock)
		a.pendingOrder = a.pendingOrder[:0]
		for _, cb := range toolCalls {
			if cb.Type == protocol.BlockToolCall {
				a.pending[cb.ToolCallID] = cb
				a.pendingOrder = append(a.pendingOrder, cb.ToolCallID)
			}
		}
		a.mu.Unlock()
	}

	return stop, nil
}

func (a *Agent) persistAssistant(id, parent string, content []protocol.ContentBlock, stop protocol.StopReason, usage *protocol.Usage, errMsg string) error {
	msg := protocol.NewAssistantMessage(id, parent, a.Model().Provider, a.Model().ID, content, stop, usage)
	if errMsg != "" {
		msg.Error = errMsg
	}
	if err := a.opts.Session.Append(session.Entry{
		Type:     session.EntryMessage,
		ID:       id,
		ParentID: parent,
		Message:  &msg,
	}); err != nil {
		return fmt.Errorf("agent: persist assistant: %w", err)
	}
	a.bus.Publish(protocol.AgentEvent{Type: protocol.EvSessionUpdated})
	return nil
}

// executeToolCalls runs the pending tool calls serially (in stream order)
// and persists results. Aborts early when ctx is cancelled.
func (a *Agent) executeToolCalls(ctx context.Context) error {
	a.mu.Lock()
	pending := a.pending
	order := append([]string(nil), a.pendingOrder...)
	a.pending = make(map[string]protocol.ContentBlock)
	a.pendingOrder = a.pendingOrder[:0]
	a.mu.Unlock()

	parent := a.opts.Session.BranchTip()
	callCount := 0

	for _, id := range order {
		cb, ok := pending[id]
		if !ok {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if a.opts.CallLimit > 0 && callCount >= a.opts.CallLimit {
			// Emit an error result for skipped calls so the provider never
			// sees tool_calls without results.
			msg := protocol.NewToolResultMessage(newID(), parent, cb.ToolCallID, cb.Name,
				[]protocol.ContentBlock{protocol.NewTextBlock(
					fmt.Sprintf("Error: tool call skipped (call limit %d reached)", a.opts.CallLimit))}, true)
			if err := a.appendToolResult(parent, msg); err != nil {
				return err
			}
			continue
		}
		callCount++
		if _, err := a.executeOne(ctx, cb, parent); err != nil {
			return err
		}
	}
	return nil
}

// appendToolResult persists a tool_result message and emits its events.
func (a *Agent) appendToolResult(parent string, msg protocol.Message) error {
	if err := a.opts.Session.Append(session.Entry{
		Type:     session.EntryMessage,
		ID:       msg.ID,
		ParentID: parent,
		Message:  &msg,
	}); err != nil {
		return fmt.Errorf("agent: append tool result: %w", err)
	}
	a.bus.Publish(protocol.AgentEvent{
		Type:       protocol.EvToolEnd,
		ToolCallID: msg.ToolCallID,
		ToolName:   msg.ToolName,
		IsError:    msg.IsError,
	})
	return nil
}

func (a *Agent) executeOne(ctx context.Context, cb protocol.ContentBlock, parent string) (protocol.Message, error) {
	// Validate args JSON.
	var args map[string]any
	rawArgs := cb.Arguments
	if len(rawArgs) == 0 || string(rawArgs) == "" {
		rawArgs = json.RawMessage("{}")
	}
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		// Malformed arguments: inject a synthetic tool result telling the model.
		msg := protocol.NewToolResultMessage(newID(), parent, cb.ToolCallID, cb.Name,
			[]protocol.ContentBlock{protocol.NewTextBlock(fmt.Sprintf(
				"Error: tool arguments are not valid JSON: %v. Raw: %s", err, string(rawArgs)))}, true)
		if err := a.appendToolResult(parent, msg); err != nil {
			return msg, err
		}
		return msg, nil
	}

	tool, ok := a.opts.Registry.Get(cb.Name)
	if !ok {
		msg := protocol.NewToolResultMessage(newID(), parent, cb.ToolCallID, cb.Name,
			[]protocol.ContentBlock{protocol.NewTextBlock(fmt.Sprintf("Error: unknown tool %q", cb.Name))}, true)
		if err := a.appendToolResult(parent, msg); err != nil {
			return msg, err
		}
		return msg, nil
	}

	// Permission gate.
	permReq := permission.Request{
		Tool:  cb.Name,
		Args:  rawArgs,
		Paths: extractPaths(args),
		Risk:  riskFor(cb.Name),
	}
	decision, err := a.opts.Permission.Authorize(ctx, permReq)
	if err != nil || decision == permission.DecisionDeny {
		reason := "denied by permission policy"
		if err != nil {
			reason = err.Error()
		}
		msg := protocol.NewToolResultMessage(newID(), parent, cb.ToolCallID, cb.Name,
			[]protocol.ContentBlock{protocol.NewTextBlock("Permission denied: " + reason)}, true)
		if err := a.appendToolResult(parent, msg); err != nil {
			return msg, err
		}
		return msg, nil
	}

	a.bus.Publish(protocol.AgentEvent{
		Type:       protocol.EvToolStart,
		ToolCallID: cb.ToolCallID,
		ToolName:   cb.Name,
	})

	// Run the tool with panic recovery.
	tr := a.runTool(ctx, tool, rawArgs)

	var out []protocol.ContentBlock
	if len(tr.Content) == 0 {
		out = []protocol.ContentBlock{protocol.NewTextBlock("(no output)")}
	} else {
		out = tr.Content
	}
	msg := protocol.NewToolResultMessage(newID(), parent, cb.ToolCallID, cb.Name, out, tr.IsError)
	if err := a.appendToolResult(parent, msg); err != nil {
		return msg, err
	}
	return msg, nil
}

func (a *Agent) runTool(ctx context.Context, tool tools.Tool, rawArgs json.RawMessage) (tr tools.ToolResult) {
	defer func() {
		if r := recover(); r != nil {
			tr = tools.ErrorResult(fmt.Errorf("tool %s panicked: %v", tool.Schema().Name, r))
		}
	}()
	res, err := tool.Run(ctx, rawArgs, a.opts.ToolHost)
	if err != nil {
		return tools.ErrorResult(err)
	}
	return res
}

// resolveCreds resolves provider credentials: explicit API key → auth.json → env.
// An empty credential is passed through and the provider's Resolve is the
// authority on whether that is acceptable (fake/test providers accept empty).
func (a *Agent) resolveCreds(ctx context.Context) auth.Credential {
	id := a.Model().Provider
	if a.opts.APIKey != "" {
		return auth.Credential{Type: auth.CredentialAPIKey, Key: a.opts.APIKey}
	}
	if a.opts.Auth != nil {
		if cred, ok := a.opts.Auth.Get(id); ok && cred.Valid() {
			return cred
		}
	}
	// Env fallback for known API-key providers.
	if id == "opencode-go" {
		if k := os.Getenv("OPENCODE_API_KEY"); k != "" {
			return auth.Credential{Type: auth.CredentialAPIKey, Key: k}
		}
	}
	return auth.Credential{}
}

// riskFor maps tool names to permission risk classes.
func riskFor(name string) permission.Risk {
	switch name {
	case "read":
		return permission.RiskRead
	case "write", "edit":
		return permission.RiskWrite
	case "bash":
		return permission.RiskExec
	default:
		return permission.RiskExec
	}
}

// extractPaths pulls likely path fields from tool args.
func extractPaths(args map[string]any) []string {
	var paths []string
	for _, k := range []string{"path", "file", "dir", "paths"} {
		switch v := args[k].(type) {
		case string:
			paths = append(paths, v)
		case []any:
			for _, item := range v {
				if s, ok := item.(string); ok {
					paths = append(paths, s)
				}
			}
		}
	}
	return paths
}

func textBlock(s string) protocol.ContentBlock {
	return protocol.NewTextBlock(s)
}

func strings_trim(s string) string { return strings.TrimSpace(s) }

// ErrStreamEOF signals a normal end of stream.
var ErrStreamEOF = errors.New("agent: stream EOF")

// ---------------------------------------------------------------------------
// Event bus
// ---------------------------------------------------------------------------

type eventBus struct {
	mu   sync.Mutex
	subs map[int]func(protocol.AgentEvent)
	next int
}

func newEventBus() *eventBus {
	return &eventBus{subs: make(map[int]func(protocol.AgentEvent))}
}

func (b *eventBus) Subscribe(fn func(protocol.AgentEvent)) func() {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := b.next
	b.next++
	b.subs[id] = fn
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		delete(b.subs, id)
	}
}

func (b *eventBus) Publish(ev protocol.AgentEvent) {
	b.mu.Lock()
	fns := make([]func(protocol.AgentEvent), 0, len(b.subs))
	for _, fn := range b.subs {
		fns = append(fns, fn)
	}
	b.mu.Unlock()
	for _, fn := range fns {
		fn(ev)
	}
}

// ---------------------------------------------------------------------------
// IDs
// ---------------------------------------------------------------------------

func newID() string {
	return fmt.Sprintf("%d-%x", time.Now().UnixNano(), time.Now().UnixNano()%0xffff)
}
