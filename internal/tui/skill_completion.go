package tui

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
)

const skillCompletionLimit = 100

type skillCompletionItem struct {
	Name        string
	Description string
}

// skillCompletionQuery recognizes a $skill token at the end of the editor.
// Exact skill tokens earlier in the prompt are returned so the picker can omit
// skills that are already selected. Activation uses the same whitespace token
// boundaries across every input surface.
func skillCompletionQuery(text string) (query string, start int, selected []string, ok bool) {
	for index, r := range text {
		if unicode.IsSpace(r) {
			_, size := utf8.DecodeRuneInString(text[index:])
			start = index + size
		}
	}
	if start >= len(text) || text[start] != '$' {
		return "", 0, nil, false
	}
	for _, field := range strings.Fields(text[:start]) {
		if len(field) >= 2 && field[0] == '$' {
			selected = append(selected, field[1:])
		}
	}
	return text[start+1:], start, selected, true
}

func matchSkillCompletions(skills []skillCompletionItem, query string, selected []string) []skillCompletionItem {
	query = strings.ToLower(query)
	used := make(map[string]bool, len(selected))
	for _, name := range selected {
		used[name] = true
	}
	matches := make([]skillCompletionItem, 0, len(skills))
	for _, skill := range skills {
		if used[skill.Name] || (query != "" && !strings.HasPrefix(strings.ToLower(skill.Name), query)) {
			continue
		}
		matches = append(matches, skill)
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].Name < matches[j].Name })
	if len(matches) > skillCompletionLimit {
		matches = matches[:skillCompletionLimit]
	}
	return matches
}

func replaceSkillCompletionToken(text string, start int, name string) string {
	return text[:start] + "$" + name + " "
}

func (m *Model) refreshSkillCompletions() {
	m.refreshSkillCompletionsFor(m.editor.Value())
}

func (m *Model) refreshSkillCompletionsFor(text string) {
	m.skillVisible = false
	m.skillMatches = nil
	m.skillIndex = 0
	if m.app == nil || m.app.Skills == nil {
		return
	}
	query, _, selected, ok := skillCompletionQuery(text)
	if !ok {
		return
	}
	catalog := m.app.Skills.List()
	items := make([]skillCompletionItem, 0, len(catalog))
	for _, skill := range catalog {
		items = append(items, skillCompletionItem{Name: skill.Name, Description: skill.Description})
	}
	m.skillMatches = matchSkillCompletions(items, query, selected)
	m.skillVisible = len(m.skillMatches) > 0
}

func (m *Model) insertSkillCompletion(name string) (tea.Model, tea.Cmd) {
	m.resetInputHistoryNavigation()
	text := m.editor.Value()
	_, start, _, ok := skillCompletionQuery(text)
	if !ok {
		return m, nil
	}
	m.editor.SetValue(replaceSkillCompletionToken(text, start, name))
	m.editor.CursorEnd()
	m.skillVisible = false
	m.skillMatches = nil
	m.refreshInputCompletions()
	return m, nil
}

func (m *Model) renderSkillCompletionPicker() string {
	if !m.skillVisible || len(m.skillMatches) == 0 {
		return ""
	}
	limit := 8
	if m.inlineInputOverlay() {
		limit = min(limit, m.availableOverlayHeight())
	}
	limit = max(1, limit)
	start := 0
	end := min(len(m.skillMatches), limit)
	if m.skillIndex >= end {
		start = m.skillIndex - limit + 1
		end = start + limit
		if end > len(m.skillMatches) {
			end = len(m.skillMatches)
			start = end - limit
		}
	}
	var b strings.Builder
	for i := start; i < end; i++ {
		item := m.skillMatches[i]
		line := "$" + item.Name
		if item.Description != "" {
			line += "  " + item.Description
		}
		line = truncateRunes(line, max(8, m.width-4))
		if i == m.skillIndex {
			b.WriteString(styleCompletionSelected.Render("› " + line))
		} else {
			b.WriteString(styleCompletion.Render("  " + line))
		}
		if i+1 < end {
			b.WriteByte('\n')
		}
	}
	return b.String()
}
