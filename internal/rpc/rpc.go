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
	"github.com/snow-core/snow/pkg/protocol"
)

// Request is one command line from the client.
type Request struct {
	ID      string          `json:"id,omitempty"`
	Type    string          `json:"type"`
	Message string          `json:"message,omitempty"`
	Model   string          `json:"model,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response acknowledges a command.
type Response struct {
	ID      string `json:"id,omitempty"`
	Type    string `json:"type"`
	Command string `json:"command,omitempty"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// Server serves RPC on stdin/stdout.
type Server struct {
	in  io.Reader
	out io.Writer
	app *app.App
	mu  sync.Mutex
	// cancel aborts the in-flight prompt.
	cancel context.CancelFunc
}

// New creates an RPC server.
func New(ctx context.Context, a *app.App, in io.Reader, out io.Writer) *Server {
	return &Server{in: in, out: out, app: a}
}

// Serve reads commands until EOF.
func (s *Server) Serve(ctx context.Context) error {
	scanner := bufio.NewScanner(s.in)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 {
			continue
		}
		var req Request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			s.write(Response{ID: req.ID, Type: "response", Command: "invalid", Success: false, Error: "invalid JSON: " + err.Error()})
			continue
		}
		if err := s.handle(ctx, req); err != nil {
			s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: false, Error: err.Error()})
		}
	}
	return scanner.Err()
}

func (s *Server) handle(ctx context.Context, req Request) error {
	switch req.Type {
	case "prompt":
		return s.handlePrompt(ctx, req)
	case "abort":
		s.mu.Lock()
		if s.cancel != nil {
			s.cancel()
		}
		s.mu.Unlock()
		s.write(Response{ID: req.ID, Type: "response", Command: "abort", Success: true})
		return nil
	case "set_model":
		if req.Model == "" {
			return errors.New("set_model requires model")
		}
		m := s.app.Model
		m.ID = req.Model
		if err := s.app.Agent.SetModel(m); err != nil {
			return err
		}
		s.write(Response{ID: req.ID, Type: "response", Command: "set_model", Success: true})
		return nil
	case "session_info":
		info := map[string]any{
			"session_id": s.app.Session.ID(),
			"path":       s.app.Session.Path(),
			"cwd":        s.app.CWD(),
			"provider":   s.app.ProviderID,
			"model":      s.app.Model.ID,
		}
		b, _ := json.Marshal(info)
		s.write(Response{ID: req.ID, Type: "response", Command: "session_info", Success: true, Error: string(b)})
		return nil
	default:
		return fmt.Errorf("unknown command %q", req.Type)
	}
}

func (s *Server) handlePrompt(ctx context.Context, req Request) error {
	if req.Message == "" {
		return errors.New("prompt requires message")
	}
	ctx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	s.cancel = cancel
	s.mu.Unlock()

	s.write(Response{ID: req.ID, Type: "response", Command: "prompt", Success: true})
	return s.app.Agent.Prompt(ctx, req.Message)
}

func (s *Server) write(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.out.Write(append(b, '\n'))
}

// Main is the RPC entry point used by cmd/snow --mode rpc.
func Main(ctx context.Context, opts app.Options) error {
	a, err := app.New(ctx, opts)
	if err != nil {
		return err
	}
	defer a.Close()

	// Stream agent events to stdout as JSONL.
	a.Agent.Subscribe(func(ev protocol.AgentEvent) {
		b, _ := json.Marshal(ev)
		_, _ = os.Stdout.Write(append(b, '\n'))
	})

	srv := New(ctx, a, os.Stdin, os.Stdout)
	return srv.Serve(ctx)
}
