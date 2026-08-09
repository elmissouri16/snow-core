package chatgpt

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/snow-core/snow/internal/auth"
)

// SourceID identifies a compatible local auth producer.
type SourceID string

const (
	SourceCodex    SourceID = "codex"
	SourcePi       SourceID = "pi"
	SourceOpenCode SourceID = "opencode"
)

// AuthSource is a discovered, secret-free description of an existing
// ChatGPT/Codex login. Credential is kept in memory only until ImportAuth is
// called; it is never rendered or logged.
type AuthSource struct {
	ID         SourceID
	Name       string
	Path       string
	Credential auth.Credential
	Status     AuthStatus
}

// DiscoverAuthSources finds compatible credentials from Codex, pi, and
// OpenCode in deterministic order. Missing or malformed files are ignored so
// one unrelated tool cannot prevent the login picker from opening.
func DiscoverAuthSources() []AuthSource {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return discoverAuthSources(home, os.Getenv("XDG_DATA_HOME"))
}

// DiscoverAuthSourcesAt is the injectable form used by tests and alternate
// home-directory environments.
func DiscoverAuthSourcesAt(home string) []AuthSource {
	return discoverAuthSources(home, "")
}

func discoverAuthSources(home, xdgDataHome string) []AuthSource {
	if strings.TrimSpace(home) == "" {
		return nil
	}
	var sources []AuthSource

	if source, ok := loadCodex(filepath.Join(home, ".codex", "auth.json")); ok {
		sources = append(sources, source)
	}
	if source, ok := loadPi(filepath.Join(home, ".pi", "agent", "auth.json")); ok {
		sources = append(sources, source)
	}

	openCodePaths := []string{}
	if xdgDataHome != "" {
		openCodePaths = append(openCodePaths, filepath.Join(xdgDataHome, "opencode", "auth.json"))
	}
	openCodePaths = append(openCodePaths,
		filepath.Join(home, ".local", "share", "opencode", "auth.json"),
		filepath.Join(home, ".opencode", "auth.json"),
	)
	seen := make(map[string]bool)
	for _, path := range openCodePaths {
		if seen[path] {
			continue
		}
		seen[path] = true
		if source, ok := loadOpenCode(path); ok {
			sources = append(sources, source)
		}
	}
	return sources
}

// ImportAuth validates and persists a discovered source as Snow's chatgpt
// credential. Expired credentials remain importable when a refresh token is
// present, allowing Snow's runtime refresh path to make them usable.
func ImportAuth(store auth.Store, source AuthSource) (AuthStatus, error) {
	status, err := CheckAuth(source.Credential)
	if err != nil {
		return status, fmt.Errorf("%s auth: %w", source.Name, err)
	}
	if status.AccountID == "" {
		return status, fmt.Errorf("%s auth: credential has no ChatGPT account ID", source.Name)
	}
	if status.Expired && !status.Refreshable {
		return status, fmt.Errorf("%s auth: credential is expired and has no refresh token; log in again there", source.Name)
	}
	if store == nil {
		return status, fmt.Errorf("%s auth: nil credential store", source.Name)
	}
	credential := source.Credential
	credential.Provider = ProviderID
	if err := store.Put(ProviderID, credential); err != nil {
		return status, fmt.Errorf("import %s auth: %w", source.Name, err)
	}
	return status, nil
}

func loadCodex(path string) (AuthSource, bool) {
	var raw struct {
		AuthMode string `json:"auth_mode"`
		Tokens   struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			AccountID    string `json:"account_id"`
		} `json:"tokens"`
	}
	if !readJSON(path, &raw) || raw.Tokens.AccessToken == "" {
		return AuthSource{}, false
	}
	// Do not import an OpenAI API-key-only Codex login as ChatGPT OAuth.
	if raw.AuthMode != "" && !strings.EqualFold(raw.AuthMode, "chatgpt") {
		return AuthSource{}, false
	}
	credential := auth.Credential{
		Provider:  ProviderID,
		Type:      auth.CredentialOAuth,
		Access:    raw.Tokens.AccessToken,
		Refresh:   raw.Tokens.RefreshToken,
		AccountID: raw.Tokens.AccountID,
	}
	return makeSource(SourceCodex, "Codex", path, credential)
}

func loadPi(path string) (AuthSource, bool) {
	var raw map[string]auth.Credential
	if !readJSON(path, &raw) {
		return AuthSource{}, false
	}
	for _, key := range []string{"openai-codex", "chatgpt"} {
		credential, ok := raw[key]
		if !ok || credential.Type != auth.CredentialOAuth || credential.Access == "" {
			continue
		}
		credential.Provider = ProviderID
		return makeSource(SourcePi, "Pi", path, credential)
	}
	return AuthSource{}, false
}

func loadOpenCode(path string) (AuthSource, bool) {
	var raw map[string]auth.Credential
	if !readJSON(path, &raw) {
		return AuthSource{}, false
	}
	// OpenCode's current auth store uses "openai" for its ChatGPT/Codex OAuth
	// entry. Accept the aliases used by earlier compatible builds as well.
	for _, key := range []string{"openai", "openai-codex", "chatgpt"} {
		credential, ok := raw[key]
		if !ok || credential.Type != auth.CredentialOAuth || credential.Access == "" {
			continue
		}
		credential.Provider = ProviderID
		return makeSource(SourceOpenCode, "OpenCode", path, credential)
	}
	return AuthSource{}, false
}

func makeSource(id SourceID, name, path string, credential auth.Credential) (AuthSource, bool) {
	status, err := CheckAuth(credential)
	if err != nil || !status.Authenticated || status.AccountID == "" {
		return AuthSource{}, false
	}
	return AuthSource{ID: id, Name: name, Path: path, Credential: credential, Status: status}, true
}

func readJSON(path string, dst any) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return json.Unmarshal(data, dst) == nil
}
