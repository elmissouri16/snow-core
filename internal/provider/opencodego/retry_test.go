package opencodego

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/auth"
	providerpkg "github.com/elmissouri16/snow-core/internal/provider"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestChatClassifiesTemporaryHTTPFailures(t *testing.T) {
	for _, status := range []int{http.StatusRequestTimeout, http.StatusTooEarly, http.StatusInternalServerError, http.StatusServiceUnavailable, 599, http.StatusTooManyRequests} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Retry-After", "4")
				http.Error(w, "temporary", status)
			}))
			defer server.Close()
			p, err := New(Config{BaseURL: server.URL, APIKey: "secret", HTTPClient: server.Client(), DefaultModel: "m"})
			if err != nil {
				t.Fatal(err)
			}
			stream, err := p.Chat(context.Background(), auth.Credential{}, protocol.ChatRequest{Model: protocol.Model{ID: "m"}})
			if err != nil {
				t.Fatal(err)
			}
			event, err := stream.Next(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			advice, ok := providerpkg.RetryAdviceFor(event.Err)
			wantKind := providerpkg.RetryTransient
			if status == http.StatusTooManyRequests {
				wantKind = providerpkg.RetryRateLimit
			}
			if !ok || advice.Kind != wantKind || advice.RetryAfter != 4*time.Second {
				t.Fatalf("event=%+v advice=%+v ok=%v", event, advice, ok)
			}
		})
	}
}

func TestChatKeepsPaymentLimitTerminal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "quota", http.StatusPaymentRequired)
	}))
	defer server.Close()
	p, _ := New(Config{BaseURL: server.URL, APIKey: "secret", HTTPClient: server.Client(), DefaultModel: "m"})
	stream, _ := p.Chat(context.Background(), auth.Credential{}, protocol.ChatRequest{Model: protocol.Model{ID: "m"}})
	event, _ := stream.Next(context.Background())
	var limited providerpkg.UsageLimitedError
	if !errors.As(event.Err, &limited) {
		t.Fatalf("event=%+v", event)
	}
	if _, ok := providerpkg.RetryAdviceFor(event.Err); ok {
		t.Fatal("payment limit was retryable")
	}
}
