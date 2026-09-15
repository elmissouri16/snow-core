package rpc

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"path/filepath"
	"strconv"
	"time"

	"github.com/elmissouri16/snow-core/internal/hostops"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const controlJobTimeout = 10 * time.Minute
const controlJobJoinTimeout = 10 * time.Second

// HostOperationService is an optional, explicitly composed filesystem facade.
// Prepare must not start a process or network request. Handles must implement
// bounded cancellation/join; StartClone only allocates a gated handle.
type HostOperationService interface {
	Prepare(context.Context, protocol.RPCProjectPrepareParams) (PreparedHostOperation, error)
}

type PreparedHostOperation interface {
	Result() protocol.RPCProjectPrepared
	StartClone(context.Context, protocol.RPCProjectCloneStartParams) (HostCloneOperation, error)
	Close() error
}

type HostCloneOperation interface {
	Release()
	Cancel()
	Done() <-chan struct{}
	Completion() (protocol.RPCProjectCompletion, bool)
}

type controlHostJob struct {
	service      HostOperationService
	prepared     PreparedHostOperation
	clone        HostCloneOperation
	cancel       context.CancelFunc
	requestID    string
	identity     protocol.RPCProjectPrepared
	deadline     <-chan struct{}
	cleanup      *time.Timer
	joinTimedOut bool
}

func isControlHostCommand(command string) bool {
	return command == "project_prepare" || command == "project_clone_start" || command == "project_cancel"
}

func (j *controlHostJob) active() bool { return j.clone != nil }
func (j *controlHostJob) done() <-chan struct{} {
	if j.clone == nil {
		return nil
	}
	return j.clone.Done()
}

func (j *controlHostJob) respond(ctx context.Context, pin controlProjectPin, request controlRequest) (Response, func()) {
	response := Response{ID: request.ID, Type: "response", Command: request.Type}
	if request.ID == "" {
		return controlFailure(request.ID, request.Type, "invalid"), nil
	}
	var err error
	var release func()
	switch request.Type {
	case "project_prepare":
		var params protocol.RPCProjectPrepareParams
		err = json.Unmarshal(request.Params, &params, json.RejectUnknownMembers(true))
		if err == nil && (!validControlOperationID(params.OperationID) || !validControlIdentity(params.Parent) ||
			!hostops.ValidLeaf(params.Leaf) || !pin.matches("project", params.Parent.Path)) {
			err = hostops.ErrInvalid
		}
		if err == nil && j.prepared != nil {
			err = hostops.ErrClosed
		}
		if err == nil {
			opCtx, cancel := context.WithTimeout(ctx, controlOperationTimeout)
			j.prepared, err = j.service.Prepare(opCtx, params)
			cancel()
			if err == nil && j.prepared == nil {
				err = hostops.ErrUnavailable
			}
			if err == nil {
				result := j.prepared.Result()
				if result.OperationID != params.OperationID || result.Parent != params.Parent || result.Leaf != params.Leaf ||
					!validControlIdentity(result.Child) || result.Child.Path != filepath.Join(params.Parent.Path, params.Leaf) {
					_ = j.prepared.Close()
					j.prepared = nil
					err = hostops.ErrUnavailable
				} else {
					j.identity = result
					response.Data = result
				}
			}
		}
	case "project_clone_start":
		var params protocol.RPCProjectCloneStartParams
		err = json.Unmarshal(request.Params, &params, json.RejectUnknownMembers(true))
		if err == nil && (!validControlOperationID(params.OperationID) || !validControlIdentity(params.Child) || !hostops.ValidCloneURL(params.URL)) {
			err = hostops.ErrInvalid
		}
		if err == nil && (j.prepared == nil || j.active() || params.OperationID != j.identity.OperationID || params.Child != j.identity.Child || !pin.matches("project", j.identity.Parent.Path)) {
			err = hostops.ErrClosed
		}
		if err == nil {
			jobCtx, cancel := context.WithTimeout(ctx, controlJobTimeout)
			clone, startErr := j.prepared.StartClone(jobCtx, params)
			err = startErr
			if err == nil && clone == nil {
				err = hostops.ErrUnavailable
			}
			if err != nil {
				cancel()
			} else {
				j.clone, j.cancel, j.requestID = clone, cancel, request.ID
				j.deadline = jobCtx.Done()
				response.Data = j.identity
				release = clone.Release
			}
		}
	case "project_cancel":
		var params protocol.RPCProjectCancelParams
		err = json.Unmarshal(request.Params, &params, json.RejectUnknownMembers(true))
		if err == nil && (!validControlOperationID(params.OperationID) || j.prepared == nil || params.OperationID != j.identity.OperationID) {
			err = hostops.ErrInvalid
		}
		if err == nil {
			response.Data = protocol.RPCProjectCancelParams{OperationID: j.identity.OperationID}
			if j.active() {
				j.clone.Cancel()
				j.cancel()
			} else {
				err = j.prepared.Close()
				j.prepared = nil
			}
		}
	}
	if err != nil {
		return controlHostFailure(request, err), nil
	}
	response.Success = true
	return response, release
}

func controlHostFailure(request controlRequest, err error) Response {
	code := "invalid"
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		code = "canceled"
	case errors.Is(err, hostops.ErrUnavailable):
		code = "unavailable"
	case errors.Is(err, hostops.ErrClosed):
		code = "busy"
	case errors.Is(err, hostops.ErrExists):
		code = "exists"
	case errors.Is(err, hostops.ErrIdentity):
		code = "identity_changed"
	}
	response := controlFailure(request.ID, request.Type, code)
	switch code {
	case "busy":
		response.Error = "host operation is not available in its current state"
	case "exists":
		response.Error = "host destination already exists"
	case "identity_changed":
		response.Error = "host directory identity changed"
	}
	return response
}

func (j *controlHostJob) complete(srv *Server) error {
	result, ok := j.clone.Completion()
	if !ok || result.OperationID != j.identity.OperationID || result.Child != j.identity.Child || !validControlTerminal(result.Status) {
		result = protocol.RPCProjectCompletion{OperationID: j.identity.OperationID, Child: j.identity.Child, Status: protocol.HostOperationCleanupFailed}
	}
	completed := protocol.RPCProjectCompleted{Type: protocol.RPCTypeProjectCompleted, RequestID: j.requestID, OperationID: result.OperationID, Child: result.Child, Status: result.Status}
	if err := j.close(); err != nil {
		return err
	}
	return writeControl(srv, completed)
}

func (j *controlHostJob) close() error {
	var err error
	if j.cleanup != nil {
		j.cleanup.Stop()
		j.cleanup = nil
	}
	j.deadline = nil
	if j.clone != nil {
		j.clone.Cancel()
		j.cancel()
		if j.joinTimedOut {
			err = errControlUnavailable
		} else {
			select {
			case <-j.clone.Done():
			case <-time.After(controlJobJoinTimeout):
				err = errControlUnavailable
			}
		}
		j.clone, j.cancel = nil, nil
	}
	if j.prepared != nil {
		if closeErr := j.prepared.Close(); closeErr != nil {
			err = errControlUnavailable
		}
		j.prepared = nil
	}
	return err
}

func validControlOperationID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, r := range id {
		if r < 33 || r > 126 || r == '/' || r == '\\' {
			return false
		}
	}
	return true
}

func validControlIdentity(identity protocol.HostDirectoryIdentity) bool {
	if !filepath.IsAbs(identity.Path) || filepath.Clean(identity.Path) != identity.Path || len(identity.Path) > 4096 {
		return false
	}
	for _, value := range []string{identity.Device, identity.Inode} {
		number, err := strconv.ParseUint(value, 10, 64)
		if err != nil || strconv.FormatUint(number, 10) != value {
			return false
		}
	}
	return identity.Inode != "0"
}

func validControlTerminal(status protocol.HostOperationStatus) bool {
	switch status {
	case protocol.HostOperationSucceeded, protocol.HostOperationFailed, protocol.HostOperationCanceled,
		protocol.HostOperationTimedOut, protocol.HostOperationOutputLimit, protocol.HostOperationCleanupFailed:
		return true
	}
	return false
}

func (j *controlHostJob) expiration() <-chan struct{} { return j.deadline }
func (j *controlHostJob) cleanupDeadline() <-chan time.Time {
	if j.cleanup == nil {
		return nil
	}
	return j.cleanup.C
}
func (j *controlHostJob) expired() {
	j.deadline = nil
	j.clone.Cancel()
	j.cleanup = time.NewTimer(controlJobJoinTimeout)
}
