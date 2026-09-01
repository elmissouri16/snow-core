package rpc

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestAuthProvidersSerializesSequenceFieldsAsArrays(t *testing.T) {
	a, err := app.New(t.Context(), app.Options{Provider: "fake", NoSession: true, Permission: "deny", CWD: t.TempDir(), AuthPath: t.TempDir() + "/auth.json", NoPlugins: true, NoMCP: true, NoSkills: true})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	var out bytes.Buffer
	s := New(t.Context(), a, strings.NewReader(""), &out)
	if err := s.handle(t.Context(), Request{ID: "providers-1", Type: "auth_providers"}); err != nil {
		t.Fatal(err)
	}
	var response struct {
		Data struct {
			Providers []map[string]any `json:"providers"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Data.Providers) == 0 {
		t.Fatal("auth_providers returned no providers")
	}
	for _, provider := range response.Data.Providers {
		for _, field := range []string{"kinds", "environment", "methods"} {
			if _, ok := provider[field].([]any); !ok {
				t.Fatalf("provider %q %s wire value = %#v, want JSON array", provider["provider_id"], field, provider[field])
			}
		}
	}
}

func TestAuthRPCAPIKeyLifecycleNeverEchoesSecret(t *testing.T) {
	a, err := app.New(t.Context(), app.Options{Provider: "fake", NoSession: true, Permission: "deny", CWD: t.TempDir(), AuthPath: t.TempDir() + "/auth.json", NoPlugins: true, NoMCP: true, NoSkills: true})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := a.AuthService.Register(auth.NewAPIKeyDriver(auth.APIKeyOptions{ProviderID: "test-key", DisplayName: "Test key", Required: true})); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	s := New(t.Context(), a, strings.NewReader(""), &out)
	const secret = "sk-this-must-never-appear-in-rpc-output"
	if err := s.handle(t.Context(), Request{ID: "start-1", Type: "auth_login_start", Provider: "test-key", Method: "api_key", Secret: secret}); err != nil {
		t.Fatal(err)
	}

	job := waitAuthJob(t, s, "auth-1")
	if job.State != "completed" || job.Status == nil || job.Status.State != "configured" {
		t.Fatalf("job = %+v", job)
	}
	if strings.Contains(out.String(), secret) {
		t.Fatal("authentication secret was echoed in an RPC response")
	}
	credential, ok := a.Auth.Get("test-key")
	if !ok || credential.Key != secret {
		t.Fatal("API key was not committed through the auth store")
	}

	if err := s.handle(t.Context(), Request{ID: "providers-1", Type: "auth_providers"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), secret) || !strings.Contains(out.String(), `"provider_id":"test-key"`) {
		t.Fatalf("unsafe or incomplete inventory response: %s", out.String())
	}
	if err := s.handle(t.Context(), Request{ID: "logout-1", Type: "auth_logout", Provider: "test-key"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.Auth.Get("test-key"); ok {
		t.Fatal("logout retained stored credential")
	}
}

func TestAuthRPCCancelStopsInteractiveLoginAndRetainsSafeProgress(t *testing.T) {
	a, err := app.New(t.Context(), app.Options{Provider: "fake", NoSession: true, Permission: "deny", CWD: t.TempDir(), AuthPath: t.TempDir() + "/auth.json", NoPlugins: true, NoMCP: true, NoSkills: true})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := a.AuthService.Register(blockingAuthDriver{}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	s := New(t.Context(), a, strings.NewReader(""), &out)
	if err := s.handle(t.Context(), Request{ID: "start-1", Type: "auth_login_start", Provider: "blocking", Method: "browser"}); err != nil {
		t.Fatal(err)
	}
	job, err := s.authJob("auth-1")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for len(job.snapshot().Progress) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if err := s.handle(t.Context(), Request{ID: "cancel-1", Type: "auth_login_cancel", Params: []byte(`{"job_id":"auth-1"}`)}); err != nil {
		t.Fatal(err)
	}
	s.authWG.Wait()
	snapshot := job.snapshot()
	if snapshot.State != "canceled" || len(snapshot.Progress) == 0 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if snapshot.Progress[0].URL != "https://login.example.invalid/authorize?state=opaque" {
		t.Fatalf("progress = %+v", snapshot.Progress)
	}
}

func TestAuthRPCRejectsSecretOnNonLoginCommands(t *testing.T) {
	a, err := app.New(t.Context(), app.Options{Provider: "fake", NoSession: true, Permission: "deny", CWD: t.TempDir(), NoPlugins: true, NoMCP: true, NoSkills: true})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	s := New(t.Context(), a, strings.NewReader(""), &bytes.Buffer{})
	err = s.handleAuthCommand(t.Context(), Request{Type: "auth_logout", Provider: "fake", Secret: "must-not-be-accepted"})
	if err == nil || !strings.Contains(err.Error(), "secret is accepted only") {
		t.Fatalf("error = %v", err)
	}
}

func waitAuthJob(t *testing.T, s *Server, id string) protocol.RPCAuthLoginJob {
	t.Helper()
	job, err := s.authJob(id)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snapshot := job.snapshot()
		if snapshot.State != protocol.RPCAuthLoginRunning {
			return snapshot
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("authentication job did not finish")
	return protocol.RPCAuthLoginJob{}
}

type blockingAuthDriver struct{}

func (blockingAuthDriver) Descriptor() auth.Descriptor {
	return auth.Descriptor{ProviderID: "blocking", DisplayName: "Blocking", Required: true, Kinds: []auth.CredentialType{auth.CredentialOAuth}, Methods: []auth.LoginMethod{{ID: "browser", DisplayName: "Browser", Kind: auth.CredentialOAuth}}}
}
func (blockingAuthDriver) Inspect(auth.Credential) (auth.Status, error) {
	return auth.Status{ProviderID: "blocking", State: auth.StateConfigured}, nil
}
func (blockingAuthDriver) Login(ctx context.Context, _ auth.LoginRequest, interaction auth.Interaction) (auth.Credential, error) {
	if err := interaction.OpenURL(ctx, "https://login.example.invalid/authorize?state=opaque"); err != nil {
		return auth.Credential{}, err
	}
	interaction.Progress(auth.Progress{Kind: "device", Message: "Waiting", UserCode: "ABCD-EFGH"})
	<-ctx.Done()
	return auth.Credential{}, ctx.Err()
}
func (blockingAuthDriver) Validate(auth.Credential) error { return nil }
func (blockingAuthDriver) NeedsRefresh(auth.Credential, time.Time) bool {
	return false
}
func (blockingAuthDriver) Refresh(context.Context, auth.Credential, auth.RefreshReason) (auth.Credential, error) {
	return auth.Credential{}, errors.New("not refreshable")
}
