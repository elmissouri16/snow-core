package provider

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"slices"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// Reuse the OAuth validation/refresh fixture, with ChatGPT's named-account scope.
type accountRuntimeDriver struct{ refreshDriver }

func (*accountRuntimeDriver) Descriptor() auth.Descriptor {
	return auth.Descriptor{ProviderID: "chatgpt", Required: true, Kinds: []auth.CredentialType{auth.CredentialOAuth}}
}
func (*accountRuntimeDriver) Inspect(auth.Credential) (auth.Status, error) {
	return auth.Status{ProviderID: "chatgpt", State: auth.StateConfigured}, nil
}

type accountRuntimeTransport struct {
	catalogs            map[string][]protocol.Model
	catalogCredentials  []auth.Credential
	requests            []protocol.ChatRequest
	credentials         []auth.Credential
	catalogErr, chatErr error
	stream              protocol.EventStream
}

func (*accountRuntimeTransport) ID() string { return "chatgpt" }
func (*accountRuntimeTransport) ListModels(context.Context) ([]protocol.Model, error) {
	return nil, errors.New("credential-free catalog must not be used")
}
func (p *accountRuntimeTransport) ListModelsWithCredential(_ context.Context, credential auth.Credential) ([]protocol.Model, error) {
	p.catalogCredentials = append(p.catalogCredentials, credential)
	return slices.Clone(p.catalogs[credential.AccountID]), p.catalogErr
}
func (p *accountRuntimeTransport) Chat(_ context.Context, credential auth.Credential, request protocol.ChatRequest) (protocol.EventStream, error) {
	p.credentials = append(p.credentials, credential)
	p.requests = append(p.requests, request)
	if p.chatErr != nil {
		return nil, p.chatErr
	}
	if p.stream != nil {
		return p.stream, nil
	}
	return registryStream{}, nil
}

func newAccountTestRuntime(t *testing.T) (*Authenticated, *auth.MemoryStore, *accountRuntimeTransport) {
	t.Helper()
	store := auth.NewMemoryStore()
	for _, account := range []string{"default", "work"} {
		key, err := auth.AccountKey("chatgpt", account)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Put(key, auth.Credential{Type: auth.CredentialOAuth, Access: "token-" + account, AccountID: account}); err != nil {
			t.Fatal(err)
		}
	}
	service := auth.NewService(store)
	if err := service.Register(&accountRuntimeDriver{}); err != nil {
		t.Fatal(err)
	}
	transport := &accountRuntimeTransport{catalogs: map[string][]protocol.Model{
		"default": {{Provider: "chatgpt", ID: "default-model"}},
		"work":    {{Provider: "chatgpt", ID: "work-model"}},
	}}
	runtime, err := NewAuthenticated(transport, service)
	if err != nil {
		t.Fatal(err)
	}
	return runtime, store, transport
}

func TestAccountRejectsWithdrawnExplicitResponseSettings(t *testing.T) {
	for _, request := range []protocol.ChatRequest{
		{ReasoningSummary: protocol.ReasoningSummaryDetailed},
		{TextVerbosity: protocol.TextVerbosityHigh},
	} {
		runtime, _, transport := newAccountTestRuntime(t)
		if err := runtime.SetAccount("work"); err != nil {
			t.Fatal(err)
		}
		transport.catalogs["work"][0].SupportsReasoningSummary = new(false)
		request.Model = protocol.Model{ID: "work-model", SupportsVerbosity: true, SupportsReasoningSummary: new(true)}
		if _, err := runtime.Chat(t.Context(), request); err == nil {
			t.Fatal("withdrawn explicit response setting was accepted")
		}
		if len(transport.requests) != 0 {
			t.Fatal("incompatible settings reached the transport")
		}
	}
}

func TestAccountForSessionSnapshotsSelectionCatalogAndGuard(t *testing.T) {
	parent, _, transport := newAccountTestRuntime(t)
	if err := parent.SetAccount("work"); err != nil {
		t.Fatal(err)
	}
	models, err := parent.ListModels(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var parentGuards, childGuards int
	parent.SetRequestGuard(func(context.Context, auth.Credential, protocol.ChatRequest) error { parentGuards++; return nil })
	child := parent.ForSession(func(_ context.Context, credential auth.Credential, _ protocol.ChatRequest) error {
		childGuards++
		if credential.Account != "work" {
			t.Fatalf("child resolved %q", credential.Account)
		}
		return nil
	})
	models[0].ID = "caller-mutated"
	if err := parent.SetAccount("default"); err != nil {
		t.Fatal(err)
	}
	if _, err := parent.ListModels(t.Context()); err != nil {
		t.Fatal(err)
	}
	if child.Account() != "work" || parent.Account() != "default" {
		t.Fatal("parent selection changed child snapshot")
	}
	for _, tc := range []struct {
		runtime        *Authenticated
		model, account string
	}{
		{child, "work-model", "work"}, {parent, "default-model", "default"},
	} {
		stream, err := tc.runtime.Chat(t.Context(), protocol.ChatRequest{Model: protocol.Model{ID: tc.model}})
		if err != nil {
			t.Fatal(err)
		}
		_ = stream.Close()
		if got := transport.credentials[len(transport.credentials)-1].Account; got != tc.account {
			t.Fatalf("request account=%q want=%q", got, tc.account)
		}
	}
	if parentGuards != 1 || childGuards != 1 || len(transport.catalogCredentials) != 2 {
		t.Fatalf("guards parent=%d child=%d catalogs=%d", parentGuards, childGuards, len(transport.catalogCredentials))
	}
	if err := child.SetAccount("default"); err != nil {
		t.Fatal(err)
	}
	if err := parent.SetAccount("work"); err != nil {
		t.Fatal(err)
	}
	if child.Account() != "default" {
		t.Fatal("child selection still coupled to parent")
	}
}

func TestAccountCatalogDoesNotSubstituteModelsAndRechecksReplacement(t *testing.T) {
	runtime, store, transport := newAccountTestRuntime(t)
	if err := runtime.SetAccount("work"); err != nil {
		t.Fatal(err)
	}
	request := protocol.ChatRequest{Model: protocol.Model{ID: "default-model"}}
	if _, err := runtime.Chat(t.Context(), request); err == nil {
		t.Fatal("unavailable model was accepted")
	}
	if len(transport.requests) != 0 || request.Model.ID != "default-model" {
		t.Fatal("catalog check substituted a model or dispatched a request")
	}
	request.Model.ID = "work-model"
	stream, err := runtime.Chat(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	_ = stream.Close()
	if len(transport.catalogCredentials) != 1 || transport.requests[0].Model.ID != "work-model" {
		t.Fatal("matching catalog was not reused or request model changed")
	}
	transport.catalogs["replacement"] = []protocol.Model{{Provider: "chatgpt", ID: "replacement-model"}}
	if err := store.Put("chatgpt/account/work", auth.Credential{Type: auth.CredentialOAuth, Access: "new-token", AccountID: "replacement"}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Chat(t.Context(), request); err == nil {
		t.Fatal("alias replacement reused stale model entitlement")
	}
	if len(transport.requests) != 1 || len(transport.catalogCredentials) != 2 || transport.catalogCredentials[1].AccountID != "replacement" {
		t.Fatal("replacement identity did not get its own catalog check")
	}
}

func TestAccountGuardAndCatalogErrorsPreventDispatch(t *testing.T) {
	for _, guardFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "catalog error", true: "guard error"}[guardFailure], func(t *testing.T) {
			runtime, _, transport := newAccountTestRuntime(t)
			failure := errors.New("unavailable")
			if err := runtime.SetAccount("work"); err != nil {
				t.Fatal(err)
			}
			if guardFailure {
				runtime.SetRequestGuard(func(context.Context, auth.Credential, protocol.ChatRequest) error { return failure })
			} else {
				transport.catalogErr = failure
			}
			if _, err := runtime.Chat(t.Context(), protocol.ChatRequest{Model: protocol.Model{ID: "work-model"}}); !errors.Is(err, failure) {
				t.Fatalf("request error=%v", err)
			}
			if len(transport.requests) != 0 || (guardFailure && len(transport.catalogCredentials) != 0) {
				t.Fatal("failed validation reached transport")
			}
		})
	}
}

type accountLimitTestStream struct {
	events []protocol.StreamEvent
	err    error
}

func (s *accountLimitTestStream) Next(context.Context) (protocol.StreamEvent, error) {
	if len(s.events) != 0 {
		event := s.events[0]
		s.events = s.events[1:]
		return event, nil
	}
	if s.err != nil {
		return protocol.StreamEvent{}, s.err
	}
	return protocol.StreamEvent{}, io.EOF
}
func (*accountLimitTestStream) Close() error { return nil }

func TestAccountQuotaUsesRequestSnapshotAndNeverRotatesAfterOutput(t *testing.T) {
	for _, output := range []protocol.StreamEventType{protocol.EvStreamTextDelta, protocol.EvStreamToolCallDone, protocol.EvStreamProviderData} {
		for _, errorEvent := range []bool{false, true} {
			t.Run(string(output)+map[bool]string{false: "/next-error", true: "/error-event"}[errorEvent], func(t *testing.T) {
				runtime, store, transport := newAccountTestRuntime(t)
				if err := runtime.SetAccount("work"); err != nil {
					t.Fatal(err)
				}
				credential, _ := store.Get("chatgpt/account/work")
				resetsAt := time.Now().Add(time.Hour)
				limit := &LimitError{Provider: "chatgpt", Status: 429, Kind: "usage_limit", ResetsAt: resetsAt}
				source := &accountLimitTestStream{events: []protocol.StreamEvent{{Type: output, Text: "already emitted"}}}
				if errorEvent {
					source.events = append(source.events, protocol.StreamEvent{Type: protocol.EvStreamError, Err: limit})
				} else {
					source.err = limit
				}
				transport.stream = source
				stream, err := runtime.Chat(t.Context(), protocol.ChatRequest{Model: protocol.Model{ID: "work-model"}})
				if err != nil {
					t.Fatal(err)
				}
				defer stream.Close()
				if event, err := stream.Next(t.Context()); err != nil || event.Type != output {
					t.Fatalf("first event=%+v err=%v", event, err)
				}
				// A later selection must not relabel an already-open request's quota.
				if err := runtime.SetAccount("default"); err != nil {
					t.Fatal(err)
				}
				replacement := credential
				replacement.AccountID, replacement.Access = "replacement", "new-token"
				if err := store.Put("chatgpt/account/work", replacement); err != nil {
					t.Fatal(err)
				}
				event, err := stream.Next(t.Context())
				if errorEvent {
					if event.Type != protocol.EvStreamError || !errors.Is(event.Err, limit) {
						t.Fatalf("terminal event=%+v err=%v", event, err)
					}
				} else if !errors.Is(err, limit) {
					t.Fatalf("terminal error=%v", err)
				}
				kind, reset := runtime.AccountQuota("work", auth.AccountIdentity(credential))
				if kind != "usage_limit" || !reset.Equal(resetsAt) {
					t.Fatalf("snapshot quota kind=%q reset=%v", kind, reset)
				}
				other, _ := store.Get("chatgpt")
				if kind, _ := runtime.AccountQuota("default", auth.AccountIdentity(other)); kind != "" {
					t.Fatal("quota attributed to later selection")
				}
				if kind, _ := runtime.AccountQuota("work", auth.AccountIdentity(replacement)); kind != "" {
					t.Fatal("quota carried across replacement identity")
				}
				if runtime.Account() != "default" || len(transport.requests) != 1 {
					t.Fatal("quota changed selection or automatically retried after output")
				}
			})
		}
	}
}

func TestAccountQuotaAtChatOpenDoesNotRotateOrInferBalance(t *testing.T) {
	runtime, store, transport := newAccountTestRuntime(t)
	if err := runtime.SetAccount("work"); err != nil {
		t.Fatal(err)
	}
	transport.chatErr = &LimitError{Provider: "chatgpt", Status: 429, Kind: "usage_limit"}
	if _, err := runtime.Chat(t.Context(), protocol.ChatRequest{Model: protocol.Model{ID: "work-model"}}); err == nil {
		t.Fatal("quota error missing")
	}
	credential, _ := store.Get("chatgpt/account/work")
	if kind, _ := runtime.AccountQuota("work", auth.AccountIdentity(credential)); kind != "usage_limit" {
		t.Fatalf("quota kind=%q", kind)
	}
	if runtime.Account() != "work" || len(transport.requests) != 1 {
		t.Fatal("quota triggered automatic rotation")
	}
	runtime.observeAccountLimit(auth.Credential{Account: "default", AccountID: "default"}, context.Canceled)
	if kind, _ := runtime.AccountQuota("default", auth.AccountIdentity(auth.Credential{AccountID: "default"})); kind != "" {
		t.Fatal("cancellation inferred quota")
	}
	credential.Account = "work" // Raw store entries are not service-bound request credentials.
	runtime.observeAccountLimit(credential, &LimitError{Kind: "usage_limit", ResetsAt: time.Now().Add(-time.Second)})
	if kind, _ := runtime.AccountQuota("work", auth.AccountIdentity(credential)); kind != "" {
		t.Fatal("expired observation still visible")
	}
}

func TestAccountQuotaAfterStreamOutputDoesNotRotate(t *testing.T) {
	for _, output := range []protocol.StreamEventType{protocol.EvStreamTextDelta, protocol.EvStreamToolCallDone, protocol.EvStreamProviderData} {
		t.Run(string(output), func(t *testing.T) {
			runtime, _, transport := newAccountTestRuntime(t)
			if err := runtime.SetAccount("work"); err != nil {
				t.Fatal(err)
			}
			limit := &LimitError{Provider: "chatgpt", Status: 429, Kind: "usage_limit"}
			transport.stream = &accountLimitTestStream{events: []protocol.StreamEvent{{Type: output}, {Type: protocol.EvStreamError, Err: limit}}}
			stream, err := runtime.Chat(t.Context(), protocol.ChatRequest{Model: protocol.Model{ID: "work-model"}})
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			if _, err := stream.Next(t.Context()); err != nil {
				t.Fatal(err)
			}
			event, err := stream.Next(t.Context())
			if err != nil || !errors.Is(event.Err, limit) {
				t.Fatalf("quota event=%+v err=%v", event, err)
			}
			if runtime.Account() != "work" || len(transport.requests) != 1 {
				t.Fatal("quota rotated accounts or retried after output")
			}
		})
	}
}

func TestAccountContextWindowChangeRequiresBudgetReadmission(t *testing.T) {
	runtime, _, transport := newAccountTestRuntime(t)
	if err := runtime.SetAccount("work"); err != nil {
		t.Fatal(err)
	}
	transport.catalogs["work"][0].ContextWindow = 8000
	request := protocol.ChatRequest{Model: protocol.Model{ID: "work-model", ContextWindow: 128000}}
	if _, err := runtime.Chat(t.Context(), request); err == nil {
		t.Fatal("request admitted against previous account's context window reached dispatch")
	}
	if len(transport.requests) != 0 {
		t.Fatal("stale context budget reached transport")
	}
	request.Model.ContextWindow = 8000
	stream, err := runtime.Chat(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	_ = stream.Close()
}

func TestAccountCatalogRefreshesSameModelCapabilitiesAndRejectsEffort(t *testing.T) {
	runtime, _, transport := newAccountTestRuntime(t)
	if err := runtime.SetAccount("work"); err != nil {
		t.Fatal(err)
	}
	available := protocol.Model{Provider: "chatgpt", ID: "work-model", ContextWindow: 1200, SupportsThinking: true, ThinkingLevels: []protocol.ThinkingLevel{protocol.ThinkingLow}, SupportsReasoningSummary: new(false)}
	transport.catalogs["work"] = []protocol.Model{available}
	request := protocol.ChatRequest{Model: protocol.Model{Provider: "chatgpt", ID: "work-model", ContextWindow: 1200, SupportsThinking: true, SupportsTools: true, SupportsVision: true, ThinkingLevels: []protocol.ThinkingLevel{protocol.ThinkingHigh}}, Thinking: protocol.ThinkingHigh}
	if _, err := runtime.Chat(t.Context(), request); err == nil {
		t.Fatal("unsupported explicit reasoning effort was silently repaired")
	}
	if len(transport.requests) != 0 {
		t.Fatal("unsupported effort dispatched a request")
	}
	request.Thinking = protocol.ThinkingLow
	stream, err := runtime.Chat(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	sent := transport.requests[0]
	if sent.Model.ID != request.Model.ID || sent.Thinking != protocol.ThinkingLow || sent.Model.ContextWindow != available.ContextWindow || sent.Model.SupportsTools || sent.Model.SupportsVision || !slices.Equal(sent.Model.ThinkingLevels, available.ThinkingLevels) || sent.Model.SupportsReasoningSummary == nil || *sent.Model.SupportsReasoningSummary {
		t.Fatalf("catalog capabilities not used for same-ID model: %+v", sent)
	}
	if request.Model.ContextWindow != 1200 || !request.Model.SupportsTools {
		t.Fatal("caller-owned model metadata mutated")
	}
}

func TestAccountCatalogSeparatesUsersInSameWorkspace(t *testing.T) {
	runtime, store, transport := newAccountTestRuntime(t)
	if err := runtime.SetAccount("work"); err != nil {
		t.Fatal(err)
	}
	first := auth.Credential{Type: auth.CredentialOAuth, AccountID: "work", Access: "header." + base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"first-user","chatgpt_account_id":"work"}`)) + ".signature"}
	if err := store.Put("chatgpt/account/work", first); err != nil {
		t.Fatal(err)
	}
	request := protocol.ChatRequest{Model: protocol.Model{ID: "work-model"}}
	stream, err := runtime.Chat(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	_ = stream.Close()
	second := first
	second.Access = "header." + base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"second-user","chatgpt_account_id":"work"}`)) + ".signature"
	if err := store.Put("chatgpt/account/work", second); err != nil {
		t.Fatal(err)
	}
	transport.catalogs["work"] = []protocol.Model{{Provider: "chatgpt", ID: "second-user-model"}}
	if _, err := runtime.Chat(t.Context(), request); err == nil {
		t.Fatal("different user in the same workspace reused old model entitlement")
	}
	if len(transport.catalogCredentials) != 2 || len(transport.requests) != 1 {
		t.Fatal("user identity change did not invalidate account catalog")
	}
}
