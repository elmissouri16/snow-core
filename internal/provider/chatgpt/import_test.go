package chatgpt

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/auth"
)

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func jwtWithExpiry(t *testing.T, expires time.Time) string {
	t.Helper()
	encode := func(value any) string {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return base64.RawURLEncoding.EncodeToString(data)
	}
	return encode(map[string]string{"alg": "none"}) + "." + encode(map[string]any{
		"exp": expires.Unix(),
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": "source-account",
		},
	}) + ".sig"
}

func TestDiscoverAuthSourcesAt(t *testing.T) {
	home := t.TempDir()
	token := jwtWithExpiry(t, time.Now().Add(time.Hour))

	writeJSON(t, filepath.Join(home, ".codex", "auth.json"), map[string]any{
		"auth_mode": "chatgpt",
		"tokens": map[string]any{
			"access_token":  token,
			"refresh_token": "codex-refresh",
			"account_id":    "codex-account",
		},
	})
	writeJSON(t, filepath.Join(home, ".pi", "agent", "auth.json"), map[string]any{
		"openai-codex": map[string]any{
			"type":      "oauth",
			"access":    token,
			"refresh":   "pi-refresh",
			"expires":   time.Now().Add(time.Hour).Unix(),
			"accountId": "pi-account",
		},
	})
	writeJSON(t, filepath.Join(home, ".local", "share", "opencode", "auth.json"), map[string]any{
		"openai": map[string]any{
			"type":      "oauth",
			"access":    token,
			"refresh":   "opencode-refresh",
			"expires":   time.Now().Add(time.Hour).Unix(),
			"accountId": "opencode-account",
		},
	})

	sources := DiscoverAuthSourcesAt(home)
	if len(sources) != 3 {
		t.Fatalf("sources = %d, want 3: %+v", len(sources), sources)
	}
	want := []SourceID{SourceOpenCode, SourcePi, SourceCodex}
	for i, source := range sources {
		if source.ID != want[i] || source.Credential.Access == "" {
			t.Fatalf("source[%d] = %+v", i, source)
		}
	}
}

func TestDiscoverSkipsAPIKeyAndMalformedSources(t *testing.T) {
	home := t.TempDir()
	writeJSON(t, filepath.Join(home, ".codex", "auth.json"), map[string]any{
		"auth_mode":      "api_key",
		"OPENAI_API_KEY": "sk-not-oauth",
	})
	if err := os.MkdirAll(filepath.Join(home, ".pi", "agent"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".pi", "agent", "auth.json"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := DiscoverAuthSourcesAt(home); len(got) != 0 {
		t.Fatalf("got invalid sources: %+v", got)
	}
}

func TestImportAuthWritesSnowCredential(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	store, err := auth.NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	source := AuthSource{
		ID:   SourcePi,
		Name: "Pi",
		Path: "~/.pi/agent/auth.json",
		Credential: auth.Credential{
			Type:      auth.CredentialOAuth,
			Access:    "access",
			Refresh:   "refresh",
			AccountID: "account",
		},
	}
	status, err := ImportAuth(store, source)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Authenticated {
		t.Fatal("import should return authenticated status")
	}
	got, ok := store.Get(ProviderID)
	if !ok || got.Type != auth.CredentialOAuth || got.Access != "access" || got.AccountID != "account" {
		t.Fatalf("imported credential = %+v, ok=%v", got, ok)
	}
}

func TestImportAuthRequiresAccountID(t *testing.T) {
	store := auth.NewMemoryStoreForTest()
	_, err := ImportAuth(store, AuthSource{Name: "Codex", Credential: auth.Credential{Type: auth.CredentialOAuth, Access: "opaque", Refresh: "refresh"}})
	if err == nil || !strings.Contains(err.Error(), "account ID") {
		t.Fatalf("missing account error=%v", err)
	}
	if _, ok := store.Get(ProviderID); ok {
		t.Fatal("account-less credential was imported")
	}
}

func TestImportAuthAcceptsExpiredRefreshableCredential(t *testing.T) {
	store := auth.NewMemoryStoreForTest()
	_, err := ImportAuth(store, AuthSource{
		ID:   SourceCodex,
		Name: "Codex",
		Credential: auth.Credential{
			Type:      auth.CredentialOAuth,
			Access:    "access",
			Refresh:   "refresh",
			Expires:   time.Now().Add(-time.Hour).Unix(),
			AccountID: "account",
		},
	})
	if err != nil {
		t.Fatalf("refreshable expired source should import: %v", err)
	}
	if got, ok := store.Get(ProviderID); !ok || got.Refresh == "" {
		t.Fatalf("refreshable credential not stored: %+v", got)
	}
}
