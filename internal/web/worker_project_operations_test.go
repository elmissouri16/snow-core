//go:build darwin || linux

package web

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestWorkerProjectOperationsCloneACKIsNotTerminal(t *testing.T) {
	w := &rpcProjectOperationWorker{kind: "clone", started: true, startRequestID: "2", completions: make(chan protocol.RPCProjectCompleted, 1), operation: ProjectOperation{ID: "operation", Child: DirectoryIdentity{Path: "/parent/child", Device: "1", Inode: "2"}}}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if _, err := w.Wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("start ACK became terminal: %v", err)
	}
	w.completions <- protocol.RPCProjectCompleted{Type: protocol.RPCTypeProjectCompleted, RequestID: "2", RPCProjectCompletion: protocol.RPCProjectCompletion{OperationID: "operation", Child: wireOperationIdentity(w.operation.Child), Status: protocol.HostOperationSucceeded}}
	terminal, err := w.Wait(t.Context())
	if err != nil || terminal.State != "succeeded" || terminal.Outcome != "observed" {
		t.Fatalf("terminal %+v %v", terminal, err)
	}
}
func TestWorkerProjectOperationsTerminalCorrelation(t *testing.T) {
	for _, mismatch := range []string{"request", "operation", "identity", "type", "unknown-status"} {
		t.Run(mismatch, func(t *testing.T) {
			w := &rpcProjectOperationWorker{kind: "clone", started: true, startRequestID: "2", completions: make(chan protocol.RPCProjectCompleted, 1), operation: ProjectOperation{ID: "operation", Child: DirectoryIdentity{Path: "/parent/child", Device: "1", Inode: "2"}}}
			frame := protocol.RPCProjectCompleted{Type: protocol.RPCTypeProjectCompleted, RequestID: "2", RPCProjectCompletion: protocol.RPCProjectCompletion{OperationID: "operation", Child: wireOperationIdentity(w.operation.Child), Status: protocol.HostOperationSucceeded}}
			switch mismatch {
			case "request":
				frame.RequestID = "3"
			case "operation":
				frame.OperationID = "other"
			case "identity":
				frame.Child.Inode = "9"
			case "type":
				frame.Type = "other"
			case "unknown-status":
				frame.Status = "future"
			}
			w.completions <- frame
			terminal, err := w.Wait(t.Context())
			if mismatch == "unknown-status" {
				if err != nil || terminal.State != "needs_review" || terminal.Outcome != "unknown" {
					t.Fatalf("unknown completion %+v %v", terminal, err)
				}
			} else if !errors.Is(err, ErrOperationUnavailable) {
				t.Fatalf("mismatched completion %+v %v", terminal, err)
			}
		})
	}
}
