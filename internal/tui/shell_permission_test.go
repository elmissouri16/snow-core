package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/app"
)

func TestManagedShellPermissionDisplaysHostAuthorityAndCommand(t *testing.T) {
	m := newModel(t.Context(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 100, 30
	m.inlineTranscript = true
	m.layout()
	event := permRequestEvent("process_start")
	event.Permission.Request.Args = json.RawMessage(`{"command":"npm run dev"}`)
	event.Permission.Request.Unknown = true
	event.Permission.Request.Rememberable = false
	m.handleAgentEvent(event)
	view := stripANSI(m.View())
	for _, want := range []string{"npm run dev", "unrestricted host process", "Allow once", "Deny"} {
		if !strings.Contains(view, want) {
			t.Fatalf("managed-process approval omitted %q", want)
		}
	}
	if strings.Contains(view, "Allow this scope") {
		t.Fatal("opaque process offered reusable approval")
	}
	m.height = 3
	m.inlineTranscript = false
	m.layout()
	if m.permissionApprovalEnabled() {
		t.Fatal("small frame permitted approval without visible shell context")
	}
}
