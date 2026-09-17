package web

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net/http"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// RuntimeBackend is deliberately typed: browser input cannot name arbitrary RPC
// commands or supply execution flags. Runtime implementation owns no agent loop.
type RuntimeBackend interface {
	Open(context.Context, Project, string, string, string) (RuntimeSnapshot, error)
	Snapshot(string) (RuntimeSnapshot, bool)
	Prompt(context.Context, string, string, string) error
	Abort(context.Context, string, string) error
	ReplyPermission(context.Context, string, string, string, protocol.PermissionDecision) error
	ReplyInput(context.Context, string, string, protocol.UserInputResponse) error
	CloseProject(context.Context, string, string) error
	Close() error
}

// RuntimeSubscriber is an optional read-only transport capability. Subscribe
// atomically captures an owned snapshot and registers revision notifications.
// Subscriptions bind one instance and never activate or control workers.
type RuntimeSubscriber interface {
	Subscribe(projectID, instanceID string) (RuntimeSnapshot, RuntimeSubscription, error)
}

// RuntimeSubscription coalesces changes into a single wakeup, not an event log.
// Snapshot returns the latest owned projection, or false once its binding closes
// or rotates. Close is idempotent and only releases this observer.
type RuntimeSubscription interface {
	Changes() <-chan struct{}
	Snapshot() (RuntimeSnapshot, bool)
	Close()
}

func (s *shell) runtimeSnapshot(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.browser(r); !ok {
		http.Error(w, "Pair this browser to continue", http.StatusUnauthorized)
		return
	}
	if s.runtimes == nil || s.registry == nil {
		http.NotFound(w, r)
		return
	}
	if _, err := s.registry.Lookup(r.Context(), r.PathValue("project")); err != nil {
		http.NotFound(w, r)
		return
	}
	snapshot, ok := s.runtimes.Snapshot(r.PathValue("project"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	s.runtimeJSON(w, snapshot)
}
func (s *shell) runtimeJSON(w http.ResponseWriter, value any) {
	if snapshot, ok := value.(RuntimeSnapshot); ok {
		value = displaySnapshot(snapshot)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.MarshalWrite(w, value)
}
func runtimePublicError(err error) string {
	switch {
	case errors.Is(err, ErrRuntimeQueueReview):
		return "Copy and remove pending or held queue items before starting new work or changing chats. Closing the project explicitly discards its live-only queue."
	case errors.Is(err, ErrRuntimeBusy):
		return "This project is busy. No action was queued."
	case errors.Is(err, ErrRuntimeLimit):
		return "Two projects are already live. Close one before activating another."
	case errors.Is(err, ErrProjectInvalid):
		return "The host folder is unavailable or its identity changed."
	case errors.Is(err, ErrRuntimeInvalid):
		return "The action was not accepted. Check the current session and pending request."
	default:
		return "Runtime action failed. No retry was queued. Close and explicitly reopen the project if needed."
	}
}
func (s *shell) runtimeAction(w http.ResponseWriter, r *http.Request) {
	limit := int64(256 << 10)
	if r.PathValue("action") == "prompt-content" {
		limit = 4 << 20 // Bounded in-memory image/text content; no disk uploads.
	}
	if _, ok := s.authorizeFormLimit(w, r, limit); !ok {
		return
	}
	if s.registry == nil || s.runtimes == nil {
		http.Error(w, "Live sessions unavailable", http.StatusServiceUnavailable)
		return
	}
	if r.PathValue("action") == "open" || r.PathValue("action") == "switch" {
		if !s.projectControl.TryLock() {
			http.Error(w, "Project activation or removal is busy", http.StatusConflict)
			return
		}
		defer s.projectControl.Unlock()
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	project, err := s.registry.Lookup(ctx, r.PathValue("project"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	action := r.PathValue("action")
	instanceID := r.PostForm.Get("instance_id")
	// Closing/aborting must remain possible even if the host directory disappears.
	if action != "close" && action != "abort" && action != "cancel" && !project.Available {
		http.Error(w, "Host folder is missing or changed", http.StatusConflict)
		return
	}
	if action == "prompt-content" {
		s.runtimeComposerPrompt(ctx, w, r, project)
		return
	}
	if action == "skills" {
		s.runtimeSkillsAction(ctx, w, r, project)
		return
	}
	if action == "permission-mode" {
		s.runtimePermissionPolicyAction(ctx, w, r, project)
		return
	}
	if action == "queue-enqueue" || action == "queue-update" || action == "queue-remove" {
		s.runtimeQueueAction(ctx, w, r, project)
		return
	}
	if action == "message-regenerate-prepare" || action == "message-regenerate-commit" {
		s.runtimeMessageRegenerateAction(ctx, w, r, project)
		return
	}
	if action == "message-edit-prepare" || action == "message-edit-commit" {
		s.runtimeMessageEditAction(ctx, w, r, project)
		return
	}
	if reasoningAction(action) {
		s.runtimeReasoningAction(ctx, w, r, project)
		return
	}
	if historyControlAction(action) {
		s.runtimeHistoryControlAction(ctx, w, r, project)
		return
	}
	if action == "compaction-start" {
		s.runtimeCompactionAction(ctx, w, r, project)
		return
	}
	if action == "steer" {
		s.runtimeSteerAction(ctx, w, r, project)
		return
	}
	if goalAction(action) {
		s.runtimeGoalAction(ctx, w, r, project)
		return
	}
	if versionAction(action) {
		s.runtimeVersionAction(ctx, w, r, project)
		return
	}
	if workflowAction(action) {
		s.runtimeWorkflowAction(ctx, w, r, project)
		return
	}
	switch action {
	case "open":
		if !s.authorizeProjectActivation(ctx, w, r, project) {
			return
		}
		enableSkills := project.SkillsEnabled
		skillsBackend, supportsSkills := s.runtimes.(RuntimeSkillsBackend)
		if enableSkills && !supportsSkills {
			http.Error(w, "Skill-enabled activation unavailable", http.StatusServiceUnavailable)
			return
		}
		releaseReads, stopErr := s.sidebarReads.preempt(ctx, project.ID)
		if stopErr != nil {
			http.Error(w, "Background session read could not be stopped. No runtime was started", http.StatusConflict)
			return
		}
		defer releaseReads()
		var snapshot RuntimeSnapshot
		if enableSkills {
			snapshot, err = skillsBackend.OpenWithSkills(ctx, project, r.PostForm.Get("session_id"), r.PostForm.Get("provider"), r.PostForm.Get("model"), true)
		} else {
			snapshot, err = s.runtimes.Open(ctx, project, r.PostForm.Get("session_id"), r.PostForm.Get("provider"), r.PostForm.Get("model"))
		}
		if err == nil {
			s.runtimeJSON(w, snapshot)
			return
		}
	case "prompt":
		err = s.runtimes.Prompt(ctx, project.ID, instanceID, r.PostForm.Get("text"))
	case "cancel":
		backend, ok := s.runtimes.(RuntimeTurnCancelBackend)
		if !ok {
			http.Error(w, "Turn cancellation unavailable", http.StatusServiceUnavailable)
			return
		}
		err = backend.CancelTurn(ctx, project.ID, instanceID, r.PostForm.Get("cancel_token"))
	case "abort":
		err = s.runtimes.Abort(ctx, project.ID, instanceID)
	case "close":
		err = s.runtimes.CloseProject(ctx, project.ID, instanceID)
	case "permission":
		err = s.runtimes.ReplyPermission(ctx, project.ID, instanceID, r.PostForm.Get("request_id"), protocol.PermissionDecision(r.PostForm.Get("decision")))
	case "input":
		var answers []protocol.UserInputAnswer
		if decodeErr := json.Unmarshal([]byte(r.PostForm.Get("answers")), &answers, json.RejectUnknownMembers(true)); decodeErr != nil {
			http.Error(w, "Invalid answers", http.StatusBadRequest)
			return
		}
		err = s.runtimes.ReplyInput(ctx, project.ID, instanceID, protocol.UserInputResponse{RequestID: r.PostForm.Get("request_id"), Answers: answers})
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, runtimePublicError(err), http.StatusConflict)
		return
	}
	s.runtimeJSON(w, struct {
		Success bool `json:"success"`
	}{true})
}
