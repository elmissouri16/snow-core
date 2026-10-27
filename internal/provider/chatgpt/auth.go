// Package chatgpt contains ChatGPT/Codex subscription authentication helpers.
//
// ChatGPT subscription auth is OAuth, not an OpenAI API key. The check in this
// package is intentionally side-effect-free: it validates the shape of a
// stored OAuth credential and extracts account metadata already stored or
// encoded in the access-token JWT. Token exchange may also consume id_token
// claims without persisting that token; refresh belongs to the runtime layer.
package chatgpt

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/elmissouri16/snow-core/internal/auth"
)

const (
	// ProviderID is the snow auth-file key for ChatGPT credentials.
	ProviderID = "chatgpt"
	// AuthBaseURL is OpenAI's OAuth issuer used by Codex-compatible clients.
	AuthBaseURL = "https://auth.openai.com"
	// BackendBaseURL is the ChatGPT Codex API host used after OAuth login.
	BackendBaseURL = "https://chatgpt.com/backend-api"
)

var (
	ErrNotOAuth      = errors.New("chatgpt: credential is not OAuth")
	ErrMissingAccess = errors.New("chatgpt: OAuth credential has no access token")
)

// AuthStatus describes a locally configured ChatGPT subscription credential.
// Authenticated means a usable credential is present locally; it does not
// claim that the token has been accepted by the remote service.
type AuthStatus struct {
	Provider      string
	Authenticated bool
	Expired       bool
	Refreshable   bool
	AccountID     string
	PlanType      string
	ExpiresAt     time.Time
}

// CheckAuth performs the same kind of non-network auth check used by pi:
// stored OAuth credentials count as configured, while JWT claims are used to
// enrich the status display. It never logs or sends token material.
func CheckAuth(cred auth.Credential) (AuthStatus, error) {
	status := AuthStatus{Provider: ProviderID}
	if cred.Type != auth.CredentialOAuth {
		return status, ErrNotOAuth
	}
	if strings.TrimSpace(cred.Access) == "" {
		return status, ErrMissingAccess
	}

	status.Authenticated = true
	status.Refreshable = strings.TrimSpace(cred.Refresh) != ""
	status.ExpiresAt = unixTime(cred.Expires)

	claims, ok := decodeJWTClaims(cred.Access)
	if ok {
		if status.ExpiresAt.IsZero() {
			if exp, ok := numberClaim(claims, "exp"); ok {
				status.ExpiresAt = time.Unix(exp, 0)
			}
		}
		status.AccountID = accountIDFromClaims(claims)
		if status.AccountID == "" {
			status.AccountID = stringExtra(cred.Extra, "account_id")
		}
		status.PlanType = planTypeFromClaims(claims)
	}
	if status.AccountID == "" {
		status.AccountID = cred.AccountID
	}
	if status.AccountID == "" {
		status.AccountID = stringExtra(cred.Extra, "account_id")
	}
	if status.PlanType == "" {
		status.PlanType = stringExtra(cred.Extra, "plan_type")
	}
	status.Expired = !status.ExpiresAt.IsZero() && !status.ExpiresAt.After(time.Now())
	return status, nil
}

// CheckStore checks the ChatGPT entry in a credential store without exposing
// the token. A missing entry is reported as an unauthenticated status rather
// than an operational error, which makes it suitable for status UIs.
func CheckStore(store auth.Store) (AuthStatus, error) {
	status := AuthStatus{Provider: ProviderID}
	if store == nil {
		return status, nil
	}
	cred, ok := store.Get(ProviderID)
	if !ok {
		return status, nil
	}
	return CheckAuth(cred)
}

func unixTime(value int64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	// Some OAuth implementations persist milliseconds; snow's documented
	// format is seconds, but accepting both makes imported Codex/pi entries
	// report correctly without changing the on-disk credential.
	if value > 1_000_000_000_000 {
		return time.UnixMilli(value)
	}
	return time.Unix(value, 0)
}

func decodeJWTClaims(token string) (map[string]any, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		// Be liberal with JWTs that include padding.
		payload, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return nil, false
		}
	}
	var claims map[string]any
	if json.Unmarshal(payload, &claims) != nil {
		return nil, false
	}
	return claims, true
}

func accountIDFromClaims(claims map[string]any) string {
	if value := nestedStringClaim(claims, "https://api.openai.com/auth", "chatgpt_account_id"); value != "" {
		return value
	}
	for _, key := range []string{"chatgpt_account_id", "account_id"} {
		if value, _ := claims[key].(string); value != "" {
			return value
		}
	}
	return ""
}

func planTypeFromClaims(claims map[string]any) string {
	if value := nestedStringClaim(claims, "https://api.openai.com/auth", "chatgpt_plan_type"); value != "" {
		return value
	}
	for _, key := range []string{"chatgpt_plan_type", "plan_type"} {
		if value, _ := claims[key].(string); value != "" {
			return value
		}
	}
	return ""
}

func nestedStringClaim(claims map[string]any, namespace, key string) string {
	obj, ok := claims[namespace].(map[string]any)
	if !ok {
		return ""
	}
	value, _ := obj[key].(string)
	return value
}

func numberClaim(claims map[string]any, key string) (int64, bool) {
	value, ok := claims[key].(float64)
	if !ok || value <= 0 {
		return 0, false
	}
	return int64(value), true
}

func stringExtra(extra map[string]any, key string) string {
	if extra == nil {
		return ""
	}
	value, _ := extra[key].(string)
	return value
}

// FormatStatus returns a secret-free human-readable status line.
func FormatStatus(status AuthStatus) string {
	if !status.Authenticated {
		return "not authenticated"
	}
	if status.Expired {
		if status.Refreshable {
			return "OAuth token expired (refresh required)"
		}
		return "OAuth token expired (login required)"
	}
	parts := []string{"authenticated via OAuth"}
	if status.AccountID != "" {
		parts = append(parts, "account "+status.AccountID)
	}
	if status.PlanType != "" {
		parts = append(parts, "plan "+status.PlanType)
	}
	if !status.ExpiresAt.IsZero() {
		parts = append(parts, "expires "+status.ExpiresAt.Format(time.RFC3339))
	}
	return strings.Join(parts, ", ")
}

// Validate is a concise error-returning form for provider Resolve methods.
func Validate(cred auth.Credential) error {
	status, err := CheckAuth(cred)
	if err != nil {
		return err
	}
	if status.Expired {
		return fmt.Errorf("chatgpt: OAuth access token expired")
	}
	return nil
}
