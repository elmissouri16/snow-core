package rpc

import (
	"context"
	"errors"
	"github.com/elmissouri16/snow-core/internal/agent"
	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/worktree"
	"github.com/elmissouri16/snow-core/pkg/protocol"
	"strings"
)

func rpcErrorCode(err error) string {
	switch {
	case errors.Is(err, agent.ErrSessionReasoningUnknown):
		return protocol.RPCSessionReasoningUnknownErrorCode
	case errors.Is(err, errSessionReasoningRejected):
		return protocol.RPCSessionReasoningRejectedErrorCode
	case errors.Is(err, app.ErrHistoryControlUnknown):
		return protocol.RPCHistoryControlUnknownErrorCode
	case errors.Is(err, app.ErrHistoryControlRejected):
		return protocol.RPCHistoryControlRejectedErrorCode
	case errors.Is(err, app.ErrCompactionOutcomeUnknown):
		return protocol.RPCCompactionUnknownErrorCode
	case errors.Is(err, app.ErrCompactionRejected):
		return protocol.RPCCompactionRejectedErrorCode
	case errors.Is(err, agent.ErrManagedSteerStale):
		return protocol.RPCManagedSteerStaleErrorCode
	case errors.Is(err, agent.ErrManagedSteerRejected):
		return protocol.RPCManagedSteerRejectedErrorCode
	case errors.Is(err, app.ErrGoalRunOutcomeUnknown):
		return protocol.RPCGoalRunUnknownErrorCode
	case errors.Is(err, app.ErrGoalRunRejected):
		return protocol.RPCGoalRunRejectedErrorCode
	case errors.Is(err, app.ErrBranchRestoreUnknown):
		return protocol.RPCBranchRestoreUnknownErrorCode
	case errors.Is(err, app.ErrBranchRestoreRejected):
		return protocol.RPCBranchRestoreRejectedErrorCode
	case errors.Is(err, errBranchVersionsRejected):
		return "branch_versions_rejected"
	case errors.Is(err, agent.ErrQueueUnknown):
		return protocol.RPCQueueUnknownErrorCode
	case errors.Is(err, agent.ErrQueueStale):
		return protocol.RPCQueueStaleErrorCode
	case errors.Is(err, agent.ErrQueueRejected):
		return protocol.RPCQueueRejectedErrorCode
	case errors.Is(err, app.ErrMessageEditOutcomeUnknown):
		return protocol.RPCMessageEditUnknownErrorCode
	case errors.Is(err, errMessageEditRejected):
		return protocol.RPCMessageEditRejectedErrorCode
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "canceled"
	case errors.Is(err, session.ErrDestinationExists), errors.Is(err, worktree.ErrDestinationExists):
		return "destination_exists"
	case errors.Is(err, worktree.ErrNotRepository):
		return "not_git_repository"
	case errors.Is(err, worktree.ErrDirty):
		return "git_dirty"
	case errors.Is(err, worktree.ErrUnsafeDestination), errors.Is(err, session.ErrInvalidForkBoundary):
		return "invalid"
	case errors.Is(err, session.ErrNotFound):
		return "not_found"
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "subagents are active"):
		return "subagents_active"
	case strings.Contains(message, "while running"), strings.Contains(message, "active session"):
		return "session_busy"
	case strings.Contains(message, "does not support"):
		return "unsupported"
	case strings.Contains(message, "already exists"), strings.Contains(message, "conflict"):
		return "conflict"
	case strings.HasPrefix(message, "worktree:"), strings.Contains(message, "git "):
		return "git_failure"
	default:
		return "invalid"
	}
}
