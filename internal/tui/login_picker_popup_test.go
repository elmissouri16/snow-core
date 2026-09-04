package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/provider/chatgpt"
	"github.com/elmissouri16/snow-core/internal/provider/openaicompat"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestLoginProviderPickerIsCenteredWithoutChangingFrame(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	beforeTranscriptHeight := m.transcript.Height
	_, _ = m.startLogin(nil)
	m.layout()
	if m.transcript.Height != beforeTranscriptHeight {
		t.Fatalf("transcript height changed %d -> %d", beforeTranscriptHeight, m.transcript.Height)
	}
	if overlay := stripANSI(m.renderOverlays()); strings.Contains(overlay, "Select a provider") {
		t.Fatalf("login leaked into lower overlay: %q", overlay)
	}

	card := m.renderLoginModal()
	if got := stripANSI(card); !strings.Contains(got, "Login") || !strings.Contains(got, "Select a provider to sign in") {
		t.Fatalf("provider card=%q", got)
	}
	assertModelCardBounds(t, m, card)
	view := m.View()
	if got := lipgloss.Height(view); got != m.managedFrameHeight() {
		t.Fatalf("view height=%d want=%d", got, m.managedFrameHeight())
	}
	cardWidth, cardHeight := transcriptSelectionBlockWidth(card), lipgloss.Height(card)
	x := (m.managedFrameWidth() - cardWidth) / 2
	y := (m.managedFrameHeight() - cardHeight) / 2
	lines := strings.Split(stripANSI(view), "\n")
	if y >= len(lines) || xansi.StringWidth(lines[y]) < x+1 || []rune(lines[y])[x] != '╭' {
		t.Fatalf("centered login border missing at (%d,%d): %q", x, y, lines[y])
	}
}

func TestLoginKeyEscapeReturnsToProviderPicker(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	_, _ = m.startLogin(nil)
	providerIndex := -1
	for i, provider := range m.providers {
		if provider == "opencode-go" {
			providerIndex = i
			break
		}
	}
	if providerIndex < 0 {
		t.Fatalf("opencode-go missing from providers: %v", m.providers)
	}
	m.provIndex = providerIndex
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.loginMode || !strings.Contains(stripANSI(m.renderLoginModal()), "Esc back") {
		t.Fatalf("key card did not expose back navigation: %q", stripANSI(m.renderLoginModal()))
	}
	for _, r := range "discard-me" {
		_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if !m.pickProvider || m.loginMode || m.secretBuf.Len() != 0 {
		t.Fatalf("back state picker=%v key=%v secret=%d", m.pickProvider, m.loginMode, m.secretBuf.Len())
	}
	if got := m.providers[m.provIndex]; got != "opencode-go" {
		t.Fatalf("restored provider selection=%q", got)
	}
	if card := stripANSI(m.renderLoginModal()); !strings.Contains(card, "Select a provider to sign in") || !strings.Contains(card, "Esc cancel") {
		t.Fatalf("restored provider card=%q", card)
	}
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.loginModalVisible() {
		t.Fatal("Esc at provider root did not close login")
	}
	if _, ok := m.app.Auth.Get("opencode-go"); ok {
		t.Fatal("back navigation persisted discarded key")
	}
}

func TestChatGPTEscapeReturnsToProviderPicker(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	_, _ = m.startLogin(nil)
	providerIndex := -1
	for i, provider := range m.providers {
		if provider == chatgpt.ProviderID {
			providerIndex = i
			break
		}
	}
	if providerIndex < 0 {
		t.Fatalf("chatgpt missing from providers: %v", m.providers)
	}
	m.provIndex = providerIndex
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.pickChatGPTAuth {
		t.Fatal("ChatGPT selection did not open auth choices")
	}
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if !m.pickProvider || m.pickChatGPTAuth || len(m.providers) == 0 {
		t.Fatalf("ChatGPT back state provider=%v auth=%v providers=%v", m.pickProvider, m.pickChatGPTAuth, m.providers)
	}
	if selected := m.providers[m.provIndex]; selected != chatgpt.ProviderID {
		t.Fatalf("restored ChatGPT selection=%q", selected)
	}
}

func TestEntireCompatibleLoginFlowStaysInCard(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	_, _ = m.startLogin([]string{openaicompat.ProviderID})
	if !m.loginProfileMode || !m.loginModalVisible() {
		t.Fatalf("profile mode=%v modal=%v", m.loginProfileMode, m.loginModalVisible())
	}
	if card := stripANSI(m.renderLoginModal()); !strings.Contains(card, "step 1 of 3") || !strings.Contains(card, "Profile name") {
		t.Fatalf("profile card=%q", card)
	}

	m.editor.SetValue("x-provider")
	m.editor.CursorEnd()
	if editor := stripANSI(m.renderEditor()); strings.Contains(editor, "x-provider") {
		t.Fatalf("underlying composer leaked popup field: %q", editor)
	}
	if card := stripANSI(m.renderLoginModal()); !strings.Contains(card, "x-provider") {
		t.Fatalf("profile field missing from card: %q", card)
	}
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.loginEndpointMode || m.loginProfileMode {
		t.Fatalf("endpoint=%v profile=%v", m.loginEndpointMode, m.loginProfileMode)
	}
	if card := stripANSI(m.renderLoginModal()); !strings.Contains(card, "step 2 of 3") || !strings.Contains(card, "Endpoint") {
		t.Fatalf("endpoint card=%q", card)
	}

	m.editor.SetValue("https://gateway.example/v1")
	m.editor.CursorEnd()
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.loginMode || m.loginEndpointMode {
		t.Fatalf("key=%v endpoint=%v", m.loginMode, m.loginEndpointMode)
	}
	for _, r := range "sëcret" {
		_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyBackspace})
	if got := m.secretBuf.String(); got != "sëcre" {
		t.Fatalf("unicode secret backspace=%q", got)
	}
	card := stripANSI(m.renderLoginModal())
	if strings.Contains(card, "sëcre") || !strings.Contains(card, "•••••") || !strings.Contains(card, "step 3 of 3") {
		t.Fatalf("masked key card=%q", card)
	}
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if !m.loginEndpointMode || m.editor.Value() != "https://gateway.example/v1" || m.secretBuf.Len() != 0 {
		t.Fatalf("key back endpoint=%v value=%q secret length=%d", m.loginEndpointMode, m.editor.Value(), m.secretBuf.Len())
	}
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if !m.loginProfileMode || m.editor.Value() != "x-provider" {
		t.Fatalf("endpoint back profile=%v value=%q", m.loginProfileMode, m.editor.Value())
	}
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.loginModalVisible() {
		t.Fatal("root profile Esc did not close login")
	}
}

func TestLoginValidationErrorIsVisibleAndClearsOnEdit(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	m.beginCompatibleProfileCapture()
	m.editor.SetValue("Invalid Name")
	_, _ = m.handleLoginProfileKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.loginError == "" || !strings.Contains(stripANSI(m.renderLoginModal()), "provider profile name") {
		t.Fatalf("error=%q card=%q", m.loginError, stripANSI(m.renderLoginModal()))
	}
	_, _ = m.handleLoginProfileKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if m.loginError != "" {
		t.Fatalf("error did not clear after edit: %q", m.loginError)
	}
}

func TestRequiredLoginKeyErrorStaysInCard(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	m.beginKeyCapture("opencode-go")
	_, _ = m.handleLoginKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.loginMode || m.loginError == "" || !strings.Contains(stripANSI(m.renderLoginModal()), "API key is required") {
		t.Fatalf("mode=%v error=%q card=%q", m.loginMode, m.loginError, stripANSI(m.renderLoginModal()))
	}
	_, _ = m.handleLoginKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if m.loginError != "" {
		t.Fatalf("key edit did not clear error: %q", m.loginError)
	}
}

func TestChatGPTLoginChoiceAndProgressUseCenteredCard(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	m.authAccounts = []chatGPTAccountChoice{{AccountID: "workspace-123", Sources: []string{"codex", "pi"}}}
	m.pickChatGPTAuth = true
	choiceCard := m.renderLoginModal()
	choicePlain := stripANSI(choiceCard)
	if !strings.Contains(choicePlain, "ChatGPT login") || !strings.Contains(choicePlain, "workspace-123") || !strings.Contains(choicePlain, "Sign in with device code") {
		t.Fatalf("choice card=%q", choicePlain)
	}
	assertModelCardBounds(t, m, choiceCard)

	m.oauthLoading = true
	m.oauthProgress = chatgpt.LoginProgress{
		Message:  "Open the authorization page",
		URL:      "https://auth.example/device",
		UserCode: "ABCD-EFGH",
	}
	progressCard := m.renderLoginModal()
	progressPlain := stripANSI(progressCard)
	for _, expected := range []string{"authorizing", "https://auth.example/device", "ABCD-EFGH", "Esc cancel"} {
		if !strings.Contains(progressPlain, expected) {
			t.Fatalf("progress missing %q: %q", expected, progressPlain)
		}
	}
	assertModelCardBounds(t, m, progressCard)
}

func TestCancelingOAuthReturnsToChatGPTChoices(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	m.authAccounts = []chatGPTAccountChoice{{AccountID: "workspace-123", Sources: []string{"codex"}}}
	m.pickChatGPTAuth = true
	m.oauthLoading = true
	canceled := false
	m.oauthCancel = func() { canceled = true }

	_, _ = m.handleChatGPTAuthPick(tea.KeyMsg{Type: tea.KeyEsc})
	if !canceled || !m.oauthBackRequested {
		t.Fatalf("OAuth cancellation canceled=%v back=%v", canceled, m.oauthBackRequested)
	}
	_, _ = m.Update(oauthDoneMsg{err: context.Canceled})
	if m.oauthLoading || !m.pickChatGPTAuth || m.oauthBackRequested {
		t.Fatalf("OAuth back state loading=%v picker=%v requested=%v", m.oauthLoading, m.pickChatGPTAuth, m.oauthBackRequested)
	}
	if card := stripANSI(m.renderLoginModal()); !strings.Contains(card, "Sign in with browser") {
		t.Fatalf("OAuth cancellation did not restore method choices: %q", card)
	}
}

func TestOAuthCompletionAbandonsFullChannelOnTUIShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	events := make(chan tea.Msg, 1)
	events <- oauthProgressMsg{}
	cancel()
	done := make(chan struct{})
	go func() {
		deliverOAuthDone(ctx, events, oauthDoneMsg{})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("OAuth completion blocked after TUI shutdown")
	}
}

func TestOAuthCompletionStillSettlesOperationCancellation(t *testing.T) {
	events := make(chan tea.Msg, 1)
	events <- oauthProgressMsg{}
	done := make(chan struct{})
	go func() {
		deliverOAuthDone(t.Context(), events, oauthDoneMsg{err: context.Canceled})
		close(done)
	}()
	<-events
	select {
	case message := <-events:
		completion, ok := message.(oauthDoneMsg)
		if !ok || !errors.Is(completion.err, context.Canceled) {
			t.Fatalf("OAuth completion = %#v", message)
		}
	case <-time.After(time.Second):
		t.Fatal("live TUI did not receive OAuth cancellation completion")
	}
	<-done
}

func TestModelCloseCancelsAndWaitsForStartedOAuthWorker(t *testing.T) {
	m := newModel(t.Context(), app.Options{})
	workerStarted := make(chan struct{})
	workerDone := make(chan struct{})
	m.oauthLogin = func(ctx context.Context, _ string, _ auth.LoginRequest, _ auth.Interaction) (auth.Status, error) {
		close(workerStarted)
		<-ctx.Done()
		close(workerDone)
		return auth.Status{}, ctx.Err()
	}
	_ = m.startChatGPTOAuth(chatgpt.LoginBrowser, nil)
	<-workerStarted
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-workerDone:
	default:
		t.Fatal("Model.Close returned before the launched OAuth worker stopped")
	}
}

func TestShortOAuthCardKeepsDeviceCodeAndURL(t *testing.T) {
	m := modelPickerTestModel(t, 60, 12)
	m.pickChatGPTAuth = true
	m.oauthLoading = true
	m.oauthProgress = chatgpt.LoginProgress{
		Message:  "Optional explanatory progress text that can be clipped",
		URL:      "https://auth.example/device",
		UserCode: "ABCD-EFGH",
	}
	card := stripANSI(m.renderLoginModal())
	if !strings.Contains(card, "ABCD-EFGH") || !strings.Contains(card, "https://auth.example/device") {
		t.Fatalf("short progress card hid required device data: %q", card)
	}
}

func TestShortLoginCardPrioritizesValidationError(t *testing.T) {
	m := modelPickerTestModel(t, 60, 10)
	m.loginEndpointMode = true
	m.loginProvider = "x-provider"
	m.loginError = "invalid endpoint"
	m.editor.SetValue("not-a-url")
	card := stripANSI(m.renderLoginModal())
	if !strings.Contains(card, "invalid endpoint") {
		t.Fatalf("short login card hid validation error: %q", card)
	}
}

func TestLoginCardsSanitizeExternalTerminalControls(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	control := "\x1b]52;c;ZXZpbA==\x07"
	m.authAccounts = []chatGPTAccountChoice{{AccountID: "workspace-" + control, Sources: []string{"codex"}}}
	m.pickChatGPTAuth = true
	if card := m.renderLoginModal(); strings.Contains(card, "\x1b]52") {
		t.Fatalf("choice card retained terminal control: %q", card)
	}
	m.oauthLoading = true
	m.oauthProgress = chatgpt.LoginProgress{Message: control, URL: "https://example.invalid/" + control, UserCode: control}
	if card := m.renderLoginModal(); strings.Contains(card, "\x1b]52") {
		t.Fatalf("progress card retained terminal control: %q", card)
	}
}

func TestLoginChoiceLabelsAreSingleLine(t *testing.T) {
	choice := "workspace\nspoofed\tcolumn\x1b]52;c;ZXZpbA==\x07"
	view := stripANSI(renderLoginChoiceList([]string{choice}, 0, 60, 1))
	if strings.ContainsAny(view, "\n\t") || strings.Contains(view, "\x1b]52") {
		t.Fatalf("choice injected layout/control data: %q", view)
	}
	if lipgloss.Width(view) > 60 {
		t.Fatalf("choice width=%d want <=60: %q", lipgloss.Width(view), view)
	}
}

func TestLoginEditorSanitizesConfigAndClipboardControls(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	const profile = "unsafe-profile"
	m.app.Cfg.Providers[profile] = config.ProviderConfig{
		Type:    openaicompat.ProviderID,
		BaseURL: "https://gateway.example/private\nspoof\tpath\x1b]52;c;ZXZpbA==\x07",
	}
	m.beginCompatibleEndpointCapture(profile)
	if value := m.editor.Value(); strings.ContainsAny(value, "\n\t\x1b\x07") {
		t.Fatalf("config controls reached endpoint editor: %q", value)
	}
	if card := m.renderLoginModal(); strings.Contains(card, "\x1b]52") {
		t.Fatalf("config OSC reached endpoint card: %q", card)
	}

	m.beginCompatibleProfileCapture()
	m.pasteCmdOverride = func() tea.Msg {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("profile\nspoof\t\x1b]52;c;ZXZpbA==\x07"), Paste: true}
	}
	_, cmd := m.handleLoginProfileKey(tea.KeyMsg{Type: tea.KeyCtrlV})
	if cmd == nil {
		t.Fatal("profile paste returned no routed command")
	}
	_, _ = m.Update(cmd())
	if value := m.editor.Value(); strings.ContainsAny(value, "\n\t\x1b\x07") {
		t.Fatalf("clipboard controls reached profile editor: %q", value)
	}
	if card := m.renderLoginModal(); strings.Contains(card, "\x1b]52") {
		t.Fatalf("clipboard OSC reached profile card: %q", card)
	}
}

func TestStaleLoginFieldPasteIsDiscarded(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	m.beginCompatibleProfileCapture()
	m.pasteCmdOverride = func() tea.Msg {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("stale-profile"), Paste: true}
	}
	_, profilePaste := m.handleLoginProfileKey(tea.KeyMsg{Type: tea.KeyCtrlV})
	if profilePaste == nil {
		t.Fatal("profile paste returned no routed command")
	}
	_, _ = m.handleLoginProfileKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.loginEndpointMode {
		t.Fatal("profile submission did not advance to endpoint")
	}
	_, _ = m.Update(profilePaste())
	if strings.Contains(m.editor.Value(), "stale-profile") {
		t.Fatalf("stale profile paste mutated endpoint: %q", m.editor.Value())
	}

	m.pasteCmdOverride = func() tea.Msg {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("private-endpoint-path"), Paste: true}
	}
	_, endpointPaste := m.handleLoginEndpointKey(tea.KeyMsg{Type: tea.KeyCtrlV})
	if endpointPaste == nil {
		t.Fatal("endpoint paste returned no routed command")
	}
	_, _ = m.handleLoginEndpointKey(tea.KeyMsg{Type: tea.KeyEsc})
	_, _ = m.Update(endpointPaste())
	if strings.Contains(m.editor.Value(), "private-endpoint-path") {
		t.Fatalf("stale endpoint paste mutated composer: %q", m.editor.Value())
	}
}

func TestComposerPasteCannotCrossIntoLoginFields(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	m.pasteCmdOverride = func() tea.Msg {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("composer-secret"), Paste: true}
	}
	_, textPaste := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlV})
	if textPaste == nil {
		t.Fatal("composer text paste returned no command")
	}
	_, _ = m.runCommandWithDisplay("/login openai-compatible", "/login openai-compatible")
	if !m.loginProfileMode {
		t.Fatal("login command did not open profile field")
	}
	_, _ = m.Update(textPaste())
	if strings.Contains(m.editor.Value(), "composer-secret") {
		t.Fatalf("stale composer text paste mutated login field: %q", m.editor.Value())
	}
	_, _ = m.handleLoginProfileKey(tea.KeyMsg{Type: tea.KeyEsc})

	image := protocol.ContentBlock{Type: protocol.BlockImage, MIMEType: "image/png", Data: []byte("image")}
	m.pasteCmdOverride = nil
	m.imagePasteCmdOverride = func() tea.Msg { return clipboardImageMsg{block: image} }
	_, imagePaste := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlV})
	if imagePaste == nil {
		t.Fatal("composer image paste returned no command")
	}
	_, _ = m.runCommandWithDisplay("/login openai-compatible", "/login openai-compatible")
	_, _ = m.Update(imagePaste())
	if len(m.promptImages) != 0 || strings.Contains(m.editor.Value(), imageAttachmentToken(0)) {
		t.Fatalf("stale composer image crossed into login: images=%d editor=%q", len(m.promptImages), m.editor.Value())
	}
}

func TestCompatibleLoginDiscoveryOwnsModalUntilCompletion(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	m.compatibleLoginPending = true
	m.compatibleLoginGeneration = 9
	m.compatibleLoginProvider = "x-provider"
	before := m.editor.Value()
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ignored")})
	if m.editor.Value() != before {
		t.Fatalf("pending discovery admitted composer input: %q", m.editor.Value())
	}
	if card := stripANSI(m.renderLoginModal()); !strings.Contains(card, "Discovering available models") || !strings.Contains(card, "x-provider") {
		t.Fatalf("discovery card=%q", card)
	}
	_, _ = m.Update(compatibleLoginDoneMsg{
		generation: 9,
		provider:   "x-provider",
	})
	if m.loginModalVisible() || m.compatibleLoginProvider != "" {
		t.Fatalf("completion modal=%v provider=%q", m.loginModalVisible(), m.compatibleLoginProvider)
	}
}

func TestCompatibleProgressAndCompletionDoNotRenderEndpointPath(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	m.loginProvider = openaicompat.ProviderID
	m.loginEndpoint = "https://gateway.example/private-token/v1"
	_, cmd := m.finishCompatibleLogin("")
	if cmd == nil || !m.compatibleLoginPending {
		t.Fatalf("cmd=%v pending=%v", cmd != nil, m.compatibleLoginPending)
	}
	if card := stripANSI(m.renderLoginModal()); strings.Contains(card, "private-token") {
		t.Fatalf("progress card exposed endpoint path: %q", card)
	}
	_, _ = m.Update(compatibleLoginDoneMsg{
		generation: m.compatibleLoginGeneration,
		provider:   openaicompat.ProviderID,
	})
	if transcript := stripANSI(strings.Join(m.lines, "\n")); strings.Contains(transcript, "private-token") {
		t.Fatalf("completion transcript exposed endpoint path: %q", transcript)
	}
}

func TestPermissionRequestPreemptsCenteredLoginCard(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	_, _ = m.startLogin(nil)
	m.permPending = true
	m.permRequest = &protocol.PermissionRequest{Tool: "bash", Risk: "exec"}
	view := stripANSI(m.View())
	if strings.Contains(view, "Select a provider to sign in") || !strings.Contains(view, "bash") {
		t.Fatalf("permission did not preempt login card: %q", view)
	}
	m.permPending = false
	if view = stripANSI(m.View()); !strings.Contains(view, "Select a provider to sign in") {
		t.Fatalf("login card did not resume: %q", view)
	}
}

func TestLoginPickerSupportsPageAndBoundaryNavigation(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	m.providers = make([]string, 30)
	for i := range m.providers {
		m.providers[i] = "provider-" + string(rune('a'+i%26))
	}
	m.pickProvider = true
	_, _ = m.handleProviderPick(tea.KeyMsg{Type: tea.KeyEnd})
	if m.provIndex != len(m.providers)-1 || !strings.Contains(stripANSI(m.renderLoginModal()), "› provider-d") {
		t.Fatalf("end index=%d card=%q", m.provIndex, stripANSI(m.renderLoginModal()))
	}
	last := m.provIndex
	_, _ = m.handleProviderPick(tea.KeyMsg{Type: tea.KeyPgUp})
	if m.provIndex >= last {
		t.Fatalf("page up index=%d last=%d", m.provIndex, last)
	}
	_, _ = m.handleProviderPick(tea.KeyMsg{Type: tea.KeyHome})
	if m.provIndex != 0 {
		t.Fatalf("home index=%d", m.provIndex)
	}
}

func TestLoginCardRowsStayBoundedNearMinimumFrame(t *testing.T) {
	m := modelPickerTestModel(t, 8, 10)
	m.providers = []string{"opencode-go", "opencode-zen", "openai-compatible", "chatgpt"}
	m.provIndex = 2
	m.pickProvider = true
	card := m.renderLoginModal()
	if got := stripANSI(card); !strings.Contains(got, "›") {
		t.Fatalf("selected provider disappeared: %q", got)
	}
	assertModelCardBounds(t, m, card)

	m.pickProvider = false
	m.loginMode = true
	m.loginProvider = "opencode-go"
	m.secretBuf.WriteString(strings.Repeat("秘密", 20))
	card = m.renderLoginModal()
	if got := stripANSI(card); strings.Contains(got, "秘密") || !strings.Contains(got, "•") {
		t.Fatalf("narrow key card leaked or lost mask: %q", got)
	}
	assertModelCardBounds(t, m, card)
}

func TestLoginModalConsumesHeaderMouseClick(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	_, _ = m.startLogin(nil)
	header := m.renderHeaderLayout(m.currentHeaderStatus())
	_, _ = m.Update(tea.MouseMsg{X: header.modelStart, Y: 0, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if !m.pickProvider || m.pickModel {
		t.Fatalf("login=%v model=%v", m.pickProvider, m.pickModel)
	}
}
