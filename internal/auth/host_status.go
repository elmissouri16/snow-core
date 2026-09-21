package auth

import (
	"context"
	"encoding/base64"
	json "encoding/json/v2"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const MaxHostAuthFileBytes = 4 << 20
const MaxHostStatusProviders = 128

var ErrHostStatusUnavailable = errors.New("host control: local authentication unavailable")

// InspectHostStatus performs bounded local reads only. It intentionally does
// not use FileStore.Get (which conceals corruption), Service, drivers, provider
// constructors, token refresh, account extraction or network requests.
func InspectHostStatus(ctx context.Context, path string, ids []string) (protocol.HostProviderStatusResponse, error) {
	response := protocol.HostProviderStatusResponse{Providers: []protocol.HostProviderStatus{}, CheckedLocally: true}
	if err := ctx.Err(); err != nil {
		return response, err
	}
	if len(ids) > MaxHostStatusProviders {
		return response, ErrHostStatusUnavailable
	}
	ids = slices.Clone(ids)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	for _, id := range ids {
		if id == "opencode-zen" || !hostStatusID(id) {
			return response, ErrHostStatusUnavailable
		}
	}
	credentials, err := readHostCredentials(ctx, path)
	if ctx.Err() != nil {
		return response, ctx.Err()
	}
	for _, id := range ids {
		status := protocol.HostProviderStatus{ProviderID: id, State: "unavailable", Reason: "credential_missing", CheckedLocally: true}
		if err != nil {
			status.Reason = "auth_store_unavailable"
		} else {
			status = inspectHostCredential(id, credentials[id], time.Now())
		}
		response.Providers = append(response.Providers, status)
	}
	return response, nil
}

func hostStatusID(id string) bool {
	if len(id) == 0 || len(id) > 64 {
		return false
	}
	for i, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || i > 0 && (r == '-' || r == '_' || r == '.')) {
			return false
		}
	}
	return true
}

func inspectHostCredential(id string, credential Credential, now time.Time) protocol.HostProviderStatus {
	status := protocol.HostProviderStatus{ProviderID: id, State: "unavailable", Reason: "credential_missing", CheckedLocally: true}
	// Same valid-store-entry-before-environment precedence as auth.Service.
	if !credential.Valid() {
		environment := ""
		switch id {
		case "opencode-go":
			environment = "OPENCODE_API_KEY"
		case "openai-compatible":
			environment = "OPENAI_API_KEY"
		}
		if environment != "" && strings.TrimSpace(os.Getenv(environment)) != "" {
			credential = Credential{Type: CredentialAPIKey, Key: "present"}
		}
	}
	if !credential.Valid() {
		return status
	}
	if id == "chatgpt" {
		if credential.Type != CredentialOAuth || strings.TrimSpace(credential.Access) == "" {
			status.Reason = "credential_invalid"
			return status
		}
		expires := credential.Expires
		if expires <= 0 {
			expires = hostJWTExpiry(credential.Access)
		}
		expiry := time.Unix(expires, 0)
		if expires > 1_000_000_000_000 {
			expiry = time.UnixMilli(expires)
		}
		if expires > 0 && !expiry.After(now) {
			status.State = "expired"
			status.Reason = "credential_expired"
			return status
		}
	} else if credential.Type != CredentialAPIKey || strings.TrimSpace(credential.Key) == "" {
		status.Reason = "credential_invalid"
		return status
	}
	status.State = "configured"
	status.Reason = "credential_present"
	return status
}

// Only expiry is decoded; account and other JWT claims are never projected.
func hostJWTExpiry(token string) int64 {
	if len(token) > MaxHostAuthFileBytes {
		return 0
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return 0
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		payload, err = base64.URLEncoding.DecodeString(parts[1])
	}
	if err != nil {
		return 0
	}
	var claims struct {
		Expires int64 `json:"exp"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return 0
	}
	return claims.Expires
}

func readHostCredentials(ctx context.Context, path string) (map[string]Credential, error) {
	absolute, err := filepath.Abs(path)
	if path == "" || err != nil {
		return nil, ErrHostStatusUnavailable
	}
	parent := filepath.Dir(absolute)
	beforeParent, err := os.Lstat(parent)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]Credential{}, nil
	}
	if err != nil || !beforeParent.IsDir() || beforeParent.Mode()&os.ModeSymlink != 0 {
		return nil, ErrHostStatusUnavailable
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		return nil, ErrHostStatusUnavailable
	}
	defer root.Close()
	afterParent, err := root.Stat(".")
	if err != nil || !os.SameFile(beforeParent, afterParent) {
		return nil, ErrHostStatusUnavailable
	}

	base := filepath.Base(absolute)
	before, err := root.Lstat(base)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]Credential{}, nil
	}
	if err != nil || !before.Mode().IsRegular() || before.Mode()&os.ModeSymlink != 0 || before.Size() > MaxHostAuthFileBytes {
		return nil, ErrHostStatusUnavailable
	}
	file, err := openHostStatusFile(root, base)
	if err != nil {
		return nil, ErrHostStatusUnavailable
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) {
		return nil, ErrHostStatusUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxHostAuthFileBytes+1))
	if err != nil || len(data) > MaxHostAuthFileBytes {
		return nil, ErrHostStatusUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	current, err := root.Lstat(base)
	if err != nil || !current.Mode().IsRegular() || !os.SameFile(after, current) {
		return nil, ErrHostStatusUnavailable
	}
	var credentials map[string]Credential
	if json.Unmarshal(data, &credentials) != nil || credentials == nil {
		return nil, ErrHostStatusUnavailable
	}
	return credentials, nil
}
