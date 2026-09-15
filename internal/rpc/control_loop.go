package rpc

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// ControlOptions contains trusted composition, never request-selected plugins
// or a generic dispatcher. Absent optional facades grant no extra capabilities.
type ControlOptions struct {
	HostOperations HostOperationService
	APIKeys        APIKeyControlService
	SessionDelete  SessionDeleteControlService
}

type controlInput struct {
	frame []byte
	err   error
}

// One input pump permits asynchronous job completion and cancellation without
// concurrent command execution. Its one-frame queue is bounded and it is joined
// after interrupting the already-validated input on every return path.
func controlInputPump(ctx context.Context, srv *Server) (<-chan controlInput, <-chan struct{}) {
	frames := make(chan controlInput, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(srv.in)
		scanner.Buffer(make([]byte, 4096), protocol.RPCControlMaxInputBytes+1)
		for scanner.Scan() {
			if len(scanner.Bytes()) == 0 {
				continue
			}
			select {
			case frames <- controlInput{frame: bytes.Clone(scanner.Bytes())}:
			case <-ctx.Done():
				return
			}
		}
		err := scanner.Err()
		if err == nil {
			err = io.EOF
		}
		select {
		case frames <- controlInput{err: err}:
		case <-ctx.Done():
		}
	}()
	return frames, done
}

func serveControlLoop(ctx context.Context, srv *Server, pin controlProjectPin, service ControlService, version string, options ControlOptions) (resultErr error) {
	loopCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(loopCtx, srv.interruptInput)
	defer stop()
	defer cancel()
	defer srv.interruptInput()
	ready := protocol.NewRPCControlReady(version)
	if options.HostOperations != nil {
		ready.Capabilities = append(ready.Capabilities, protocol.HostOperationsCapability, protocol.RPCProjectCreateCapability, protocol.RPCProjectCloneCapability)
	}
	if options.SessionDelete != nil {
		ready.Capabilities = append(ready.Capabilities, protocol.RPCSessionDeleteControlCapability)
	}
	if options.APIKeys != nil {
		ready.Capabilities = append(ready.Capabilities, controlAPIKeyCapability)
	}
	if err := writeControl(srv, ready); err != nil {
		return err
	}
	frames, inputDone := controlInputPump(loopCtx, srv)
	defer func() {
		cancel()
		srv.interruptInput()
		<-inputDone
	}()
	jobs := controlHostJob{service: options.HostOperations}
	defer func() {
		if err := jobs.close(); err != nil && resultErr == nil {
			resultErr = err
		}
	}()
	idle := time.NewTimer(controlReadTimeout)
	defer idle.Stop()
	for {
		var idleDone <-chan time.Time
		if !jobs.active() {
			idleDone = idle.C
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-idleDone:
			return errControlUnavailable
		case <-jobs.expiration():
			jobs.expired()
		case <-jobs.cleanupDeadline():
			jobs.joinTimedOut = true
			return errControlUnavailable
		case <-jobs.done():
			if err := jobs.complete(srv); err != nil {
				return err
			}
			idle.Reset(controlReadTimeout)
		case input := <-frames:
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if input.err == io.EOF {
				return nil
			}
			if input.err != nil {
				return errControlUnavailable
			}
			request, err := decodeControlRequest(input.frame)
			response := controlFailure("", "invalid", "invalid")
			var release func()
			if err == nil {
				if isControlHostCommand(request.Type) && jobs.service != nil {
					response, release = jobs.respond(loopCtx, pin, request)
				} else {
					opCtx, cancel := context.WithTimeout(loopCtx, controlOperationTimeout)
					if request.Type == "session_delete" && options.SessionDelete != nil {
						response = controlSessionDeleteResponse(opCtx, pin, options.SessionDelete, request)
					} else if isControlAPIKeyCommand(request.Type) && options.APIKeys != nil {
						response = controlAPIKeyResponse(opCtx, options.APIKeys, request)
					} else {
						response = controlResponse(opCtx, pin, service, request)
					}
					cancel()
				}
			}
			if err := writeControl(srv, response); err != nil {
				return err
			}
			// Release is intentionally after the complete successful ACK write.
			// Failed output reaches close(), which cancels/joins the gated job.
			if release != nil {
				release()
			}
			idle.Reset(controlReadTimeout)
		}
	}
}
