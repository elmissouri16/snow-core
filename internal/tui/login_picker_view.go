package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
)

func (m *Model) loginModalVisible() bool {
	return m.pickProvider || m.pickChatGPTAuth || m.loginProfileMode || m.loginEndpointMode ||
		m.loginMode || m.compatibleLoginPending || m.logoutPending
}

func (m *Model) renderLoginModal() string {
	switch {
	case m.logoutPending:
		return m.renderLogoutProgressCard()
	case m.compatibleLoginPending:
		return m.renderCompatibleLoginProgressCard()
	case m.pickProvider:
		return m.renderProviderPicker()
	case m.pickChatGPTAuth:
		return m.renderChatGPTAuthPicker()
	case m.loginProfileMode:
		return m.renderLoginProfileCard()
	case m.loginEndpointMode:
		return m.renderLoginEndpointCard()
	case m.loginMode:
		return m.renderLoginKeyCard()
	default:
		return ""
	}
}

func (m *Model) overlayLoginModal(frame string) string {
	return m.overlayCenteredModal(frame, m.renderLoginModal())
}

func (m *Model) renderProviderPickerCard() string {
	if !m.pickProvider || len(m.providers) == 0 {
		return ""
	}
	geometry := m.pickerCardGeometry()
	title, instruction, action := "Login", " Select a provider to sign in", "Enter continue"
	if m.providerLogout {
		title, instruction, action = "Logout", " Select a provider to sign out", "Enter sign out"
	}
	header := renderPickerCardHeader(title, fmt.Sprintf("%d providers", len(m.providers)), geometry.innerWidth)
	separator := styleSep.Render(strings.Repeat("─", geometry.innerWidth))
	choices := make([]string, 0, len(m.providers))
	for _, provider := range m.providers {
		choices = append(choices, m.providerStatus(provider))
	}
	bodyHeight := max(1, geometry.innerHeight-4)
	body := renderLoginChoiceList(choices, m.provIndex, geometry.innerWidth, bodyHeight)
	footer := styleFooter.Render(truncateDisplayText(" ↑/↓ navigate · "+action+" · Esc cancel ", geometry.innerWidth))
	content := lipgloss.JoinVertical(lipgloss.Left,
		header,
		styleHeaderDim.Render(truncateDisplayText(instruction, geometry.innerWidth)),
		separator,
		fixedLoginBody(body, geometry.innerWidth, bodyHeight),
		footer,
	)
	return renderPickerCard(content, geometry)
}

func (m *Model) renderChatGPTAuthPickerCard() string {
	if !m.pickChatGPTAuth {
		return ""
	}
	geometry := m.pickerCardGeometry()
	if m.oauthLoading {
		return m.renderChatGPTOAuthProgressCard(geometry)
	}
	choices := make([]string, 0, len(m.authAccounts)+2)
	for _, account := range m.authAccounts {
		choices = append(choices, "Authorize "+account.AccountID+" for Snow  (used by "+strings.Join(account.Sources, ", ")+")")
	}
	choices = append(choices, "Sign in with browser (any ChatGPT account)", "Sign in with device code")
	header := renderPickerCardHeader("ChatGPT login", fmt.Sprintf("%d choices", len(choices)), geometry.innerWidth)
	separator := styleSep.Render(strings.Repeat("─", geometry.innerWidth))
	bodyHeight := max(1, geometry.innerHeight-4)
	body := renderLoginChoiceList(choices, m.authIndex, geometry.innerWidth, bodyHeight)
	footer := styleFooter.Render(truncateDisplayText(" ↑/↓ navigate · Enter authorize · Esc "+m.loginEscapeAction()+" ", geometry.innerWidth))
	content := lipgloss.JoinVertical(lipgloss.Left,
		header,
		styleHeaderDim.Render(truncateDisplayText(" Snow obtains its own OAuth token", geometry.innerWidth)),
		separator,
		fixedLoginBody(body, geometry.innerWidth, bodyHeight),
		footer,
	)
	return renderPickerCard(content, geometry)
}

func (m *Model) renderChatGPTOAuthProgressCard(geometry pickerCardGeometry) string {
	header := renderPickerCardHeader("ChatGPT login", "authorizing…", geometry.innerWidth)
	separator := styleSep.Render(strings.Repeat("─", geometry.innerWidth))
	bodyHeight := max(1, geometry.innerHeight-4)
	lines := make([]string, 0, 4)
	// The code and URL are required to complete device login, so keep them
	// ahead of optional progress prose when a short card must clip rows.
	if code := strings.TrimSpace(sanitizeTerminalLine(m.oauthProgress.UserCode)); code != "" {
		lines = append(lines, styleHeader.Render(" Device code: "+code))
	}
	if target := strings.TrimSpace(sanitizeTerminalLine(m.oauthProgress.URL)); target != "" {
		lines = append(lines, styleAssistant.Render(" "+target))
	}
	message := strings.TrimSpace(sanitizeTerminalLine(m.oauthProgress.Message))
	if message == "" {
		message = "Starting authorization…"
	}
	if len(lines) == 0 {
		lines = append(lines, styleCompletionSelected.Render(" Authorization in progress"))
	}
	lines = append(lines, styleHeaderDim.Render(" "+message))
	body := wrapLoginLines(lines, geometry.innerWidth, bodyHeight)
	footer := styleFooter.Render(truncateDisplayText(" Esc cancel authorization ", geometry.innerWidth))
	content := lipgloss.JoinVertical(lipgloss.Left,
		header,
		styleHeaderDim.Render(truncateDisplayText(" Complete the browser or device flow", geometry.innerWidth)),
		separator,
		fixedLoginBody(body, geometry.innerWidth, bodyHeight),
		footer,
	)
	return renderPickerCard(content, geometry)
}

func (m *Model) renderLoginProfileCard() string {
	return m.renderLoginTextCard(
		"OpenAI-compatible login",
		"step 1 of 3",
		" Profile name",
		"Blank uses openai-compatible. Allowed: lowercase letters, digits, . _ -",
		" Enter continue · Esc "+m.loginEscapeAction()+" · Ctrl+V paste ",
		false,
	)
}

func (m *Model) renderLoginEndpointCard() string {
	return m.renderLoginTextCard(
		"OpenAI-compatible login",
		"step 2 of 3 · "+m.loginProvider,
		" Endpoint",
		"Enter an API root or a full /responses or /chat/completions URL.",
		" Enter continue · Esc "+m.loginEscapeAction()+" · Ctrl+V paste ",
		false,
	)
}

func (m *Model) renderLoginKeyCard() string {
	optional := m.providerAuthOptional(m.loginProvider) || m.loginEndpoint != ""
	hint := "Enter the API key to store in Snow's 0600 credential file."
	if optional {
		hint = "The key is optional. Blank keeps an existing/fallback key or uses keyless access."
	}
	status := m.loginProvider
	if m.loginEndpoint != "" {
		status = "step 3 of 3 · " + m.loginProvider
	}
	return m.renderLoginTextCard(
		"API key",
		status,
		" Hidden credential",
		hint,
		" Enter save · Backspace edit · Esc "+m.loginEscapeAction()+" ",
		true,
	)
}

func (m *Model) renderLoginTextCard(title, status, label, hint, footer string, secret bool) string {
	geometry := m.pickerCardGeometry()
	header := renderPickerCardHeader(title, status, geometry.innerWidth)
	separator := styleSep.Render(strings.Repeat("─", geometry.innerWidth))
	bodyHeight := max(1, geometry.innerHeight-4)
	fieldWidth := max(1, geometry.innerWidth-2)
	var field string
	if secret {
		masked := strings.Repeat("•", utf8.RuneCountInString(m.secretBuf.String()))
		if masked == "" {
			masked = styleHeaderDim.Render("key will remain hidden")
		} else {
			masked = styleAssistant.Render(masked)
		}
		field = truncateInputTail(masked+styleHeader.Render("_"), fieldWidth)
	} else {
		field = m.renderLoginEditorField(fieldWidth)
	}
	fieldBox := field
	if geometry.innerWidth >= 3 && bodyHeight >= 4 {
		fieldBox = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(colorAccent).
			Width(fieldWidth).
			Height(1).
			Render(field)
	} else {
		fieldBox = styleCompletionSelected.Render(truncateDisplayText(" "+field, geometry.innerWidth))
	}
	bodyLines := []string{fieldBox}
	if m.loginError != "" {
		bodyLines = append(bodyLines, styleError.Render(" "+sanitizeTerminalLine(m.loginError)))
		if bodyHeight > lipgloss.Height(fieldBox)+2 {
			bodyLines = append(bodyLines, "", styleHeaderDim.Render(" "+hint))
		}
	} else {
		bodyLines = append(bodyLines, "", styleHeaderDim.Render(" "+hint))
	}
	body := wrapLoginLines(bodyLines, geometry.innerWidth, bodyHeight)
	content := lipgloss.JoinVertical(lipgloss.Left,
		header,
		styleHeaderDim.Render(truncateDisplayText(label, geometry.innerWidth)),
		separator,
		fixedLoginBody(body, geometry.innerWidth, bodyHeight),
		styleFooter.Render(truncateDisplayText(footer, geometry.innerWidth)),
	)
	return renderPickerCard(content, geometry)
}

func (m *Model) renderLogoutProgressCard() string {
	geometry := m.pickerCardGeometry()
	provider := sanitizeTerminalLine(m.logoutProvider)
	if provider == "" {
		provider = "provider"
	}
	header := renderPickerCardHeader("Logout", provider, geometry.innerWidth)
	separator := styleSep.Render(strings.Repeat("─", geometry.innerWidth))
	bodyHeight := max(1, geometry.innerHeight-4)
	body := wrapLoginLines([]string{
		styleCompletionSelected.Render(" Removing stored credential…"),
		"",
		styleHeaderDim.Render(" Other explicit or environment credentials are unchanged."),
	}, geometry.innerWidth, bodyHeight)
	content := lipgloss.JoinVertical(lipgloss.Left,
		header,
		styleHeaderDim.Render(truncateDisplayText(" Updating authentication", geometry.innerWidth)),
		separator,
		fixedLoginBody(body, geometry.innerWidth, bodyHeight),
		styleFooter.Render(truncateDisplayText(" Please wait · Ctrl+C exits Snow ", geometry.innerWidth)),
	)
	return renderPickerCard(content, geometry)
}

func (m *Model) renderCompatibleLoginProgressCard() string {
	geometry := m.pickerCardGeometry()
	provider := sanitizeTerminalLine(m.compatibleLoginProvider)
	if provider == "" {
		provider = "OpenAI-compatible"
	}
	header := renderPickerCardHeader("Login", provider, geometry.innerWidth)
	separator := styleSep.Render(strings.Repeat("─", geometry.innerWidth))
	bodyHeight := max(1, geometry.innerHeight-4)
	lines := []string{
		styleCompletionSelected.Render(" Endpoint saved"),
		"",
		styleHeaderDim.Render(" Discovering available models…"),
	}
	body := wrapLoginLines(lines, geometry.innerWidth, bodyHeight)
	content := lipgloss.JoinVertical(lipgloss.Left,
		header,
		styleHeaderDim.Render(truncateDisplayText(" Configuring provider", geometry.innerWidth)),
		separator,
		fixedLoginBody(body, geometry.innerWidth, bodyHeight),
		styleFooter.Render(truncateDisplayText(" Please wait · Ctrl+C exits Snow ", geometry.innerWidth)),
	)
	return renderPickerCard(content, geometry)
}

func (m *Model) renderLoginEditorField(width int) string {
	field := m.editor
	if value := sanitizeTerminalLine(field.Value()); value != field.Value() {
		// Keep terminal controls and row injection out of textarea.View even when
		// a hostile persisted config was loaded before this flow opened.
		field.SetValue(value)
		field.CursorEnd()
	}
	field.Prompt = ""
	field.SetWidth(max(1, width))
	field.SetHeight(1)
	field.MaxWidth = max(1, width)
	field.MaxHeight = 1
	view := field.View()
	if line, _, ok := strings.Cut(view, "\n"); ok {
		view = line
	}
	return truncateDisplayText(view, width)
}

func (m *Model) loginPickerVisibleChoices() int {
	height := max(1, m.pickerCardGeometry().innerHeight-4)
	if height > 2 {
		return height - 2
	}
	return height
}

func renderLoginChoiceList(choices []string, selected, width, height int) string {
	if len(choices) == 0 || height <= 0 {
		return ""
	}
	selected = clampPickerIndex(selected, len(choices))
	visible := min(height, len(choices))
	if len(choices) > height {
		visible = max(1, height-2)
	}
	start := max(0, selected-visible/2)
	if start+visible > len(choices) {
		start = len(choices) - visible
	}
	end := min(len(choices), start+visible)
	lines := make([]string, 0, height)
	hasTop := start > 0
	if hasTop {
		lines = append(lines, styleHeaderDim.Render(truncateDisplayText("  ↑ more", width)))
	}
	for i := start; i < end; i++ {
		prefix := "  "
		style := styleCompletion
		if i == selected {
			prefix = "› "
			style = styleCompletionSelected
		}
		lines = append(lines, style.Render(truncateDisplayText(prefix+sanitizeTerminalLine(choices[i]), width)))
	}
	hasBottom := end < len(choices)
	if hasBottom {
		lines = append(lines, styleHeaderDim.Render(truncateDisplayText("  ↓ more", width)))
	}
	if len(lines) > height && hasTop {
		lines = lines[1:]
	}
	if len(lines) > height && hasBottom {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}

func fixedLoginBody(body string, width, height int) string {
	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		MaxWidth(width).
		MaxHeight(height).
		Render(body)
}

func wrapLoginLines(lines []string, width, height int) string {
	wrapped := make([]string, 0, height)
	for _, line := range lines {
		for _, part := range strings.Split(xansi.Wordwrap(line, max(1, width), ""), "\n") {
			wrapped = append(wrapped, truncateDisplayText(part, width))
			if len(wrapped) == height {
				return strings.Join(wrapped, "\n")
			}
		}
	}
	return strings.Join(wrapped, "\n")
}

func truncateInputTail(value string, width int) string {
	if width <= 0 || xansi.StringWidth(value) <= width {
		return value
	}
	if width == 1 {
		return xansi.Cut(value, xansi.StringWidth(value)-1, xansi.StringWidth(value))
	}
	valueWidth := xansi.StringWidth(value)
	return "…" + xansi.Cut(value, valueWidth-(width-1), valueWidth)
}
