package provider

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

type transientTestError struct{ retry bool }

func (e transientTestError) Error() string   { return "temporary provider failure" }
func (e transientTestError) Transient() bool { return e.retry }

func TestIsTransientErrorRequiresEntireErrorChainToBeRetryable(t *testing.T) {
	transient := transientTestError{retry: true}
	if !IsTransientError(fmt.Errorf("provider request: %w", transient)) {
		t.Fatal("wrapped transient marker was not recognized")
	}
	if IsTransientError(errors.Join(transient, errors.New("session write failed"))) {
		t.Fatal("mixed joined failure was treated as transient")
	}
	if IsTransientError(context.Canceled) || IsTransientError(context.DeadlineExceeded) {
		t.Fatal("caller cancellation was treated as transient")
	}
	if IsTransientError(&LimitError{Provider: "x", Status: 429, Message: "quota"}) {
		t.Fatal("usage limit was treated as transient")
	}
	if IsTransientError(transientTestError{retry: false}) {
		t.Fatal("negative transient marker was ignored")
	}
}

func TestIsContextWindowExceededIsConservative(t *testing.T) {
	for _, err := range []error{
		errors.New("maximum context length exceeded"),
		errors.New("context_length_exceeded"),
		errors.New("prompt is too long"),
		errors.New("too many tokens in prompt"),
	} {
		if !IsContextWindowExceeded(err) {
			t.Fatalf("not recognized: %v", err)
		}
	}
	for _, err := range []error{
		errors.New("HTTP 400"),
		errors.New("max_tokens must be positive"),
		errors.New("response text exceeds size limit"),
		&LimitError{Provider: "x", Status: 429, Message: "token quota"},
	} {
		if IsContextWindowExceeded(err) {
			t.Fatalf("false positive: %v", err)
		}
	}
}
