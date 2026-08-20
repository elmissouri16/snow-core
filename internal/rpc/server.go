package rpc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/subagent"
	"github.com/elmissouri16/snow-core/internal/worktree"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// New creates an RPC server using the assembled app's build metadata.
func New(ctx context.Context, a *app.App, in io.Reader, out io.Writer) *Server {
	snowVersion := "dev"
	if a != nil && a.BuildVersion != "" {
		snowVersion = a.BuildVersion
	}
	return NewWithOptions(ctx, a, in, out, ServerOptions{SnowVersion: snowVersion})
}

// NewWithOptions creates an RPC server.
func NewWithOptions(ctx context.Context, a *app.App, in io.Reader, out io.Writer, opts ServerOptions) *Server {
	if ctx == nil {
		ctx = context.Background()
	}
	a.EnableUserInputReplies()
	a.EnablePermissionReplies()
	input, hasClose := in.(io.ReadCloser)
	if !hasClose {
		input = io.NopCloser(in)
	}
	independentInputInterrupt := false
	switch typed := in.(type) {
	case *bytes.Buffer, *bytes.Reader, *strings.Reader, *io.PipeReader:
		independentInputInterrupt = true
	case *os.File:
		if info, err := typed.Stat(); err == nil && info.Mode().IsRegular() {
			independentInputInterrupt = true
		}
	}
	if declared, ok := in.(InterruptibleInput); ok && declared.InterruptsReadOnClose() {
		independentInputInterrupt = true
	}
	var inputDeadline func(time.Time) error
	if setter, ok := in.(interface{ SetReadDeadline(time.Time) error }); ok {
		inputDeadline = setter.SetReadDeadline
	}
	interruptible := independentInputInterrupt || inputDeadline != nil
	independentOutputBound := false
	switch out.(type) {
	case *bytes.Buffer, *strings.Builder:
		independentOutputBound = true
	}
	if declared, ok := out.(BoundedOutput); ok && declared.RPCWriteBounded() {
		independentOutputBound = true
	}
	if typ := reflect.TypeOf(out); typ != nil && typ.Comparable() && out == io.Discard {
		independentOutputBound = true
	}
	_, deadlineOutput := out.(interface{ SetWriteDeadline(time.Time) error })
	return &Server{in: input, inputInterruptible: interruptible, inputIndependentInterruptible: independentInputInterrupt, inputDeadline: inputDeadline, out: out, outputBounded: independentOutputBound || deadlineOutput, outputIndependentBound: independentOutputBound, app: a, writeFailed: make(chan struct{}), snowVersion: opts.SnowVersion, waitSlots: make(chan struct{}, maxConcurrentWaits)}
}

func (s *Server) interruptInput() {
	if s.inputDeadline != nil {
		_ = s.inputDeadline(time.Now())
	}
	_ = s.in.Close()
}

// Serve reads commands until EOF.
func (s *Server) Serve(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if !s.inputInterruptible {
		return errors.New("rpc: input must be finite or guarantee that Close/deadline interrupts reads")
	}
	if s.inputDeadline != nil && !s.inputIndependentInterruptible {
		if err := s.inputDeadline(time.Now()); err != nil {
			return fmt.Errorf("rpc: input read deadline unavailable: %w", err)
		}
		if err := s.inputDeadline(time.Time{}); err != nil {
			return fmt.Errorf("rpc: clear input read deadline: %w", err)
		}
	}
	if !s.outputBounded {
		return errors.New("rpc: output must be deadline-capable or guarantee bounded writes")
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
	scanDone := make(chan struct{})
	defer func() {
		close(scanStop)
		s.interruptInput()
		<-scanDone
	}()
	go func() {
		defer close(scanDone)
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
			s.interruptInput()
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
			_ = s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: false, Error: err.Error(), ErrorCode: rpcErrorCode(err)})
		}
	}
finish:
	// No more replies can arrive after EOF or cancellation. Release pending/future interaction
	// and wait commands. Ordinary prompts retain their own documented join path.
	cancelServe()
	s.app.CloseUserInput()
	s.app.ClosePermissionBroker()
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

func rpcErrorCode(err error) string {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "canceled"
	case errors.Is(err, session.ErrDestinationExists), errors.Is(err, worktree.ErrDestinationExists):
		return "destination_exists"
	case errors.Is(err, worktree.ErrNotRepository):
		return "not_git_repository"
	case errors.Is(err, worktree.ErrDirty):
		return "git_dirty"
	case errors.Is(err, worktree.ErrUnsafeDestination), errors.Is(err, session.ErrInvalidForkBoundary):
		return "invalid"
	case errors.Is(err, session.ErrNotFound):
		return "not_found"
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "subagents are active"):
		return "subagents_active"
	case strings.Contains(message, "while running"):
		return "session_busy"
	case strings.Contains(message, "does not support"):
		return "unsupported"
	case strings.Contains(message, "already exists"), strings.Contains(message, "conflict"):
		return "conflict"
	case strings.HasPrefix(message, "worktree:"), strings.Contains(message, "git "):
		return "git_failure"
	default:
		return "invalid"
	}
}

func (s *Server) handle(ctx context.Context, req Request) error {
	if ctx == nil {
		ctx = context.Background()
	}
	switch req.Type {
	case "prompt":
		return s.handlePrompt(ctx, req)
	case "abort":
		// Cancel the RPC prompt context before waiting for Agent.Abort. Abort may
		// let the agent goroutine settle synchronously; canceling afterward races
		// prompt_completed and can incorrectly report a completed prompt.
		s.mu.Lock()
		cancel := s.cancel
		s.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		s.app.Agent.Abort()
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
	case "permission_reply":
		return s.handlePermissionReply(req)
	case "permission_reject":
		return s.handlePermissionReject(req)

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
		select {
		case s.waitSlots <- struct{}{}:
		default:
			return fmt.Errorf("rpc: concurrent subagent_wait limit %d reached", maxConcurrentWaits)
		}
		s.promptWG.Add(1)
		go func() {
			defer s.promptWG.Done()
			defer func() { <-s.waitSlots }()
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
	case "set_reasoning_summary":
		if req.ReasoningSummary == "" {
			return errors.New("set_reasoning_summary requires reasoning_summary")
		}
		summary, err := protocol.ParseReasoningSummary(req.ReasoningSummary)
		if err != nil {
			return err
		}
		if err := s.app.Agent.SetReasoningSummary(summary); err != nil {
			return err
		}
		s.write(Response{ID: req.ID, Type: "response", Command: "set_reasoning_summary", Success: true})
		return nil
	case "set_text_verbosity":
		if req.TextVerbosity == "" {
			return errors.New("set_text_verbosity requires text_verbosity")
		}
		verbosity, err := protocol.ParseTextVerbosity(req.TextVerbosity)
		if err != nil {
			return err
		}
		if err := s.app.Agent.SetTextVerbosity(verbosity); err != nil {
			return err
		}
		s.write(Response{ID: req.ID, Type: "response", Command: "set_text_verbosity", Success: true})
		return nil
	case "compact":
		result, err := s.app.Agent.Compact(ctx)
		if err != nil {
			return err
		}
		s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: result})
		return nil
	case "branches_list":
		branches, err := s.app.Agent.Branches()
		if err != nil {
			return err
		}
		if branches == nil {
			branches = []protocol.SessionBranch{}
		}
		s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: protocol.RPCBranchList{Branches: branches}})
		return nil
	case "branch_select":
		var p struct {
			BranchID string `json:"branch_id"`
		}
		if len(req.Params) == 0 {
			return errors.New("branch_select requires params")
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return fmt.Errorf("branch_select params: %w", err)
		}
		if p.BranchID == "" {
			return errors.New("branch_select requires branch_id")
		}
		if err := s.app.SelectBranch(p.BranchID); err != nil {
			return err
		}
		s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true})
		return nil
	case "branch_rename":
		var p struct {
			BranchID string `json:"branch_id"`
			Name     string `json:"name"`
		}
		if len(req.Params) == 0 {
			return errors.New("branch_rename requires params")
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return fmt.Errorf("branch_rename params: %w", err)
		}
		if p.BranchID == "" || p.Name == "" {
			return errors.New("branch_rename requires branch_id and name")
		}
		branch, err := s.app.RenameBranch(p.BranchID, p.Name)
		if err != nil {
			return err
		}
		s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: branch})
		return nil
	case "branch_delete":
		var p struct {
			BranchID string `json:"branch_id"`
		}
		if len(req.Params) == 0 {
			return errors.New("branch_delete requires params")
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return fmt.Errorf("branch_delete params: %w", err)
		}
		if p.BranchID == "" {
			return errors.New("branch_delete requires branch_id")
		}
		if err := s.app.DeleteBranch(p.BranchID); err != nil {
			return err
		}
		s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true})
		return nil
	case "messages_list":
		messages, err := s.app.Agent.Messages()
		if err != nil {
			return err
		}
		messages = publicMessages(messages)
		s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: protocol.RPCMessagesList{Messages: messages}})
		return nil
	case "usage":
		usage, err := s.app.Agent.Usage()
		if err != nil {
			return err
		}
		s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: usage})
		return nil
	case "pending_inputs":
		queue := s.app.Agent.PendingInputs()
		if queue.Items == nil {
			queue.Items = []protocol.QueuedInput{}
		}
		s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: queue})
		return nil
	case "pending_inputs_clear":
		queue := s.app.Agent.ClearPendingInputs()
		if queue.Items == nil {
			queue.Items = []protocol.QueuedInput{}
		}
		s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: queue})
		return nil
	case "diagnostics":
		diagnostics := s.app.ConfigDiagnostics()
		s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: protocol.RPCDiagnosticsList{Diagnostics: diagnostics}})
		return nil
	case "mcp_servers":
		return s.handleMCPServers(req)
	case "skills":
		return s.handleSkills(req)
	case "sandbox_status":
		return s.handleSandboxStatus(req)
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
	case "branch_fork":
		var p protocol.BranchForkOptions
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return fmt.Errorf("branch_fork params: %w", err)
			}
		}
		branch, err := s.app.ForkBranchWithOptions(p)
		if err != nil {
			return err
		}
		s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: branch})
		return nil
	case "session_fork":
		var p protocol.SessionForkOptions
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return fmt.Errorf("session_fork params: %w", err)
			}
		}
		result, err := s.app.ForkSession(ctx, p)
		if err != nil {
			return err
		}
		s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: result})
		return nil
	case "session_worktree_fork":
		var p protocol.SessionWorktreeForkOptions
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return fmt.Errorf("session_worktree_fork params: %w", err)
			}
		}
		result, err := s.app.ForkWorktree(ctx, p)
		if err != nil {
			return err
		}
		s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: result})
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
			ReasoningSummary:  s.app.Agent.ReasoningSummary(),
			TextVerbosity:     s.app.Agent.TextVerbosity(),
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
	if req.Content != nil {
		if err := validatePromptContent(req.Message, req.Content); err != nil {
			return err
		}
	} else if req.Message == "" {
		return errors.New("prompt requires message")
	}
	if hasImageContent(req.Content) {
		if !s.app.Agent.Model().SupportsVision {
			return errors.New("model does not support image input")
		}
		return s.handleImagePrompt(ctx, req)
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
		switch {
		case len(req.Content) > 0 && req.Mode != "":
			mode, parseErr := protocol.ParseCollaborationMode(req.Mode)
			if parseErr != nil {
				err = parseErr
			} else {
				err = s.app.Agent.PromptContentWithMode(promptCtx, req.Message, req.Content, mode)
			}
		case len(req.Content) > 0:
			err = s.app.Agent.PromptContent(promptCtx, req.Message, req.Content)
		case req.Mode != "":
			mode, parseErr := protocol.ParseCollaborationMode(req.Mode)
			if parseErr != nil {
				err = parseErr
			} else {
				err = s.app.Agent.PromptWithMode(promptCtx, req.Message, mode)
			}
		default:
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

// publicMessages returns an independent wire-safe history snapshot. Provider
// continuity blocks are durable context for provider adapters only and must
// never cross an RPC, rendering, or logging boundary.
func publicMessages(messages []protocol.Message) []protocol.Message {
	out := make([]protocol.Message, 0, len(messages))
	for _, message := range messages {
		copy := message.Clone()
		content := make([]protocol.ContentBlock, 0, len(copy.Content))
		for _, block := range copy.Content {
			if block.Type != protocol.BlockProviderData {
				content = append(content, block)
			}
		}
		copy.Content = content
		out = append(out, copy)
	}
	return out
}

func (s *Server) write(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		s.recordWriteErr(err)
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.mu.Lock()
	priorErr := s.writeErr
	s.mu.Unlock()
	if priorErr != nil {
		return priorErr
	}
	if !s.outputBounded {
		err := errors.New("rpc: output must be deadline-capable or guarantee bounded writes")
		s.recordWriteErr(err)
		return err
	}
	deadlineSet := false
	if setter, ok := s.out.(interface{ SetWriteDeadline(time.Time) error }); ok {
		if err := setter.SetWriteDeadline(time.Now().Add(rpcWriteTimeout)); err != nil {
			if !s.outputIndependentBound {
				err = fmt.Errorf("rpc: output write deadline unavailable: %w", err)
				s.recordWriteErr(err)
				return err
			}
		} else {
			deadlineSet = true
		}
		if deadlineSet {
			defer setter.SetWriteDeadline(time.Time{})
		}
	}
	payload := append(b, '\n')
	n, err := s.out.Write(payload)
	if err == nil && n != len(payload) {
		err = io.ErrShortWrite
	}
	if err != nil {
		s.recordWriteErr(err)
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
