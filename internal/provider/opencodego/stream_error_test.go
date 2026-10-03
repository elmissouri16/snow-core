package opencodego

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/auth"
	providerpkg "github.com/elmissouri16/snow-core/internal/provider"
	"github.com/elmissouri16/snow-core/internal/provider/responsesapi"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestChatUnnamedErrorEnvelope(t *testing.T) {
	const secret = "active-test-credential"
	for _, partial := range []bool{false, true} {
		for _, code := range []string{"", "context_length_exceeded", "upstream_error"} {
			t.Run(fmt.Sprintf("partial=%t/code=%s", partial, code), func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "text/event-stream")
					if partial {
						_, _ = fmt.Fprint(w, sseChunk(chunkWith(map[string]any{"content": "partial"}, "", nil)))
					}
					// The error must win over any ordinary chunk fields and must
					// retain classification even with an oversized diagnostic.
					payload := chunkWith(map[string]any{"content": "ignored"}, "stop", nil)
					payload["error"] = map[string]any{
						"message": "failed " + secret + "\x1b[31m " + strings.Repeat("x", 1200),
						"code":    code,
					}
					_, _ = fmt.Fprint(w, sseChunk(payload))
					_, _ = fmt.Fprint(w, sseChunk(chunkWith(map[string]any{"content": "also ignored"}, "stop", nil)))
					_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
					w.(http.Flusher).Flush()
					// Error envelopes are terminal even if the server stays open.
					<-r.Context().Done()
				}))
				defer server.Close()

				p := mustNew(t, server.URL, secret)
				stream, err := p.Chat(t.Context(), auth.Credential{Key: secret}, protocol.ChatRequest{Model: protocol.Model{ID: "m"}})
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				events := drain(t, stream, ctx)
				wantEvents := 1
				if partial {
					wantEvents++
				}
				if len(events) != wantEvents || events[len(events)-1].Type != protocol.EvStreamError {
					t.Fatalf("events=%+v, want optional partial text followed by one terminal error", events)
				}
				if partial && (events[0].Type != protocol.EvStreamTextDelta || events[0].Text != "partial") {
					t.Fatalf("partial event=%+v", events[0])
				}
				streamErr := events[len(events)-1].Err
				if streamErr == nil {
					t.Fatal("missing error diagnostic")
				}
				message := streamErr.Error()
				if strings.Contains(message, secret) || strings.ContainsRune(message, '\x1b') || len(message) > 800 || !strings.Contains(message, "[redacted]") {
					t.Fatalf("unbounded or unsafe diagnostic: %q", message)
				}
				responseErr, ok := errors.AsType[*responsesapi.ResponseError](streamErr)
				if !ok || responseErr.Code != code {
					t.Fatalf("classification code lost: %v", streamErr)
				}
				if got := providerpkg.IsContextWindowExceeded(streamErr); got != (code == "context_length_exceeded") {
					t.Fatalf("context-window classification=%t for %v", got, streamErr)
				}
				_, retryable := providerpkg.RetryAdviceFor(streamErr)
				if retryable != (code == "upstream_error") {
					t.Fatalf("retryable=%t for %v", retryable, streamErr)
				}
			})
		}
	}
}

func TestChatUnnamedErrorEnvelopeShapes(t *testing.T) {
	for _, payload := range []string{
		`{"error":{"message":"failed"}}`,
		`{"error":"failed"}`,
		`{"error":{}}`,
		`{"choices":"invalid","error":{"message":"failed"}}`,
		`{"error":{"message":"failed"},"choices":"invalid"}`,
	} {
		t.Run(payload, func(t *testing.T) {
			s := newStream(t.Context(), 8, nil, ProviderID, "")
			go s.readSSE(&http.Response{Body: io.NopCloser(strings.NewReader("data: " + payload + "\n\ndata: [DONE]\n\n"))})
			events := drain(t, s, t.Context())
			if len(events) != 1 || events[0].Type != protocol.EvStreamError || events[0].Err == nil {
				t.Fatalf("events=%+v, want terminal error", events)
			}
		})
	}
}

func TestChatNullErrorIsNotFailure(t *testing.T) {
	payload := chunkWith(map[string]any{"content": "answer"}, "stop", nil)
	payload["error"] = nil
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	s := newStream(t.Context(), 8, nil, ProviderID, "")
	go s.readSSE(&http.Response{Body: io.NopCloser(strings.NewReader("data: " + string(data) + "\n\ndata: [DONE]\n\n"))})
	events := drain(t, s, t.Context())
	if len(events) != 2 || events[0].Type != protocol.EvStreamTextDelta || events[1].Type != protocol.EvStreamDone {
		t.Fatalf("events=%+v, want text followed by completion", events)
	}
}
