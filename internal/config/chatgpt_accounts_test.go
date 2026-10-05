package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChatGPTPreferredAccountRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := Default()
	cfg.ChatGPTAccounts.Preferred = "work-team"
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ChatGPTAccounts.Preferred != "work-team" {
		t.Fatalf("preferred = %q", loaded.ChatGPTAccounts.Preferred)
	}
	if _, err := Update(path, func(cfg *Config) error {
		cfg.ChatGPTAccounts.Preferred = "default"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	loaded, err = Load(path)
	if err != nil || loaded.ChatGPTAccounts.Preferred != "default" {
		t.Fatalf("updated preferred = %q, err = %v", loaded.ChatGPTAccounts.Preferred, err)
	}
}

func TestChatGPTAccountsRejectInvalidPolicy(t *testing.T) {
	for _, tc := range []struct {
		name string
		data string
		want string
	}{
		{"invalid name", `{"chatgpt_accounts":{"preferred":"../work"}}`, "preferred"},
		{"blank name", `{"chatgpt_accounts":{"preferred":" "}}`, "preferred"},
		{"unsupported failover", `{"chatgpt_accounts":{"fallback":["work"]}}`, "automatic account failover"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(tc.data), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load error = %v, want %s", err, tc.want)
			}
		})
	}
}

func TestChatGPTAccountsUpdateRejectsUnsupportedFailoverWithoutWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Save(path, Default()); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Update(path, func(cfg *Config) error {
		cfg.ChatGPTAccounts.Fallback = []string{"work"}
		return nil
	}); err == nil {
		t.Fatal("unsupported fallback was saved")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("failed update changed config")
	}
}

func TestProjectCannotOverrideChatGPTAccounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	// Even invalid global policy in a project file is ignored, not validated
	// or applied as operator account policy after project trust is granted.
	if err := os.WriteFile(path, []byte(`{"chatgpt_accounts":{"preferred":"../evil","fallback":["attacker"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	project, err := LoadProjectExtensions(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg := Default()
	cfg.ChatGPTAccounts.Preferred = "work"
	if err := ApplyProjectPreferences(&cfg, project); err != nil {
		t.Fatal(err)
	}
	if cfg.ChatGPTAccounts.Preferred != "work" || len(cfg.ChatGPTAccounts.Fallback) != 0 {
		t.Fatalf("project changed account policy: %+v", cfg.ChatGPTAccounts)
	}
}
