package plugin

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"strings"

	public "github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// ReservedCommands is shared with the TUI's registry consistency test.
var ReservedCommands = []string{"q", "agent", "abort", "allow", "clear", "compact", "context", "debug", "default", "deny", "fork", "help", "goal", "init", "keybindings", "login", "logout", "mcp", "model", "new", "permissions", "plan", "plugins", "processes", "quit", "resume", "sessions", "settings", "skills", "tree", "thinking", "trust", "agents"}

func (m *Manager) extensionList() []public.Extension {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	var out []public.Extension
	for _, p := range m.plugins {
		if extension, ok := p.goPlugin.(public.Extension); ok {
			out = append(out, extension)
		}
	}
	return out
}

func (m *Manager) ExtensionInfos() []protocol.PluginInfo {
	var out []protocol.PluginInfo
	for _, e := range m.extensionList() {
		info := e.ExtensionInfo()
		info.Scope = m.JavaScriptScope(info.ID)
		out = append(out, info)
	}
	slices.SortFunc(out, func(a, b protocol.PluginInfo) int { return strings.Compare(a.ID, b.ID) })
	return out
}

func (m *Manager) ValidateCommands() error {
	shortcuts := map[string]bool{}
	names := map[string]bool{}
	for _, name := range ReservedCommands {
		names[name] = true
	}
	for _, info := range m.ExtensionInfos() {
		for _, c := range info.Commands {
			if c.Shortcut != "" {
				if shortcuts[c.Shortcut] {
					return fmt.Errorf("duplicate plugin shortcut %s", c.Shortcut)
				}
				shortcuts[c.Shortcut] = true
			}
			if names[c.ID] {
				return fmt.Errorf("duplicate plugin command %s", c.ID)
			}
			names[c.ID] = true
			if c.Alias != "" {
				if names[c.Alias] {
					return fmt.Errorf("plugin command alias %q collides with another command", c.Alias)
				}
				names[c.Alias] = true
			}
		}
	}
	return nil
}

func (m *Manager) BindExtensions(factory func(protocol.PluginInfo) public.ExtensionHost) {
	for _, e := range m.extensionList() {
		e.BindHost(factory(e.ExtensionInfo()))
	}
}

func (m *Manager) ReadyExtensions(ctx context.Context) {
	for _, e := range m.extensionList() {
		if err := e.Ready(ctx); err != nil {
			m.RecordDiagnostic(e.ExtensionInfo().ID, "ready", err.Error())
		}
	}
}

func (m *Manager) Commands() []protocol.PluginCommand {
	var commands []protocol.PluginCommand
	for _, info := range m.ExtensionInfos() {
		commands = append(commands, info.Commands...)
	}
	return commands
}

func (m *Manager) RunCommand(ctx context.Context, id, input string) (public.ToolResult, error) {
	for _, e := range m.extensionList() {
		for _, command := range e.ExtensionInfo().Commands {
			if command.ID == id || (command.Alias != "" && command.Alias == id) {
				return e.RunCommand(ctx, command.Name, input)
			}
		}
	}
	return public.ToolResult{}, fmt.Errorf("unknown plugin command %q", id)
}

func (m *Manager) HasHook(phase string, child bool) bool {
	for _, e := range m.extensionList() {
		if e.HasHook(phase, child) {
			return true
		}
	}
	return false
}

func (m *Manager) RunHooks(ctx context.Context, request public.HookRequest) (public.HookRequest, []protocol.PluginTransform, error) {
	var changes []protocol.PluginTransform
	extensions := m.extensionList()
	slices.SortFunc(extensions, func(a, b public.Extension) int { return strings.Compare(a.ExtensionInfo().ID, b.ExtensionInfo().ID) })
	for _, e := range extensions {
		if !e.HasHook(request.Phase, request.Agent != nil) {
			continue
		}
		id := e.ExtensionInfo().ID
		original, err := jsonv2.Marshal(request)
		if err != nil {
			return request, changes, err
		}
		change, err := e.RunHook(ctx, request)
		if err != nil {
			return request, changes, fmt.Errorf("plugin %s %s: %w", id, request.Phase, err)
		}
		if change.Block != "" {
			return request, changes, fmt.Errorf("plugin %s blocked %s: %s", id, request.Phase, boundUTF8(change.Block, 2048))
		}
		switch request.Phase {
		case "before_prompt":
			if change.Arguments != nil || change.Content != nil || change.Context != nil {
				return request, changes, errors.New("before_prompt can only change text")
			}
			if change.Text != nil {
				request.Text = *change.Text
			}
		case "before_tool":
			if change.Text != nil || change.Content != nil || change.Context != nil {
				return request, changes, errors.New("before_tool can only change arguments")
			}
			if change.Arguments != nil {
				var args map[string]any
				if jsonv2.Unmarshal(change.Arguments, &args) != nil || args == nil {
					return request, changes, errors.New("hook arguments must be a JSON object")
				}
				request.Arguments = change.Arguments
			}
		case "after_tool":
			if change.Text != nil || change.Arguments != nil || change.Context != nil {
				return request, changes, errors.New("after_tool can only change content")
			}
			if change.Content != nil {
				for _, b := range change.Content {
					if b.Type != protocol.BlockText {
						return request, changes, errors.New("after_tool can only return text blocks")
					}
				}
				request.Content = change.Content
			}
		case "before_request":
			if change.Text != nil || change.Arguments != nil || change.Content != nil {
				return request, changes, errors.New("before_request can only supply plugin context")
			}
			if len(change.Context) > 32 {
				return request, changes, errors.New("plugin context fragment limit exceeded")
			}
			for _, fragment := range change.Context {
				if len(fragment.Text) > 16<<10 {
					return request, changes, errors.New("plugin context fragment exceeds 16 KiB")
				}
				attributed := protocol.InternalContextFragment{Source: "plugin-" + id, Text: fragment.Text}
				if err := attributed.Validate(); err != nil {
					return request, changes, fmt.Errorf("plugin %s request context: %w", id, err)
				}
				request.Context = append(request.Context, attributed)
			}
		}
		effective, err := jsonv2.Marshal(request)
		if err != nil {
			return request, changes, err
		}
		if len(effective) > m.options.MaxOutputBytes {
			return request, changes, errors.New("plugin hook output exceeds limit")
		}
		if string(original) != string(effective) {
			changes = append(changes, protocol.PluginTransform{PluginID: id, Hook: request.Phase, Original: original, Effective: effective})
		}
	}
	return request, changes, nil
}

func (m *Manager) RenderTool(ctx context.Context, tool string, data json.RawMessage) (*protocol.PluginNode, error) {
	for _, e := range m.extensionList() {
		info := e.ExtensionInfo()
		if name, ok := strings.CutPrefix(tool, "plugin_"+info.ID+"_"); ok {
			node, err := e.RenderTool(ctx, name, data)
			if err == nil && node != nil {
				err = public.ValidateNode(*node)
			}
			return node, err
		}
	}
	return nil, nil
}

func (m *Manager) SetJavaScriptScope(id, scope string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.plugins {
		if p.id == id {
			p.scope = scope
			return
		}
	}
}
func (m *Manager) JavaScriptScope(id string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.plugins {
		if p.id == id {
			return p.scope
		}
	}
	return ""
}
