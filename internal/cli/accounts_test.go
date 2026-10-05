package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/internal/config"
)

func accountTestCommand(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := &cobra.Command{Use: "snow", SilenceUsage: true, SilenceErrors: true}
	cmd.PersistentFlags().String("config", "", "")
	cmd.PersistentFlags().String("auth", "", "")
	cmd.PersistentFlags().String("account", "", "")
	cmd.PersistentFlags().String("base-url", "", "")
	cmd.AddCommand(accountsCmd(), authCmd(), loginCmd(), logoutCmd())
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(t.Context())
	return out.String(), err
}

func putCLIAccount(t *testing.T, account string, expires int64) {
	t.Helper()
	_, path, _ := config.DefaultPaths()
	store, err := auth.NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	key, err := auth.AccountKey("chatgpt", account)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(key, auth.Credential{
		Type: auth.CredentialOAuth, Access: "private-access-" + account,
		Refresh: "private-refresh-" + account, AccountID: "workspace-" + account, Expires: expires,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestCLIAccountsListsStoredAliasesAndLocalStatusWithoutSecrets(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	putCLIAccount(t, "work", time.Now().Add(time.Hour).Unix())
	putCLIAccount(t, "old", 1)
	path, authPath, _ := config.DefaultPaths()
	cfg := config.Default()
	cfg.ChatGPTAccounts.Preferred = "work"
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"accounts"}, {"auth", "accounts", "chatgpt"}} {
		output, err := accountTestCommand(t, args...)
		if err != nil {
			t.Fatal(err)
		}
		if output != "old: expired\nwork: configured (preferred)\n" {
			t.Fatalf("accounts output = %q", output)
		}
		if strings.Contains(output, "private-") || strings.Contains(output, "workspace-") || strings.Contains(output, "default") {
			t.Fatalf("accounts leaked private data or listed an unstored default: %q", output)
		}
	}
	after, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("local account listing mutated credentials")
	}
}

func TestCLIAuthUseSelectsStoredAccountWithoutChangingCredentials(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	putCLIAccount(t, "work", time.Now().Add(time.Hour).Unix())
	path, authPath, _ := config.DefaultPaths()
	cfg := config.Default()
	cfg.Thinking = "high"
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatal(err)
	}
	output, err := accountTestCommand(t, "auth", "use", "chatgpt", "work")
	if err != nil {
		t.Fatal(err)
	}
	if output != "chatgpt: preferred account is work\n" {
		t.Fatalf("auth use output = %q", output)
	}
	loaded, err := config.Load(path)
	if err != nil || loaded.ChatGPTAccounts.Preferred != "work" || loaded.Thinking != "high" {
		t.Fatalf("config = %+v, err = %v", loaded, err)
	}
	after, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("auth use mutated credentials")
	}
	for _, args := range [][]string{
		{"auth", "use", "chatgpt", "missing"},
		{"auth", "use", "chatgpt", "../work"},
		{"auth", "use", "chatgpt", ""},
		{"auth", "use", "openai-compatible", "work"},
	} {
		if _, err := accountTestCommand(t, args...); err == nil {
			t.Fatalf("invalid auth use unexpectedly succeeded: %v", args)
		}
	}
	loaded, err = config.Load(path)
	if err != nil || loaded.ChatGPTAccounts.Preferred != "work" {
		t.Fatalf("failed auth use changed config: %+v, err = %v", loaded.ChatGPTAccounts, err)
	}
}

func TestCLIManagementDefaultsToDefaultNotPreferredAccount(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	putCLIAccount(t, "default", time.Now().Add(time.Hour).Unix())
	putCLIAccount(t, "work", time.Now().Add(time.Hour).Unix())
	path, authPath, _ := config.DefaultPaths()
	cfg := config.Default()
	cfg.ChatGPTAccounts.Preferred = "work"
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	output, err := accountTestCommand(t, "auth", "check", "chatgpt")
	if err != nil || !strings.HasPrefix(output, "chatgpt: ") || strings.Contains(output, "workspace-work") {
		t.Fatalf("default check output = %q, err = %v", output, err)
	}
	output, err = accountTestCommand(t, "auth", "check", "chatgpt", "--account", "work")
	if err != nil || !strings.HasPrefix(output, "chatgpt account work: ") || strings.Contains(output, "private-") {
		t.Fatalf("named check output = %q, err = %v", output, err)
	}
	if _, err := accountTestCommand(t, "logout", "chatgpt"); err != nil {
		t.Fatal(err)
	}
	store, err := auth.NewFileStore(authPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Get("chatgpt"); ok {
		t.Fatal("logout did not remove default")
	}
	workKey, err := auth.AccountKey("chatgpt", "work")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Get(workKey); !ok {
		t.Fatal("default logout removed preferred named credential")
	}
	if _, err := accountTestCommand(t, "logout", "chatgpt", "--account", "work"); err != nil {
		t.Fatal(err)
	}
	store, err = auth.NewFileStore(authPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Get(workKey); ok {
		t.Fatal("named logout did not remove named credential")
	}
}

func TestCLILoginRejectsUnsupportedAccountBeforeRegisteringProfile(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	_, err := accountTestCommand(t, "login", "openai-compatible", "--name", "local", "--base-url", "https://example.invalid/v1", "--account", "work")
	if err == nil || !strings.Contains(err.Error(), "only for chatgpt") {
		t.Fatalf("unsupported account error = %v", err)
	}
	path, _, _ := config.DefaultPaths()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected login created config: %v", err)
	}
}

func TestBuildOptionsForwardsAccountSelectionAndFallback(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("account", "", "")
	cmd.Flags().StringSlice("account-fallback", nil, "")
	if err := cmd.ParseFlags([]string{"--account", "work", "--account-fallback", "personal,default"}); err != nil {
		t.Fatal(err)
	}
	opts, err := buildOptions(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if opts.Account != "work" || strings.Join(opts.AccountFallback, ",") != "personal,default" {
		t.Fatalf("account selection = %q, fallback = %v", opts.Account, opts.AccountFallback)
	}
}

func TestCLIAccountManagementUsesExplicitConfigAndAuthPaths(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	dir := t.TempDir()
	configPath := filepath.Join(dir, "custom-config.json")
	authPath := filepath.Join(dir, "custom-auth.json")
	store, err := auth.NewFileStore(authPath)
	if err != nil {
		t.Fatal(err)
	}
	key, err := auth.AccountKey("chatgpt", "custom")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(key, auth.Credential{Type: auth.CredentialOAuth, Access: "private-custom"}); err != nil {
		t.Fatal(err)
	}
	if _, err := accountTestCommand(t, "auth", "use", "chatgpt", "custom", "--config", configPath, "--auth", authPath); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil || cfg.ChatGPTAccounts.Preferred != "custom" {
		t.Fatalf("explicit config preferred = %q, err = %v", cfg.ChatGPTAccounts.Preferred, err)
	}
	defaultPath, _, _ := config.DefaultPaths()
	if _, err := os.Stat(defaultPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("auth use changed default config instead: %v", err)
	}
}
