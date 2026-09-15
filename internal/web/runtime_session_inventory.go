package web

import (
	"context"
	"encoding/json/v2"
	"slices"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// RuntimeSessionInventoryBackend is an optional session-only read capability.
// Unlike Choices it never discovers models or requests provider telemetry.
type RuntimeSessionInventoryBackend interface {
	SessionInventory(context.Context, string, string) (RuntimeSessionInventory, error)
}

type RuntimeSessionInventory struct {
	ProjectID  string
	InstanceID string
	Sessions   []RuntimeSessionChoice
	Available  bool
	Truncated  bool
}

func (m *RuntimeManager) SessionInventory(ctx context.Context, projectID, instanceID string) (RuntimeSessionInventory, error) {
	r, err := m.controlRuntime(ctx, projectID, instanceID)
	if err != nil {
		return RuntimeSessionInventory{}, err
	}
	defer r.control.Unlock()
	if err := r.idle(); err != nil {
		return RuntimeSessionInventory{}, err
	}
	// Read cancellation belongs to the browser, unlike admitted mutations. Also
	// stop on worker shutdown; an abandoned read never fails the runtime itself.
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	stop := context.AfterFunc(r.ctx, cancel)
	defer stop()
	response, err := r.worker.Client.Call(ctx, protocol.RPCRequest{Type: "sessions_list"})
	if err != nil {
		return RuntimeSessionInventory{}, ErrRuntimeUnavailable
	}
	result := RuntimeSessionInventory{ProjectID: projectID, InstanceID: instanceID, Sessions: []RuntimeSessionChoice{}}
	if !response.Success {
		return result, nil
	}
	var list protocol.RPCSessionList
	encoded, err := json.Marshal(response.Data)
	if err != nil || json.Unmarshal(encoded, &list) != nil {
		return RuntimeSessionInventory{}, ErrRuntimeUnavailable
	}
	r.applySessionList(list)
	result.Available = r.choices.SessionsAvailable
	result.Truncated = r.choices.SessionsTruncated
	result.Sessions = slices.Clone(r.choices.Sessions)
	return result, nil
}

var _ RuntimeSessionInventoryBackend = (*RuntimeManager)(nil)
