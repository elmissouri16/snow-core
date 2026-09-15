package web

import (
	"context"
	"net/http"
	"strconv"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type RuntimeGoalBackend interface {
	InspectGoal(context.Context, string, string, string, string) (RuntimeSnapshot, error)
	RunGoal(context.Context, string, string, RuntimeGoalRunInput) (RuntimeSnapshot, error)
}

func goalAction(action string) bool {
	return action == "goal-inspect" || action == "goal-start" || action == "goal-resume"
}

// Called only after paired-browser authentication, CSRF and registered project
// lookup. Forms are typed; no command, provider or permission override is accepted.
func (s *shell) runtimeGoalAction(ctx context.Context, w http.ResponseWriter, r *http.Request, project Project) {
	backend, ok := s.runtimes.(RuntimeGoalBackend)
	if !ok {
		http.Error(w, "Thread Goals are unavailable", http.StatusServiceUnavailable)
		return
	}
	action := r.PathValue("action")
	allowed := map[string]bool{"csrf": true, "instance_id": true, "session_id": true, "branch_id": true}
	if action != "goal-inspect" {
		allowed["expected_tip_id"] = true
		allowed["expected_goal_id"] = true
		allowed["expected_revision"] = true
	}
	if action == "goal-start" {
		allowed["objective"] = true
		allowed["token_budget"] = true
	}
	for key, values := range r.PostForm {
		if !allowed[key] || len(values) != 1 {
			http.Error(w, "Invalid goal authority", http.StatusConflict)
			return
		}
	}
	for _, key := range []string{"instance_id", "session_id", "branch_id"} {
		if len(r.PostForm[key]) != 1 || !runtimeOption(r.PostForm.Get(key)) {
			http.Error(w, "Reload the current goal", http.StatusConflict)
			return
		}
	}
	instance, session, branch := r.PostForm.Get("instance_id"), r.PostForm.Get("session_id"), r.PostForm.Get("branch_id")
	if instance == "" || session == "" {
		http.Error(w, "Reload the current goal", http.StatusConflict)
		return
	}
	// Query authority is never an alternative or shadow source.
	for key := range r.URL.Query() {
		if allowed[key] {
			http.Error(w, "Invalid goal authority", http.StatusConflict)
			return
		}
	}
	var snapshot RuntimeSnapshot
	var err error
	if action == "goal-inspect" {
		snapshot, err = backend.InspectGoal(ctx, project.ID, instance, session, branch)
	} else {
		if branch == "" || len(r.PostForm["expected_tip_id"]) != 1 || len(r.PostForm["expected_goal_id"]) != 1 {
			http.Error(w, "Reload the current goal", http.StatusConflict)
			return
		}
		rawRevision := r.PostForm.Get("expected_revision")
		revision, parseErr := strconv.ParseUint(rawRevision, 10, 64)
		if len(r.PostForm["expected_revision"]) != 1 || parseErr != nil || revision == 0 || strconv.FormatUint(revision, 10) != rawRevision {
			http.Error(w, "Reload the reviewed goal", http.StatusConflict)
			return
		}
		params := protocol.RPCGoalRunParams{SessionID: session, BranchID: branch, ExpectedTipID: r.PostForm.Get("expected_tip_id"), ExpectedGoalID: r.PostForm.Get("expected_goal_id")}
		switch action {
		case "goal-start":
			params.Action = "create"
			params.Objective = r.PostForm.Get("objective")
			if len(r.PostForm["objective"]) != 1 || !validMessageEditText(params.Objective) {
				http.Error(w, "Enter a goal objective", http.StatusConflict)
				return
			}
			if raw := r.PostForm.Get("token_budget"); raw != "" {
				n, e := strconv.ParseInt(raw, 10, 64)
				if e != nil || n <= 0 || strconv.FormatInt(n, 10) != raw {
					http.Error(w, "Invalid goal budget", http.StatusConflict)
					return
				}
				params.TokenBudget = new(n)
			}
		case "goal-resume":
			params.Action = "resume"
			if params.ExpectedGoalID == "" {
				http.Error(w, "Reload the current goal", http.StatusConflict)
				return
			}
		default:
			http.Error(w, "Unknown goal action", http.StatusBadRequest)
			return
		}
		if !runtimeOption(params.ExpectedTipID) || !runtimeOption(params.ExpectedGoalID) {
			http.Error(w, "Reload the current goal", http.StatusConflict)
			return
		}
		snapshot, err = backend.RunGoal(ctx, project.ID, instance, RuntimeGoalRunInput{RPCGoalRunParams: params, ExpectedRevision: revision})
	}
	if err != nil {
		http.Error(w, runtimePublicError(err), http.StatusConflict)
		return
	}
	s.runtimeJSON(w, snapshot)
}

var _ RuntimeGoalBackend = (*RuntimeManager)(nil)
