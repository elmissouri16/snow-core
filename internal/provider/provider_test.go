package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"
)

type transientTestError struct{ retry bool }

func (e transientTestError) Error() string   { return "temporary provider failure" }
func (e transientTestError) Transient() bool { return e.retry }

func TestRetryAdviceRequiresEntireErrorChainToBeRetryable(t *testing.T) {
	transient := transientTestError{retry: true}
	advice, ok := RetryAdviceFor(fmt.Errorf("provider request: %w", transient))
	if !ok || advice.Kind != RetryTransient {
		t.Fatalf("wrapped transient advice=%+v ok=%v", advice, ok)
	}
	for _, err := range []error{
		errors.Join(transient, errors.New("session write failed")),
		context.Canceled,
		context.DeadlineExceeded,
		&LimitError{Provider: "x", Status: 429, Message: "quota"},
		transientTestError{retry: false},
	} {
		if advice, ok := RetryAdviceFor(err); ok {
			t.Fatalf("non-retryable error %v returned advice %+v", err, advice)
		}
	}
}

func TestRetryAdviceSeparatesThrottleAndRejectsMixedJoins(t *testing.T) {
	throttle := &RateLimitError{Provider: "x", Status: 429, Message: "slow down", RetryAfter: 3 * time.Second}
	advice, ok := RetryAdviceFor(fmt.Errorf("request: %w", throttle))
	if !ok || advice.Kind != RetryRateLimit || advice.RetryAfter != 3*time.Second {
		t.Fatalf("advice=%+v ok=%v", advice, ok)
	}
	if _, ok := RetryAdviceFor(errors.Join(throttle, errors.New("persist failed"))); ok {
		t.Fatal("mixed joined failure was retryable")
	}
	joined, ok := RetryAdviceFor(errors.Join(throttle, transientTestError{retry: true}))
	if !ok || joined.Kind != RetryRateLimit || joined.RetryAfter != 3*time.Second {
		t.Fatalf("joined advice=%+v ok=%v", joined, ok)
	}
}

func TestParseRetryAfterFormsAndBounds(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		header http.Header
		want   time.Duration
	}{
		{name: "seconds", header: http.Header{"Retry-After": []string{"7"}}, want: 7 * time.Second},
		{name: "milliseconds", header: http.Header{"Retry-After-Ms": []string{"1250"}}, want: 1250 * time.Millisecond},
		{name: "date", header: http.Header{"Retry-After": []string{now.Add(9 * time.Second).Format(http.TimeFormat)}}, want: 9 * time.Second},
		{name: "larger form wins", header: http.Header{"Retry-After": []string{"2"}, "Retry-After-Ms": []string{"5000"}}, want: 5 * time.Second},
		{name: "malformed", header: http.Header{"Retry-After": []string{"later"}}, want: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseRetryAfter(tc.header, now, time.Minute); got != tc.want {
				t.Fatalf("delay=%v want %v", got, tc.want)
			}
		})
	}
	if got := ParseRetryAfter(http.Header{"Retry-After": []string{"999"}}, now, 30*time.Second); got != 30*time.Second {
		t.Fatalf("bounded delay=%v", got)
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
