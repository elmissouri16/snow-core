//go:build darwin || linux

package web

import (
	"context"
	"encoding/json/v2"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/elmissouri16/snow-core/pkg/agentclient/process"
	clientrpc "github.com/elmissouri16/snow-core/pkg/agentclient/rpc"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// WorkerProjectOperations has a dedicated one-job admission slot, independent
// of WorkerControl's two short settings workers. Git descendants are supervised
// by the trusted host-operation core (group + liveness pipe), NOT process.Worker.
type WorkerProjectOperations struct {
	executable string
	managerDir string
	env        []string
	slot       chan struct{}
	start      func(context.Context, process.Options) (*process.Worker, error)
}

func NewWorkerProjectOperations(executable, managerDir string) *WorkerProjectOperations {
	env, err := freezeWorkerEnvironment("")
	if err != nil {
		executable = ""
	} // Open fails closed before spawning.
	return &WorkerProjectOperations{executable: executable, managerDir: managerDir, env: env, slot: make(chan struct{}, 1), start: process.Start}
}
func (b *WorkerProjectOperations) Open(ctx context.Context, op ProjectOperation) (ProjectOperationWorker, error) {
	kind := op.Kind
	if !matchesDirectoryIdentity(op.Parent) || !filepath.IsAbs(b.executable) || !filepath.IsAbs(b.managerDir) || (kind != "create" && kind != "clone") {
		return nil, ErrOperationUnavailable
	}
	select {
	case b.slot <- struct{}{}:
	default:
		return nil, ErrOperationBusy
	}
	release := func() { <-b.slot }
	// Keep the transport alive during explicit cancellation long enough to receive
	// cleanup confirmation. The manager still owns and joins this bounded child.
	lifetime, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Minute+6*time.Second)
	stop := context.AfterFunc(ctx, cancel)
	worker, err := b.start(lifetime, process.Options{Executable: b.executable, Args: []string{"--mode", "rpc", "--rpc-startup", "control"}, Dir: op.Parent.Path, Env: b.env,
		RPC: clientrpc.Options{MaxFrameBytes: 64 << 10, MaxPending: 2, EventBuffer: 4, HandshakeTimeout: 3 * time.Second, WriteTimeout: time.Second}, ShutdownTimeout: time.Second})
	stop()
	if err != nil {
		cancel()
		release()
		return nil, ErrOperationUnavailable
	}
	capability := "host_project_create_v1"
	if kind == "clone" {
		capability = "host_project_clone_v1"
	}
	caps := worker.Client.Ready().Capabilities
	if !matchesDirectoryIdentity(op.Parent) || !slices.Contains(caps, "runtime_free_control") || !slices.Contains(caps, capability) {
		_ = worker.Close()
		cancel()
		release()
		return nil, ErrOperationUnavailable
	}
	w := &rpcProjectOperationWorker{worker: worker, kind: kind, cancel: cancel, release: release, completions: make(chan protocol.RPCProjectCompleted, 1), drained: make(chan struct{})}
	w.close = sync.OnceValue(func() error { err := worker.Close(); cancel(); <-w.drained; release(); return err })
	go w.readEvents()
	return w, nil
}

type rpcProjectOperationWorker struct {
	worker         *process.Worker
	kind           string
	cancel         context.CancelFunc
	release        func()
	close          func() error
	completions    chan protocol.RPCProjectCompleted
	drained        chan struct{}
	operation      ProjectOperation
	startRequestID string
	started        bool
	terminal       *ProjectOperationTerminal
}

func (w *rpcProjectOperationWorker) readEvents() {
	defer close(w.drained)
	defer close(w.completions)
	for event := range w.worker.Client.Events() {
		if event.ProjectCompleted != nil {
			select {
			case w.completions <- *event.ProjectCompleted:
			default:
			}
		}
	}
}
func (w *rpcProjectOperationWorker) call(ctx context.Context, command string, params any, result any) (string, error) {
	data, err := json.Marshal(params)
	if err != nil {
		return "", ErrOperationInvalid
	}
	response, err := w.worker.Client.Call(ctx, protocol.RPCRequest{Type: command, Params: data})
	if err != nil || !response.Success {
		return "", ErrOperationUnavailable
	}
	if result != nil {
		data, err = json.Marshal(response.Data)
		if err != nil || len(data) > 64<<10 || json.Unmarshal(data, result) != nil {
			return "", ErrOperationUnavailable
		}
	}
	return response.ID, nil
}
func wireOperationIdentity(d DirectoryIdentity) protocol.HostDirectoryIdentity {
	return protocol.HostDirectoryIdentity{Path: d.Path, Device: d.Device, Inode: d.Inode}
}
func managerOperationIdentity(d protocol.HostDirectoryIdentity) DirectoryIdentity {
	return DirectoryIdentity{Path: d.Path, Device: d.Device, Inode: d.Inode}
}
func (w *rpcProjectOperationWorker) Prepare(ctx context.Context, op ProjectOperation) (DirectoryIdentity, error) {
	if w.operation.ID != "" || op.Kind != w.kind {
		return DirectoryIdentity{}, ErrOperationConflict
	}
	// Reserve locally before dispatch: ambiguous prepare is never replayed.
	w.operation = op
	callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var result protocol.RPCProjectPrepared
	_, err := w.call(callCtx, "project_prepare", protocol.RPCProjectPrepareParams{OperationID: op.ID, Parent: wireOperationIdentity(op.Parent), Leaf: op.Name}, &result)
	if err != nil {
		return DirectoryIdentity{}, err
	}
	child := managerOperationIdentity(result.Child)
	if result.OperationID != op.ID || result.Leaf != op.Name || managerOperationIdentity(result.Parent) != op.Parent || child.Path != filepath.Join(op.Parent.Path, op.Name) || !validDirectoryIdentity(child) {
		return DirectoryIdentity{}, ErrOperationUnavailable
	}
	w.operation.Child = child
	return child, nil
}
func (w *rpcProjectOperationWorker) Start(ctx context.Context, op ProjectOperation) error {
	if w.started || w.operation.ID != op.ID || w.operation.Child != op.Child || !validDirectoryIdentity(op.Child) {
		return ErrOperationConflict
	}
	w.started = true
	if w.kind == "create" {
		// There is no external work after mkdir. The manager has durably acknowledged
		// its returned identity; Close below releases the prepared host handle.
		return nil
	}
	callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	id, err := w.call(callCtx, "project_clone_start", protocol.RPCProjectCloneStartParams{OperationID: op.ID, Child: wireOperationIdentity(op.Child), URL: op.Remote}, nil)
	if err != nil {
		return err
	}
	w.startRequestID = id
	return nil
}
func (w *rpcProjectOperationWorker) Wait(ctx context.Context) (ProjectOperationTerminal, error) {
	if w.terminal != nil {
		return *w.terminal, nil
	}
	if w.kind == "create" && w.started {
		result := ProjectOperationTerminal{State: "succeeded", Outcome: "observed"}
		w.terminal = &result
		return result, nil
	}
	select {
	case completion, ok := <-w.completions:
		if !ok || completion.Type != protocol.RPCTypeProjectCompleted || completion.OperationID != w.operation.ID || managerOperationIdentity(completion.Child) != w.operation.Child || w.startRequestID == "" || completion.RequestID != w.startRequestID {
			return ProjectOperationTerminal{}, ErrOperationUnavailable
		}
		result := ProjectOperationTerminal{Outcome: "observed"}
		switch completion.Status {
		case protocol.HostOperationSucceeded:
			result.State = "succeeded"
		case protocol.HostOperationCanceled:
			result.State = "canceled"
		case protocol.HostOperationFailed, protocol.HostOperationTimedOut, protocol.HostOperationOutputLimit:
			result.State = "failed"
		default:
			result.State = "needs_review"
			result.Outcome = "unknown"
		}
		w.terminal = &result
		return result, nil
	case <-ctx.Done():
		return ProjectOperationTerminal{}, ctx.Err()
	}
}
func (w *rpcProjectOperationWorker) Cancel(ctx context.Context) error {
	if w.operation.ID == "" {
		return nil
	}
	_, err := w.call(ctx, "project_cancel", protocol.RPCProjectCancelParams{OperationID: w.operation.ID}, nil)
	// A prepared destination has no descendant work before clone start. Its
	// cancellation ACK plus subsequent Close confirms retained cancellation.
	if err == nil && (w.kind == "create" || !w.started) {
		w.terminal = &ProjectOperationTerminal{State: "canceled", Outcome: "observed"}
	}
	return err
}
func (w *rpcProjectOperationWorker) Close() error { return w.close() }
