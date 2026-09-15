package web

import (
	"context"
	"encoding/hex"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// HostAPIKeyBackend is optional. It exposes no credential read, OAuth, refresh,
// deletion or runtime activation capability. Never log the secret request.
type HostAPIKeyBackend interface {
	InspectAPIKey(context.Context, string) (protocol.HostAPIKeyStatusResponse, error)
	SetAPIKey(context.Context, protocol.HostAPIKeySetRequest) (protocol.HostAPIKeyStatusResponse, error)
}

type hostAPIKeyInspection struct {
	provider, revision string
	replace            bool
	expires            time.Time
}

// One latest inspection per paired browser, bounded by maxBrowsers. No secret
// is retained. A write consumes the inspection even if its outcome is unknown.
type hostAPIKeyState struct {
	mu          sync.Mutex
	inspections map[string]hostAPIKeyInspection
}

func hostAPIKeyRevision(value string) bool {
	if value == "missing" {
		return true
	}
	if len(value) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && hex.EncodeToString(decoded) == value
}
func hostAPIKeySecret(value string) bool {
	return len(value) > 0 && len(value) <= 4096 && utf8.ValidString(value) && strings.TrimSpace(value) == value && !strings.ContainsFunc(value, unicode.IsControl)
}
func projectHostAPIKey(raw protocol.HostAPIKeyStatusResponse, provider string) (protocol.HostAPIKeyStatusResponse, error) {
	if !hostProvider(provider) || raw.ProviderID != provider || raw.Status.ProviderID != provider || !raw.CheckedLocally || !hostAPIKeyRevision(raw.Revision) || raw.AppliesTo != "future_runtime" || raw.APIKeySupported && provider == "chatgpt" {
		return protocol.HostAPIKeyStatusResponse{}, ErrHostControlUnavailable
	}
	statuses, err := projectProviderStatus(protocol.HostProviderStatusResponse{Providers: []protocol.HostProviderStatus{raw.Status}, CheckedLocally: raw.CheckedLocally})
	if err != nil {
		return protocol.HostAPIKeyStatusResponse{}, err
	}
	return protocol.HostAPIKeyStatusResponse{ProviderID: provider, APIKeySupported: raw.APIKeySupported, ReplaceRequired: raw.ReplaceRequired, Revision: raw.Revision, Status: statuses.Providers[0], CheckedLocally: true, AppliesTo: "future_runtime"}, nil
}
func projectHostAPIKeyWritten(raw protocol.HostAPIKeyStatusResponse, provider string) (protocol.HostAPIKeyStatusResponse, error) {
	result, err := projectHostAPIKey(raw, provider)
	if err != nil || !result.APIKeySupported || !result.ReplaceRequired || result.Revision == "missing" || result.Status.State != "configured" || result.Status.Reason != "credential_present" {
		return protocol.HostAPIKeyStatusResponse{}, ErrHostControlUnavailable
	}
	return result, nil
}
func (c *WorkerControl) InspectAPIKey(ctx context.Context, provider string) (protocol.HostAPIKeyStatusResponse, error) {
	if !hostProvider(provider) {
		return protocol.HostAPIKeyStatusResponse{}, ErrHostControlInvalid
	}
	var raw protocol.HostAPIKeyStatusResponse
	if err := c.call(ctx, "global", "", "api_key_inspect", protocol.HostAPIKeyInspectRequest{ProviderID: provider}, &raw); err != nil {
		return raw, err
	}
	return projectHostAPIKey(raw, provider)
}
func (c *WorkerControl) SetAPIKey(ctx context.Context, input protocol.HostAPIKeySetRequest) (protocol.HostAPIKeyStatusResponse, error) {
	var raw protocol.HostAPIKeyStatusResponse
	if !hostProvider(input.ProviderID) || input.ProviderID == "chatgpt" || !hostAPIKeyRevision(input.ExpectedRevision) || !hostAPIKeySecret(input.Secret) {
		return raw, ErrHostControlInvalid
	}
	if err := c.call(ctx, "global", "", "api_key_set", input, &raw); err != nil {
		return protocol.HostAPIKeyStatusResponse{}, err
	}
	return projectHostAPIKeyWritten(raw, input.ProviderID)
}

func (s *shell) hostAPIKeyEnabled() bool {
	_, ok := s.hostSettings.(HostAPIKeyBackend)
	return ok && s.hostAPIKeyTLSOrigin()
}
func (s *shell) hostAPIKeyTLSOrigin() bool {
	canonical, host, err := canonicalLoopbackOrigin(s.origin)
	return err == nil && canonical == s.origin && host == s.host && strings.HasPrefix(canonical, "https://")
}
func (s *hostAPIKeyState) remember(browser string, value protocol.HostAPIKeyStatusResponse, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inspections == nil {
		s.inspections = make(map[string]hostAPIKeyInspection)
	}
	for key, inspection := range s.inspections {
		if !now.Before(inspection.expires) {
			delete(s.inspections, key)
		}
	}
	delete(s.inspections, browser)
	// Stale/revoked browsers may retain metadata briefly. Bound storage and fail
	// closed instead of retaining unbounded inspection tickets.
	if len(s.inspections) >= maxBrowsers || !value.APIKeySupported {
		return
	}
	s.inspections[browser] = hostAPIKeyInspection{provider: value.ProviderID, revision: value.Revision, replace: value.ReplaceRequired, expires: now.Add(5 * time.Minute)}
}
func (s *hostAPIKeyState) consume(browser, provider, revision string, replace bool, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	inspection, ok := s.inspections[browser]
	delete(s.inspections, browser)
	return ok && inspection.provider == provider && inspection.revision == revision && now.Before(inspection.expires) && (!inspection.replace || replace)
}

func (s *hostAPIKeyState) forget(browser string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.inspections, browser)
}
