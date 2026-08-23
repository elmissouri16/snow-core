package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/elmissouri16/snow-core/internal/permission"
	managedprocess "github.com/elmissouri16/snow-core/internal/process"
	"github.com/elmissouri16/snow-core/internal/tools"
)

// RegisterProcessTools adds the app-owned managed-process control surface.
func RegisterProcessTools(reg tools.Registry, manager *managedprocess.Manager) error {
	if manager == nil {
		return errors.New("managed process manager is nil")
	}
	descriptors := []tools.ToolDescriptor{
		processDescriptor(&processStartTool{manager: manager}, permission.RiskExec),
		processDescriptor(&processStatusTool{manager: manager}, permission.RiskRead),
		processDescriptor(&processLogsTool{manager: manager}, permission.RiskRead),
		processDescriptor(&processStopTool{manager: manager}, permission.RiskExec),
		processDescriptor(&processListTool{manager: manager}, permission.RiskRead),
	}
	for _, descriptor := range descriptors {
		if err := reg.RegisterDescriptor(descriptor); err != nil {
			return err
		}
	}
	return nil
}

func processDescriptor(tool tools.Tool, risk permission.Risk) tools.ToolDescriptor {
	return tools.ToolDescriptor{Schema: tool.Schema(), Tool: tool, Source: tools.SourceBuiltin, Owner: "builtin", Risk: risk}
}

type processStartTool struct{ manager *managedprocess.Manager }

type processStartArgs struct {
	Command   string                           `json:"command"`
	Name      string                           `json:"name,omitempty"`
	Readiness *managedprocess.ReadinessRequest `json:"readiness,omitempty"`
}

func (t *processStartTool) Schema() tools.ToolSchema {
	return tools.ToolSchema{
		Name:        "process_start",
		Description: "Prefer this for development servers, preview servers, watchers, background workers, and other long-running commands. Starts one managed non-interactive process that persists across later turns and stops when Snow closes; use a stable name and a reliable readiness check when inferable.",
		Parameters: json.RawMessage(`{
  "type":"object",
  "additionalProperties":false,
  "required":["command"],
  "properties":{
    "command":{"type":"string","description":"Non-interactive POSIX shell command to run in the project directory without shell backgrounding such as trailing &, nohup, or disown."},
    "name":{"type":"string","pattern":"^[a-z][a-z0-9_-]{0,63}$","description":"Optional stable safe display name, unique among running processes."},
    "readiness":{
      "description":"Optional reliable startup evidence. Infer from project configuration or command output; do not guess ports, URLs, or patterns.",
      "oneOf":[
        {"type":"object","additionalProperties":false,"required":["type","port"],"properties":{"type":{"const":"tcp"},"host":{"type":"string","description":"Loopback host; defaults to 127.0.0.1."},"port":{"type":"integer","minimum":1,"maximum":65535},"timeout_ms":{"type":"integer","minimum":1,"maximum":120000,"default":30000}}},
        {"type":"object","additionalProperties":false,"required":["type","url"],"properties":{"type":{"const":"http"},"url":{"type":"string","description":"Absolute loopback HTTP(S) readiness URL."},"timeout_ms":{"type":"integer","minimum":1,"maximum":120000,"default":30000}}},
        {"type":"object","additionalProperties":false,"required":["type","pattern"],"properties":{"type":{"const":"log"},"pattern":{"type":"string","description":"RE2 pattern matched against combined process output."},"timeout_ms":{"type":"integer","minimum":1,"maximum":120000,"default":30000}}}
      ]
    }
  }
}`),
	}
}

func (t *processStartTool) Run(ctx context.Context, args json.RawMessage, host tools.ToolHost) (tools.ToolResult, error) {
	var input processStartArgs
	if err := json.Unmarshal(args, &input); err != nil {
		return tools.ErrorResult(fmt.Errorf("process_start: invalid arguments: %w", err)), nil
	}
	state, err := t.manager.Start(ctx, managedprocess.StartRequest{Command: input.Command, Name: input.Name, Readiness: input.Readiness}, func(message string) {
		emitProgress(host, message, false, false)
	})
	if err != nil {
		if state.ProcessID != "" {
			return processStartErrorResult(state, err), nil
		}
		return tools.ErrorResult(err), nil
	}
	return processJSONResult(state)
}

type processStatusTool struct{ manager *managedprocess.Manager }

type processIDArgs struct {
	ProcessID string `json:"process_id"`
}

func (t *processStatusTool) Schema() tools.ToolSchema {
	return tools.ToolSchema{Name: "process_status", Description: "Get the current state of one managed process by its opaque process ID.", Parameters: processIDSchema()}
}

func (t *processStatusTool) Run(_ context.Context, args json.RawMessage, _ tools.ToolHost) (tools.ToolResult, error) {
	var input processIDArgs
	if err := json.Unmarshal(args, &input); err != nil {
		return tools.ErrorResult(fmt.Errorf("process_status: invalid arguments: %w", err)), nil
	}
	state, err := t.manager.Status(input.ProcessID)
	if err != nil {
		return tools.ErrorResult(err), nil
	}
	return processJSONResult(state)
}

type processLogsTool struct{ manager *managedprocess.Manager }

type processLogsArgs struct {
	ProcessID string `json:"process_id"`
	Cursor    *int64 `json:"cursor,omitempty"`
	MaxBytes  int    `json:"max_bytes,omitempty"`
	WaitMS    int    `json:"wait_ms,omitempty"`
}

func (t *processLogsTool) Schema() tools.ToolSchema {
	return tools.ToolSchema{
		Name:        "process_logs",
		Description: "Read bounded combined stdout/stderr from a managed process using a retry-safe cursor.",
		Parameters: json.RawMessage(`{
  "type":"object",
  "additionalProperties":false,
  "required":["process_id"],
  "properties":{
    "process_id":{"type":"string"},
    "cursor":{"type":"integer","minimum":0},
    "max_bytes":{"type":"integer","minimum":4},
    "wait_ms":{"type":"integer","minimum":0,"maximum":30000,"description":"Optionally wait for new output or process exit."}
  }
}`),
	}
}

func (t *processLogsTool) Run(ctx context.Context, args json.RawMessage, _ tools.ToolHost) (tools.ToolResult, error) {
	var input processLogsArgs
	if err := json.Unmarshal(args, &input); err != nil {
		return tools.ErrorResult(fmt.Errorf("process_logs: invalid arguments: %w", err)), nil
	}
	result, err := t.manager.Logs(ctx, managedprocess.LogsRequest{ProcessID: input.ProcessID, Cursor: input.Cursor, MaxBytes: input.MaxBytes, Wait: time.Duration(input.WaitMS) * time.Millisecond})
	if err != nil {
		return tools.ErrorResult(err), nil
	}
	return processJSONResult(result)
}

type processStopTool struct{ manager *managedprocess.Manager }

type processStopArgs struct {
	ProcessID string `json:"process_id"`
	GraceMS   int    `json:"grace_ms,omitempty"`
}

func (t *processStopTool) Schema() tools.ToolSchema {
	return tools.ToolSchema{
		Name:        "process_stop",
		Description: "Stop and reap a managed process group, escalating from graceful termination to kill after a bounded grace period.",
		Parameters: json.RawMessage(`{
  "type":"object",
  "additionalProperties":false,
  "required":["process_id"],
  "properties":{
    "process_id":{"type":"string"},
    "grace_ms":{"type":"integer","minimum":1,"maximum":30000,"default":2000}
  }
}`),
	}
}

func (t *processStopTool) Run(ctx context.Context, args json.RawMessage, host tools.ToolHost) (tools.ToolResult, error) {
	var input processStopArgs
	if err := json.Unmarshal(args, &input); err != nil {
		return tools.ErrorResult(fmt.Errorf("process_stop: invalid arguments: %w", err)), nil
	}
	emitProgress(host, "stopping process", false, false)
	state, err := t.manager.Stop(ctx, input.ProcessID, time.Duration(input.GraceMS)*time.Millisecond)
	if err != nil {
		return tools.ErrorResult(err), nil
	}
	emitProgress(host, "process stopped", true, false)
	return processJSONResult(state)
}

type processListTool struct{ manager *managedprocess.Manager }

func (t *processListTool) Schema() tools.ToolSchema {
	return tools.ToolSchema{Name: "process_list", Description: "List bounded secret-safe metadata for managed processes in the active session. Check stable names before starting a development server or watcher to avoid duplicates.", Parameters: json.RawMessage(`{"type":"object","additionalProperties":false}`)}
}

func (t *processListTool) Run(context.Context, json.RawMessage, tools.ToolHost) (tools.ToolResult, error) {
	return processJSONResult(struct {
		Processes []managedprocess.State `json:"processes"`
	}{Processes: t.manager.List()})
}

func processIDSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["process_id"],"properties":{"process_id":{"type":"string"}}}`)
}

func processStartErrorResult(state managedprocess.State, runErr error) tools.ToolResult {
	encoded, err := json.Marshal(struct {
		State managedprocess.State `json:"state"`
		Error string               `json:"error"`
	}{State: state, Error: runErr.Error()})
	if err != nil {
		return tools.ErrorResult(errors.Join(runErr, err))
	}
	result := tools.TextResult(string(encoded))
	result.IsError = true
	return result
}

func processJSONResult(value any) (tools.ToolResult, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return tools.ErrorResult(fmt.Errorf("managed process result: %w", err)), nil
	}
	return tools.TextResult(string(encoded)), nil
}
