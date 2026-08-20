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

func testJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	encode := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return base64.RawURLEncoding.EncodeToString(b)
	}
	return encode(map[string]string{"alg": "none"}) + "." + encode(claims) + ".sig"
}

func TestCheckAuthOAuthJWT(t *testing.T) {
	token := testJWT(t, map[string]any{
		"exp": float64(time.Now().Add(time.Hour).Unix()),
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": "account-123",
			"chatgpt_plan_type":  "plus",
		},
	})

	status, err := CheckAuth(auth.Credential{
		Type:    auth.CredentialOAuth,
		Access:  token,
		Refresh: "refresh-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !status.Authenticated || status.Expired || !status.Refreshable {
		t.Fatalf("unexpected status: %+v", status)
	}
	if status.AccountID != "account-123" || status.PlanType != "plus" {
		t.Fatalf("claims not extracted: %+v", status)
	}
	if !strings.Contains(FormatStatus(status), "account-123") {
		t.Fatalf("status should include account id: %s", FormatStatus(status))
	}
}

func TestCheckAuthRejectsWrongCredentialType(t *testing.T) {
	_, err := CheckAuth(auth.Credential{Type: auth.CredentialAPIKey, Key: "sk-test"})
	if err != ErrNotOAuth {
		t.Fatalf("err = %v, want %v", err, ErrNotOAuth)
	}
}

func TestCheckAuthRejectsMissingAccess(t *testing.T) {
	_, err := CheckAuth(auth.Credential{Type: auth.CredentialOAuth, Refresh: "refresh"})
	if err != ErrMissingAccess {
		t.Fatalf("err = %v, want %v", err, ErrMissingAccess)
	}
}

func TestCheckAuthUsesPiCompatibleAccountID(t *testing.T) {
	status, err := CheckAuth(auth.Credential{
		Type:      auth.CredentialOAuth,
		Access:    "opaque-access-token",
		AccountID: "pi-account",
	})
	if err != nil {
		t.Fatal(err)
	}
	if status.AccountID != "pi-account" {
		t.Fatalf("account id = %q, want pi-account", status.AccountID)
	}
}

func TestCheckStoreReadsPiCompatibleAuthJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	data := `{"chatgpt":{"type":"oauth","access":"opaque-access-token","refresh":"refresh","accountId":"pi-account"}}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := auth.NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	status, err := CheckStore(store)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Authenticated || status.AccountID != "pi-account" {
		t.Fatalf("unexpected imported pi status: %+v", status)
	}
}

func TestCheckAuthExpiredAndImportedMetadata(t *testing.T) {
	status, err := CheckAuth(auth.Credential{
		Type:    auth.CredentialOAuth,
		Access:  "opaque-access-token",
		Refresh: "refresh",
		Expires: time.Now().Add(-time.Minute).Unix(),
		Extra: map[string]any{
			"account_id": "imported-account",
			"plan_type":  "pro",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !status.Authenticated || !status.Expired || status.AccountID != "imported-account" || status.PlanType != "pro" {
		t.Fatalf("unexpected imported status: %+v", status)
	}
	if err := Validate(auth.Credential{
		Type:    auth.CredentialOAuth,
		Access:  "opaque-access-token",
		Expires: time.Now().Add(-time.Minute).Unix(),
	}); err == nil {
		t.Fatal("Validate should reject expired credentials")
	}
}

func TestCheckStoreMissing(t *testing.T) {
	status, err := CheckStore(auth.NewMemoryStoreForTest())
	if err != nil {
		t.Fatal(err)
	}
	if status.Authenticated || status.Provider != ProviderID {
		t.Fatalf("unexpected missing status: %+v", status)
	}
}

func TestCheckAuthAcceptsMilliseconds(t *testing.T) {
	expires := time.Now().Add(time.Hour)
	status, err := CheckAuth(auth.Credential{
		Type:    auth.CredentialOAuth,
		Access:  "opaque",
		Expires: expires.UnixMilli(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if status.Expired || status.ExpiresAt.Unix() != expires.Unix() {
		t.Fatalf("millisecond expiry was not normalized: %+v", status)
	}
}
