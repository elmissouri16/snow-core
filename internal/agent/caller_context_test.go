package agent

import (
	"context"
	"errors"
	goalpkg "github.com/elmissouri16/snow-core/internal/goal"
	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

type callerContextDeadlineProvider struct {
	calls     atomic.Int32
	restarted chan struct{}
	startup   bool
}

func (*callerContextDeadlineProvider) ID() string { return "callerContext" }
func (*callerContextDeadlineProvider) ListModels(context.Context) ([]protocol.Model, error) {
	return nil, nil
}
func (p *callerContextDeadlineProvider) Chat(ctx context.Context, _ protocol.ChatRequest) (protocol.EventStream, error) {
	if p.calls.Add(1) > 1 {
		select {
		case p.restarted <- struct{}{}:
		default:
		}
	}
	if p.startup {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return &callerContextDeadlineStream{}, nil
}

type callerContextDeadlineStream struct{ sent bool }

func (s *callerContextDeadlineStream) Next(ctx context.Context) (protocol.StreamEvent, error) {
	if !s.sent {
		s.sent = true
		return protocol.StreamEvent{Type: protocol.EvStreamTextDelta, Text: "Partial work"}, nil
	}
	<-ctx.Done()
	return protocol.StreamEvent{}, ctx.Err()
}
func (*callerContextDeadlineStream) Close() error { return nil }
func TestCallerDeadlineOutcome(t *testing.T) {
	for _, withGoal := range []bool{false, true} {
		for _, startup := range []bool{false, true} {
			name := "ordinary"
			if withGoal {
				name = "goal"
			}
			if startup {
				name += "/startup"
			} else {
				name += "/stream"
			}
			t.Run(name, func(t *testing.T) {
				st, err := session.NewSQLiteStore(filepath.Join(t.TempDir(), "callerContext.db"), t.TempDir(), session.Options{})
				if err != nil {
					t.Fatal(err)
				}
				defer st.Close()
				reg := tools.NewRegistry()
				c, err := goalpkg.New(st, t.TempDir(), nil)
				if err != nil {
					t.Fatal(err)
				}
				for _, tool := range goalpkg.Tools(c) {
					if err := reg.Register(tool); err != nil {
						t.Fatal(err)
					}
				}
				p := &callerContextDeadlineProvider{restarted: make(chan struct{}, 1), startup: startup}
				a, err := New(Options{Provider: p, Registry: reg, Session: st, Permission: permission.NewService(permission.ModeDeny, nil), Model: protocol.Model{Provider: p.ID(), ID: "m", SupportsTools: true}, Goal: c})
				if err != nil {
					t.Fatal(err)
				}
				defer a.Close()
				if withGoal {
					if _, err := c.Create("finish task", nil, false); err != nil {
						t.Fatal(err)
					}
				}
				ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
				defer cancel()
				err = a.Prompt(ctx, "do work")
				t.Logf("prompt error=%v context error=%v", err, ctx.Err())
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Errorf("prompt error=%v; want deadline exceeded", err)
				}
				if withGoal {
					goal, getErr := c.Get()
					if getErr != nil || goal.Status != protocol.GoalPaused {
						t.Errorf("goal after cancellation=%+v err=%v", goal, getErr)
					}
					select {
					case <-p.restarted:
						t.Errorf("new provider request started after caller deadline; calls=%d", p.calls.Load())
					case <-time.After(150 * time.Millisecond):
					}
				}
			})
		}
	}
}
