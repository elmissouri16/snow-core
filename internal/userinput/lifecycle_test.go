package userinput

import (
	"context"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestBrokerDoneTracksRequestSettlement(t *testing.T) {
	for _, action := range []string{"reply", "reject", "cancel", "close"} {
		t.Run(action, func(t *testing.T) {
			b := New(nil)
			b.EnableManual()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			published := make(chan protocol.UserInputRequest, 1)
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				_, _ = b.Ask(ctx, testRequest(), func(req protocol.UserInputRequest) { published <- req })
			}()
			req := receive(t, published)
			done := b.Done(req.ID)
			select {
			case <-done:
				t.Fatal("pending request already settled")
			default:
			}
			select {
			case <-b.Done("unrelated"):
			default:
				t.Fatal("unrelated request shares pending lifetime")
			}
			switch action {
			case "reply":
				if err := b.Reply(protocol.UserInputResponse{RequestID: req.ID, Answers: []protocol.UserInputAnswer{{QuestionID: "first", Answer: "one"}, {QuestionID: "second", Answer: "two"}}}); err != nil {
					t.Fatal(err)
				}
			case "reject":
				if err := b.Reject(req.ID); err != nil {
					t.Fatal(err)
				}
			case "cancel":
				cancel()
			case "close":
				b.Close()
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("settlement was not signalled")
			}
			select {
			case <-finished:
			case <-time.After(time.Second):
				t.Fatal("Ask did not return")
			}
			select {
			case <-b.Done(req.ID):
			default:
				t.Fatal("late subscriber missed settlement")
			}
		})
	}
}

func TestBrokerInvalidChoiceKeepsRequestPending(t *testing.T) {
	b := New(nil)
	b.EnableManual()
	t.Cleanup(b.Close)
	published := make(chan protocol.UserInputRequest, 1)
	finished := make(chan error, 1)
	req := protocol.UserInputRequest{ID: "selection", Questions: []protocol.UserInputQuestion{{ID: "color", ChoicesOnly: true, Options: []protocol.UserInputOption{{Label: "Blue"}, {Label: "Green"}}}}}
	go func() {
		_, err := b.Ask(t.Context(), req, func(req protocol.UserInputRequest) { published <- req })
		finished <- err
	}()
	receive(t, published)
	response := protocol.UserInputResponse{RequestID: req.ID, Answers: []protocol.UserInputAnswer{{QuestionID: "color", Answer: "Red"}}}
	if err := b.Reply(response); err == nil {
		t.Fatal("accepted a value outside the closed selection")
	}
	select {
	case <-b.Done(req.ID):
		t.Fatal("invalid answer dismissed the request")
	default:
	}
	response.Answers[0].Answer = "Blue"
	if err := b.Reply(response); err != nil {
		t.Fatal(err)
	}
	if err := receive(t, finished); err != nil {
		t.Fatal(err)
	}
}
