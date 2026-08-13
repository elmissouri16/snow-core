package provider

import (
	"errors"
	"testing"
)

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
