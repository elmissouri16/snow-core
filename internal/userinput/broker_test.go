package userinput

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func testRequest() protocol.UserInputRequest {
	return protocol.UserInputRequest{
		ID: "call-1",
		Questions: []protocol.UserInputQuestion{
			{ID: "first", Header: "First", Question: "First question?"},
			{ID: "second", Header: "Second", Question: "Second question?"},
		},
	}
}

func TestBrokerManualReplyNormalizesAnswerOrder(t *testing.T) {
	b := New(nil)
	b.EnableManual()
	published := make(chan protocol.UserInputRequest, 1)
	resolved := make(chan result, 1)
	go func() {
		response, err := b.Ask(context.Background(), testRequest(), func(req protocol.UserInputRequest) { published <- req })
		resolved <- result{response: response, err: err}
	}()

	req := receive(t, published)
	if req.ID != "call-1" || len(req.Questions) != 2 {
		t.Fatalf("published request = %+v", req)
	}
	err := b.Reply(protocol.UserInputResponse{RequestID: req.ID, Answers: []protocol.UserInputAnswer{
		{QuestionID: "second", Answer: "  two  "},
		{QuestionID: "first", Answer: "one"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	got := receive(t, resolved)
	if got.err != nil {
		t.Fatal(got.err)
	}
	if len(got.response.Answers) != 2 || got.response.Answers[0].QuestionID != "first" || got.response.Answers[0].Answer != "one" || got.response.Answers[1].QuestionID != "second" || got.response.Answers[1].Answer != "two" {
		t.Fatalf("normalized response = %+v", got.response)
	}
}

func TestBrokerCallbackPublishesBeforeInvokingAndAcceptsOmittedRequestID(t *testing.T) {
	published := make(chan struct{})
	b := New(func(_ context.Context, req protocol.UserInputRequest) (protocol.UserInputResponse, error) {
		select {
		case <-published:
		default:
			t.Fatal("handler ran before request publication")
		}
		return protocol.UserInputResponse{Answers: []protocol.UserInputAnswer{
			{QuestionID: "second", Answer: "two"},
			{QuestionID: "first", Answer: "one"},
		}}, nil
	})
	response, err := b.Ask(context.Background(), testRequest(), func(protocol.UserInputRequest) { close(published) })
	if err != nil {
		t.Fatal(err)
	}
	if response.RequestID != "call-1" || response.Answers[0].QuestionID != "first" {
		t.Fatalf("response = %+v", response)
	}
}

func TestBrokerRejectCancelUnavailableAndValidation(t *testing.T) {
	t.Run("unavailable", func(t *testing.T) {
		_, err := New(nil).Ask(context.Background(), testRequest(), nil)
		if !errors.Is(err, ErrUnavailable) {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("reject", func(t *testing.T) {
		b := New(nil)
		b.EnableManual()
		published := make(chan protocol.UserInputRequest, 1)
		resolved := make(chan error, 1)
		go func() {
			_, err := b.Ask(context.Background(), testRequest(), func(req protocol.UserInputRequest) { published <- req })
			resolved <- err
		}()
		req := receive(t, published)
		if err := b.Reject(req.ID); err != nil {
			t.Fatal(err)
		}
		if err := receive(t, resolved); !errors.Is(err, ErrRejected) {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("cancel", func(t *testing.T) {
		b := New(nil)
		b.EnableManual()
		ctx, cancel := context.WithCancel(context.Background())
		published := make(chan protocol.UserInputRequest, 1)
		resolved := make(chan error, 1)
		go func() {
			_, err := b.Ask(ctx, testRequest(), func(req protocol.UserInputRequest) { published <- req })
			resolved <- err
		}()
		receive(t, published)
		cancel()
		if err := receive(t, resolved); !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("invalid reply remains pending", func(t *testing.T) {
		b := New(nil)
		b.EnableManual()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		published := make(chan protocol.UserInputRequest, 1)
		go b.Ask(ctx, testRequest(), func(req protocol.UserInputRequest) { published <- req }) //nolint:errcheck
		req := receive(t, published)
		err := b.Reply(protocol.UserInputResponse{RequestID: req.ID, Answers: []protocol.UserInputAnswer{{QuestionID: "first", Answer: strings.Repeat("x", MaxAnswerBytes+1)}}})
		if err == nil || !strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("error = %v", err)
		}
		if err := b.Reject(req.ID); err != nil {
			t.Fatalf("invalid reply cleared pending request: %v", err)
		}
	})
}

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for broker result")
		var zero T
		return zero
	}
}
