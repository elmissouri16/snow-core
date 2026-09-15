package web

import (
	"context"
	"encoding/json/v2"
	"errors"
	"path/filepath"
	"slices"
	"time"

	"github.com/elmissouri16/snow-core/pkg/agentclient/process"
	clientrpc "github.com/elmissouri16/snow-core/pkg/agentclient/rpc"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// workerCatalog keeps at most two short-lived workers, with no hidden request
// queue and no idle process retention. All worker arguments are manager-owned.
// Opening another project never turns catalog access into runtime activation.
type workerCatalog struct {
	executable string
	slots      chan struct{}
	env        []string
}

func newWorkerCatalog(executable, sessionsRoot string) *workerCatalog {
	env, err := freezeWorkerEnvironment(sessionsRoot)
	if err != nil {
		executable = ""
	} // call fails closed before spawning.
	return &workerCatalog{executable: executable, slots: make(chan struct{}, 2), env: env}
}

func (c *workerCatalog) call(ctx context.Context, project Project, command string, params any, result any) error {
	return c.callStartup(ctx, project, "catalog", command, params, result)
}

func (c *workerCatalog) callStartup(ctx context.Context, project Project, startup, command string, params any, result any) error {
	if !filepath.IsAbs(c.executable) {
		return errors.New("catalog requires an absolute Snow executable")
	}
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	default:
		return errors.New("catalog worker limit reached")
	}
	project.checkIdentity()
	if !project.Available {
		return errors.New("catalog project is unavailable")
	}
	maxFrame := 4 << 20
	if startup == "control" {
		maxFrame = protocol.RPCControlMaxOutputBytes
	}
	worker, err := process.Start(ctx, process.Options{
		Executable: c.executable,
		Args:       []string{"--mode", "rpc", "--rpc-startup", startup},
		Dir:        project.Path,
		Env:        c.env,
		RPC: clientrpc.Options{MaxFrameBytes: maxFrame, MaxPending: 1, EventBuffer: 1,
			HandshakeTimeout: 4 * time.Second, WriteTimeout: time.Second},
		ShutdownTimeout: 500 * time.Millisecond,
	})
	if err != nil {
		return errors.New("catalog worker did not start")
	}
	defer worker.Close()
	project.checkIdentity()
	if !project.Available {
		return errors.New("catalog project changed during startup")
	}
	ready := worker.Client.Ready()
	if history, ok := params.(protocol.RPCCatalogMessagesParams); ok {
		// Old catalog workers keep their original strict text-only contract.
		// Request the additive public projection only when advertised.
		history.IncludeTools = slices.Contains(ready.Capabilities, "catalog_public_tools")
		params = history
	}
	capability := command
	if startup == "control" {
		capability = protocol.RPCSessionDeleteControlCapability
	}
	if !slices.Contains(ready.Capabilities, "runtime_free_"+startup) || !slices.Contains(ready.Capabilities, capability) {
		return errors.New("worker lacks the required runtime-free catalog capability")
	}
	// Consume even unexpected notifications so transport overflow remains a
	// deliberate error, not a leaked reader. Never project them into the UI.
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for range worker.Client.Events() {
		}
	}()
	defer func() { _ = worker.Close(); <-drained }()
	encoded, err := json.Marshal(params)
	if err != nil {
		return errors.New("invalid catalog request")
	}
	response, err := worker.Client.Call(ctx, protocol.RPCRequest{Type: command, Params: encoded})
	if err != nil || !response.Success {
		return errors.New("catalog read failed")
	}
	data, err := json.Marshal(response.Data)
	if err != nil || json.Unmarshal(data, result) != nil {
		return errors.New("invalid catalog response")
	}
	return nil
}

func (c *workerCatalog) Sessions(ctx context.Context, project Project, offset int) (CatalogSessions, error) {
	var page protocol.RPCCatalogSessionsPage
	err := c.call(ctx, project, "catalog_sessions", protocol.RPCCatalogSessionsParams{Offset: offset, Limit: 25}, &page)
	result := CatalogSessions{NextOffset: page.NextOffset, HasMore: page.HasMore}
	for _, session := range page.Sessions {
		result.Sessions = append(result.Sessions, SessionSummary{ID: session.SessionID, Name: session.Name,
			Updated: time.UnixMilli(session.UpdatedAt).UTC().Format(time.RFC3339)})
	}
	return result, err
}

func (c *workerCatalog) Messages(ctx context.Context, project Project, sessionID string, offset int) (CatalogMessages, error) {
	var page protocol.RPCCatalogMessagesPage
	err := c.call(ctx, project, "catalog_messages", protocol.RPCCatalogMessagesParams{SessionID: sessionID, Offset: offset, Limit: 25}, &page)
	result := CatalogMessages{NextOffset: page.NextOffset, HasMore: page.HasMore, ToolsTruncated: page.ToolsTruncated}
	for _, message := range page.Messages {
		result.Messages = append(result.Messages, HistoryMessage{ID: message.ID, Role: message.Role, Text: message.Text, Truncated: message.Truncated, Tools: slices.Clone(message.Tools), Images: messageImages(message.Images)})
	}
	return result, err
}

// Image uses a separate bounded catalog read, never runtime activation or SQL.
func (c *workerCatalog) Image(ctx context.Context, project Project, p protocol.RPCCatalogImageParams) (protocol.RPCCatalogImage, error) {
	var result protocol.RPCCatalogImage
	err := c.call(ctx, project, "catalog_image", p, &result)
	return result, err
}

// DeleteSession owns a short-lived runtime-free control worker in the same
// bounded pool as catalog reads. Only manager-selected launch paths are used.
func (c *workerCatalog) DeleteSession(ctx context.Context, project Project, id string) error {
	if !runtimeIdentifier(id) {
		return ErrRuntimeInvalid
	}
	var result protocol.RPCSessionDeleteResult
	if err := c.callStartup(ctx, project, "control", "session_delete", protocol.RPCSessionDeleteParams{SessionID: id}, &result); err != nil {
		return ErrSessionDeleteUncertain
	}
	if !result.Deleted || result.SessionID != id {
		return ErrSessionDeleteUncertain
	}
	return nil
}
