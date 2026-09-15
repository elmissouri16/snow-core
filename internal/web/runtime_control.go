package web

import (
	"context"
	"crypto/rand"
	"encoding/json/v2"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// call uses worker lifetime rather than the browser request lifetime. A browser
// disconnect does not cancel an admitted mutation, and mutations are never retried.
func (r *liveRuntime) call(request protocol.RPCRequest, params, result any) error {
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return ErrRuntimeInvalid
		}
		request.Params = data
	}
	ctx, cancel := context.WithTimeout(r.ctx, 8*time.Second)
	defer cancel()
	response, err := r.worker.Client.Call(ctx, request)
	if err != nil {
		r.fail()
		return ErrRuntimeUnavailable
	}
	if !response.Success {
		return ErrRuntimeInvalid
	}
	if result != nil {
		data, err := json.Marshal(response.Data)
		if err != nil || json.Unmarshal(data, result) != nil {
			r.fail()
			return ErrRuntimeUnavailable
		}
	}
	return nil
}

func (r *liveRuntime) fail() {
	r.mu.Lock()
	if r.snapshot.Status != "closing" {
		r.snapshot.Status = "failed"
		r.uncertainCompactionLocked()
		r.unknownActivitiesLocked()
		r.snapshot.Error = "Worker connection failed. Close and explicitly reopen this project."
		r.snapshot.Permission = nil
		r.snapshot.Input = nil
		r.publishLocked()
	}
	r.mu.Unlock()
	r.cancel()
	r.persistRecovery()
}

func (m *RuntimeManager) controlRuntime(ctx context.Context, projectID, instanceID string) (*liveRuntime, error) {
	if ctx.Err() != nil {
		return nil, ErrRuntimeInvalid
	}
	r, err := m.runtime(projectID, instanceID)
	if err != nil {
		return nil, err
	}
	if !r.control.TryLock() {
		return nil, ErrRuntimeBusy
	}
	if err := m.revalidate(r, projectID, instanceID); err != nil {
		r.control.Unlock()
		return nil, err
	}
	r.mu.Lock()
	status := r.snapshot.Status
	r.mu.Unlock()
	if r.ctx.Err() != nil || status == "opening" || status == "closing" || status == "failed" {
		r.control.Unlock()
		return nil, ErrRuntimeBusy
	}
	return r, nil
}

// Prompt admits one text-only prompt and returns after its RPC admission ack.
// Definitive completion arrives in Snapshot; no follow-ups or retries are queued.
func (m *RuntimeManager) Prompt(ctx context.Context, projectID, instanceID, text string) error {
	if len(text) > 64<<10 || strings.TrimSpace(text) == "" || !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
		return ErrRuntimeInvalid
	}
	return m.prompt(ctx, projectID, instanceID, text, nil)
}

// prompt is the sole prompt admission path for legacy text and composer content.
// Callers validate and own their payload before entering the control gate.
func (m *RuntimeManager) prompt(ctx context.Context, projectID, instanceID, text string, content []protocol.ContentBlock) error {
	r, err := m.controlRuntime(ctx, projectID, instanceID)
	if err != nil {
		return err
	}
	defer r.control.Unlock()
	if len(content) != 0 && !r.supports("multimodal_prompts") {
		return ErrRuntimeInvalid
	}
	project := r.project
	project.checkIdentity()
	if !project.Available {
		return ErrProjectInvalid
	}
	r.mu.Lock()
	if r.busy || r.snapshot.CancelRequested || r.goal.pending || r.goal.active {
		r.mu.Unlock()
		return ErrRuntimeBusy
	}
	// The core never replays held follow-ups on a new Prompt. Reject before
	// resetting root authority so review/copy/removal remains possible.
	if r.snapshot.Queue != nil && len(r.snapshot.Queue.Items) != 0 {
		r.mu.Unlock()
		return ErrRuntimeQueueReview
	}
	r.mu.Unlock()
	if err := r.savePromptIntent(); err != nil {
		return err
	}
	r.mu.Lock()
	if r.ctx.Err() != nil || r.snapshot.Status == "failed" || r.snapshot.Status == "closing" {
		r.mu.Unlock()
		return ErrRuntimeUnavailable
	}
	// A new ordinary prompt retires only the completed run's local authority.
	// The durable goal remains visible and deferred until explicit Resume.
	r.goal = runtimeGoalState{revision: r.goal.revision + 1}
	previousMessages := slices.Clone(r.snapshot.Messages)
	previousTruncated := r.snapshot.HistoryTruncated
	r.busy = true
	r.snapshot.CancelToken = rand.Text()
	r.snapshot.CancelRequested = false
	r.activityPrompt++
	r.activityCanceled = false
	r.promptID = ""
	r.earlyCompletion = ""
	r.pendingRegenerateReplyID = ""
	r.assistantHasPlan = false
	r.assistant = -1
	r.plan = -1
	r.turnID = ""
	if r.snapshot.Telemetry != nil {
		r.usageBase = *r.snapshot.Telemetry
	}
	r.snapshot.Status = "running"
	r.snapshot.Error = ""
	r.messageEdit = runtimeMessageEditState{}
	// The public projection contains only the browser's message summary, never
	// image bytes. Image-only SDK callers receive a fixed nonempty placeholder.
	summary := text
	if strings.TrimSpace(summary) == "" && len(content) != 0 {
		summary = "[Image attachment]"
	}
	r.addMessage(RuntimeMessage{Role: "user", Text: summary, Images: promptImages(text, content)})
	r.pendingUserID = r.snapshot.Messages[len(r.snapshot.Messages)-1].ID
	r.publishLocked()
	r.mu.Unlock()
	callCtx, cancel := context.WithTimeout(r.ctx, 8*time.Second)
	defer cancel()
	response, err := r.worker.Client.Call(callCtx, protocol.RPCRequest{Type: "prompt", Message: text, Content: content})
	if err != nil {
		r.fail()
		return ErrRuntimeUnavailable
	}
	return r.promptAcknowledged(response, previousMessages, previousTruncated)
}

func (r *liveRuntime) promptAcknowledged(response protocol.RPCResponse, previousMessages []RuntimeMessage, previousTruncated bool) error {
	r.mu.Lock()
	// Client.Call has already correlated the response to this prompt. Its caller
	// can be descheduled while the event consumer observes EOF or shutdown.
	// Keep received admission evidence even then, without reviving the runtime
	// or claiming that agent.Prompt durably saved the admitted message.
	mismatch := (r.earlyCompletion != "" && r.earlyCompletion != response.ID) || (r.promptID != "" && r.promptID != response.ID)
	admitted := response.Success && !mismatch && r.snapshot.Recovery.State == RecoveryAdmissionUnknown
	if admitted {
		r.promptID = response.ID
		r.snapshot.Recovery.State = RecoveryAdmitted
		r.snapshot.Recovery.UpdatedAt = time.Now().UTC()
		r.publishLocked()
	}
	// Acknowledgment never overrides definitive completion evidence or grants
	// permission to resume controls after transport loss, shutdown or cancellation.
	if r.ctx.Err() != nil || r.snapshot.Status == "failed" || r.snapshot.Status == "closing" {
		r.mu.Unlock()
		if admitted {
			r.persistRecovery()
		}
		return ErrRuntimeUnavailable
	}
	if !response.Success && r.earlyCompletion != "" {
		r.mu.Unlock()
		r.fail()
		return ErrRuntimeUnavailable
	}
	if !response.Success {
		r.pendingUserID = ""
		r.snapshot.Messages = previousMessages
		r.snapshot.HistoryTruncated = previousTruncated
		r.busy = false
		r.clearTurnCancelLocked()
		r.unknownActivitiesLocked()
		r.snapshot.Permission = nil
		r.snapshot.Input = nil
		r.snapshot.Status = "idle"
		r.snapshot.Error = "Prompt was not accepted. No retry was queued."
		r.snapshot.Recovery.State = RecoveryRejected
		r.snapshot.Recovery.UpdatedAt = time.Now().UTC()
		r.publishLocked()
		r.mu.Unlock()
		r.persistRecovery()
		return ErrRuntimeInvalid
	}
	if !mismatch {
		r.promptID = response.ID
		r.acknowledgeRegenerateReplyLocked()
		r.publishLocked() // Queue capability also waits for correlated admission.
	}
	r.mu.Unlock()
	if mismatch {
		r.fail()
		return ErrRuntimeUnavailable
	}
	r.persistRecovery()
	return nil
}

// Abort remains available while a provider or tool is running or awaiting input.
func (m *RuntimeManager) Abort(ctx context.Context, projectID, instanceID string) error {
	r, err := m.controlRuntime(ctx, projectID, instanceID)
	if err != nil {
		return err
	}
	defer r.control.Unlock()
	if err := r.call(protocol.RPCRequest{Type: "abort"}, nil, nil); err != nil {
		return err
	}
	r.mu.Lock()
	r.cancelActivities()
	r.snapshot.Permission = nil
	r.snapshot.Input = nil
	r.publishLocked()
	r.mu.Unlock()
	return nil
}

// ReplyPermission deliberately cannot grant remembered, session-wide, or always
// permission. The current pending request is the only valid correlation target.
func (m *RuntimeManager) ReplyPermission(ctx context.Context, projectID, instanceID, requestID string, decision protocol.PermissionDecision) error {
	if decision != protocol.PermissionAllow && decision != protocol.PermissionDeny {
		return ErrRuntimeInvalid
	}
	r, err := m.controlRuntime(ctx, projectID, instanceID)
	if err != nil {
		return err
	}
	defer r.control.Unlock()
	r.mu.Lock()
	pending := r.snapshot.Permission
	valid := pending != nil && pending.ID == requestID
	r.mu.Unlock()
	if !valid || (decision == protocol.PermissionAllow && pending.Truncated) {
		return ErrRuntimeInvalid
	}
	if decision == protocol.PermissionAllow {
		project := r.project
		project.checkIdentity()
		if !project.Available {
			return ErrProjectInvalid
		}
	}
	if err := r.call(protocol.RPCRequest{Type: "permission_reply"}, protocol.PermissionResponse{RequestID: requestID, Decision: decision}, nil); err != nil {
		return err
	}
	r.mu.Lock()
	if r.snapshot.Permission != nil && r.snapshot.Permission.ID == requestID {
		r.snapshot.Permission = nil
		if r.busy {
			r.snapshot.Status = "running"
		}
		r.publishLocked()
	}
	r.mu.Unlock()
	return nil
}

// ReplyInput takes typed answers, validates the complete currently pending set,
// and never maps client input into RPC command names or tool arguments.
func (m *RuntimeManager) ReplyInput(ctx context.Context, projectID, instanceID string, response protocol.UserInputResponse) error {
	r, err := m.controlRuntime(ctx, projectID, instanceID)
	if err != nil {
		return err
	}
	defer r.control.Unlock()
	r.mu.Lock()
	pending := cloneRuntimeInput(r.snapshot.Input)
	r.mu.Unlock()
	if pending == nil || pending.ID != response.RequestID || len(response.Answers) != len(pending.Questions) {
		return ErrRuntimeInvalid
	}
	seen := make(map[string]bool, len(response.Answers))
	total := 0
	for _, answer := range response.Answers {
		if seen[answer.QuestionID] || strings.TrimSpace(answer.Answer) == "" || !utf8.ValidString(answer.Answer) || strings.ContainsRune(answer.Answer, 0) {
			return ErrRuntimeInvalid
		}
		seen[answer.QuestionID] = true
		total += len(answer.Answer)
		if total > 64<<10 {
			return ErrRuntimeInvalid
		}
		found := false
		for _, question := range pending.Questions {
			if answer.QuestionID == question.ID {
				found = true
				if question.ChoicesOnly {
					matched := false
					for _, option := range question.Options {
						if answer.Answer == option.Label {
							matched = true
						}
					}
					if !matched {
						return ErrRuntimeInvalid
					}
				}
				break
			}
		}
		if !found {
			return ErrRuntimeInvalid
		}
	}
	if err := r.call(protocol.RPCRequest{Type: "user_input_reply"}, response, nil); err != nil {
		return err
	}
	r.mu.Lock()
	if r.snapshot.Input != nil && r.snapshot.Input.ID == response.RequestID {
		r.snapshot.Input = nil
		if r.busy {
			r.snapshot.Status = "running"
		}
		r.publishLocked()
	}
	r.mu.Unlock()
	return nil
}
