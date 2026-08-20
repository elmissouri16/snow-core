package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestClipboardImageMIME(t *testing.T) {
	cases := []struct {
		data []byte
		mime string
	}{
		{append([]byte("\x89PNG\r\n\x1a\n"), 0), "image/png"},
		{[]byte{0xff, 0xd8, 0xff, 0}, "image/jpeg"},
		{[]byte("GIF89a0"), "image/gif"},
		{[]byte("RIFF0000WEBP"), "image/webp"},
	}
	for _, tc := range cases {
		if got, ok := clipboardImageMIME(tc.data); !ok || got != tc.mime {
			t.Fatalf("MIME(%x)=(%q,%v), want %q", tc.data, got, ok, tc.mime)
		}
	}
	if _, ok := clipboardImageMIME([]byte("text")); ok {
		t.Fatal("text detected as image")
	}
}

func TestImageAttachmentTokenRoundTrip(t *testing.T) {
	text := "cool" + imageAttachmentInsertion("cool", 0, 4, 0) + "shsh"
	if text != "cool [Image #1] shsh" {
		t.Fatalf("inline token = %q", text)
	}
	if got := stripImageAttachmentTokens(text, 1); got != "cool shsh" {
		t.Fatalf("provider text = %q", got)
	}
	if got := removeImageAttachmentToken("[Image #1] ", 0); got != "" {
		t.Fatalf("removed token = %q", got)
	}
	if got := promptImageBytes([]protocol.ContentBlock{{Data: []byte("one")}, {Data: []byte("two")}}); got != 6 {
		t.Fatalf("image bytes = %d", got)
	}
}

func TestClipboardImageTokenInsertsAtComposerCursor(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	m.editor.SetValue("cool shsh")
	m.editor.SetCursor(5)
	_, _ = m.Update(clipboardImageMsg{block: protocol.ContentBlock{
		Type: protocol.BlockImage, MIMEType: "image/png", Data: []byte("x"),
	}})
	if got := m.editor.Value(); got != "cool [Image #1] shsh" {
		t.Fatalf("cursor insertion = %q", got)
	}
}

func TestComposerCtrlVAttachesImageAndSubmitPersistsMixedContent(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 80, 20
	m.layout()
	m.editor.SetValue("describe this")
	image := protocol.ContentBlock{Type: protocol.BlockImage, MIMEType: "image/png", Data: []byte("\x89PNG\r\n\x1a\nbytes")}
	m.imagePasteCmdOverride = func() tea.Msg { return clipboardImageMsg{block: image} }
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlV})
	if cmd == nil {
		t.Fatal("image paste returned no command")
	}
	_, _ = m.Update(cmd())
	if len(m.promptImages) != 1 || m.editor.Value() != "describe this [Image #1] " {
		t.Fatalf("attachment state/editor = %d %q", len(m.promptImages), m.editor.Value())
	}
	view := stripANSI(m.renderEditor())
	if !strings.Contains(view, "describe this [Image #1]") || strings.Contains(view, "Backspace removes last") {
		t.Fatalf("inline attachment render = %q", view)
	}
	m.editor.InsertString("carefully")
	if got := m.editor.Value(); got != "describe this [Image #1] carefully" {
		t.Fatalf("continued text after attachment = %q", got)
	}
	// The deterministic fake model in this fixture is text-only; opt it into
	// vision so this test exercises admission and durable mixed content.
	model := m.app.Agent.Model()
	model.SupportsVision = true
	if err := m.app.Agent.SetModel(model); err != nil {
		t.Fatal(err)
	}
	_, promptCmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if promptCmd == nil {
		t.Fatal("image prompt returned no command")
	}
	_ = promptCmd()
	messages, err := m.app.Session.Messages()
	if err != nil {
		t.Fatal(err)
	}
	var user *protocol.Message
	for i := range messages {
		if messages[i].Role == protocol.RoleUser {
			user = &messages[i]
			break
		}
	}
	if user == nil || len(user.Content) != 2 || user.Content[0].Text != "describe this carefully" || user.Content[1].MIMEType != "image/png" || string(user.Content[1].Data) != string(image.Data) {
		t.Fatalf("mixed user content = %+v", user)
	}
	if len(m.promptImages) != 0 {
		t.Fatal("submitted images remained in composer")
	}
}

func TestBusyPromptRetainsImagesInsteadOfQueuingTextOnly(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.busy = true
	m.editor.SetValue("queued text")
	m.promptImages = []protocol.ContentBlock{{Type: protocol.BlockImage, MIMEType: "image/png", Data: []byte("x")}}
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || len(m.promptImages) != 1 || !strings.Contains(m.lastStatus, "cannot be queued") {
		t.Fatalf("busy image submission: cmd=%v images=%d status=%q", cmd != nil, len(m.promptImages), m.lastStatus)
	}
}

func TestRejectedPromptRestoresImages(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	image := protocol.ContentBlock{Type: protocol.BlockImage, MIMEType: "image/png", Data: []byte("x")}
	m.promptImages = []protocol.ContentBlock{image}
	cmd := m.startPrompt("describe [Image #1] ")
	if len(m.promptImages) != 0 {
		t.Fatal("start did not transfer attachment ownership")
	}
	result := cmd().(promptDoneMsg)
	if result.err == nil || result.admitted {
		t.Fatalf("text-only model unexpectedly admitted image: %+v", result)
	}
	_, _ = m.Update(result)
	if len(m.promptImages) != 1 || string(m.promptImages[0].Data) != "x" || m.editor.Value() != "describe [Image #1] " {
		t.Fatalf("rejected prompt did not restore draft: images=%d text=%q", len(m.promptImages), m.editor.Value())
	}
}

func TestStaleClipboardImageDoesNotAttachAfterSubmission(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.imagePasteGeneration = 4
	m.editor.SetValue("submitted")
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	_, _ = m.Update(clipboardImageMsg{generation: 4, block: protocol.ContentBlock{Type: protocol.BlockImage, MIMEType: "image/png", Data: []byte("x")}})
	if len(m.promptImages) != 0 {
		t.Fatal("stale clipboard result attached to next prompt")
	}
}

func TestStaleTextFallbackDoesNotPasteIntoNextPrompt(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.imagePasteGeneration = 3
	_, cmd := m.Update(clipboardImageMsg{generation: 3, err: errClipboardHasNoImage})
	if cmd == nil {
		t.Fatal("no-image result did not start text fallback")
	}
	m.imagePasteGeneration++
	result := cmd().(textareaResultMsg)
	_, _ = m.Update(result)
	if m.editor.Value() != "" {
		t.Fatalf("stale text fallback changed next prompt: %q", m.editor.Value())
	}
}

func TestComposerTextClipboardFallsBackToTextareaPaste(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.imagePasteCmdOverride = func() tea.Msg { return clipboardImageMsg{err: errClipboardHasNoImage} }
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlV})
	if cmd == nil {
		t.Fatal("clipboard probe returned no command")
	}
	_, fallback := m.Update(cmd())
	if fallback == nil {
		t.Fatal("non-image clipboard did not fall back to textarea paste")
	}
}

func TestHydrationRendersImageOnlyUserMessage(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	message := protocol.NewUserContentMessage("image-user", "", []protocol.ContentBlock{{Type: protocol.BlockImage, MIMEType: "image/png", Data: []byte("x")}})
	if err := m.app.Session.Append(session.Entry{Type: session.EntryMessage, ID: message.ID, Message: &message}); err != nil {
		t.Fatal(err)
	}
	m.hydrateSession()
	if !strings.Contains(stripANSI(strings.Join(m.lines, "\n")), "[1 image(s)]") {
		t.Fatalf("image-only hydration = %q", stripANSI(strings.Join(m.lines, "\n")))
	}
}

func TestBackspaceRemovesLastImageFromEmptyComposer(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.promptImages = []protocol.ContentBlock{{Type: protocol.BlockImage, MIMEType: "image/png", Data: []byte("x")}}
	m.editor.SetValue("[Image #1] ")
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyBackspace})
	if len(m.promptImages) != 0 || m.editor.Value() != "" {
		t.Fatalf("Backspace did not remove inline image: images=%d text=%q", len(m.promptImages), m.editor.Value())
	}
}
