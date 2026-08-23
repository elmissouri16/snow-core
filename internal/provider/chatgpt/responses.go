package chatgpt

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/elmissouri16/snow-core/internal/auth"
	providerpkg "github.com/elmissouri16/snow-core/internal/provider"
	"github.com/elmissouri16/snow-core/internal/provider/responsesapi"
	"github.com/elmissouri16/snow-core/pkg/protocol"
	"github.com/klauspost/compress/zstd"
)

const (
	requestCompressMinimum   = 32 << 10
	maxHTTPErrorReadBytes    = 64 << 10
	maxHTTPErrorSnippetBytes = 1000
)

var requestEncoderPool sync.Pool

// Chat implements the Codex Responses streaming protocol used by ChatGPT
// subscription credentials. The access token is only placed in the request
// header and is never included in errors or stream events.
func (p *Provider) Chat(ctx context.Context, creds auth.Credential, req protocol.ChatRequest) (protocol.EventStream, error) {
	status, err := CheckAuth(creds)
	if err != nil {
		return errorStream(ctx, err), nil
	}
	if status.Expired {
		return errorStream(ctx, errors.New("chatgpt: OAuth access token expired")), nil
	}
	if status.AccountID == "" {
		return errorStream(ctx, errors.New("chatgpt: OAuth token has no ChatGPT account ID")), nil
	}

	affinity := normalizeAffinityKey(req.SessionAffinityKey)
	parallel := true
	body, err := responsesapi.BuildRequest(req, responsesapi.RequestOptions{
		ProviderID:                ProviderID,
		IncludeEncryptedReasoning: true,
		AllowLegacyVerbosity:      req.Model.Provider != ProviderID,
		PromptCacheKey:            affinity,
		ToolChoice:                "auto",
		ParallelToolCalls:         &parallel,
		OmitMaxOutputTokens:       true,
		OmitTemperature:           true,
	})
	if err != nil {
		return errorStream(ctx, fmt.Errorf("chatgpt: build request: %w", err)), nil
	}
	encoded, contentEncoding := compressRequestBody(body)
	streamCtx, cancel := context.WithCancel(ctx)
	return &retryingCodexStream{
		ctx: streamCtx, cancel: cancel, provider: p, creds: creds, status: status,
		body: encoded, contentEncoding: contentEncoding, affinity: affinity,
	}, nil
}

type retryingCodexStream struct {
	ctx             context.Context
	cancel          context.CancelFunc
	provider        *Provider
	creds           auth.Credential
	status          AuthStatus
	body            []byte
	contentEncoding string
	affinity        string

	mu              sync.Mutex
	current         protocol.EventStream
	pending         chan codexAttemptResult
	closed          bool
	terminal        bool
	authRetried     bool
	requestAttempts int
}

func (s *retryingCodexStream) Next(ctx context.Context) (protocol.StreamEvent, error) {
	if err := ctx.Err(); err != nil {
		return protocol.StreamEvent{}, err
	}
	if err := s.ctx.Err(); err != nil {
		return protocol.StreamEvent{}, err
	}
	if s.isTerminal() {
		return protocol.StreamEvent{}, io.EOF
	}
	stream := s.activeStream()
	if stream == nil {
		result, waitErr := s.awaitAttempt(ctx)
		if waitErr != nil {
			return protocol.StreamEvent{}, waitErr
		}
		if result.err != nil {
			s.markTerminal()
			return protocol.StreamEvent{Type: protocol.EvStreamError, Err: result.err}, nil
		}
		stream = result.stream
		s.setActiveStream(stream)
	}

	event, err := stream.Next(ctx)
	if err != nil {
		if ctx.Err() != nil && s.ctx.Err() == nil {
			return protocol.StreamEvent{}, ctx.Err()
		}
		s.markTerminal()
		return protocol.StreamEvent{}, err
	}
	if event.Type == protocol.EvStreamError || event.Type == protocol.EvStreamDone {
		s.markTerminal()
	}
	return event, nil
}

func (s *retryingCodexStream) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	stream := s.current
	s.current = nil
	s.mu.Unlock()
	s.cancel()
	if stream != nil {
		return stream.Close()
	}
	return nil
}

type codexAttemptResult struct {
	stream protocol.EventStream
	err    error
}

func (s *retryingCodexStream) awaitAttempt(ctx context.Context) (codexAttemptResult, error) {
	s.mu.Lock()
	pending := s.pending
	if pending == nil && !s.closed {
		pending = make(chan codexAttemptResult, 1)
		s.pending = pending
		go func(ch chan codexAttemptResult) {
			stream, err := s.startAttempt()
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed && stream != nil {
				_ = stream.Close()
				stream = nil
			}
			ch <- codexAttemptResult{stream: stream, err: err}
		}(pending)
	}
	s.mu.Unlock()
	if pending == nil {
		return codexAttemptResult{}, io.EOF
	}
	select {
	case result := <-pending:
		s.mu.Lock()
		if s.pending == pending {
			s.pending = nil
		}
		s.mu.Unlock()
		return result, nil
	case <-ctx.Done():
		return codexAttemptResult{}, ctx.Err()
	case <-s.ctx.Done():
		return codexAttemptResult{}, s.ctx.Err()
	}
}

func (s *retryingCodexStream) activeStream() protocol.EventStream {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	return s.current
}

func (s *retryingCodexStream) setActiveStream(stream protocol.EventStream) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = stream.Close()
		return
	}
	s.current = stream
	s.mu.Unlock()
}

func (s *retryingCodexStream) isTerminal() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed || s.terminal
}

func (s *retryingCodexStream) markTerminal() {
	s.mu.Lock()
	s.terminal = true
	s.mu.Unlock()
}

func (s *retryingCodexStream) startAttempt() (protocol.EventStream, error) {
	for {
		s.requestAttempts++
		req, err := http.NewRequestWithContext(s.ctx, http.MethodPost, s.provider.endpoint(), bytes.NewReader(s.body))
		if err != nil {
			return nil, responsesapi.NewResponseError(ProviderID, 0, "create request failed", "request_build_error", "")
		}
		req.Header.Set("Authorization", "Bearer "+s.creds.Access)
		req.Header.Set("chatgpt-account-id", s.status.AccountID)
		req.Header.Set("originator", "snow")
		req.Header.Set("User-Agent", "snow")
		req.Header.Set("OpenAI-Beta", "responses=experimental")
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("Content-Type", "application/json")
		if s.contentEncoding != "" {
			req.Header.Set("Content-Encoding", s.contentEncoding)
		}
		if s.affinity != "" {
			req.Header.Set("session-id", s.affinity)
			req.Header.Set("x-client-request-id", s.affinity)
		}

		resp, requestErr := redirectSafeClient(s.provider.client).Do(req)
		if requestErr != nil {
			message := providerpkg.RedactSecrets("chatgpt: network request failed: "+requestErr.Error(), s.creds.Access, s.creds.Refresh)
			return nil, &providerpkg.AdvisedError{Err: &providerpkg.CauseError{Message: message, Cause: requestErr}, Advice: providerpkg.RetryAdvice{Kind: providerpkg.RetryTransient}}
		}
		if resp.StatusCode == http.StatusUnauthorized && !s.authRetried {
			resp.Body.Close()
			s.authRetried = true
			fresh, refreshErr := s.provider.refreshRejected(s.ctx, s.creds)
			if refreshErr != nil {
				return nil, refreshErr
			}
			s.creds = fresh
			s.status, refreshErr = CheckAuth(fresh)
			if refreshErr != nil {
				return nil, refreshErr
			}
			continue
		}
		if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
			return responsesapi.NewStreamWithIdleTimeout(s.ctx, resp, ProviderID, s.provider.streamIdleTimeout, s.creds.Access, s.creds.Refresh), nil
		}

		now := time.Now()
		if s.provider.now != nil {
			now = s.provider.now()
		}
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxHTTPErrorReadBytes))
		resp.Body.Close()
		requestID := responseRequestID(resp.Header, s.creds.Access, s.creds.Refresh)
		message := responsesapi.SanitizeErrorText(string(snippet), maxHTTPErrorSnippetBytes, s.creds.Access, s.creds.Refresh)
		if requestID != "" {
			message += " (request ID " + requestID + ")"
		}
		if resp.StatusCode == http.StatusPaymentRequired {
			return nil, &providerpkg.LimitError{Provider: ProviderID, Status: resp.StatusCode, Message: message}
		}
		advice, retryable := providerpkg.HTTPRetryAdvice(resp.StatusCode, resp.Header, now, 24*time.Hour)
		if retryable && advice.Kind == providerpkg.RetryRateLimit {
			return nil, &providerpkg.RateLimitError{Provider: ProviderID, Status: resp.StatusCode, Message: message, RetryAfter: advice.RetryAfter}
		}
		responseErr := responseError(resp.StatusCode, snippet, requestID, s.creds.Access, s.creds.Refresh)
		if retryable {
			return nil, &providerpkg.AdvisedError{Err: responseErr, Advice: advice}
		}
		return nil, responseErr
	}
}

func (p *Provider) endpoint() string {
	base := strings.TrimRight(p.baseURL, "/")
	if base == "" {
		base = BackendBaseURL
	}
	if strings.HasSuffix(base, "/codex/responses") {
		return base
	}
	if strings.HasSuffix(base, "/codex") {
		return base + "/responses"
	}
	return base + "/codex/responses"
}

func responseError(status int, body []byte, diagnostics ...string) error {
	message := strings.TrimSpace(string(body))
	var payload struct {
		Error struct {
			Message   string `json:"message"`
			Code      string `json:"code"`
			RequestID string `json:"request_id"`
		} `json:"error"`
		RequestID string `json:"request_id"`
	}
	_ = json.Unmarshal(body, &payload)
	if payload.Error.Message != "" {
		message = payload.Error.Message
	}
	if message == "" {
		message = http.StatusText(status)
	}
	id := payload.Error.RequestID
	if id == "" {
		id = payload.RequestID
	}
	if id == "" && len(diagnostics) > 0 {
		id = diagnostics[0]
	}
	if status == http.StatusUnauthorized {
		message = "OAuth credential rejected"
	}
	var secrets []string
	if len(diagnostics) > 1 {
		secrets = diagnostics[1:]
	}
	return responsesapi.NewResponseError(ProviderID, status, message, payload.Error.Code, id, secrets...)
}

func normalizeAffinityKey(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) == 64 {
		if _, err := hex.DecodeString(value); err == nil && value == strings.ToLower(value) {
			return value
		}
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func compressRequestBody(body []byte) ([]byte, string) {
	if len(body) < requestCompressMinimum {
		return append([]byte(nil), body...), ""
	}
	encoder, _ := requestEncoderPool.Get().(*zstd.Encoder)
	if encoder == nil {
		var err error
		encoder, err = zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest), zstd.WithEncoderConcurrency(1))
		if err != nil {
			return append([]byte(nil), body...), ""
		}
	}
	encoded := encoder.EncodeAll(body, make([]byte, 0, len(body)/2))
	requestEncoderPool.Put(encoder)
	return encoded, "zstd"
}

func responseRequestID(header http.Header, secrets ...string) string {
	for _, name := range []string{"x-request-id", "request-id", "openai-request-id"} {
		if value := strings.TrimSpace(header.Get(name)); value != "" {
			value = providerpkg.RedactSecrets(value, secrets...)
			value = strings.Map(func(r rune) rune {
				if unicode.IsControl(r) {
					return -1
				}
				return r
			}, value)
			return truncate(value, 200)
		}
	}
	return ""
}

// buildResponsesBody remains as a thin compatibility seam for ChatGPT package
// regression tests. Runtime and tests use the shared Responses encoder.
func buildResponsesBody(req protocol.ChatRequest) ([]byte, error) {
	affinity := normalizeAffinityKey(req.SessionAffinityKey)
	parallel := true
	return responsesapi.BuildRequest(req, responsesapi.RequestOptions{
		ProviderID:                ProviderID,
		IncludeEncryptedReasoning: true,
		AllowLegacyVerbosity:      req.Model.Provider != ProviderID,
		PromptCacheKey:            affinity,
		ToolChoice:                "auto",
		ParallelToolCalls:         &parallel,
		OmitMaxOutputTokens:       true,
		OmitTemperature:           true,
	})
}

// responseInput and messageText are thin compatibility seams for package tests.
func responseInput(msg protocol.Message) ([]any, error) {
	if msg.Provider == "" {
		msg.Provider = ProviderID
	}
	return responsesapi.MessageInput(msg, ProviderID)
}

func messageText(msg protocol.Message) string { return responsesapi.MessageText(msg) }

func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max] + "…"
}

func errorStream(ctx context.Context, err error) protocol.EventStream {
	return responsesapi.ErrorStream(ctx, err)
}
