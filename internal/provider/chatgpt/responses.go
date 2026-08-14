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
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/klauspost/compress/zstd"
	"github.com/snow-core/snow/internal/auth"
	providerpkg "github.com/snow-core/snow/internal/provider"
	"github.com/snow-core/snow/internal/provider/responsesapi"
	"github.com/snow-core/snow/pkg/protocol"
)

const (
	maxTransientRetries      = 2
	initialRetryDelay        = time.Second
	maxRetryDelay            = 30 * time.Second
	requestCompressMinimum   = 32 << 10
	maxHTTPErrorReadBytes    = 64 << 10
	maxHTTPErrorSnippetBytes = 1000
)

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

	mu               sync.Mutex
	current          protocol.EventStream
	pending          chan codexAttemptResult
	closed           bool
	terminal         bool
	delivered        bool
	authRetried      bool
	transientRetries int
	requestAttempts  int
}

func (s *retryingCodexStream) Next(ctx context.Context) (protocol.StreamEvent, error) {
	for {
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
			stream = result.stream
			err, delay := result.err, result.delay
			if err != nil {
				if s.canRetry(err) {
					s.transientRetries++
					if delay < 0 {
						delay = retryBackoff(s.transientRetries)
					}
					if waitErr := s.wait(ctx, delay); waitErr != nil {
						return protocol.StreamEvent{}, waitErr
					}
					continue
				}
				s.markTerminal()
				return protocol.StreamEvent{Type: protocol.EvStreamError, Err: withAttemptCount(err, s.requestAttempts)}, nil
			}
			s.setActiveStream(stream)
		}

		event, err := stream.Next(ctx)
		if err != nil {
			if ctx.Err() != nil && s.ctx.Err() == nil {
				return protocol.StreamEvent{}, ctx.Err()
			}
			if errors.Is(err, io.EOF) {
				s.markTerminal()
				return protocol.StreamEvent{}, io.EOF
			}
			if !s.delivered && s.canRetry(err) {
				s.transientRetries++
				s.clearActiveStream()
				if waitErr := s.wait(ctx, retryBackoff(s.transientRetries)); waitErr != nil {
					return protocol.StreamEvent{}, waitErr
				}
				continue
			}
			s.markTerminal()
			return protocol.StreamEvent{}, withAttemptCount(err, s.requestAttempts)
		}
		if event.Type == protocol.EvStreamError {
			if !s.delivered && s.canRetry(event.Err) {
				s.transientRetries++
				s.clearActiveStream()
				if waitErr := s.wait(ctx, retryBackoff(s.transientRetries)); waitErr != nil {
					return protocol.StreamEvent{}, waitErr
				}
				continue
			}
			s.markTerminal()
			event.Err = withAttemptCount(event.Err, s.requestAttempts)
			return event, nil
		}
		s.delivered = true
		if event.Type == protocol.EvStreamDone {
			s.markTerminal()
		}
		return event, nil
	}
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
	delay  time.Duration
}

func (s *retryingCodexStream) awaitAttempt(ctx context.Context) (codexAttemptResult, error) {
	s.mu.Lock()
	pending := s.pending
	if pending == nil && !s.closed {
		pending = make(chan codexAttemptResult, 1)
		s.pending = pending
		go func(ch chan codexAttemptResult) {
			stream, err, delay := s.startAttempt()
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed && stream != nil {
				_ = stream.Close()
				stream = nil
			}
			ch <- codexAttemptResult{stream: stream, err: err, delay: delay}
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

func (s *retryingCodexStream) clearActiveStream() {
	s.mu.Lock()
	stream := s.current
	s.current = nil
	s.mu.Unlock()
	if stream != nil {
		_ = stream.Close()
	}
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

func (s *retryingCodexStream) canRetry(err error) bool {
	return !s.delivered && s.transientRetries < maxTransientRetries && s.requestAttempts < maxTransientRetries+1 && retryableCodexError(err)
}

func (s *retryingCodexStream) wait(ctx context.Context, delay time.Duration) error {
	if delay > maxRetryDelay {
		delay = maxRetryDelay
	}
	if delay < 0 {
		delay = 0
	}
	if s.provider.wait != nil {
		return s.provider.wait(s.ctx, ctx, delay)
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}

func (s *retryingCodexStream) startAttempt() (protocol.EventStream, error, time.Duration) {
	for {
		s.requestAttempts++
		req, err := http.NewRequestWithContext(s.ctx, http.MethodPost, s.provider.endpoint(), bytes.NewReader(s.body))
		if err != nil {
			return nil, responsesapi.NewResponseError(ProviderID, 0, "create request failed", "request_build_error", ""), -1
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
			return nil, responsesapi.NewResponseError(ProviderID, 0, "network request failed", "network_error", ""), -1
		}
		if resp.StatusCode == http.StatusUnauthorized && !s.authRetried && s.requestAttempts < maxTransientRetries+1 {
			resp.Body.Close()
			s.authRetried = true
			fresh, refreshErr := s.provider.resolve(s.ctx, s.creds, true)
			if refreshErr != nil {
				return nil, refreshErr, -1
			}
			s.creds = fresh
			s.status, refreshErr = CheckAuth(fresh)
			if refreshErr != nil {
				return nil, refreshErr, -1
			}
			continue
		}
		if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
			return responsesapi.NewStreamWithIdleTimeout(s.ctx, resp, ProviderID, s.provider.streamIdleTimeout, s.creds.Access, s.creds.Refresh), nil, 0
		}

		now := time.Now()
		if s.provider.now != nil {
			now = s.provider.now()
		}
		delay := responseRetryDelay(resp, now)
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxHTTPErrorReadBytes))
		resp.Body.Close()
		requestID := responseRequestID(resp.Header, s.creds.Access, s.creds.Refresh)
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusPaymentRequired {
			message := responsesapi.SanitizeErrorText(string(snippet), maxHTTPErrorSnippetBytes, s.creds.Access, s.creds.Refresh)
			if requestID != "" {
				message += " (request ID " + requestID + ")"
			}
			return nil, &providerpkg.LimitError{Provider: ProviderID, Status: resp.StatusCode, Message: message}, -1
		}
		return nil, responseError(resp.StatusCode, snippet, requestID, s.creds.Access, s.creds.Refresh), delay
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
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest), zstd.WithEncoderConcurrency(1))
	if err != nil {
		return append([]byte(nil), body...), ""
	}
	defer encoder.Close()
	return encoder.EncodeAll(body, make([]byte, 0, len(body)/2)), "zstd"
}

func retryBackoff(retry int) time.Duration {
	if retry <= 1 {
		return initialRetryDelay
	}
	delay := initialRetryDelay << (retry - 1)
	if delay > maxRetryDelay {
		return maxRetryDelay
	}
	return delay
}

func responseRetryDelay(resp *http.Response, now time.Time) time.Duration {
	if value := strings.TrimSpace(resp.Header.Get("retry-after-ms")); value != "" {
		if ms, err := strconv.ParseInt(value, 10, 64); err == nil && ms >= 0 {
			if ms >= int64(maxRetryDelay/time.Millisecond) {
				return maxRetryDelay
			}
			return time.Duration(ms) * time.Millisecond
		}
	}
	value := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if value == "" {
		return -1
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds >= 0 {
		if seconds >= int64(maxRetryDelay/time.Second) {
			return maxRetryDelay
		}
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(value); err == nil {
		return min(max(time.Duration(0), when.Sub(now)), maxRetryDelay)
	}
	return -1
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

func retryableCodexError(err error) bool {
	if err == nil {
		return false
	}
	var responseErr *responsesapi.ResponseError
	if !errors.As(err, &responseErr) {
		return false
	}
	switch responseErr.Status {
	case http.StatusRequestTimeout, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	code := strings.ToLower(responseErr.Code)
	if code != "" {
		return code == "network_error" || code == "stream_truncated" || code == "stream_idle" || strings.Contains(code, "overload") || strings.Contains(code, "service_unavailable") || strings.Contains(code, "upstream") || strings.Contains(code, "timeout")
	}
	message := strings.ToLower(responseErr.Message)
	return strings.Contains(message, "overload") || strings.Contains(message, "service unavailable") || strings.Contains(message, "upstream connect") || strings.Contains(message, "temporarily unavailable")
}

func withAttemptCount(err error, attempts int) error {
	if err == nil || attempts <= 1 {
		return err
	}
	var responseErr *responsesapi.ResponseError
	if errors.As(err, &responseErr) {
		copy := *responseErr
		copy.Attempts = attempts
		return &copy
	}
	return fmt.Errorf("%w (after %d attempts)", err, attempts)
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
