package web

import (
	"fmt"
	"strings"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// Additional fixtures are public DTOs only: no shell, auth store, project
// registration, catalog scan, runtime manager, or provider is constructed.
type harnessVisualFixture struct {
	Name     string            `json:"name"`
	Source   string            `json:"source,omitempty"`
	Snapshot *RuntimeSnapshot  `json:"snapshot"`
	Updates  []RuntimeSnapshot `json:"updates,omitempty"`
	Page     pageData          `json:"-"`
	Template string            `json:"-"`
}

func (f harnessVisualFixture) template() string {
	if f.Template != "" {
		return f.Template
	}
	return "page"
}

func harnessPolishFixtures(base pageData, project Project) []harnessVisualFixture {
	many := base
	many.Projects = []Project{project}
	for i := range 99 {
		// The stress suffix is ASCII, so this exact UTF-8 byte cap cannot split a rune.
		name := fmt.Sprintf("Workspace %03d — %s", i, strings.Repeat("long-name-", 15))[:128]
		many.Projects = append(many.Projects, Project{ID: fmt.Sprintf("00000000-0000-4000-8000-%012d", 100+i), Name: name, Path: "/fixture/" + strings.Repeat("nested-folder/", 12) + fmt.Sprint(i), Available: i%5 != 0, State: "available"})
	}
	registration := many
	registration.View = "projects"
	registrationError := registration
	registrationError.Error = "Cannot register this host folder. Check its absolute path and access permissions."
	access := base
	access.View, access.PairingCode = "access", "fixture-public-pairing-code-not-a-credential"
	login := base
	login.Error = "Pairing code invalid or expired. Check the Snow terminal and try again."
	fixtures := []harnessVisualFixture{
		{Name: "many-home", Page: many},
		{Name: "registration", Page: registration},
		{Name: "registration-error", Page: registrationError},
		{Name: "pairing", Page: access},
		{Name: "login", Page: base, Template: "login"},
		{Name: "login-error", Page: login, Template: "login"},
	}
	for _, name := range []string{"questions", "permission-unknown", "permission-truncated", "stream", "usage-known"} {
		page := many
		page.View, page.Project, page.SessionID = "projects", &project, "fixture-session"
		snapshot := RuntimeSnapshot{ProjectID: project.ID, InstanceID: "fixture-instance", SessionID: page.SessionID, SessionName: "Long task — " + strings.Repeat("keep-the-full-accessible-title-", 12), Provider: "host-provider", Model: "host-model", Mode: "default", Thinking: "off", Status: "idle", Revision: 1, Telemetry: &RuntimeTelemetry{}}
		snapshot.Messages = []RuntimeMessage{{ID: "fixture-user", Role: "user", Text: "Inspect the public fixture only."}}
		switch name {
		case "questions":
			snapshot.Status = "input"
			request := &protocol.UserInputRequest{ID: "fixture-questions", ToolCallID: "fixture-questions"}
			for i := range 16 {
				question := protocol.UserInputQuestion{ID: fmt.Sprintf("question-%02d", i), Header: fmt.Sprintf("Question %d of 16", i+1), Question: "Choose a public fixture answer. " + strings.Repeat("Long explanatory text remains selectable and scrollable. ", 8), ChoicesOnly: i%3 == 1}
				if i%3 != 2 {
					for j := range 32 {
						label := fmt.Sprintf("Choice %02d — %s", j, strings.Repeat("descriptive label ", 3))
						if j == 0 {
							label += " (Recommended)"
						}
						question.Options = append(question.Options, protocol.UserInputOption{Label: label, Description: "Public option description with meaningful wrapping."})
					}
				}
				request.Questions = append(request.Questions, question)
			}
			snapshot.Input = request
		case "permission-unknown", "permission-truncated":
			snapshot.Status = "permission"
			snapshot.Permission = &RuntimePermission{ID: "fixture-permission", Tool: "bash", Risk: "high", Reason: strings.Repeat("Host process effects need explicit review. ", 24), ScopeLabel: "Host process, not sandboxed", Unknown: name == "permission-unknown", Truncated: name == "permission-truncated", Capabilities: []string{"filesystem", "process", "network"}}
			for i := range 32 {
				snapshot.Permission.Paths = append(snapshot.Permission.Paths, fmt.Sprintf("/fixture/%02d/%s", i, strings.Repeat("long-path/", 12)))
			}
		case "stream":
			snapshot.Status = "running"
			history := &liveRuntime{assistant: -1, plan: -1}
			history.projectHistory(protocol.RPCMessagesPage{Messages: []protocol.Message{{ID: "fixture-saved-interleaved", Role: protocol.RoleAssistant, Content: []protocol.ContentBlock{
				{Type: protocol.BlockText, Text: "Saved answer before the plan."},
				{Type: protocol.BlockPlan, Text: "## Saved plan between answer segments"},
				{Type: protocol.BlockText, Text: "Saved answer after the plan."},
			}}}})
			snapshot.Messages = append(snapshot.Messages, history.snapshot.Messages...)
			for i := range 60 {
				snapshot.Messages = append(snapshot.Messages, RuntimeMessage{ID: fmt.Sprintf("stream-%03d", i), Role: "assistant", Text: fmt.Sprintf("## Stable message %03d\n\nPublic visible anchor %03d.\n\n```go\nfmt.Println(%d)\n```\n", i, i, i)})
			}
			// Six real columns exceed the widest transcript even when production
			// caps individual cells. Two long cells can legitimately fit desktop.
			snapshot.Messages[len(snapshot.Messages)-1].Text += "\n| Source | Public streamed content | Review context | File details | Change summary | Result details |\n| --- | --- | --- | --- | --- | --- |\n| Initial |" + strings.Repeat(" "+strings.Repeat("wide-column-", 80)+" |", 5) + "\n"
		case "usage-known":
			snapshot.Telemetry = &RuntimeTelemetry{InputTokens: 1200, OutputTokens: 300, TotalTokens: 1500, ContextTokens: 4096, ContextWindow: 32768, Available: true, ContextAvailable: true, Estimated: true}
		}
		snapshot = displaySnapshot(snapshot)
		page.Live = &snapshot
		fixture := harnessVisualFixture{Name: name, Page: page, Snapshot: &snapshot}
		if name == "stream" {
			growth := snapshot.clone()
			last := len(growth.Messages) - 1
			growth.Messages[last].Text = strings.Replace(growth.Messages[last].Text, "fmt.Println(59)", "fmt.Println(59)\nfmt.Println(\"incremental\")", 1)
			growth.Messages[last].Text += "| Incremental | Server-rendered table continuation | Public review | Public file | Public change | Public result |\n"
			growth.Revision++
			fixture.Updates = []RuntimeSnapshot{displaySnapshot(growth)}
		}
		fixtures = append(fixtures, fixture)
	}
	return fixtures
}
