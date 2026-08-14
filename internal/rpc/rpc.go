// Package rpc implements the JSONL-over-stdio control plane for foreign
// hosts (pi-inspired). Commands are read from stdin, events stream to stdout.
//
// Framing: strictly newline-delimited JSON (LF). Clients must split on '\n'
// only — never on Unicode separators.
package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/snow-core/snow/internal/app"
	"github.com/snow-core/snow/internal/subagent"
	"github.com/snow-core/snow/pkg/protocol"
)

// Request is the public JSONL command envelope.
type Request = protocol.RPCRequest

// Response is the public JSONL response envelope.
type Response = protocol.RPCResponse

// Server serves RPC on stdin/stdout.
type Server struct {
	in            io.Reader
	out           io.Writer
	app           *app.App
	mu            sync.Mutex
	writeErr      error
	writeFailed   chan struct{}
	writeFailOnce sync.Once
	readyOnce     sync.Once
	snowVersion   string
	// cancel aborts the in-flight prompt.
	cancel     context.CancelFunc
	promptDone chan struct{}
	promptWG   sync.WaitGroup
}

// ServerOptions configures RPC transport metadata.
type ServerOptions struct {
	SnowVersion string
}

// New creates an RPC server with development-version metadata.
func New(ctx context.Context, a *app.App, in io.Reader, out io.Writer) *Server {
	return NewWithOptions(ctx, a, in, out, ServerOptions{SnowVersion: "dev"})
}

// NewWithOptions creates an RPC server.
func NewWithOptions(ctx context.Context, a *app.App, in io.Reader, out io.Writer, opts ServerOptions) *Server {
	if ctx == nil {
		ctx = context.Background()
	}
	a.EnableUserInputReplies()
	return &Server{in: in, out: out, app: a, writeFailed: make(chan struct{}), snowVersion: opts.SnowVersion}
}

// Serve reads commands until EOF.
func (s *Server) Serve(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := s.announceReady(); err != nil {
		return err
	}
	serveCtx, cancelServe := context.WithCancel(ctx)
	defer cancelServe()
	type scanResult struct {
		line string
		err  error
		done bool
	}
	scans := make(chan scanResult, 1)
	scanStop := make(chan struct{})
	defer close(scanStop)
	go func() {
		scanner := bufio.NewScanner(s.in)
		scanner.Buffer(make([]byte, 0, 64*1024), protocol.RPCMaxInputBytes)
		for scanner.Scan() {
			select {
			case scans <- scanResult{line: scanner.Text()}:
			case <-scanStop:
				return
			}
		}
		select {
		case scans <- scanResult{err: scanner.Err(), done: true}:
		case <-scanStop:
		}
	}()
	var terminalErr error
	for {
		var line string
		select {
		case <-s.writeFailed:
			goto finish
		case <-ctx.Done():
			if closer, ok := s.in.(io.Closer); ok {
				_ = closer.Close()
			}
			cancelServe()
			goto finish
		case result := <-scans:
			if result.done {
				if result.err != nil && serveCtx.Err() == nil && ctx.Err() == nil {
					terminalErr = result.err
				}
				goto finish
			}
			if result.err != nil {
				if serveCtx.Err() == nil && ctx.Err() == nil {
					terminalErr = result.err
				}
				goto finish
			}
			line = result.line
		}
		if len(line) == 0 {
			continue
		}
		var req Request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			s.write(Response{ID: req.ID, Type: "response", Command: "invalid", Success: false, Error: "invalid JSON: " + err.Error()})
			continue
		}
		if err := s.handle(serveCtx, req); err != nil {
			_ = s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: false, Error: err.Error()})
		}
	}
finish:
	// No more replies can arrive after EOF or cancellation. Release pending/future interaction
	// and wait commands. Ordinary prompts retain their own documented join path.
	cancelServe()
	s.app.CloseUserInput()
	s.mu.Lock()
	done := s.promptDone
	s.mu.Unlock()
	if done != nil {
		<-done
	}
	s.promptWG.Wait()
	s.mu.Lock()
	writeErr := s.writeErr
	s.mu.Unlock()
	if terminalErr == nil {
		return writeErr
	}
	if writeErr == nil {
		return terminalErr
	}
	return errors.Join(terminalErr, writeErr)
}

func (s *Server) handle(ctx context.Context, req Request) error {
	if ctx == nil {
		ctx = context.Background()
	}
	switch req.Type {
	case "prompt":
		return s.handlePrompt(ctx, req)
	case "abort":
		s.app.Agent.Abort()
		s.mu.Lock()
		if s.cancel != nil {
			s.cancel()
		}
		s.mu.Unlock()
		s.write(Response{ID: req.ID, Type: "response", Command: "abort", Success: true})
		return nil
	case "steer", "follow_up":
		if req.Message == "" {
			return fmt.Errorf("%s requires message", req.Type)
		}
		var err error
		if req.Type == "steer" {
			err = s.app.Agent.Steer(req.Message)
		} else {
			err = s.app.Agent.FollowUp(req.Message)
		}
		if err != nil {
			return err
		}
		s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true})
		return nil
	case "user_input_reply":
		var response protocol.UserInputResponse
		if len(req.Params) == 0 {
			return errors.New("user_input_reply requires params")
		}
		if err := json.Unmarshal(req.Params, &response); err != nil {
			return fmt.Errorf("user_input_reply params: %w", err)
		}
		if err := s.app.ReplyUserInput(response); err != nil {
			return err
		}
		s.write(Response{ID: req.ID, Type: "response", Command: "user_input_reply", Success: true})
		return nil
	case "user_input_reject":
		var payload struct {
			RequestID string `json:"request_id"`
		}
		if len(req.Params) == 0 {
			return errors.New("user_input_reject requires params")
		}
		if err := json.Unmarshal(req.Params, &payload); err != nil {
			return fmt.Errorf("user_input_reject params: %w", err)
		}
		if payload.RequestID == "" {
			return errors.New("user_input_reject requires request_id")
		}
		if err := s.app.RejectUserInput(payload.RequestID); err != nil {
			return err
		}
		s.write(Response{ID: req.ID, Type: "response", Command: "user_input_reject", Success: true})
		return nil
	case "subagent_spawn":
		var p protocol.SpawnSubagentRequest
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return err
		}
		state, err := s.app.SpawnSubagent(ctx, p)
		if err != nil {
			return err
		}
		s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: state})
		return nil
	case "subagent_send_message", "subagent_followup":
		var p struct {
			Target  string `json:"target"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return err
		}
		var err error
		if req.Type == "subagent_followup" {
			err = s.app.FollowupSubagent(ctx, p.Target, p.Message)
		} else {
			err = s.app.SendSubagentMessage(ctx, p.Target, p.Message)
		}
		if err != nil {
			return err
		}
		s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true})
		return nil
	case "subagent_wait":
		var p struct {
			TimeoutMS int    `json:"timeout_ms"`
			Until     string `json:"until,omitempty"`
		}
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return err
			}
		}
		timeout, err := subagent.ParseWaitTimeoutMS(p.TimeoutMS)
		if err != nil {
			return err
		}
		s.promptWG.Add(1)
		go func() {
			defer s.promptWG.Done()
			var (
				res protocol.WaitSubagentsResult
				err error
			)
			switch p.Until {
			case "", "activity":
				res, err = s.app.WaitSubagents(ctx, timeout)
			case "all":
				res, err = s.app.WaitSubagentsUntilAll(ctx, timeout)
			default:
				err = fmt.Errorf("rpc: invalid subagent_wait mode %q (use activity or all)", p.Until)
			}
			if err != nil {
				s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: false, Error: err.Error()})
				return
			}
			s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: res})
		}()
		return nil
	case "subagent_interrupt":
		var p struct {
			Target string `json:"target"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return err
		}
		previous, err := s.app.InterruptSubagent(ctx, p.Target)
		if err != nil {
			return err
		}
		s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: map[string]any{"previous_status": previous}})
		return nil
	case "subagent_list":
		var p struct {
			PathPrefix string `json:"path_prefix"`
		}
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return err
			}
		}
		list, err := s.app.ListSubagents(ctx, p.PathPrefix)
		if err != nil {
			return err
		}
		s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: list})
		return nil
	case "subagent_get":
		var p struct {
			Target string `json:"target"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return err
		}
		state, err := s.app.Subagent(ctx, p.Target)
		if err != nil {
			return err
		}
		s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: state})
		return nil
	case "subagent_ready":
		if err := s.app.ReadySubagents(); err != nil {
			return err
		}
		s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true})
		return nil
	case "goal_get":
		g, err := s.app.GoalState()
		if err != nil {
			return err
		}
		s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: g})
		return nil
	case "goal_set", "goal_create":
		var p struct {
			Objective   string `json:"objective"`
			TokenBudget *int64 `json:"token_budget"`
			Replace     bool   `json:"replace"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return err
		}
		g, err := s.app.CreateGoal(p.Objective, p.TokenBudget, p.Replace)
		if err != nil {
			return err
		}
		s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: g})
		return nil
	case "goal_edit":
		var p struct {
			Objective string `json:"objective"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return err
		}
		g, err := s.app.EditGoal(p.Objective)
		if err != nil {
			return err
		}
		s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: g})
		return nil
	case "goal_pause", "goal_resume":
		var g *protocol.ThreadGoal
		var err error
		if req.Type == "goal_pause" {
			g, err = s.app.PauseGoal()
		} else {
			g, err = s.app.ResumeGoal()
		}
		if err != nil {
			return err
		}
		s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: g})
		return nil
	case "goal_clear":
		before, err := s.app.GoalState()
		if err != nil {
			return err
		}
		if err := s.app.ClearGoal(); err != nil {
			return err
		}
		s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: map[string]bool{"cleared": before != nil}})
		return nil
	case "goal_continue":
		if err := s.app.ContinueGoal(); err != nil {
			return err
		}
		g, err := s.app.GoalState()
		if err != nil {
			return err
		}
		s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: g})
		return nil
	case "models_list":
		providerID, model, models := s.app.ActiveModelsSnapshot()
		s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: protocol.RPCModelList{Provider: providerID, Current: model.ID, Models: models}})
		return nil
	case "subagent_models":
		models := cloneModels(s.app.SubagentModels())
		enabled := s.app.Subagents != nil
		s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: protocol.RPCModelList{Enabled: &enabled, Models: models}})
		return nil
	case "set_model":
		if req.Model == "" {
			return errors.New("set_model requires model")
		}
		providerID, _, catalog := s.app.ActiveModelsSnapshot()
		m := protocol.Model{Provider: providerID, ID: req.Model, SupportsTools: true}
		for _, cached := range catalog {
			if cached.ID == req.Model {
				m = cached
				break
			}
		}
		if err := s.app.SetModel(m); err != nil {
			return err
		}
		if err := s.write(Response{ID: req.ID, Type: "response", Command: "set_model", Success: true}); err != nil {
			return err
		}
		return nil
	case "set_mode":
		mode, err := protocol.ParseCollaborationMode(req.Mode)
		if err != nil {
			return err
		}
		if err := s.app.Agent.SetMode(mode); err != nil {
			return err
		}
		s.write(Response{ID: req.ID, Type: "response", Command: "set_mode", Success: true})
		return nil
	case "set_thinking":
		if req.Thinking == "" {
			return errors.New("set_thinking requires thinking")
		}
		level, err := protocol.ParseThinkingLevel(req.Thinking)
		if err != nil {
			return err
		}
		if err := s.app.Agent.SetThinking(level); err != nil {
			return err
		}
		s.write(Response{ID: req.ID, Type: "response", Command: "set_thinking", Success: true})
		return nil
	case "session_rename":
		var p struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return err
		}
		if err := s.app.RenameSession(p.Name); err != nil {
			return err
		}
		title, err := s.app.Agent.SessionTitle()
		if err != nil {
			return err
		}
		sessionID, _, err := s.app.Agent.SessionIdentity()
		if err != nil {
			return err
		}
		s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: map[string]any{"session_id": sessionID, "name": title}})
		return nil
	case "session_info":
		model := s.app.Agent.Model()
		sessionID, sessionPath, err := s.app.Agent.SessionIdentity()
		if err != nil {
			return err
		}
		title, err := s.app.Agent.SessionTitle()
		if err != nil {
			return err
		}
		info := protocol.RPCSessionInfo{
			SessionID:         sessionID,
			Name:              title,
			Path:              sessionPath,
			CWD:               s.app.CWD(),
			Provider:          s.app.ProviderID,
			Model:             model.ID,
			Thinking:          s.app.Agent.Thinking(),
			ThinkingLevels:    model.SupportedThinkingLevels(),
			CollaborationMode: s.app.Agent.Mode(),
			Subagents: protocol.RPCSubagentLimits{
				Enabled:              s.app.Subagents != nil,
				MaxConcurrentAgents:  s.app.Cfg.Subagents.MaxConcurrentThreads,
				MaxConcurrentThreads: s.app.Cfg.Subagents.MaxConcurrentThreads,
				MaxAgentsPerSession:  s.app.Cfg.Subagents.MaxAgentsPerSession,
				MaxDepth:             s.app.Cfg.Subagents.MaxDepth,
				Durable:              s.app.Cfg.Subagents.Durable,
				AllowMutation:        s.app.Cfg.Subagents.AllowMutation,
			},
		}
		goal, _ := s.app.GoalState()
		if goal != nil {
			info.Goal = &protocol.RPCGoalSummary{GoalID: goal.GoalID, Status: goal.Status, TokensUsed: goal.TokensUsed, TokenBudget: goal.TokenBudget, EstimatedCosts: append([]protocol.Cost(nil), goal.EstimatedCosts...)}
		}
		steering, followUps := s.app.Agent.PendingInputs().Counts()
		info.PendingInputs = protocol.RPCPendingInputCounts{Steering: steering, FollowUp: followUps, Total: steering + followUps}
		s.write(Response{ID: req.ID, Type: "response", Command: "session_info", Success: true, Data: info})
		return nil
	default:
		return fmt.Errorf("unknown command %q", req.Type)
	}
}

func (s *Server) handlePrompt(ctx context.Context, req Request) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if req.Message == "" {
		return errors.New("prompt requires message")
	}
	if req.Mode != "" {
		if _, err := protocol.ParseCollaborationMode(req.Mode); err != nil {
			return err
		}
	}
	// Serve keeps scanning while the prompt runs, enabling explicit queue,
	// abort, and user-input commands. A second prompt never implicitly cancels
	// accepted work: callers must choose steer, follow_up, or abort.
	s.mu.Lock()
	active := s.promptDone != nil
	s.mu.Unlock()
	if active {
		return errors.New("rpc: prompt already running; use steer, follow_up, or abort")
	}

	promptCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	s.mu.Lock()
	s.cancel = cancel
	s.promptDone = done
	s.mu.Unlock()

	s.write(Response{ID: req.ID, Type: "response", Command: "prompt", Success: true})
	s.promptWG.Add(1)
	go func() {
		defer s.promptWG.Done()
		var err error
		if req.Mode != "" {
			mode, parseErr := protocol.ParseCollaborationMode(req.Mode)
			if parseErr != nil {
				err = parseErr
			} else {
				err = s.app.Agent.PromptWithMode(promptCtx, req.Message, mode)
			}
		} else {
			err = s.app.Agent.Prompt(promptCtx, req.Message)
		}
		canceled := promptCtx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
		if err != nil && !canceled {
			s.write(Response{ID: req.ID, Type: "response", Command: "prompt", Success: false, Error: err.Error()})
		}
		completed := protocol.RPCPromptCompleted{
			Type:      protocol.RPCTypePromptCompleted,
			RequestID: req.ID,
			Status:    protocol.RPCPromptCompletedStatus,
		}
		if canceled {
			completed.Status = protocol.RPCPromptCanceledStatus
		} else if err != nil {
			completed.Status = protocol.RPCPromptFailedStatus
		}
		if completed.Status == protocol.RPCPromptFailedStatus {
			completed.Error = err.Error()
		}
		s.write(completed)
		close(done)
		s.mu.Lock()
		if s.promptDone == done {
			s.promptDone = nil
			s.cancel = nil
		}
		s.mu.Unlock()
	}()
	return nil
}

func (s *Server) announceReady() error {
	s.readyOnce.Do(func() {
		_ = s.write(protocol.NewRPCReady(s.snowVersion))
	})
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeErr
}

func cloneModels(models []protocol.Model) []protocol.Model {
	out := make([]protocol.Model, len(models))
	for i, model := range models {
		out[i] = model.Clone()
	}
	return out
}

func (s *Server) write(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		s.recordWriteErr(err)
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writeErr != nil {
		return s.writeErr
	}
	payload := append(b, '\n')
	n, err := s.out.Write(payload)
	if err == nil && n != len(payload) {
		err = io.ErrShortWrite
	}
	if err != nil && s.writeErr == nil {
		s.writeErr = err
		s.writeFailOnce.Do(func() { close(s.writeFailed) })
	}
	return err
}

func (s *Server) recordWriteErr(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writeErr == nil {
		s.writeErr = err
		s.writeFailOnce.Do(func() { close(s.writeFailed) })
	}
}

// Main is the RPC entry point used by embedders that do not supply build metadata.
func Main(ctx context.Context, opts app.Options) error {
	return MainWithVersion(ctx, opts, "dev")
}

// MainWithVersion is the RPC entry point used by cmd/snow --mode rpc.
func MainWithVersion(ctx context.Context, opts app.Options, snowVersion string) (err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	a, err := app.New(ctx, opts)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, a.Close()) }()
	for _, diagnostic := range a.Diagnostics {
		fmt.Fprintf(os.Stderr, "config warning: %s: %s\n", diagnostic.Path, diagnostic.Message)
	}

	// Stream agent events to stdout as JSONL through the server's locked
	// writer so responses and events can never interleave corruptly.
	srv := NewWithOptions(ctx, a, os.Stdin, os.Stdout, ServerOptions{SnowVersion: snowVersion})
	if err := srv.announceReady(); err != nil {
		return err
	}
	a.Agent.Subscribe(func(ev protocol.AgentEvent) {
		srv.write(ev)
	})
	srv.write(a.Agent.StateEvent())
	if err := a.ReadyGoal(); err != nil {
		return err
	}
	if err := a.ReadySubagents(); err != nil {
		return err
	}
	return srv.Serve(ctx)
}
