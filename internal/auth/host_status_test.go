package auth

import (
	"encoding/base64"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func hostAuthPath(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", dir)
	t.Setenv("SNOW_HOME", dir)
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENCODE_API_KEY", "")
	return filepath.Join(dir, "auth.json")
}
func writeHostAuth(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestHostStatusRedactsAndDoesNotRefresh(t *testing.T) {
	path := hostAuthPath(t)
	data := `{"chatgpt":{"type":"oauth","access":"access-canary","refresh":"refresh-canary","expires":1,"accountId":"account-canary","extra":{"email":"email-canary","headers":{"authorization":"header-canary"}}},"opencode-go":{"type":"api_key","key":"key-canary"},"unknown-account-canary":{"type":"api_key","key":"unknown-key-canary"}}`
	writeHostAuth(t, path, data)
	response, err := InspectHostStatus(t.Context(), path, []string{"opencode-go", "chatgpt", "opencode-zen", "profile"})
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range response.Providers {
		if !status.CheckedLocally {
			t.Fatal("missing local flag")
		}
		switch status.ProviderID {
		case "chatgpt":
			if status.State != "expired" || status.Reason != "credential_expired" {
				t.Fatal("expired status")
			}
		case "opencode-go":
			if status.State != "configured" {
				t.Fatal("key status")
			}
		case "opencode-zen":
			if status.State != "configured" || status.Reason != "anonymous_access" {
				t.Fatal("anonymous status")
			}
		case "profile":
			if status.State != "unavailable" {
				t.Fatal("profile status")
			}
		}
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"canary", "account", "email", "expires", "refresh", "summary", "headers", "environment", "key"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("exposed forbidden data category %s", forbidden)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != data {
		t.Fatal("auth inspection mutated credentials")
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatal("auth inspection created side-effect files")
	}
}

func TestHostStatusPrecedenceAndEnvironmentIsolation(t *testing.T) {
	path := hostAuthPath(t)
	t.Setenv("OPENAI_API_KEY", "environment-canary")
	t.Setenv("OPENCODE_API_KEY", "environment-canary")
	writeHostAuth(t, path, `{"opencode-go":{"type":"oauth","access":"wrong-type-valid-token"},"opencode-zen":{"type":"api_key","key":""},"chatgpt":{"type":"api_key","key":"not-subscription-auth"}}`)
	response, err := InspectHostStatus(t.Context(), path, []string{"openai-compatible", "profile", "opencode-go", "opencode-zen", "chatgpt"})
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range response.Providers {
		switch status.ProviderID {
		case "openai-compatible", "opencode-zen":
			if status.State != "configured" || status.Reason != "credential_present" {
				t.Fatal("environment fallback missing")
			}
		case "opencode-go", "chatgpt":
			if status.State != "unavailable" || status.Reason != "credential_invalid" {
				t.Fatal("valid stored wrong-kind credential lost precedence")
			}
		case "profile":
			if status.State != "unavailable" || status.Reason != "credential_missing" {
				t.Fatal("named profile inherited shared environment key")
			}
		}
	}
}

func TestHostStatusMissingMalformedAndOversize(t *testing.T) {
	path := hostAuthPath(t)
	response, err := InspectHostStatus(t.Context(), path, []string{"chatgpt"})
	if err != nil || response.Providers[0].Reason != "credential_missing" {
		t.Fatal("missing store classification")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("GET created store")
	}
	for _, data := range []string{`{"secret":"parse-canary",`, "null", "", strings.Repeat(" ", MaxHostAuthFileBytes+1), `{"chatgpt":{},"chatgpt":{}}`} {
		writeHostAuth(t, path, data)
		response, err = InspectHostStatus(t.Context(), path, []string{"chatgpt", "opencode-zen"})
		if err != nil {
			t.Fatal(err)
		}
		for _, status := range response.Providers {
			if status.State != "unavailable" || status.Reason != "auth_store_unavailable" {
				t.Fatal("corruption concealed as missing/anonymous")
			}
		}
	}
}

func TestHostStatusExpirySecondsMillisecondsAndJWT(t *testing.T) {
	path := hostAuthPath(t)
	now := time.Now()
	for _, test := range []struct {
		name    string
		expires int64
		access  string
		state   string
	}{
		{"seconds", now.Add(-time.Hour).Unix(), "access-canary", "expired"},
		{"milliseconds", now.Add(-time.Hour).UnixMilli(), "access-canary", "expired"},
		{"future", now.Add(time.Hour).Unix(), "access-canary", "configured"},
		{"jwt", 0, "header." + base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d,"email":"email-canary"}`, now.Add(-time.Hour).Unix()))) + ".signature", "expired"},
		{"unknown", 0, "opaque-token-canary", "configured"},
	} {
		t.Run(test.name, func(t *testing.T) {
			writeHostAuth(t, path, fmt.Sprintf(`{"chatgpt":{"type":"oauth","expires":%d,"access":%q,"refresh":"refresh-canary"}}`, test.expires, test.access))
			response, err := InspectHostStatus(t.Context(), path, []string{"chatgpt"})
			if err != nil || response.Providers[0].State != test.state {
				t.Fatalf("expiry state: %v", err)
			}
		})
	}
}

func TestHostStatusIDsBoundedAndSymlinkRefused(t *testing.T) {
	path := hostAuthPath(t)
	for _, ids := range [][]string{make([]string, MaxHostStatusProviders+1), {"email@canary"}, {strings.Repeat("a", 65)}, {"UPPER"}} {
		if _, err := InspectHostStatus(t.Context(), path, ids); !errors.Is(err, ErrHostStatusUnavailable) {
			t.Fatal("invalid IDs accepted")
		}
	}
	target := filepath.Join(filepath.Dir(path), "target")
	writeHostAuth(t, target, `{"chatgpt":{"type":"oauth","access":"token"}}`)
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	response, err := InspectHostStatus(t.Context(), path, []string{"chatgpt"})
	if err != nil || response.Providers[0].Reason != "auth_store_unavailable" {
		t.Fatal("followed auth symlink")
	}
}
