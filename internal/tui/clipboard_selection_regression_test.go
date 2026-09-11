package tui

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestOccupiedClipboardRetryPreservesEditableSelection(t *testing.T) {
	for _, surface := range []string{"composer-all", "composer-partial", "dialog", "profile", "endpoint", "secret"} {
		for _, state := range []string{"pending", "timeout", "canceled", "other-owner"} {
			for _, custom := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/custom=%v", surface, state, custom), func(t *testing.T) {
					m := remoteClipboardModel(t)
					if state == "other-owner" {
						m.startClipboardTextRead()
					}
					switch surface {
					case "dialog":
						m.startUserInput(protocol.UserInputRequest{ID: "input", Questions: []protocol.UserInputQuestion{{ID: "q", Question: "Answer"}}})
					case "profile":
						m.beginCompatibleProfileCapture()
					case "endpoint":
						m.beginCompatibleEndpointCapture("fixture")
					case "secret":
						m.beginKeyCapture("openai")
					}
					if state != "other-owner" {
						m.startClipboardTextRead()
					}
					if m.terminalClipboard == nil {
						t.Fatal("missing outstanding request")
					}
					switch state {
					case "timeout":
						m.Update(terminalClipboardTimeoutMsg(m.terminalClipboard.generation))
					case "canceled", "other-owner":
						m.cancelClipboardReads()
					}
					editor := &m.editor
					if surface == "dialog" {
						editor = &m.userInputEditor
					}
					editor.SetValue("draft")
					editor.CursorEnd()
					if strings.HasPrefix(surface, "composer") {
						m.promptImages = []protocol.ContentBlock{{Type: protocol.BlockImage, MIMEType: "image/png", Data: []byte("fixture")}}
						attachment := m.newPastedTextAttachment(strings.Repeat("x", largePasteRuneThreshold))
						m.pastedTexts = []pastedTextAttachment{attachment}
						editor.SetValue("draft " + imageAttachmentToken(0) + " " + attachment.token)
						editor.CursorEnd()
					}
					if surface == "composer-all" {
						m.Update(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
					} else if surface == "secret" {
						m.secretBuf.WriteString("fixture-secret")
					} else {
						m.Update(tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModShift})
						if !editor.HasSelection() {
							t.Fatal("missing partial selection")
						}
					}
					request := *m.terminalClipboard
					generation, imageGeneration := m.clipboardGeneration, m.imagePasteGeneration
					value, selected := editor.Value(), editor.SelectedText()
					start, end, hasSelection := editor.Selection()
					all := m.composerSelectAll
					images, pastes := slices.Clone(m.promptImages), slices.Clone(m.pastedTexts)
					paste := tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl}
					if custom {
						m.keys.Paste = key.NewBinding(key.WithKeys("alt+v"))
						paste = tea.KeyPressMsg{Code: 'v', Mod: tea.ModAlt}
					}
					_, cmd := m.Update(paste)
					if cmd != nil {
						t.Fatal("blocked retry started a command")
					}
					gotStart, gotEnd, gotSelection := editor.Selection()
					if editor.Value() != value || editor.SelectedText() != selected || gotStart != start || gotEnd != end || gotSelection != hasSelection || m.composerSelectAll != all {
						t.Fatal("blocked retry mutated text or selection")
					}
					if !reflect.DeepEqual(images, m.promptImages) || !reflect.DeepEqual(pastes, m.pastedTexts) {
						t.Fatal("blocked retry mutated attachments")
					}
					if *m.terminalClipboard != request || m.clipboardGeneration != generation || m.imagePasteGeneration != imageGeneration {
						t.Fatal("blocked retry invalidated request owner/generations")
					}
					if surface == "secret" && m.secretBuf.String() != "fixture-secret" {
						t.Fatal("blocked retry changed masked input")
					}
				})
			}
		}
	}
}
