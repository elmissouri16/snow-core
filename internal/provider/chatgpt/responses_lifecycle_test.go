package chatgpt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// Exercise the actual pooled HTTP transport: completed turns can be closed and
// their caller contexts canceled both before and during the following request.
// Neither cleanup must cancel a different Chat stream on the same Provider.
func TestChatRepeatedTurnsIsolateCancellation(t *testing.T) {
	for _, http2 := range []bool{false, true} {
		for _, cleanup := range []string{"before_next", "awaiting_headers", "after_delta"} {
			t.Run(fmt.Sprintf("http2=%t/%s", http2, cleanup), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				const turns = 3
				started := make(chan int, turns)
				release := make([]chan struct{}, turns)
				for i := range turns {
					release[i] = make(chan struct{})
				}
				var requests atomic.Int32
				server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					i := int(requests.Add(1)) - 1
					if i >= turns {
						t.Error("unexpected extra request")
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
					if _, err := io.Copy(io.Discard, r.Body); err != nil {
						t.Error("read local mock request failed")
						return
					}
					started <- i
					select {
					case <-release[i]:
					case <-ctx.Done():
						return
					case <-r.Context().Done():
						return
					}
					body := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n"
					w.Header().Set("Content-Type", "text/event-stream")
					// A known length lets HTTP/1.1 return this connection to its
					// pool before the SSE reader closes the completed body.
					w.Header().Set("Content-Length", fmt.Sprint(len(body)))
					_, _ = io.WriteString(w, body)
				}))
				server.EnableHTTP2 = http2
				server.StartTLS()
				defer server.Close()
				// Release a blocked local handler before server.Close on failure.
				defer cancel()
				p := New(Config{BaseURL: server.URL, HTTPClient: server.Client()})
				creds := auth.Credential{Type: auth.CredentialOAuth, Access: "local-test", AccountID: "local-test"}
				req := protocol.ChatRequest{Model: protocol.Model{Provider: ProviderID, ID: "gpt-5.6-sol"}, SessionAffinityKey: "local-repeated-turns"}
				var previous protocol.EventStream
				var previousCancel context.CancelFunc
				cleanupPrevious := func() {
					if previous != nil {
						if err := previous.Close(); err != nil {
							t.Fatal("close completed stream failed")
						}
						previousCancel()
						previous = nil
					}
				}
				defer cleanupPrevious()
				for i := range turns {
					if cleanup == "before_next" {
						cleanupPrevious()
					}
					turnCtx, turnCancel := context.WithCancel(ctx)
					defer turnCancel()
					var reused atomic.Bool
					turnCtx = httptrace.WithClientTrace(turnCtx, &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) {
						reused.Store(info.Reused)
					}})
					stream, err := p.Chat(turnCtx, creds, req)
					if err != nil {
						t.Fatalf("turn %d Chat failed: %v", i, err)
					}
					defer stream.Close()
					type firstResult struct {
						event protocol.StreamEvent
						err   error
					}
					first := make(chan firstResult, 1)
					go func() {
						event, err := stream.Next(turnCtx)
						first <- firstResult{event: event, err: err}
					}()
					select {
					case got := <-started:
						if got != i {
							t.Fatalf("request index = %d, want %d", got, i)
						}
					case <-ctx.Done():
						t.Fatal("local request did not start")
					}
					if cleanup == "awaiting_headers" {
						cleanupPrevious()
					}
					close(release[i])
					select {
					case result := <-first:
						if result.err != nil || result.event.Err != nil || result.event.Type != protocol.EvStreamTextDelta || result.event.Text != "ok" {
							t.Fatalf("turn %d first event type=%s err=%v eventErr=%v", i, result.event.Type, result.err, result.event.Err)
						}
					case <-ctx.Done():
						t.Fatal("local stream did not produce first event")
					}
					if cleanup == "after_delta" {
						cleanupPrevious()
					}
					event, err := stream.Next(turnCtx)
					if err != nil || event.Type != protocol.EvStreamDone || event.StopReason != protocol.StopStop {
						t.Fatalf("turn %d terminal event type=%s stop=%s err=%v eventErr=%v", i, event.Type, event.StopReason, err, event.Err)
					}
					if _, err := stream.Next(turnCtx); !errors.Is(err, io.EOF) {
						t.Fatalf("turn %d final Next = %v, want EOF", i, err)
					}
					if turnCtx.Err() != nil {
						t.Fatalf("turn %d current context canceled", i)
					}
					if i > 0 && !reused.Load() {
						t.Fatalf("turn %d did not reuse the HTTP connection", i)
					}
					previous, previousCancel = stream, turnCancel
				}
				if got := requests.Load(); got != turns {
					t.Fatalf("requests = %d, want %d", got, turns)
				}
			})
		}
	}
}
