package tui

import (
	"slices"
	"strings"
	"unicode"
)

// commandSpec describes one slash command for the completion palette.
type commandSpec struct {
	name    string
	desc    string
	argHint string
	// requiresArg marks commands whose no-arg form is meaningless and that
	// must therefore be inserted into the editor for argument completion
	// instead of run immediately when picked from the palette.
	requiresArg bool
}

// commands is the registry shown by the "/" palette and /help.
var commands = []commandSpec{
	{name: "/agent", desc: "open live fleet inspector or set concurrency", argHint: "[path | concurrency N]"},
	{name: "/allow", desc: "approve a pending permission request", argHint: "[always]"},
	{name: "/compact", desc: "compact older conversation context"},
	{name: "/context", desc: "show what is filling the model context"},
	{name: "/default", desc: "switch to Default collaboration mode"},
	{name: "/debug", desc: "control diagnostics or create a sensitive dump", argHint: "[status|on|off|clear|dump [path]]"},
	{name: "/deny", desc: "deny a pending permission request"},
	{name: "/fork", desc: "fork into this workspace or a Git worktree"},
	{name: "/help", desc: "show command help"},
	{name: "/goal", desc: "show or control a persistent thread goal", argHint: "[objective|edit|pause|resume|clear]"},
	{name: "/init", desc: "create an AGENTS.md guide and project config"},
	{name: "/keybindings", desc: "configure keyboard shortcuts"},
	{name: "/login", desc: "configure a provider endpoint or credentials", argHint: "<provider>"},
	{name: "/logout", desc: "choose and remove a stored credential", argHint: "[provider]"},
	{name: "/mcp", desc: "inspect configured MCP server status"},
	{name: "/model", desc: "pick a model (persisted)", argHint: "<id>"},
	{name: "/new", desc: "start a new session"},
	{name: "/permissions", desc: "choose permission mode", argHint: "ask|allow|deny"},
	{name: "/plan", desc: "switch to Plan mode", argHint: "[message]"},
	{name: "/processes", desc: "open managed process fleet and live output", argHint: "[id | name]"},
	{name: "/quit", desc: "exit snow"},
	{name: "/resume", desc: "resume a session for this directory", argHint: "[path]"},
	{name: "/sessions", desc: "choose, rename, or delete a session for this directory"},
	{name: "/settings", desc: "configure model and response behavior"},
	{name: "/skills", desc: "inspect or clear session-active Agent Skills", argHint: "[clear]"},
	{name: "/tree", desc: "navigate branches in this session"},
	{name: "/thinking", desc: "choose reasoning effort", argHint: "[off|minimal|low|medium|high|xhigh|max|ultra]"},
	{name: "/trust", desc: "show or set project trust", argHint: "[allow|deny]"},
	{name: "/plugins", desc: "inspect or enable/disable JavaScript plugins", argHint: "[enable|disable <id>]"},
}

// completeCommand returns exact matches, then prefixes, then stable
// subsequence matches. Empty prefix returns the complete registry; rendering
// applies a selection-following viewport without removing navigable matches.
func completeCommand(prefix string, extra ...commandSpec) []string {
	prefix = strings.ToLower(strings.TrimPrefix(prefix, "/"))
	if prefix == "" {
		out := make([]string, 0, len(commands))
		for _, c := range combinedCommands(extra) {
			out = append(out, c.name)
		}
		return out
	}
	var out []string
	var exact []string
	var fuzzy []string
	for _, c := range combinedCommands(extra) {
		name := strings.ToLower(strings.TrimPrefix(c.name, "/"))
		switch {
		case name == prefix:
			exact = append(exact, c.name)
		case strings.HasPrefix(name, prefix):
			out = append(out, c.name)
		case len(prefix) >= 3 && subsequenceMatch(name, prefix):
			fuzzy = append(fuzzy, c.name)
		}
	}
	return append(append(exact, out...), fuzzy...)
}

func subsequenceMatch(value, query string) bool {
	if query == "" {
		return true
	}
	qi := 0
	for _, r := range value {
		if qi < len(query) && byte(r) == query[qi] {
			qi++
		}
	}
	return qi == len(query)
}

// isCommandPrefix reports whether the editor text is a slash-command first
// token being typed (starts with '/' and contains no space yet). Once the
// user starts typing arguments the palette hides.
func isCommandPrefix(text string) bool {
	if !strings.HasPrefix(text, "/") {
		return false
	}
	return strings.IndexFunc(text, unicode.IsSpace) < 0
}

// commandByExact returns the command spec for an exact command name match.
func commandByExact(name string, extra ...commandSpec) (commandSpec, bool) {
	for _, c := range combinedCommands(extra) {
		if c.name == name {
			return c, true
		}
	}
	return commandSpec{}, false
}

// needsArgs reports whether a command should be inserted into the editor for
// argument completion rather than run immediately. Only commands whose no-arg
// form is meaningless need this: the rest (e.g. /login, /permissions, /trust)
// have real no-arg behavior and must run immediately so their pickers or
// status output appear. `/logout` also has a no-argument provider picker.
func (c commandSpec) needsArgs() bool {
	return c.requiresArg
}

func formatCommandListWithKeys(keys tuiKeyMap, extra ...commandSpec) string {
	var b strings.Builder
	b.WriteString("Commands\n")
	for _, c := range combinedCommands(extra) {
		b.WriteString("  ")
		b.WriteString(sanitizeTerminalLine(c.name))
		if c.argHint != "" {
			b.WriteString(" ")
			b.WriteString(sanitizeTerminalLine(c.argHint))
		}
		b.WriteString(" — ")
		b.WriteString(sanitizeTerminalLine(c.desc))
		b.WriteByte('\n')
	}
	b.WriteString("\nComposer\n")
	b.WriteString("  $skill — complete an enabled Agent Skill directive\n")
	b.WriteString("  @path — complete and attach a project file\n")
	b.WriteString("\nShortcuts\n")
	for _, group := range keys.FullHelp() {
		for _, binding := range group {
			b.WriteString("  ")
			b.WriteString(binding.Help().Key)
			b.WriteString(" — ")
			b.WriteString(binding.Help().Desc)
			b.WriteByte('\n')
		}
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// renderCompletions renders the palette lines: name + dimmed description
// (+ arg hint), selected line highlighted, truncated to width.
func renderCompletions(matches []string, selected int, width int, extra ...commandSpec) string {
	if width <= 0 {
		return ""
	}
	if len(matches) == 0 {
		return styleCompletion.Render(truncateDisplayText("  no matching commands", width))
	}
	var b strings.Builder
	for i, name := range matches {
		spec, ok := commandByExact(name, extra...)
		line := sanitizeTerminalLine(name)
		if ok {
			line += "  " + sanitizeTerminalLine(spec.desc)
			if spec.argHint != "" {
				line += "  (" + sanitizeTerminalLine(spec.argHint) + ")"
			}
		}
		prefix := "  "
		style := styleCompletion
		if i == selected {
			prefix = "› "
			style = styleCompletionSelected
		}
		b.WriteString(style.Render(truncateDisplayText(prefix+line, width)))
		b.WriteString("\n")
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func combinedCommands(extra []commandSpec) []commandSpec {
	if len(extra) == 0 {
		return commands
	}
	return append(slices.Clone(commands), extra...)
}
