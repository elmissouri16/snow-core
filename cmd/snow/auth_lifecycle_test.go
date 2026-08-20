package main

import (
	"context"
	"testing"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/internal/config"
)

func TestCLIAuthServiceRegistersNamedCompatibleProfiles(t *testing.T) {
	store := auth.NewMemoryStore()
	if err := store.Put("x-provider", auth.Credential{Type: auth.CredentialAPIKey, Key: "secret"}); err != nil {
		t.Fatal(err)
	}
	service, _, err := newCLIAuthService(store, map[string]config.ProviderConfig{
		"x-provider": {Type: config.ProviderTypeOpenAICompatible, BaseURL: "https://example.invalid/v1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	status, err := service.Status(context.Background(), "x-provider")
	if err != nil || status.State != auth.StateConfigured {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	credential, err := service.Resolve(context.Background(), "x-provider")
	if err != nil || credential.Key != "secret" {
		t.Fatalf("credential=%+v err=%v", credential, err)
	}
}
