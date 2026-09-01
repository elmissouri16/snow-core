package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/config"
)

func TestConfigureOpenAICompatibleAuthSeparatesEndpointAndSecret(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	authPath := filepath.Join(home, "auth.json")
	a, err := New(t.Context(), Options{Provider: "fake", NoSession: true, Permission: "deny", CWD: home, ConfigPath: configPath, AuthPath: authPath, NoPlugins: true, NoMCP: true, NoSkills: true})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel() // Catalog refresh must remain network-free in this unit test.
	const secret = "profile-secret-must-not-enter-config"
	status, err := a.ConfigureOpenAICompatibleAuth(ctx, "x-provider", "https://gateway.example.invalid/v1", secret)
	if err != nil {
		t.Fatal(err)
	}
	if status.ProviderID != "x-provider" || !status.Configured() {
		t.Fatalf("status = %+v", status)
	}
	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	profile := loaded.Providers["x-provider"]
	if profile.Type != config.ProviderTypeOpenAICompatible || profile.BaseURL != "https://gateway.example.invalid/v1" {
		t.Fatalf("profile = %+v", profile)
	}
	configData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(configData), secret) {
		t.Fatal("API key leaked into config.json")
	}
	credential, ok := a.Auth.Get("x-provider")
	if !ok || credential.Key != secret {
		t.Fatal("API key was not persisted in the auth store")
	}
	info, err := os.Stat(authPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("auth mode = %o", info.Mode().Perm())
	}
}

func TestConfigureOpenAICompatibleAuthRejectsSecretBearingEndpoint(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	a, err := New(t.Context(), Options{Provider: "fake", NoSession: true, Permission: "deny", CWD: home, ConfigPath: configPath, AuthPath: filepath.Join(home, "auth.json"), NoPlugins: true, NoMCP: true, NoSkills: true})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	for _, endpoint := range []string{
		"https://user:password@gateway.example.invalid/v1",
		"https://gateway.example.invalid/v1?api_key=secret",
		"https://gateway.example.invalid/v1#secret",
	} {
		if _, err := a.ConfigureOpenAICompatibleAuth(t.Context(), "x-provider", endpoint, ""); err == nil {
			t.Fatalf("endpoint %q was accepted", endpoint)
		}
	}
	if _, err := os.Stat(configPath); err == nil {
		loaded, loadErr := config.Load(configPath)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		if _, exists := loaded.Providers["x-provider"]; exists {
			t.Fatal("rejected endpoint was persisted")
		}
	}
}
