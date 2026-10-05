package auth

import (
	"context"
	"encoding/base64"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAccountKeyValidation(t *testing.T) {
	for _, name := range []string{"", "default", "a", "0", "work.acct_2-x", strings.Repeat("a", 64)} {
		if err := ValidateAccountName(name); err != nil {
			t.Fatalf("valid name %q: %v", name, err)
		}
		key, err := AccountKey("chatgpt", name)
		want := "chatgpt/account/" + name
		if name == "" || name == "default" {
			want = "chatgpt"
		}
		if err != nil || key != want {
			t.Fatalf("name %q: key=%q err=%v", name, key, err)
		}
	}
	for _, name := range []string{"A", " work", "work ", "../work", "work/a", "work\\a", "é", ".a", "a_", "a-", strings.Repeat("a", 65)} {
		if err := ValidateAccountName(name); err == nil {
			t.Errorf("accepted invalid name %q", name)
		}
		if _, err := AccountKey("chatgpt", name); err == nil {
			t.Errorf("accepted invalid key name %q", name)
		}
	}
	if _, err := AccountKey("other", "work"); err == nil {
		t.Fatal("accepted named account for other provider")
	}
	for _, account := range []string{"", "default"} {
		key, err := AccountKey("other", account)
		if err != nil || key != "other" {
			t.Fatalf("legacy key=%q err=%v", key, err)
		}
	}
}

type accountDriver struct {
	*rotatingDriver
}

func (*accountDriver) Descriptor() Descriptor {
	return Descriptor{ProviderID: "chatgpt", Required: true, Kinds: []CredentialType{CredentialOAuth}, Environment: []string{"AUTH_ACCOUNTS_TEST_KEY"}}
}

func (*accountDriver) Login(_ context.Context, request LoginRequest, _ Interaction) (Credential, error) {
	get := func(key string) string {
		if len(request.Params[key]) == 0 {
			return ""
		}
		return request.Params[key][0]
	}
	return Credential{Type: CredentialOAuth, Access: get("access"), Refresh: "refresh", AccountID: get("workspace"), Expires: 1000}, nil
}

func accountService(t *testing.T, store Store, driver *accountDriver) *Service {
	t.Helper()
	service := NewServiceForTest(store, func() time.Time { return time.Unix(10, 0) })
	if err := service.Register(driver); err != nil {
		t.Fatal(err)
	}
	return service
}

func accountStores(t *testing.T, run func(*testing.T, Store)) {
	t.Helper()
	t.Run("memory", func(t *testing.T) { run(t, NewMemoryStore()) })
	t.Run("file", func(t *testing.T) {
		store, err := NewFileStore(filepath.Join(t.TempDir(), "auth.json"))
		if err != nil {
			t.Fatal(err)
		}
		run(t, store)
	})
}

func accountLogin(t *testing.T, service *Service, account, workspace, access string) (Status, error) {
	t.Helper()
	return service.LoginAccount(t.Context(), "chatgpt", account, LoginRequest{Params: map[string][]string{"workspace": {workspace}, "access": {access}}}, nil)
}

func TestAccountLifecycleIsolationAndFileReopen(t *testing.T) {
	accountStores(t, func(t *testing.T, store Store) {
		service := accountService(t, store, &accountDriver{rotatingDriver: &rotatingDriver{}})
		for _, name := range []string{"", "work", "personal"} {
			status, err := accountLogin(t, service, name, "workspace-"+name, "access-"+name)
			if err != nil || status.Account != normalizeAccount(name) || status.ProviderID != "chatgpt" {
				t.Fatalf("login %q: status=%+v err=%v", name, status, err)
			}
		}
		if file, ok := store.(*FileStore); ok {
			reopened, err := NewFileStore(file.Path())
			if err != nil {
				t.Fatal(err)
			}
			service = accountService(t, reopened, &accountDriver{rotatingDriver: &rotatingDriver{}})
			data, err := os.ReadFile(file.Path())
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), `"Account"`) || strings.Contains(string(data), `"Provider"`) {
				t.Fatal("runtime account/provider binding persisted")
			}
			info, err := os.Stat(file.Path())
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("credential file permissions: info=%v err=%v", info, err)
			}
		}
		accounts, err := service.Accounts("chatgpt")
		if err != nil || !slices.Equal(accounts, []string{"default", "personal", "work"}) {
			t.Fatalf("accounts=%v err=%v", accounts, err)
		}
		for _, name := range accounts {
			credential, err := service.ResolveAccount(t.Context(), "chatgpt", name)
			if err != nil || credential.Account != name || credential.Provider != "chatgpt" {
				t.Fatalf("resolve %s: credential=%+v err=%v", name, credential, err)
			}
		}
		legacy, err := service.Resolve(t.Context(), "chatgpt")
		if err != nil || legacy.Access != "access-" || legacy.Account != "default" {
			t.Fatalf("legacy resolve=%+v err=%v", legacy, err)
		}
		if err := service.LogoutAccount(t.Context(), "chatgpt", "work"); err != nil {
			t.Fatal(err)
		}
		status, err := service.StatusAccount(t.Context(), "chatgpt", "work")
		if err != nil || status.Account != "work" || status.State != StateMissing {
			t.Fatalf("logout status=%+v err=%v", status, err)
		}
		if _, err := service.ResolveAccount(t.Context(), "chatgpt", "work"); !errors.Is(err, ErrLoginRequired) {
			t.Fatalf("removed account resolve error=%v", err)
		}
		if err := service.Logout(t.Context(), "chatgpt"); err != nil {
			t.Fatal(err)
		}
		accounts, err = service.Accounts("chatgpt")
		if err != nil || !slices.Equal(accounts, []string{"personal"}) {
			t.Fatalf("accounts after logout=%v err=%v", accounts, err)
		}
		if _, err := service.ResolveAccount(t.Context(), "chatgpt", "personal"); err != nil {
			t.Fatal("logout affected unrelated account:", err)
		}
	})
}

func TestNamedAccountDoesNotInheritDefaultOverrides(t *testing.T) {
	t.Setenv("AUTH_ACCOUNTS_TEST_KEY", "environment")
	service := accountService(t, NewMemoryStore(), &accountDriver{rotatingDriver: &rotatingDriver{}})
	service.SetExplicit("chatgpt", Credential{Type: CredentialOAuth, Access: "explicit", Refresh: "explicit-refresh"})
	status, err := service.StatusAccount(t.Context(), "chatgpt", "work")
	if err != nil || status.State != StateMissing {
		t.Fatalf("named account inherited default credential: status=%+v err=%v", status, err)
	}
	credential, err := service.Resolve(t.Context(), "chatgpt")
	if err != nil || credential.Access != "explicit" || credential.Account != "default" {
		t.Fatalf("default explicit credential=%+v err=%v", credential, err)
	}
	service.SetExplicit("chatgpt", Credential{})
	status, err = service.StatusAccount(t.Context(), "chatgpt", "work")
	if err != nil || status.State != StateMissing {
		t.Fatalf("named account inherited environment: status=%+v err=%v", status, err)
	}
}

func subjectToken(subject string) string {
	payload, _ := json.Marshal(map[string]string{"sub": subject})
	return "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}

func TestAccountLoginDuplicateIdentity(t *testing.T) {
	accountStores(t, func(t *testing.T, store Store) {
		service := accountService(t, store, &accountDriver{rotatingDriver: &rotatingDriver{}})
		if _, err := accountLogin(t, service, "", "workspace", subjectToken("user-a")); err != nil {
			t.Fatal(err)
		}
		if _, err := accountLogin(t, service, "work", "workspace", subjectToken("user-a")); !errors.Is(err, ErrDuplicateAccount) {
			t.Fatalf("same user/workspace error=%v", err)
		}
		if _, err := accountLogin(t, service, "work", "workspace", "opaque"); !errors.Is(err, ErrDuplicateAccount) {
			t.Fatalf("unavailable subject error=%v", err)
		}
		if _, err := accountLogin(t, service, "work", "workspace", subjectToken("user-b")); err != nil {
			t.Fatal("distinct workspace member rejected:", err)
		}
		if _, err := accountLogin(t, service, "personal", "another-workspace", subjectToken("user-a")); err != nil {
			t.Fatal("distinct workspace rejected:", err)
		}
		if _, err := accountLogin(t, service, "default", "workspace", subjectToken("user-a")); err != nil {
			t.Fatal("same alias relogin rejected:", err)
		}
		if _, err := accountLogin(t, service, "invalid", "another", ""); err == nil {
			t.Fatal("invalid login credential accepted")
		}
		accounts, err := service.Accounts("chatgpt")
		if err != nil || !slices.Equal(accounts, []string{"default", "personal", "work"}) {
			t.Fatalf("failed login changed aliases: accounts=%v err=%v", accounts, err)
		}
	})
}

func TestConcurrentAliasLoginRejectsDuplicateAcrossServices(t *testing.T) {
	accountStores(t, func(t *testing.T, store Store) {
		otherStore := store
		if file, ok := store.(*FileStore); ok {
			var err error
			otherStore, err = NewFileStore(file.Path())
			if err != nil {
				t.Fatal(err)
			}
		}
		services := []*Service{
			accountService(t, store, &accountDriver{rotatingDriver: &rotatingDriver{}}),
			accountService(t, otherStore, &accountDriver{rotatingDriver: &rotatingDriver{}}),
		}
		var wg sync.WaitGroup
		errs := make(chan error, 2)
		for i, service := range services {
			wg.Go(func() {
				_, err := accountLogin(t, service, []string{"work", "personal"}[i], "same-workspace", subjectToken("same-user"))
				errs <- err
			})
		}
		wg.Wait()
		close(errs)
		var saved, rejected int
		for err := range errs {
			if err == nil {
				saved++
			} else if errors.Is(err, ErrDuplicateAccount) {
				rejected++
			} else {
				t.Fatal(err)
			}
		}
		if saved != 1 || rejected != 1 {
			t.Fatalf("saved=%d rejected=%d", saved, rejected)
		}
	})
}

func TestAccountRefreshIsolationAndLateRejection(t *testing.T) {
	accountStores(t, func(t *testing.T, store Store) {
		driver := &accountDriver{rotatingDriver: &rotatingDriver{}}
		service := accountService(t, store, driver)
		for _, account := range []string{"default", "work", "personal"} {
			key, _ := AccountKey("chatgpt", account)
			if err := store.Put(key, Credential{Type: CredentialOAuth, Access: account, Refresh: account + "-refresh", Expires: 1, AccountID: account + "-workspace"}); err != nil {
				t.Fatal(err)
			}
		}
		var wg sync.WaitGroup
		for range 8 {
			wg.Go(func() {
				credential, err := service.ResolveAccount(t.Context(), "chatgpt", "work")
				if err != nil || credential.Account != "work" || credential.Provider != "chatgpt" || credential.Access != "new" {
					t.Errorf("refresh credential=%+v err=%v", credential, err)
				}
			})
		}
		wg.Wait()
		// Resolve already replaced work's old token. Its late rejection must
		// reuse the newer work credential, not refresh default or personal.
		rejected := Credential{Type: CredentialOAuth, Access: "work", Refresh: "work-refresh", Expires: 1, AccountID: "work-workspace", Account: "work"}
		service.SetExplicit("chatgpt", Credential{Type: CredentialOAuth, Access: "default-override"})
		credential, err := service.RefreshRejected(t.Context(), "chatgpt", rejected)
		if err != nil || credential.Account != "work" || credential.Access != "new" {
			t.Fatalf("late rejection=%+v err=%v", credential, err)
		}
		driver.mu.Lock()
		count := driver.refresh
		driver.mu.Unlock()
		if count != 1 {
			t.Fatalf("work refreshes=%d, want 1", count)
		}
		for _, account := range []string{"default", "personal"} {
			key, _ := AccountKey("chatgpt", account)
			stored, _ := store.Get(key)
			if stored.Access != account || stored.Refresh != account+"-refresh" {
				t.Fatalf("work refresh changed %s: %+v", account, stored)
			}
		}
		if err := service.LogoutAccount(t.Context(), "chatgpt", "work"); err != nil {
			t.Fatal(err)
		}
		if _, err := service.RefreshRejected(t.Context(), "chatgpt", rejected); !errors.Is(err, ErrLoginRequired) {
			t.Fatalf("late rejection after logout=%v", err)
		}
	})
}

func TestAccountRefreshIndependentLocksAndConcurrentMutation(t *testing.T) {
	for _, mutation := range []string{"logout", "relogin"} {
		t.Run(mutation, func(t *testing.T) {
			accountStores(t, func(t *testing.T, store Store) {
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				blocked := &accountDriver{rotatingDriver: &rotatingDriver{started: make(chan struct{}, 1), release: make(chan struct{})}}
				var once sync.Once
				unblock := func() { once.Do(func() { close(blocked.release) }) }
				t.Cleanup(unblock)
				service := accountService(t, store, blocked)
				otherStore := store
				if file, ok := store.(*FileStore); ok {
					var err error
					otherStore, err = NewFileStore(file.Path())
					if err != nil {
						t.Fatal(err)
					}
				}
				other := accountService(t, otherStore, &accountDriver{rotatingDriver: &rotatingDriver{}})
				for _, account := range []string{"work", "personal"} {
					key, _ := AccountKey("chatgpt", account)
					if err := store.Put(key, Credential{Type: CredentialOAuth, Access: account, Refresh: account + "-refresh", Expires: 1, AccountID: account}); err != nil {
						t.Fatal(err)
					}
				}
				done := make(chan error, 1)
				go func() {
					credential, err := service.ResolveAccount(ctx, "chatgpt", "work")
					if err == nil && credential.Access != "replacement" {
						err = errors.New("refresh overwrote concurrent relogin")
					}
					done <- err
				}()
				select {
				case <-blocked.started:
				case <-ctx.Done():
					t.Fatal("work refresh did not start")
				}
				// A different account must complete even while work holds its
				// refresh lock, including across FileStore instances.
				personalDone := make(chan error, 1)
				go func() {
					_, err := other.ResolveAccount(ctx, "chatgpt", "personal")
					personalDone <- err
				}()
				select {
				case err := <-personalDone:
					if err != nil {
						t.Fatal(err)
					}
				case <-ctx.Done():
					t.Fatal("work refresh blocked personal account")
				}
				if mutation == "logout" {
					if err := other.LogoutAccount(ctx, "chatgpt", "work"); err != nil {
						t.Fatal(err)
					}
				} else if _, err := accountLogin(t, other, "work", "new-workspace", "replacement"); err != nil {
					t.Fatal(err)
				}
				unblock()
				select {
				case err := <-done:
					if mutation == "logout" && !errors.Is(err, ErrLoginRequired) || mutation == "relogin" && err != nil {
						t.Fatalf("%s during refresh: %v", mutation, err)
					}
				case <-ctx.Done():
					t.Fatal("work refresh did not finish")
				}
			})
		})
	}
}

func TestAccountIdentityStableAndUserScoped(t *testing.T) {
	credential := Credential{Type: CredentialOAuth, AccountID: "workspace", Access: subjectToken("user-a"), Account: "work"}
	identity := AccountIdentity(credential)
	if len(identity) != 64 || strings.Contains(identity, "workspace") || strings.Contains(identity, "user-a") {
		t.Fatalf("identity must be a non-secret SHA-256 hash: %q", identity)
	}
	rotated := credential
	rotated.Account = "renamed"
	rotated.Refresh = "rotated-refresh"
	rotated.Expires = 999999
	payload, _ := json.Marshal(map[string]any{"sub": "user-a", "exp": 999999})
	rotated.Access = "different-header." + base64.RawURLEncoding.EncodeToString(payload) + ".different-signature"
	if AccountIdentity(rotated) != identity {
		t.Fatal("token rotation or alias changed identity")
	}
	rotated.Access = subjectToken("user-b")
	if AccountIdentity(rotated) == identity {
		t.Fatal("different users in same workspace share identity")
	}
	rotated = credential
	rotated.AccountID = "different-workspace"
	if AccountIdentity(rotated) == identity {
		t.Fatal("different workspace shares identity")
	}
	if AccountIdentity(Credential{Access: "opaque", AccountID: "workspace"}) == "" {
		t.Fatal("workspace-only credential has no identity")
	}
	if AccountIdentity(Credential{Access: subjectToken("user-a")}) != "" {
		t.Fatal("credential without workspace has an identity")
	}
	payload, _ = json.Marshal(map[string]any{"https://api.openai.com/auth": map[string]string{"chatgpt_user_id": "user-a", "chatgpt_account_id": "workspace"}})
	fromClaims := Credential{Access: "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"}
	if AccountIdentity(fromClaims) != identity {
		t.Fatal("namespaced user/workspace claims not used as identity fallback")
	}
}

func TestAccountEnumerationReportsCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	service := accountService(t, store, &accountDriver{rotatingDriver: &rotatingDriver{}})
	if _, err := service.Accounts("chatgpt"); err == nil {
		t.Fatal("corrupt store reported as an empty account list")
	}
	if _, err := accountLogin(t, service, "work", "workspace", "access"); err == nil {
		t.Fatal("login overwrote corrupt credential store")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "{broken" {
		t.Fatalf("corrupt store changed: data=%q err=%v", data, err)
	}
}

// Embed only the legacy contract, deliberately omitting optional enumeration.
type legacyAccountStore struct{ Store }

func TestNamedLoginFailsClosedWithoutEnumeration(t *testing.T) {
	service := accountService(t, legacyAccountStore{NewMemoryStore()}, &accountDriver{rotatingDriver: &rotatingDriver{}})
	if _, err := accountLogin(t, service, "default", "workspace", "access"); err != nil {
		t.Fatal("legacy default login no longer works:", err)
	}
	if _, err := accountLogin(t, service, "work", "workspace", "access"); err == nil {
		t.Fatal("named login bypassed duplicate checks without enumeration")
	}
	accounts, err := service.Accounts("chatgpt")
	if err != nil || !slices.Equal(accounts, []string{"default"}) {
		t.Fatalf("legacy account enumeration=%v err=%v", accounts, err)
	}
}

func TestAccountIdentityUsesTokenWorkspaceBeforeStoredMetadata(t *testing.T) {
	for _, tc := range []struct {
		name   string
		claims map[string]any
	}{
		{
			name: "namespaced claim",
			claims: map[string]any{
				"https://api.openai.com/auth": map[string]string{"chatgpt_account_id": "actual-workspace"},
				"chatgpt_account_id":          "other-top-level-workspace",
				"account_id":                  "other-legacy-workspace",
			},
		},
		{
			name: "top-level claim",
			claims: map[string]any{
				"chatgpt_account_id": "actual-workspace",
				"account_id":         "other-legacy-workspace",
			},
		},
		{
			name:   "legacy claim",
			claims: map[string]any{"account_id": "actual-workspace"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.claims["sub"] = "user-a"
			payload, err := json.Marshal(tc.claims)
			if err != nil {
				t.Fatal(err)
			}
			credential := Credential{
				Access:    "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature",
				AccountID: "stale-stored-workspace",
				Extra:     map[string]any{"account_id": "stale-extra-workspace"},
			}
			actual := Credential{AccountID: "actual-workspace", Access: subjectToken("user-a")}
			if AccountIdentity(credential) != AccountIdentity(actual) {
				t.Fatal("fingerprint followed stored metadata instead of actual token workspace")
			}
			service := accountService(t, NewMemoryStore(), &accountDriver{rotatingDriver: &rotatingDriver{}})
			if _, err := accountLogin(t, service, "default", "actual-workspace", actual.Access); err != nil {
				t.Fatal(err)
			}
			if _, err := accountLogin(t, service, "work", credential.AccountID, credential.Access); !errors.Is(err, ErrDuplicateAccount) {
				t.Fatalf("conflicting metadata bypassed duplicate detection: %v", err)
			}
		})
	}
}

func TestRejectedAccountReplacementRequiresKnownSameIdentity(t *testing.T) {
	for _, tc := range []struct {
		name      string
		oldID     string
		newID     string
		oldAccess string
		newAccess string
		allowed   bool
	}{
		{name: "different workspace", oldID: "old-workspace", newID: "new-workspace", oldAccess: "rejected-token", newAccess: "replacement-token"},
		{name: "different user", oldID: "workspace", newID: "workspace", oldAccess: subjectToken("old-user"), newAccess: subjectToken("new-user")},
		{name: "unknown rejected identity", newID: "workspace", oldAccess: "rejected-token", newAccess: "replacement-token"},
		{name: "unknown replacement identity", oldID: "workspace", oldAccess: "rejected-token", newAccess: "replacement-token"},
		{name: "both unknown", oldAccess: "rejected-token", newAccess: "replacement-token"},
		{name: "same workspace opaque tokens", oldID: "workspace", newID: "workspace", oldAccess: "rejected-token", newAccess: "replacement-token", allowed: true},
		{name: "same workspace and user", oldID: "workspace", newID: "workspace", oldAccess: subjectToken("same-user"), newAccess: subjectToken("same-user"), allowed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			accountStores(t, func(t *testing.T, store Store) {
				driver := &accountDriver{rotatingDriver: &rotatingDriver{}}
				service := accountService(t, store, driver)
				rejected := Credential{Account: "work", Type: CredentialOAuth, AccountID: tc.oldID, Access: tc.oldAccess, Refresh: "rejected-refresh", Expires: 1000}
				replacement := Credential{Type: CredentialOAuth, AccountID: tc.newID, Access: tc.newAccess, Refresh: "replacement-refresh", Expires: 2000}
				if err := store.Put("chatgpt/account/work", replacement); err != nil {
					t.Fatal(err)
				}
				credential, err := service.RefreshRejected(t.Context(), "chatgpt", rejected)
				if tc.allowed {
					if err != nil || credential.Access != replacement.Access || credential.Account != "work" {
						t.Fatalf("same-identity rotation failed: credential=%+v err=%v", credential, err)
					}
				} else {
					if !errors.Is(err, ErrAccountChanged) || credential.Valid() {
						t.Fatalf("replacement available for replay: credential=%+v err=%v", credential, err)
					}
					for _, secret := range []string{rejected.Access, replacement.Access, rejected.Refresh, replacement.Refresh} {
						if strings.Contains(err.Error(), secret) {
							t.Fatal("identity mismatch error exposed a token")
						}
					}
				}
				stored, _ := store.Get("chatgpt/account/work")
				if !sameCredential(stored, replacement) {
					t.Fatal("late rejection changed replacement credential")
				}
				driver.mu.Lock()
				refreshes := driver.refresh
				driver.mu.Unlock()
				if refreshes != 0 {
					t.Fatalf("late rejection unnecessarily refreshed replacement %d times", refreshes)
				}
			})
		})
	}
}

type identityChangingDriver struct{ *accountDriver }

func (*identityChangingDriver) Refresh(_ context.Context, credential Credential, _ RefreshReason) (Credential, error) {
	credential.AccountID = "another-workspace"
	credential.Access = "network-replacement-token"
	credential.Refresh = "network-replacement-refresh"
	return credential, nil
}

func TestRefreshCannotPersistDifferentUpstreamIdentity(t *testing.T) {
	accountStores(t, func(t *testing.T, store Store) {
		for _, reason := range []RefreshReason{RefreshRejected, RefreshExpiring} {
			t.Run(string(reason), func(t *testing.T) {
				driver := &identityChangingDriver{&accountDriver{rotatingDriver: &rotatingDriver{}}}
				service := NewServiceForTest(store, func() time.Time { return time.Unix(10, 0) })
				if err := service.Register(driver); err != nil {
					t.Fatal(err)
				}
				old := Credential{Account: "work", Type: CredentialOAuth, AccountID: "workspace", Access: "old-token", Refresh: "old-refresh", Expires: 1}
				if err := store.Put("chatgpt/account/work", old); err != nil {
					t.Fatal(err)
				}
				var credential Credential
				var err error
				if reason == RefreshRejected {
					credential, err = service.RefreshRejected(t.Context(), "chatgpt", old)
				} else {
					credential, err = service.ResolveAccount(t.Context(), "chatgpt", "work")
				}
				if !errors.Is(err, ErrAccountChanged) || credential.Valid() {
					t.Fatalf("refresh identity change accepted: credential=%+v err=%v", credential, err)
				}
				stored, _ := store.Get("chatgpt/account/work")
				if !sameCredential(stored, old) {
					t.Fatal("identity-changing network refresh persisted replacement")
				}
			})
		}
	})
}

func TestRejectedRefreshDoesNotReturnConcurrentDifferentIdentity(t *testing.T) {
	accountStores(t, func(t *testing.T, store Store) {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		driver := &accountDriver{rotatingDriver: &rotatingDriver{started: make(chan struct{}, 1), release: make(chan struct{})}}
		var once sync.Once
		unblock := func() { once.Do(func() { close(driver.release) }) }
		t.Cleanup(unblock)
		service := accountService(t, store, driver)
		rejected := Credential{Account: "work", Type: CredentialOAuth, AccountID: "workspace", Access: "rejected-token", Refresh: "refresh", Expires: 1}
		if err := store.Put("chatgpt/account/work", rejected); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() {
			credential, err := service.RefreshRejected(ctx, "chatgpt", rejected)
			if credential.Valid() {
				err = errors.New("concurrent different identity was returned for replay")
			}
			done <- err
		}()
		select {
		case <-driver.started:
		case <-ctx.Done():
			t.Fatal("rejected refresh did not start")
		}
		if _, err := accountLogin(t, service, "work", "new-workspace", "replacement-token"); err != nil {
			t.Fatal(err)
		}
		unblock()
		select {
		case err := <-done:
			if !errors.Is(err, ErrAccountChanged) {
				t.Fatalf("concurrent relogin rejection error=%v", err)
			}
		case <-ctx.Done():
			t.Fatal("rejected refresh did not finish")
		}
		stored, _ := store.Get("chatgpt/account/work")
		if stored.AccountID != "new-workspace" || stored.Access != "replacement-token" {
			t.Fatal("refresh overwrote concurrent relogin")
		}
	})
}
