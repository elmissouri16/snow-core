package rpc

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/internal/diagnostics"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const (
	maxAuthJobs          = 8
	maxAuthProgressItems = 16
	maxAuthTextBytes     = 4 << 10
	maxAuthSecretBytes   = 64 << 10
	maxAuthParamBytes    = 8 << 10
	maxWorkspaceIDs      = 32
)

type authLoginJob struct {
	mu         sync.Mutex
	id         string
	providerID string
	method     string
	state      protocol.RPCAuthLoginState
	progress   []protocol.RPCAuthProgress
	status     *protocol.RPCAuthStatus
	error      string
	cancel     context.CancelFunc
}

type authLoginParams struct {
	AllowedWorkspaceIDs []string `json:"allowed_workspace_ids,omitempty"`
	ProfileID           string   `json:"profile_id,omitempty"`
	BaseURL             string   `json:"base_url,omitempty"`
}

func isAuthCommand(command string) bool {
	switch command {
	case "auth_providers", "auth_login_start", "auth_login_status", "auth_login_cancel", "auth_logout", "auth_profile_set":
		return true
	default:
		return false
	}
}

func (s *Server) handleAuthCommand(ctx context.Context, req Request) error {
	if req.Secret != "" && req.Type != "auth_login_start" && req.Type != "auth_profile_set" {
		return errors.New("auth: secret is accepted only by auth_login_start and auth_profile_set")
	}
	switch req.Type {
	case "auth_providers":
		if req.Provider != "" || req.Method != "" || req.Secret != "" || len(req.Params) != 0 {
			return errors.New("auth_providers does not accept provider, method, secret, or params")
		}
		return s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: s.authProviders(ctx)})
	case "auth_login_start", "auth_profile_set":
		job, err := s.startAuthLogin(ctx, req)
		if err != nil {
			return err
		}
		return s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: job})
	case "auth_login_status":
		jobID, err := authJobID(req)
		if err != nil {
			return err
		}
		job, err := s.authJob(jobID)
		if err != nil {
			return err
		}
		return s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: job.snapshot()})
	case "auth_login_cancel":
		jobID, err := authJobID(req)
		if err != nil {
			return err
		}
		job, err := s.authJob(jobID)
		if err != nil {
			return err
		}
		job.cancelLogin()
		return s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: job.snapshot()})
	case "auth_logout":
		providerID, err := authProviderID(req)
		if err != nil {
			return err
		}
		if req.Method != "" || len(req.Params) != 0 {
			return errors.New("auth_logout accepts only provider")
		}
		if err := s.app.Logout(ctx, providerID); err != nil {
			return err
		}
		status, _ := s.app.AuthStatus(ctx, providerID)
		return s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: protocol.RPCAuthLogoutResult{ProviderID: providerID, Status: rpcAuthStatus(status)}})
	default:
		return fmt.Errorf("unknown authentication command %q", req.Type)
	}
}

func (s *Server) authProviders(ctx context.Context) protocol.RPCAuthProviderList {
	descriptors := s.app.AuthProviders()
	providers := make([]protocol.RPCAuthProvider, 0, len(descriptors))
	for _, descriptor := range descriptors {
		status, _ := s.app.AuthStatus(ctx, descriptor.ProviderID)
		methods := make([]protocol.RPCAuthMethod, len(descriptor.Methods))
		for i, method := range descriptor.Methods {
			methods[i] = protocol.RPCAuthMethod{ID: method.ID, DisplayName: method.DisplayName, Kind: string(method.Kind)}
		}
		kinds := make([]string, len(descriptor.Kinds))
		for i, kind := range descriptor.Kinds {
			kinds[i] = string(kind)
		}
		providers = append(providers, protocol.RPCAuthProvider{
			ProviderID: descriptor.ProviderID, DisplayName: descriptor.DisplayName, Required: descriptor.Required,
			Kinds: kinds, Environment: cloneAuthSequence(descriptor.Environment), Methods: methods,
			Status: rpcAuthStatus(status),
		})
	}
	return protocol.RPCAuthProviderList{Providers: providers}
}

func cloneAuthSequence[T any](values []T) []T {
	cloned := slices.Clone(values)
	if cloned == nil {
		return []T{}
	}
	return cloned
}

func rpcAuthStatus(status auth.Status) protocol.RPCAuthStatus {
	expiresAt := int64(0)
	if !status.ExpiresAt.IsZero() {
		expiresAt = status.ExpiresAt.Unix()
	}
	return protocol.RPCAuthStatus{
		ProviderID: status.ProviderID, State: string(status.State), Method: string(status.Method),
		Refreshable: status.Refreshable, ExpiresAt: expiresAt, AccountID: boundedAuthText(status.AccountID),
		Summary: boundedAuthText(diagnostics.RedactText(status.Summary)),
	}
}

func (s *Server) startAuthLogin(parent context.Context, req Request) (protocol.RPCAuthLoginJob, error) {
	providerID, err := authProviderID(req)
	if err != nil {
		return protocol.RPCAuthLoginJob{}, err
	}
	if len(req.Secret) > maxAuthSecretBytes {
		return protocol.RPCAuthLoginJob{}, fmt.Errorf("auth: secret exceeds %d byte limit", maxAuthSecretBytes)
	}
	if len(req.Params) > maxAuthParamBytes {
		return protocol.RPCAuthLoginJob{}, fmt.Errorf("auth: params exceed %d byte limit", maxAuthParamBytes)
	}
	var params authLoginParams
	if len(req.Params) != 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return protocol.RPCAuthLoginJob{}, fmt.Errorf("%s params: %w", req.Type, err)
		}
	}
	if len(params.AllowedWorkspaceIDs) > maxWorkspaceIDs {
		return protocol.RPCAuthLoginJob{}, fmt.Errorf("auth: at most %d allowed workspace IDs are accepted", maxWorkspaceIDs)
	}
	for i := range params.AllowedWorkspaceIDs {
		params.AllowedWorkspaceIDs[i] = strings.TrimSpace(params.AllowedWorkspaceIDs[i])
		if params.AllowedWorkspaceIDs[i] == "" || len(params.AllowedWorkspaceIDs[i]) > 256 {
			return protocol.RPCAuthLoginJob{}, errors.New("auth: allowed workspace IDs must be non-empty and at most 256 bytes")
		}
	}
	profile := req.Type == "auth_profile_set" || strings.TrimSpace(params.BaseURL) != ""
	method := strings.TrimSpace(req.Method)
	if profile {
		if method == "" {
			method = "api_key"
		}
		if method != "api_key" {
			return protocol.RPCAuthLoginJob{}, errors.New("auth_profile_set supports only api_key")
		}
		if strings.TrimSpace(params.BaseURL) == "" {
			return protocol.RPCAuthLoginJob{}, errors.New("auth_profile_set requires params.base_url")
		}
		if strings.TrimSpace(params.ProfileID) != "" && strings.TrimSpace(params.ProfileID) != providerID {
			return protocol.RPCAuthLoginJob{}, errors.New("auth_profile_set provider and params.profile_id must match")
		}
	} else {
		descriptor, err := s.app.AuthService.Descriptor(providerID)
		if err != nil {
			return protocol.RPCAuthLoginJob{}, err
		}
		if method == "" && len(descriptor.Methods) > 0 {
			method = descriptor.Methods[0].ID
		}
		validMethod := false
		var kind auth.CredentialType
		for _, candidate := range descriptor.Methods {
			if candidate.ID == method {
				validMethod = true
				kind = candidate.Kind
				break
			}
		}
		if !validMethod {
			return protocol.RPCAuthLoginJob{}, fmt.Errorf("auth: provider %q does not support login method %q", providerID, method)
		}
		if kind == auth.CredentialAPIKey && strings.TrimSpace(req.Secret) == "" {
			return protocol.RPCAuthLoginJob{}, errors.New("auth: api_key login requires secret")
		}
		if kind != auth.CredentialAPIKey && req.Secret != "" {
			return protocol.RPCAuthLoginJob{}, errors.New("auth: secret is accepted only for api_key login")
		}
	}

	s.authMu.Lock()
	s.evictFinishedAuthJobsLocked()
	for _, existing := range s.authJobs {
		if existing.running() {
			s.authMu.Unlock()
			return protocol.RPCAuthLoginJob{}, errors.New("auth: another login operation is already running")
		}
	}
	if len(s.authJobs) >= maxAuthJobs {
		s.authMu.Unlock()
		return protocol.RPCAuthLoginJob{}, errors.New("auth: login job limit reached")
	}
	jobID := fmt.Sprintf("auth-%d", s.authSerial.Add(1))
	ctx, cancel := context.WithCancel(parent)
	job := &authLoginJob{id: jobID, providerID: providerID, method: method, state: protocol.RPCAuthLoginRunning, cancel: cancel}
	s.authJobs[jobID] = job
	s.authMu.Unlock()

	secret := req.Secret
	s.authWG.Go(func() {
		interaction := &rpcAuthInteraction{job: job, secret: secret}
		var status auth.Status
		var loginErr error
		if profile {
			status, loginErr = s.app.ConfigureOpenAICompatibleAuth(ctx, providerID, params.BaseURL, secret)
		} else {
			request := auth.LoginRequest{Method: method, Params: map[string][]string{"allowed_workspace_id": append([]string(nil), params.AllowedWorkspaceIDs...)}}
			status, loginErr = s.app.Login(ctx, providerID, request, interaction)
		}
		interaction.clearSecret()
		job.finish(ctx, status, loginErr, secret)
	})
	return job.snapshot(), nil
}

func authProviderID(req Request) (string, error) {
	providerID := strings.TrimSpace(req.Provider)
	if providerID == "" {
		return "", fmt.Errorf("%s requires provider", req.Type)
	}
	if len(providerID) > 256 {
		return "", errors.New("auth: provider exceeds 256 byte limit")
	}
	return providerID, nil
}

func authJobID(req Request) (string, error) {
	if req.Provider != "" || req.Method != "" || req.Secret != "" {
		return "", fmt.Errorf("%s accepts only params.job_id", req.Type)
	}
	var params struct {
		JobID string `json:"job_id"`
	}
	if len(req.Params) == 0 {
		return "", fmt.Errorf("%s requires params.job_id", req.Type)
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return "", fmt.Errorf("%s params: %w", req.Type, err)
	}
	params.JobID = strings.TrimSpace(params.JobID)
	if params.JobID == "" || len(params.JobID) > 128 {
		return "", fmt.Errorf("%s requires a valid params.job_id", req.Type)
	}
	return params.JobID, nil
}

func (s *Server) authJob(jobID string) (*authLoginJob, error) {
	s.authMu.Lock()
	defer s.authMu.Unlock()
	job := s.authJobs[jobID]
	if job == nil {
		return nil, fmt.Errorf("auth: login job %q not found", jobID)
	}
	return job, nil
}

func (s *Server) evictFinishedAuthJobsLocked() {
	for id, job := range s.authJobs {
		if len(s.authJobs) < maxAuthJobs {
			return
		}
		if !job.running() {
			delete(s.authJobs, id)
		}
	}
}

func (s *Server) cancelAuthJobs() {
	s.authMu.Lock()
	jobs := make([]*authLoginJob, 0, len(s.authJobs))
	for _, job := range s.authJobs {
		jobs = append(jobs, job)
	}
	s.authMu.Unlock()
	for _, job := range jobs {
		job.cancelLogin()
	}
}

func (j *authLoginJob) running() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.state == protocol.RPCAuthLoginRunning
}

func (j *authLoginJob) snapshot() protocol.RPCAuthLoginJob {
	j.mu.Lock()
	defer j.mu.Unlock()
	progress := cloneAuthSequence(j.progress)
	var status *protocol.RPCAuthStatus
	if j.status != nil {
		status = new(*j.status)
	}
	return protocol.RPCAuthLoginJob{JobID: j.id, ProviderID: j.providerID, Method: j.method, State: j.state, Progress: progress, Status: status, Error: j.error}
}

func (j *authLoginJob) addProgress(progress protocol.RPCAuthProgress) {
	progress.Kind = boundedAuthText(progress.Kind)
	progress.Message = boundedAuthText(diagnostics.RedactText(progress.Message))
	progress.URL = boundedAuthText(progress.URL)
	progress.UserCode = boundedAuthText(progress.UserCode)
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.state != protocol.RPCAuthLoginRunning {
		return
	}
	if len(j.progress) == maxAuthProgressItems {
		copy(j.progress, j.progress[1:])
		j.progress = j.progress[:maxAuthProgressItems-1]
	}
	j.progress = append(j.progress, progress)
}

func (j *authLoginJob) cancelLogin() {
	j.mu.Lock()
	if j.state == protocol.RPCAuthLoginRunning {
		j.state = protocol.RPCAuthLoginCanceled
		j.error = "authentication canceled"
		j.cancel()
	}
	j.mu.Unlock()
}

func (j *authLoginJob) finish(ctx context.Context, status auth.Status, err error, secret string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.state != protocol.RPCAuthLoginRunning {
		return
	}
	if err != nil {
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			j.state = protocol.RPCAuthLoginCanceled
			j.error = "authentication canceled"
			return
		}
		j.state = protocol.RPCAuthLoginFailed
		message := diagnostics.RedactText(err.Error())
		if len(secret) >= 4 {
			message = strings.ReplaceAll(message, secret, "[REDACTED CREDENTIAL]")
		}
		j.error = boundedAuthText(message)
		return
	}
	rpcStatus := rpcAuthStatus(status)
	j.status = &rpcStatus
	j.state = protocol.RPCAuthLoginCompleted
	j.error = ""
}

type rpcAuthInteraction struct {
	mu     sync.Mutex
	job    *authLoginJob
	secret string
}

func (i *rpcAuthInteraction) Prompt(_ context.Context, prompt auth.Prompt) (auth.Response, error) {
	if prompt.Kind != auth.PromptSecret {
		return auth.Response{}, auth.ErrInteractionUnavailable
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.secret == "" {
		return auth.Response{}, auth.ErrInteractionUnavailable
	}
	value := i.secret
	i.secret = ""
	return auth.Response{Value: value}, nil
}

func (i *rpcAuthInteraction) OpenURL(_ context.Context, target string) error {
	i.job.addProgress(protocol.RPCAuthProgress{Kind: "open_url", Message: "Open this URL in a browser to continue authentication", URL: target})
	return nil
}

func (i *rpcAuthInteraction) Progress(progress auth.Progress) {
	i.job.addProgress(protocol.RPCAuthProgress{Kind: progress.Kind, Message: progress.Message, URL: progress.URL, UserCode: progress.UserCode})
}

func (*rpcAuthInteraction) PromptAvailable() bool { return false }

func (i *rpcAuthInteraction) clearSecret() {
	i.mu.Lock()
	i.secret = ""
	i.mu.Unlock()
}

func boundedAuthText(value string) string {
	value = strings.ToValidUTF8(value, "�")
	if len(value) <= maxAuthTextBytes {
		return value
	}
	value = value[:maxAuthTextBytes]
	for !utf8.ValidString(value) {
		_, size := utf8.DecodeLastRuneInString(value)
		if size == 0 {
			break
		}
		value = value[:len(value)-size]
	}
	return value + "…"
}
