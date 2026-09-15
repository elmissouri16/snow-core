package auth

import (
	"context"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"os"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const MaxHostAPIKeyBytes = 4096
const HostAuthMissingRevision = "missing"

var (
	ErrHostKeyInvalid              = errors.New("host control: invalid API-key request")
	ErrHostKeyUnavailable          = errors.New("host control: local authentication unavailable")
	ErrHostKeyConflict             = errors.New("host control: revision conflict")
	ErrHostKeyConfirmationRequired = errors.New("host control: replacement confirmation required")
)

type hostKeyObject map[string]jsontext.Value

type hostKeySnapshot struct {
	document hostKeyObject
	info     os.FileInfo
	revision string
}

func validHostKeySecret(secret string) bool {
	return len(secret) > 0 && len(secret) <= MaxHostAPIKeyBytes && utf8.ValidString(secret) && strings.TrimSpace(secret) != "" && !strings.ContainsFunc(secret, unicode.IsControl)
}

func validHostKeyRevision(revision string) bool {
	if revision == HostAuthMissingRevision {
		return true
	}
	if len(revision) != 64 {
		return false
	}
	for _, r := range revision {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func hostKeyCredential(document hostKeyObject, id string) (Credential, bool, error) {
	raw, exists := document[id]
	if !exists {
		return Credential{}, false, nil
	}
	var fields hostKeyObject
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return Credential{}, false, ErrHostKeyUnavailable
	}
	var credential Credential
	if json.Unmarshal(raw, &credential) != nil {
		return Credential{}, false, ErrHostKeyUnavailable
	}
	return credential, true, nil
}

func hostKeyResponse(snapshot hostKeySnapshot, id string) (protocol.HostAPIKeyStatusResponse, error) {
	credential, exists, err := hostKeyCredential(snapshot.document, id)
	if err != nil {
		return protocol.HostAPIKeyStatusResponse{}, err
	}
	status := inspectHostCredential(id, credential, time.Now())
	return protocol.HostAPIKeyStatusResponse{
		ProviderID: id, APIKeySupported: id != "chatgpt", Revision: snapshot.revision,
		ReplaceRequired: exists || status.State == "configured" && status.Reason == "credential_present",
		Status:          status, CheckedLocally: true, AppliesTo: "future_runtime",
	}, nil
}

// InspectHostAPIKey inspects local state without creating a directory, lock or
// credential. The caller must first authorize the locally configured provider.
func InspectHostAPIKey(ctx context.Context, path, id string) (protocol.HostAPIKeyStatusResponse, error) {
	var zero protocol.HostAPIKeyStatusResponse
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if !hostStatusID(id) {
		return zero, ErrHostKeyInvalid
	}
	root, base, err := openHostKeyParent(path, false)
	if errors.Is(err, os.ErrNotExist) {
		return hostKeyResponse(hostKeySnapshot{document: hostKeyObject{}, revision: HostAuthMissingRevision}, id)
	}
	if err != nil {
		return zero, err
	}
	defer root.Close()
	snapshot, err := readHostKeySnapshot(ctx, root, base)
	if err != nil {
		return zero, err
	}
	return hostKeyResponse(snapshot, id)
}

// SetHostAPIKey is a narrow write-only credential mutation, not an auth service.
// The caller must first authorize the locally configured API-key provider. It
// shares the exact persistent .lock inode with FileStore.Update/Put/Delete.
func SetHostAPIKey(ctx context.Context, path string, request protocol.HostAPIKeySetRequest) (protocol.HostAPIKeyStatusResponse, error) {
	var zero protocol.HostAPIKeyStatusResponse
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if !hostStatusID(request.ProviderID) || request.ProviderID == "chatgpt" || !validHostKeySecret(request.Secret) || !validHostKeyRevision(request.ExpectedRevision) {
		return zero, ErrHostKeyInvalid
	}
	root, base, err := openHostKeyParent(path, true)
	if err != nil {
		return zero, err
	}
	defer root.Close()
	lock, err := openHostKeyLock(root, base+".lock")
	if err != nil {
		return zero, err
	}
	defer lock.Close()
	if err = lockHostKeyFile(ctx, lock); err != nil {
		return zero, err
	}
	defer unlockFile(lock)
	if verifyHostKeyFile(root, base+".lock", lock) != nil {
		return zero, ErrHostKeyUnavailable
	}
	snapshot, err := readHostKeySnapshot(ctx, root, base)
	if err != nil {
		return zero, err
	}
	if snapshot.revision != request.ExpectedRevision {
		return zero, ErrHostKeyConflict
	}
	current, err := hostKeyResponse(snapshot, request.ProviderID)
	if err != nil {
		return zero, err
	}
	if current.ReplaceRequired && !request.ConfirmReplace {
		return zero, ErrHostKeyConfirmationRequired
	}
	selected := hostKeyObject{}
	if raw, exists := snapshot.document[request.ProviderID]; exists {
		if json.Unmarshal(raw, &selected) != nil || selected == nil {
			return zero, ErrHostKeyUnavailable
		}
	}
	// Preserve unknown target fields and extra, while removing obsolete known
	// OAuth fields from the explicitly replaced API-key credential.
	for _, key := range []string{"access", "refresh", "expires", "accountId"} {
		delete(selected, key)
	}
	selected["type"], _ = json.Marshal(CredentialAPIKey)
	selected["key"], _ = json.Marshal(request.Secret)
	snapshot.document[request.ProviderID], err = json.Marshal(selected)
	if err != nil {
		return zero, ErrHostKeyUnavailable
	}
	data, err := json.Marshal(snapshot.document, jsontext.WithIndent("  "))
	if err != nil {
		return zero, ErrHostKeyUnavailable
	}
	data = append(data, '\n')
	if len(data) > MaxHostAuthFileBytes {
		return zero, ErrHostKeyUnavailable
	}
	info, err := writeHostKeyRoot(ctx, root, base, snapshot.info, data)
	if err != nil {
		return zero, err
	}
	revision, err := hostKeyRevision(info)
	if err != nil {
		return zero, err
	}
	// Do not marshal a Credential or echo the submitted key in the response.
	return protocol.HostAPIKeyStatusResponse{
		ProviderID: request.ProviderID, APIKeySupported: true, ReplaceRequired: true,
		Revision: revision, CheckedLocally: true, AppliesTo: "future_runtime",
		Status: protocol.HostProviderStatus{ProviderID: request.ProviderID, State: "configured", Reason: "credential_present", CheckedLocally: true},
	}, nil
}
