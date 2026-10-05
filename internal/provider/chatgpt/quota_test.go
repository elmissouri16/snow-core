package chatgpt

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/auth"
	providerpkg "github.com/elmissouri16/snow-core/internal/provider"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestResponsesQuotaClassification(t *testing.T) {
	const resetSeconds = 2000000000
	cases := []struct {
		name   string
		fields string
		kind   string
		reset  bool
	}{
		{name: "ordinary throttling", fields: `"type":"rate_limit_exceeded","code":"rate_limit_exceeded"`},
		{name: "missing metadata", fields: `"message":"usage_limit_reached insufficient_quota credit balance exhausted"`},
		{name: "unknown type", fields: `"type":"future_quota","code":"unknown"`},
		{name: "subscription code alone", fields: `"code":"usage_limit_reached","resets_at":2000000000`},
		{name: "subscription type", fields: `"type":"usage_limit_reached"`, kind: "usage_limit_reached"},
		{name: "subscription reset", fields: `"type":"usage_limit_reached","resets_at":2000000000`, kind: "usage_limit_reached", reset: true},
		{name: "subscription type wins", fields: `"type":"usage_limit_reached","code":"insufficient_quota","resets_at":2000000000`, kind: "usage_limit_reached", reset: true},
		{name: "not included", fields: `"type":"usage_not_included","resets_at":2000000000`, kind: "usage_not_included"},
		{name: "quota type", fields: `"type":"insufficient_quota","resets_at":2000000000`, kind: "insufficient_quota"},
		{name: "quota code", fields: `"code":"insufficient_quota","resets_at":2000000000`, kind: "insufficient_quota"},
		{name: "credit code", fields: `"code":"credit_balance_exhausted"`, kind: "credit_balance_exhausted"},
		{name: "organization spend code", fields: `"code":"organization_spend_limit_exceeded"`, kind: "organization_spend_limit_exceeded"},
		{name: "project spend code", fields: `"code":"project_spend_limit_exceeded"`, kind: "project_spend_limit_exceeded"},
		{name: "organization usage code", fields: `"type":"unknown","code":"organization_usage_limit_exceeded"`, kind: "organization_usage_limit_exceeded"},
		{name: "code in type is unverified", fields: `"type":"credit_balance_exhausted"`},
		{name: "wrong case", fields: `"type":"USAGE_LIMIT_REACHED"`},
		{name: "string reset", fields: `"type":"usage_limit_reached","resets_at":"2000000000"`, kind: "usage_limit_reached"},
		{name: "fractional reset", fields: `"type":"usage_limit_reached","resets_at":2000000000.5`, kind: "usage_limit_reached"},
		{name: "object reset", fields: `"type":"usage_limit_reached","resets_at":{}`, kind: "usage_limit_reached"},
		{name: "null reset", fields: `"type":"usage_limit_reached","resets_at":null`, kind: "usage_limit_reached"},
		{name: "zero reset", fields: `"type":"usage_limit_reached","resets_at":0`, kind: "usage_limit_reached"},
		{name: "negative reset", fields: `"type":"usage_limit_reached","resets_at":-1`, kind: "usage_limit_reached"},
		{name: "millisecond reset", fields: `"type":"usage_limit_reached","resets_at":2000000000000`, kind: "usage_limit_reached"},
		{name: "out of RFC3339 range", fields: `"type":"usage_limit_reached","resets_at":253402300800`, kind: "usage_limit_reached"},
		{name: "overflow reset", fields: `"type":"usage_limit_reached","resets_at":9223372036854775808`, kind: "usage_limit_reached"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const access = "access-token-secret"
			const refresh = "refresh-token-secret"
			var calls atomic.Int32
			body := fmt.Sprintf(`{"error":{%s},"diagnostics":"%s %s"}`, tc.fields, access, refresh)
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls.Add(1)
				return &http.Response{StatusCode: http.StatusTooManyRequests,
					Header: http.Header{"Retry-After": {"15"}, "X-Request-Id": {access + refresh}},
					Body:   io.NopCloser(strings.NewReader(body))}, nil
			})}
			p := New(Config{HTTPClient: client})
			stream, err := p.Chat(t.Context(), auth.Credential{Type: auth.CredentialOAuth, Access: access, Refresh: refresh, AccountID: "acct"}, protocol.ChatRequest{Model: protocol.Model{Provider: ProviderID, ID: "m"}})
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			ev, err := stream.Next(t.Context())
			if err != nil || ev.Type != protocol.EvStreamError || ev.Err == nil {
				t.Fatalf("event=%+v err=%v", ev, err)
			}
			if calls.Load() != 1 {
				t.Fatalf("unexpected retry or failover: calls=%d", calls.Load())
			}
			if strings.Contains(ev.Err.Error(), access) || strings.Contains(ev.Err.Error(), refresh) {
				t.Fatal("credential leaked in quota/throttling diagnostics")
			}
			limit, limited := errors.AsType[*providerpkg.LimitError](ev.Err)
			if tc.kind == "" {
				throttle, ok := errors.AsType[*providerpkg.RateLimitError](ev.Err)
				if limited || !ok || throttle.RetryAfter != 15*time.Second {
					t.Fatalf("expected ordinary throttling: %T %v", ev.Err, ev.Err)
				}
				return
			}
			if !limited || limit.Kind != tc.kind || limit.Provider != ProviderID || limit.Status != http.StatusTooManyRequests {
				t.Fatalf("expected terminal quota %q: %T %v", tc.kind, ev.Err, ev.Err)
			}
			if _, retryable := errors.AsType[providerpkg.RetryableError](ev.Err); retryable {
				t.Fatal("terminal quota advertises retry advice")
			}
			if tc.reset {
				if !limit.ResetsAt.Equal(time.Unix(resetSeconds, 0)) || limit.ResetsAt.Location() != time.UTC {
					t.Fatalf("reset=%v, want UTC Unix seconds", limit.ResetsAt)
				}
			} else if !limit.ResetsAt.IsZero() {
				t.Fatalf("unverified reset=%v", limit.ResetsAt)
			}
		})
	}
}

func TestQuotaMetadataRejectsUnverifiedPayloads(t *testing.T) {
	for _, body := range []string{
		`not JSON`,
		`{"error":{"type":"usage_limit_reached"}`,
		`{"error":"usage_limit_reached"}`,
		`{"error":{"type":["usage_limit_reached"]}}`,
		`{"error":{"type":"unknown","code":42}}`,
		`{"error":{"type":"usage_limit_reached","type":"unknown"}}`,
		`{"error":{"type":"usage_limit_reached"},"padding":"` + strings.Repeat("x", maxHTTPErrorReadBytes) + `"}`,
	} {
		if kind, _ := quotaMetadata(http.StatusTooManyRequests, []byte(body)); kind != "" {
			t.Fatalf("unverified payload classified as %q", kind)
		}
	}
	for _, status := range []int{http.StatusBadRequest, http.StatusPaymentRequired, http.StatusForbidden, http.StatusServiceUnavailable} {
		if kind, _ := quotaMetadata(status, []byte(`{"error":{"type":"usage_limit_reached","resets_at":2000000000}}`)); kind != "" {
			t.Fatalf("HTTP %d interpreted as a Codex subscription exhaustion", status)
		}
	}
}
