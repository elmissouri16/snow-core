package web

import (
	"context"
	"encoding/json/v2"
	"errors"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/elmissouri16/snow-core/pkg/agentclient/process"
	clientrpc "github.com/elmissouri16/snow-core/pkg/agentclient/rpc"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

var (
	ErrHostControlBusy        = errors.New("Host settings are busy; retry explicitly")
	ErrHostControlUnavailable = errors.New("Host settings are unavailable; no runtime was activated")
	ErrHostControlConflict    = errors.New("Host settings changed or could not be saved; explicitly reload and review before retrying")
	ErrHostControlInvalid     = errors.New("Invalid host settings request")
)

// HostSettingsBackend has no activation, raw settings, or credential capability.
// Wiring: shell.hostSettings, registerHostSettingsRoutes, HostSettingsEnabled,
// template "host-settings", its React frontend, and host-settings.css.
type HostSettingsBackend interface {
	Defaults(context.Context, string, string) (HostSettings, error)
	UpdateDefaults(context.Context, string, string, protocol.HostDefaultsUpdateRequest) (HostSettings, error)
	ProviderStatus(context.Context) (protocol.HostProviderStatusResponse, error)
}

// WorkerControl retains no idle workers. Each explicit operation owns one child;
// at most two may run, and writes have a separate non-queued serialization gate.
// The same instance must be shared by every manager-originated defaults writer.
type WorkerControl struct {
	executable string
	managerDir string
	registry   *Registry
	env        []string
	slots      chan struct{}
	writes     sync.Mutex
	start      func(context.Context, process.Options) (*process.Worker, error)
}

// NewWorkerControl never starts an agent or reads configuration/default files.
// managerDir is fixed operator configuration, never a browser-selected path.
func NewWorkerControl(executable, managerDir string, registry *Registry) *WorkerControl {
	env, err := freezeWorkerEnvironment("")
	if err != nil {
		executable = ""
	} // fail closed; never inherit project-relative roots
	return &WorkerControl{executable: executable, managerDir: managerDir, registry: registry, env: env, slots: make(chan struct{}, 2), start: process.Start}
}

func (c *WorkerControl) project(ctx context.Context, scope, id string) (Project, error) {
	if scope == "global" && id == "" {
		return Project{}, nil
	}
	if scope != "project" || c.registry == nil || !validProjectID(id) {
		return Project{}, ErrHostControlInvalid
	}
	p, err := c.registry.Lookup(ctx, id)
	if err != nil || !p.Available {
		return Project{}, ErrHostControlUnavailable
	}
	return p, nil
}

func (c *WorkerControl) call(ctx context.Context, scope, id, command string, params any, result any) error {
	capability := "defaults_control"
	switch command {
	case "defaults_get", "defaults_update":
	case "provider_status_list":
		capability = "provider_status"
	default:
		return ErrHostControlInvalid
	}
	if !filepath.IsAbs(c.executable) || !filepath.IsAbs(c.managerDir) {
		return ErrHostControlUnavailable
	}
	timeout := 3 * time.Second
	if command == "defaults_update" {
		timeout = 5 * time.Second
		if !c.writes.TryLock() {
			return ErrHostControlBusy
		}
		defer c.writes.Unlock()
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	default:
		return ErrHostControlBusy
	}
	project, err := c.project(ctx, scope, id)
	if err != nil {
		return err
	}
	dir := c.managerDir
	if scope == "project" {
		dir = project.Path
	}
	switch p := params.(type) {
	case protocol.HostDefaultsRequest:
		p.CWD = project.Path
		params = p
	case protocol.HostDefaultsUpdateRequest:
		p.CWD = project.Path
		params = p
	}
	worker, err := c.start(ctx, process.Options{Executable: c.executable,
		Args: []string{"--mode", "rpc", "--rpc-startup", "control"}, Dir: dir, Env: c.env,
		RPC:             clientrpc.Options{MaxFrameBytes: 64 << 10, MaxPending: 1, EventBuffer: 1, HandshakeTimeout: timeout, WriteTimeout: time.Second},
		ShutdownTimeout: 100 * time.Millisecond})
	if err != nil {
		return ErrHostControlUnavailable
	}
	defer worker.Close()
	if scope == "project" {
		after, err := c.project(ctx, scope, id)
		if err != nil || after.Path != project.Path || after.device != project.device || after.inode != project.inode {
			return ErrHostControlUnavailable
		}
	}
	ready := worker.Client.Ready()
	if !slices.Contains(ready.Capabilities, "runtime_free_control") || !slices.Contains(ready.Capabilities, capability) {
		return ErrHostControlUnavailable
	}
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for range worker.Client.Events() {
		}
	}()
	defer func() { _ = worker.Close(); <-drained }()
	encoded, err := json.Marshal(params)
	if err != nil {
		return ErrHostControlInvalid
	}
	response, err := worker.Client.Call(ctx, protocol.RPCRequest{Type: command, Params: encoded})
	if err != nil {
		return ErrHostControlUnavailable
	}
	if !response.Success {
		if command == "defaults_update" {
			return ErrHostControlConflict
		}
		return ErrHostControlUnavailable
	}
	data, err := json.Marshal(response.Data)
	if err != nil || len(data) > 64<<10 || json.Unmarshal(data, result) != nil {
		return ErrHostControlUnavailable
	}
	if scope == "project" {
		after, err := c.project(ctx, scope, id)
		if err != nil || after.Path != project.Path || after.device != project.device || after.inode != project.inode {
			return ErrHostControlUnavailable
		}
	}
	return nil
}

func (c *WorkerControl) Defaults(ctx context.Context, scope, project string) (HostSettings, error) {
	var raw protocol.HostDefaultsResponse
	if err := c.call(ctx, scope, project, "defaults_get", protocol.HostDefaultsRequest{Scope: scope}, &raw); err != nil {
		return HostSettings{}, err
	}
	return projectHostSettings(raw, scope, project)
}

func (c *WorkerControl) UpdateDefaults(ctx context.Context, scope, project string, input protocol.HostDefaultsUpdateRequest) (HostSettings, error) {
	if input.Scope != scope || input.CWD != "" || !validHostPatch(input) {
		return HostSettings{}, ErrHostControlInvalid
	}
	var raw protocol.HostDefaultsResponse
	if err := c.call(ctx, scope, project, "defaults_update", input, &raw); err != nil {
		return HostSettings{}, err
	}
	return projectHostSettings(raw, scope, project)
}

func (c *WorkerControl) ProviderStatus(ctx context.Context) (protocol.HostProviderStatusResponse, error) {
	var raw protocol.HostProviderStatusResponse
	if err := c.call(ctx, "global", "", "provider_status_list", struct{}{}, &raw); err != nil {
		return protocol.HostProviderStatusResponse{}, err
	}
	return projectProviderStatus(raw)
}
