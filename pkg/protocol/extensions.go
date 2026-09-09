package protocol

import "encoding/json"

// PluginCommand is an explicitly invoked action, independent of model tools.
type PluginCommand struct {
	ID           string `json:"id"`
	PluginID     string `json:"plugin_id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	ArgumentHint string `json:"argument_hint,omitempty"`
	Alias        string `json:"alias,omitempty"`
	Shortcut     string `json:"shortcut,omitempty"`
	TimeoutMS    int    `json:"timeout_ms,omitzero"`
}

// PluginNode is a terminal-independent, bounded component tree. Action names
// refer to commands owned by the contributing plugin, never executable code.
type PluginNode struct {
	Type     string       `json:"type"`
	ID       string       `json:"id,omitempty"`
	Text     string       `json:"text,omitempty"`
	Tone     string       `json:"tone,omitempty"`
	Children []PluginNode `json:"children,omitempty"`
	Columns  []string     `json:"columns,omitempty"`
	Rows     [][]string   `json:"rows,omitempty"`
	Value    float64      `json:"value,omitzero"`
	Action   string       `json:"action,omitempty"`
	Input    string       `json:"input,omitempty"`
}

type PluginView struct {
	ID        string      `json:"id"`
	PluginID  string      `json:"plugin_id"`
	Name      string      `json:"name"`
	Title     string      `json:"title"`
	Placement string      `json:"placement"`
	Content   *PluginNode `json:"content,omitempty"`
}

type PluginTheme struct {
	ID       string                 `json:"id"`
	PluginID string                 `json:"plugin_id"`
	Name     string                 `json:"name"`
	Colors   map[string]PluginColor `json:"colors"`
}

type PluginColor struct {
	Light string `json:"light"`
	Dark  string `json:"dark"`
}

type PluginSetting struct {
	Name    string          `json:"name"`
	Title   string          `json:"title"`
	Type    string          `json:"type"`
	Default json.RawMessage `json:"default,omitempty"`
	Choices []string        `json:"choices,omitempty"`
}

type PluginInfo struct {
	Path         string          `json:"path,omitempty"`
	Scope        string          `json:"scope,omitempty"`
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Version      string          `json:"version"`
	APIVersion   int             `json:"api_version"`
	Capabilities []string        `json:"capabilities,omitempty"`
	HostTools    []string        `json:"host_tools,omitempty"`
	Commands     []PluginCommand `json:"commands,omitempty"`
	Views        []PluginView    `json:"views,omitempty"`
	Themes       []PluginTheme   `json:"themes,omitempty"`
	Settings     []PluginSetting `json:"settings,omitempty"`
	Config       json.RawMessage `json:"config,omitempty"`
}

// PluginUIEvent is a presentation request, never a permission decision.
type PluginUIEvent struct {
	PluginID   string          `json:"plugin_id"`
	Operation  string          `json:"operation"`
	Arguments  json.RawMessage `json:"arguments,omitempty"`
	Generation uint64          `json:"generation"`
}

// PluginTransform records effective changes without altering original model
// tool calls. It is excluded from provider payloads by the normal projection.
type PluginTransform struct {
	PluginID  string          `json:"plugin_id"`
	Hook      string          `json:"hook"`
	Original  json.RawMessage `json:"original"`
	Effective json.RawMessage `json:"effective"`
}

func (n *PluginNode) Clone() *PluginNode {
	if n == nil {
		return nil
	}
	out := *n
	out.Columns = append([]string(nil), n.Columns...)
	out.Rows = make([][]string, len(n.Rows))
	for i, row := range n.Rows {
		out.Rows[i] = append([]string(nil), row...)
	}
	out.Children = make([]PluginNode, len(n.Children))
	for i := range n.Children {
		out.Children[i] = *n.Children[i].Clone()
	}
	return &out
}
