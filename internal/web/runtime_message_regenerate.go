package web

import "context"

func (m *RuntimeManager) PrepareMessageRegenerate(ctx context.Context, projectID, instanceID, messageID string) (RuntimeMessageRegeneratePreparation, error) {
	prepared, err := m.prepareMessageRevision(ctx, projectID, instanceID, messageID, messageRevisionRegenerate)
	if err != nil {
		return RuntimeMessageRegeneratePreparation{}, err
	}
	return RuntimeMessageRegeneratePreparation{ProjectID: projectID, SessionID: prepared.SessionID, InstanceID: instanceID, MessageID: messageID, EditToken: prepared.EditToken}, nil
}

// Regeneration deliberately has no text argument. The core owns the original
// input and verifies its exact relation to the selected terminal assistant.
func (m *RuntimeManager) CommitMessageRegenerate(ctx context.Context, projectID, instanceID, editToken string) (RuntimeSnapshot, error) {
	if editToken == "" || !runtimeOption(editToken) {
		return RuntimeSnapshot{}, ErrRuntimeInvalid
	}
	return m.commitMessageRevision(ctx, projectID, instanceID, editToken, "", messageRevisionRegenerate)
}

var _ RuntimeMessageRegenerateBackend = (*RuntimeManager)(nil)
