package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCredentialMarshalRecursivelyRedactsSecretNamedExtraKeys(t *testing.T) {
	cred := Credential{Type: CredentialOAuth, Access: "access-value", Extra: map[string]any{
		"account_id": "acct",
		"nested": map[string]any{
			"refresh_token": "nested-refresh",
			"items":         []any{map[string]any{"client-secret": "nested-secret", "safe": "visible"}},
		},
	}}
	data, err := json.Marshal(cred)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, secret := range []string{"access-value", "nested-refresh", "nested-secret"} {
		if strings.Contains(text, secret) {
			t.Fatalf("secret %q leaked: %s", secret, text)
		}
	}
	for _, visible := range []string{"acct", "visible"} {
		if !strings.Contains(text, visible) {
			t.Fatalf("safe metadata %q missing: %s", visible, text)
		}
	}

	path := filepath.Join(t.TempDir(), "auth.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put("chatgpt", cred); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "nested-refresh") || !strings.Contains(string(raw), "nested-secret") {
		t.Fatalf("raw persistence was redacted: %s", raw)
	}
}
