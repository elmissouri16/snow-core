package tui

import "strings"

// commandSpec describes one slash command for the completion palette.
type commandSpec struct {
	name    string
	desc    string
	argHint string
}

// commands is the registry shown by the "/" palette and /help.
var commands = []commandSpec{
	{name: "/allow", desc: "approve a pending permission request", argHint: "[always]"},
	{name: "/deny", desc: "deny a pending permission request"},
	{name: "/help", desc: "show command help"},
	{name: "/login", desc: "store an API key for a provider", argHint: "<provider>"},
	{name: "/logout", desc: "remove a stored credential", argHint: "<provider>"},
	{name: "/model", desc: "switch model", argHint: "<id>"},
	{name: "/new", desc: "start a new session"},
	{name: "/permission", desc: "set permission mode", argHint: "ask|allow|deny"},
	{name: "/quit", desc: "exit snow"},
	{name: "/session", desc: "show session info"},
	{name: "/trust", desc: "show or set project trust", argHint: "[allow|deny]"},
}

// completeCommand returns commands matching the typed prefix (without the
// leading '/'), case-insensitive, in registry order. Empty prefix returns all.
func completeCommand(prefix string) []string {
	prefix = strings.ToLower(prefix)
	var out []string
	for _, c := range commands {
		if strings.HasPrefix(strings.ToLower(c.name), "/"+prefix) {
			out = append(out, c.name)
		}
	}
	return out
}

// isCommandPrefix reports whether the editor text is a slash-command first
// token being typed (starts with '/' and contains no space yet). Once the
// user starts typing arguments the palette hides.
func isCommandPrefix(text string) bool {
	if !strings.HasPrefix(text, "/") {
		return false
	}
	return !strings.Contains(text, " ")
}

// commandByExact returns the command spec for an exact command name match.
func commandByExact(name string) (commandSpec, bool) {
	for _, c := range commands {
		if c.name == name {
			return c, true
		}
	}
	return commandSpec{}, false
}

// needsArgs reports whether a command should be inserted into the editor for
// argument completion rather than run immediately.
func (c commandSpec) needsArgs() bool {
	return c.argHint != ""
}

// formatCommandList renders a compact one-line reference for /help.
func formatCommandList() string {
	var b strings.Builder
	for _, c := range commands {
		b.WriteString(c.name)
		if c.argHint != "" {
			b.WriteString(" " + c.argHint)
		}
		b.WriteString(" — " + c.desc + " · ")
	}
	return strings.TrimSuffix(b.String(), " · ")
}

// renderCompletions renders the palette lines: name + dimmed description
// (+ arg hint), selected line highlighted, truncated to width.
func renderCompletions(matches []string, selected int, width int) string {
	if len(matches) == 0 || width <= 0 {
		return ""
	}
	var b strings.Builder
	for i, name := range matches {
		spec, ok := commandByExact(name)
		line := name
		if ok {
			line = name + "  " + spec.desc
			if spec.argHint != "" {
				line += "  (" + spec.argHint + ")"
			}
		}
		if width > 2 && len(line) > width-2 {
			line = line[:width-3] + "…"
		}
		if i == selected {
			b.WriteString(styleCompletionSelected.Render("› " + line))
		} else {
			b.WriteString(styleCompletion.Render("  " + line))
		}
		b.WriteString("\n")
	}
	return strings.TrimSuffix(b.String(), "\n")
}
