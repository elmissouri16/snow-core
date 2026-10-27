package web

import (
	"context"
	"strings"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// RuntimeAuthBackend exposes the worker's provider sign-in lifecycle to the
// browser. Everything runs inside the explicitly activated worker through its
// existing auth RPC commands; the manager never reads, stores, or forwards a
// credential except an API key it relays once to the worker.
type RuntimeAuthBackend interface {
	AuthProviders(ctx context.Context, projectID, instanceID string) (RuntimeAuth, error)
	AuthLoginStart(ctx context.Context, projectID, instanceID string, input RuntimeAuthLoginInput) (RuntimeAuthJob, error)
	AuthLoginStatus(ctx context.Context, projectID, instanceID, jobID string) (RuntimeAuthJob, error)
	AuthLoginCancel(ctx context.Context, projectID, instanceID, jobID string) (RuntimeAuthJob, error)
	AuthLogout(ctx context.Context, projectID, instanceID, providerID string) (RuntimeAuth, error)
}

// RuntimeAuthLoginInput carries one explicit login request. Secret is an API
// key typed by the user; it is passed straight to the worker and never kept.
type RuntimeAuthLoginInput struct {
	ProviderID string
	Method     string
	Secret     string
}

// RuntimeAuth is the secret-free provider credential inventory.
type RuntimeAuth struct {
	ProjectID  string                `json:"project_id"`
	InstanceID string                `json:"instance_id"`
	SessionID  string                `json:"session_id"`
	Providers  []RuntimeAuthProvider `json:"providers"`
}

type RuntimeAuthProvider struct {
	ID          string              `json:"id"`
	DisplayName string              `json:"display_name"`
	Required    bool                `json:"required"`
	Environment []string            `json:"environment"`
	Methods     []RuntimeAuthMethod `json:"methods"`
	Status      RuntimeAuthStatus   `json:"status"`
}

type RuntimeAuthMethod struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Kind        string `json:"kind"`
}

type RuntimeAuthStatus struct {
	State     string `json:"state"`
	Method    string `json:"method"`
	AccountID string `json:"account_id"`
	ExpiresAt int64  `json:"expires_at"`
	Summary   string `json:"summary"`
}

// RuntimeAuthJob is one pollable sign-in job. Progress holds the provider's
// instructions (a URL to open, a device code to enter); it never holds tokens.
type RuntimeAuthJob struct {
	ProjectID  string                `json:"project_id"`
	InstanceID string                `json:"instance_id"`
	SessionID  string                `json:"session_id"`
	JobID      string                `json:"job_id"`
	ProviderID string                `json:"provider_id"`
	Method     string                `json:"method"`
	State      string                `json:"state"`
	Progress   []RuntimeAuthProgress `json:"progress"`
	Status     *RuntimeAuthStatus    `json:"status,omitempty"`
	Error      string                `json:"error"`
}

type RuntimeAuthProgress struct {
	Kind     string `json:"kind"`
	Message  string `json:"message"`
	URL      string `json:"url"`
	UserCode string `json:"user_code"`
}

const (
	runtimeAuthProvidersLimit = 32
	runtimeAuthTextBytes      = 1024
	runtimeAuthSecretBytes    = 64 << 10
)

// authRuntime admits one auth control. Sign-in is an idle-only control like the
// other panels: it never interrupts a running turn, and a running login job is
// polled through the same gate so instance retirement fences stale polls.
func (m *RuntimeManager) authRuntime(ctx context.Context, projectID, instanceID string) (*liveRuntime, error) {
	r, err := m.controlRuntime(ctx, projectID, instanceID)
	if err != nil {
		return nil, err
	}
	if err := r.idle(); err != nil {
		r.control.Unlock()
		return nil, err
	}
	if !r.supports("authentication") {
		r.control.Unlock()
		return nil, ErrRuntimeInvalid
	}
	return r, nil
}

func (m *RuntimeManager) AuthProviders(ctx context.Context, projectID, instanceID string) (RuntimeAuth, error) {
	r, err := m.authRuntime(ctx, projectID, instanceID)
	if err != nil {
		return RuntimeAuth{}, err
	}
	defer r.control.Unlock()
	var list protocol.RPCAuthProviderList
	if err := r.call(protocol.RPCRequest{Type: "auth_providers"}, nil, &list); err != nil {
		return RuntimeAuth{}, err
	}
	return m.authInventory(r, projectID, instanceID, list)
}

func (m *RuntimeManager) authInventory(r *liveRuntime, projectID, instanceID string, list protocol.RPCAuthProviderList) (RuntimeAuth, error) {
	if err := m.revalidate(r, projectID, instanceID); err != nil {
		return RuntimeAuth{}, err
	}
	result := projectRuntimeAuth(list)
	r.mu.Lock()
	result.ProjectID, result.InstanceID, result.SessionID = projectID, instanceID, r.snapshot.SessionID
	r.mu.Unlock()
	return result, nil
}

func (m *RuntimeManager) AuthLoginStart(ctx context.Context, projectID, instanceID string, input RuntimeAuthLoginInput) (RuntimeAuthJob, error) {
	if !runtimeIdentifier(input.ProviderID) || !runtimeIdentifier(input.Method) || len(input.Secret) > runtimeAuthSecretBytes || strings.ContainsRune(input.Secret, 0) {
		return RuntimeAuthJob{}, ErrRuntimeInvalid
	}
	r, err := m.authRuntime(ctx, projectID, instanceID)
	if err != nil {
		return RuntimeAuthJob{}, err
	}
	defer r.control.Unlock()
	var job protocol.RPCAuthLoginJob
	request := protocol.RPCRequest{Type: "auth_login_start", Provider: input.ProviderID, Method: input.Method, Secret: input.Secret}
	if err := r.call(request, nil, &job); err != nil {
		return RuntimeAuthJob{}, err
	}
	return m.authJob(r, projectID, instanceID, job)
}

func (m *RuntimeManager) AuthLoginStatus(ctx context.Context, projectID, instanceID, jobID string) (RuntimeAuthJob, error) {
	return m.authJobCommand(ctx, projectID, instanceID, jobID, "auth_login_status")
}

func (m *RuntimeManager) AuthLoginCancel(ctx context.Context, projectID, instanceID, jobID string) (RuntimeAuthJob, error) {
	return m.authJobCommand(ctx, projectID, instanceID, jobID, "auth_login_cancel")
}

func (m *RuntimeManager) authJobCommand(ctx context.Context, projectID, instanceID, jobID, command string) (RuntimeAuthJob, error) {
	if !runtimeIdentifier(jobID) {
		return RuntimeAuthJob{}, ErrRuntimeInvalid
	}
	r, err := m.authRuntime(ctx, projectID, instanceID)
	if err != nil {
		return RuntimeAuthJob{}, err
	}
	defer r.control.Unlock()
	var job protocol.RPCAuthLoginJob
	params := struct {
		JobID string `json:"job_id"`
	}{jobID}
	if err := r.call(protocol.RPCRequest{Type: command}, params, &job); err != nil {
		return RuntimeAuthJob{}, err
	}
	return m.authJob(r, projectID, instanceID, job)
}

func (m *RuntimeManager) authJob(r *liveRuntime, projectID, instanceID string, job protocol.RPCAuthLoginJob) (RuntimeAuthJob, error) {
	if err := m.revalidate(r, projectID, instanceID); err != nil {
		return RuntimeAuthJob{}, err
	}
	if job.State == protocol.RPCAuthLoginCompleted {
		// A completed sign-in may have replaced the startup placeholder model
		// inside the worker. Re-read the verified session so the composer's
		// model label and catalog follow it without a manual reload.
		r.mu.Lock()
		sessionID := r.snapshot.SessionID
		r.mu.Unlock()
		info, err := r.verifiedInfo(sessionID)
		if err != nil {
			return RuntimeAuthJob{}, err
		}
		r.mu.Lock()
		r.applyInfo(info)
		r.publishLocked()
		r.mu.Unlock()
		_ = r.refreshModels()
	}
	result := projectRuntimeAuthJob(job)
	r.mu.Lock()
	result.ProjectID, result.InstanceID, result.SessionID = projectID, instanceID, r.snapshot.SessionID
	r.mu.Unlock()
	return result, nil
}

func (m *RuntimeManager) AuthLogout(ctx context.Context, projectID, instanceID, providerID string) (RuntimeAuth, error) {
	if !runtimeIdentifier(providerID) {
		return RuntimeAuth{}, ErrRuntimeInvalid
	}
	r, err := m.authRuntime(ctx, projectID, instanceID)
	if err != nil {
		return RuntimeAuth{}, err
	}
	defer r.control.Unlock()
	if err := r.call(protocol.RPCRequest{Type: "auth_logout", Provider: providerID}, nil, nil); err != nil {
		return RuntimeAuth{}, err
	}
	var list protocol.RPCAuthProviderList
	if err := r.call(protocol.RPCRequest{Type: "auth_providers"}, nil, &list); err != nil {
		return RuntimeAuth{}, err
	}
	return m.authInventory(r, projectID, instanceID, list)
}

func projectRuntimeAuth(list protocol.RPCAuthProviderList) RuntimeAuth {
	result := RuntimeAuth{Providers: []RuntimeAuthProvider{}}
	for _, provider := range list.Providers[:min(len(list.Providers), runtimeAuthProvidersLimit)] {
		if !runtimeIdentifier(provider.ProviderID) {
			continue
		}
		entry := RuntimeAuthProvider{
			ID: provider.ProviderID, DisplayName: runtimeText(provider.DisplayName, 256), Required: provider.Required,
			Environment: []string{}, Methods: []RuntimeAuthMethod{}, Status: projectRuntimeAuthStatus(provider.Status),
		}
		for _, name := range provider.Environment {
			if runtimeIdentifier(name) {
				entry.Environment = append(entry.Environment, name)
			}
		}
		for _, method := range provider.Methods {
			if !runtimeIdentifier(method.ID) {
				continue
			}
			entry.Methods = append(entry.Methods, RuntimeAuthMethod{ID: method.ID, DisplayName: runtimeText(method.DisplayName, 256), Kind: runtimeText(method.Kind, 64)})
		}
		result.Providers = append(result.Providers, entry)
	}
	return result
}

func projectRuntimeAuthStatus(status protocol.RPCAuthStatus) RuntimeAuthStatus {
	state := status.State
	switch state {
	case "missing", "configured", "expired", "invalid":
	default:
		state = "unknown"
	}
	return RuntimeAuthStatus{State: state, Method: runtimeText(status.Method, 64), AccountID: runtimeText(status.AccountID, 256), ExpiresAt: status.ExpiresAt, Summary: runtimeText(status.Summary, runtimeAuthTextBytes)}
}

func projectRuntimeAuthJob(job protocol.RPCAuthLoginJob) RuntimeAuthJob {
	state := string(job.State)
	switch job.State {
	case protocol.RPCAuthLoginRunning, protocol.RPCAuthLoginCompleted, protocol.RPCAuthLoginFailed, protocol.RPCAuthLoginCanceled:
	default:
		state = "unknown"
	}
	result := RuntimeAuthJob{JobID: runtimeText(job.JobID, 128), ProviderID: runtimeText(job.ProviderID, 256), Method: runtimeText(job.Method, 64), State: state, Progress: []RuntimeAuthProgress{}, Error: runtimeText(job.Error, runtimeAuthTextBytes)}
	for _, progress := range job.Progress {
		url := strings.TrimSpace(progress.URL)
		if !strings.HasPrefix(url, "https://") {
			url = ""
		}
		result.Progress = append(result.Progress, RuntimeAuthProgress{Kind: runtimeText(progress.Kind, 64), Message: runtimeText(progress.Message, runtimeAuthTextBytes), URL: runtimeText(url, 2048), UserCode: runtimeText(progress.UserCode, 64)})
	}
	if job.Status != nil {
		status := projectRuntimeAuthStatus(*job.Status)
		result.Status = &status
	}
	return result
}

var _ RuntimeAuthBackend = (*RuntimeManager)(nil)
