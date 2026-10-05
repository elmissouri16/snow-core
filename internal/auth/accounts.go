package auth

import (
	"cmp"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"strings"
)

var ErrDuplicateAccount = errors.New("auth: upstream identity already stored under another account")

// ValidateAccountName accepts default and lowercase aliases of at most 64
// bytes. Names start and end with an ASCII letter or digit; internal periods,
// underscores and hyphens are permitted. Empty denotes the default account.
func ValidateAccountName(name string) error {
	if name == "" {
		return nil
	}
	alnum := func(c byte) bool { return c >= 'a' && c <= 'z' || c >= '0' && c <= '9' }
	if len(name) > 64 || !alnum(name[0]) || !alnum(name[len(name)-1]) {
		return errors.New("auth: account name must be 1-64 lowercase letters or digits with internal ._- characters")
	}
	for i := range len(name) {
		if !alnum(name[i]) && name[i] != '.' && name[i] != '_' && name[i] != '-' {
			return errors.New("auth: account name must be 1-64 lowercase letters or digits with internal ._- characters")
		}
	}
	return nil
}

func normalizeAccount(account string) string {
	if account == "" {
		return "default"
	}
	return account
}

// AccountKey returns the credential storage key. Legacy/default entries retain
// the provider ID; only ChatGPT supports named accounts.
func AccountKey(providerID, account string) (string, error) {
	if err := ValidateAccountName(account); err != nil {
		return "", err
	}
	if account == "" || account == "default" {
		return providerID, nil
	}
	if providerID != "chatgpt" {
		return "", errors.New("auth: named accounts are supported only for chatgpt")
	}
	return providerID + "/account/" + account, nil
}

func storedAccounts(providerID string, credentials map[string]Credential) []string {
	var accounts []string
	if _, ok := credentials[providerID]; ok {
		accounts = append(accounts, "default")
	}
	if providerID == "chatgpt" {
		for key := range credentials {
			account, ok := strings.CutPrefix(key, "chatgpt/account/")
			if ok && account != "" && account != "default" && ValidateAccountName(account) == nil {
				accounts = append(accounts, account)
			}
		}
	}
	slices.Sort(accounts)
	return accounts
}

// Accounts returns only stored aliases in sorted order, without network I/O.
// Callers displaying status should additionally include the default account,
// even when no default credential is stored. Legacy stores without enumeration
// support expose only their default entry.
func (s *Service) Accounts(providerID string) ([]string, error) {
	if _, err := s.driver(providerID); err != nil {
		return nil, err
	}
	if s.store == nil {
		return nil, nil
	}
	if store, ok := s.store.(AccountStore); ok {
		accounts, err := store.Accounts(providerID)
		if err != nil {
			return nil, err
		}
		accounts = slices.Clone(accounts)
		slices.Sort(accounts)
		return slices.Compact(accounts), nil
	}
	if _, ok := s.store.Get(providerID); ok {
		return []string{"default"}, nil
	}
	return nil, nil
}

func (s *Service) rejectDuplicateAccount(driver Driver, account string, credential Credential) error {
	accounts, err := s.Accounts("chatgpt")
	if err != nil {
		return err
	}
	workspace := upstreamWorkspace(driver, credential)
	if workspace == "" {
		return nil
	}
	_, subject := accountIdentityParts(credential)
	for _, other := range accounts {
		if other == account {
			continue
		}
		key, err := AccountKey("chatgpt", other)
		if err != nil {
			return err
		}
		stored, exists := s.store.Get(key)
		if !exists || upstreamWorkspace(driver, stored) != workspace {
			continue
		}
		_, otherSubject := accountIdentityParts(stored)
		// Missing subjects cannot establish distinct users in the same workspace.
		if subject == "" || otherSubject == "" || subject == otherSubject {
			return fmt.Errorf("%w: %q", ErrDuplicateAccount, other)
		}
	}
	return nil
}

func upstreamWorkspace(driver Driver, credential Credential) string {
	if workspace, _ := accountIdentityParts(credential); workspace != "" {
		return workspace
	}
	status, _ := driver.Inspect(bindAccount(credential, "chatgpt", normalizeAccount(credential.Account)))
	return status.AccountID
}

// AccountIdentity returns a non-secret, stable identity hash for a ChatGPT
// workspace and user. It excludes alias names, tokens and expiration so it
// survives token rotation. When no user claim is available, the workspace alone
// is used; without a workspace the result is empty. JWT claims here are local
// identity hints, not proof of authentication.
func AccountIdentity(credential Credential) string {
	workspace, subject := accountIdentityParts(credential)
	if workspace == "" {
		return ""
	}
	// Length-prefix each component so embedded separators cannot collide.
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%s%d:%s", len(workspace), workspace, len(subject), subject)))
	return fmt.Sprintf("%x", sum)
}

func accountIdentityParts(credential Credential) (workspace, subject string) {
	claims := jwtIdentityClaims(credential.Access)
	// Match the workspace selected by access-token claims for request headers,
	// rather than fingerprinting stale imported credential metadata.
	workspace = cmp.Or(claims.Auth.AccountID, claims.AccountID, claims.LegacyAccountID, credential.AccountID)
	if workspace == "" {
		workspace, _ = credential.Extra["account_id"].(string)
	}
	subject = claims.Subject
	if subject == "" {
		subject = claims.Auth.UserID
	}
	if subject == "" {
		subject = claims.UserID
	}
	return workspace, subject
}

type identityClaims struct {
	Subject         string `json:"sub"`
	AccountID       string `json:"chatgpt_account_id"`
	LegacyAccountID string `json:"account_id"`
	UserID          string `json:"chatgpt_user_id"`
	Auth            struct {
		AccountID string `json:"chatgpt_account_id"`
		UserID    string `json:"chatgpt_user_id"`
	} `json:"https://api.openai.com/auth"`
}

// Bound decoding so malformed or oversized stored tokens do not allocate
// unbounded memory. Opaque tokens simply have no available claims.
func jwtIdentityClaims(token string) identityClaims {
	if len(token) > 64<<10 {
		return identityClaims{}
	}
	_, rest, ok := strings.Cut(token, ".")
	if !ok {
		return identityClaims{}
	}
	payload, signature, ok := strings.Cut(rest, ".")
	if !ok || strings.Contains(signature, ".") {
		return identityClaims{}
	}
	data, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		data, err = base64.URLEncoding.DecodeString(payload)
		if err != nil {
			return identityClaims{}
		}
	}
	var claims identityClaims
	if json.Unmarshal(data, &claims) != nil {
		return identityClaims{}
	}
	return claims
}
