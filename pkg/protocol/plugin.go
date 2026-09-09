package protocol

// PluginOrigin attributes a permission request to its host-owned plugin call.
type PluginOrigin struct {
	PluginID         string `json:"plugin_id"`
	ToolName         string `json:"tool_name"`
	ParentToolCallID string `json:"parent_tool_call_id"`
	HostTool         string `json:"host_tool,omitempty"`
}
