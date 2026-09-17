package web

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// TestExportHarnessVisualFixtures is an opt-in, credential-free browser fixture
// exporter. It executes the production page template and copies embedded public
// assets, without constructing a shell, registry, worker, or on-disk catalog.
// Ordinary go test runs skip it and leave no persistent browser artifacts.
func TestExportHarnessVisualFixtures(t *testing.T) {
	destination := os.Getenv("SNOW_WEB_FIXTURE_DIR")
	if destination == "" {
		t.Skip("set SNOW_WEB_FIXTURE_DIR to an explicit temporary output directory")
	}
	if !filepath.IsAbs(destination) {
		t.Fatal("SNOW_WEB_FIXTURE_DIR must be an absolute temporary directory")
	}
	if err := os.MkdirAll(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	write := func(name string, data []byte) {
		t.Helper()
		if err := root.MkdirAll(path.Dir(name), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := root.WriteFile(name, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := fs.WalkDir(assets, "static", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := assets.ReadFile(name)
		if err != nil {
			return err
		}
		write(name, data)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	project := Project{ID: "00000000-0000-4000-8000-000000000001", Name: "snow-core", Path: "/fixture/workspaces/snow-core", Available: true, State: "available", SkillsEnabled: true}
	base := pageData{
		View: "overview", Version: "visual-fixture", CSRF: "fixture-only-not-a-credential",
		RegistryEnabled: true, RuntimeEnabled: true, WorkflowEnabled: true, PermissionPolicyEnabled: true, TurnCancelEnabled: true,
		Projects: []Project{project},
	}
	fixtures := []harnessVisualFixture{{Name: "home", Page: base}}
	empty := base
	empty.Projects = nil
	fixtures = append(fixtures, harnessVisualFixture{Name: "empty-home", Page: empty})
	for _, name := range []string{"chat", "plan", "attention", "inspector", "markdown", "model-empty", "model-unavailable", "model-disconnected", "workflow", "runtime-panels", "runtime-unsupported"} {
		data := base
		data.View, data.Project, data.SessionID = "projects", &project, "fixture-session"
		data.Sessions = &CatalogSessions{Sessions: []SessionSummary{
			{ID: "fixture-session", Name: "Refine the workspace", Updated: "Today"},
			{ID: "fixture-previous", Name: "Review project structure", Updated: "Yesterday"},
		}}
		snapshot := RuntimeSnapshot{
			ProjectID: project.ID, InstanceID: "fixture-instance", SessionID: data.SessionID,
			SessionName: "Refine the workspace", Provider: "host-provider", Model: "host-model",
			Mode: "default", PermissionMode: "ask", Thinking: "off", Status: "idle", Revision: 1,
			CancelToken: "fixture-turn",
			Telemetry:   &RuntimeTelemetry{},
			Messages: []RuntimeMessage{
				{ID: "fixture-user", Role: "user", Text: "Help me refine the workspace layout."},
				{ID: "fixture-assistant", Role: "assistant", Text: "I’ll keep the conversation focused and the workspace controls close at hand.\n\n- Review the current layout\n- Preserve keyboard navigation\n- Verify the narrow-screen experience"},
			},
		}
		if name == "plan" {
			snapshot.Mode = "plan"
			snapshot.Messages[1].Role = "plan"
			snapshot.Messages[1].Text = "## Workspace refinement plan\n\n1. Inspect the existing templates and styles.\n2. Keep activation and permission boundaries explicit.\n3. Verify desktop and mobile layouts.\n\nNo implementation has started."
		}
		if name == "attention" {
			snapshot.Status = "permission"
			snapshot.Permission = &RuntimePermission{
				ID: "fixture-permission", Tool: "read", Risk: "low", Reason: "Read the workspace stylesheet for this review.",
				ScopeLabel: "Project files", Paths: []string{"internal/web/static/app.css"},
			}
		}
		if name == "markdown" {
			snapshot.Messages[1].Text = "## Bounded Markdown presentation\n\n- Keep long content inside the transcript\n- Preserve copyable public output\n\n```go\n" + strings.Repeat("long_identifier_", 28) + " := 42\n```\n\n| File | Description |\n| --- | --- |\n| `internal/web/static/messages.js` | " + strings.Repeat("wide-column-", 30) + " |\n"
			snapshot.Activities = []RuntimeActivity{{ID: "fixture-tool", Tool: "read", Status: "completed", Summary: "Read stylesheet", Output: strings.Repeat("bounded-public-output ", 60)}}
		}
		if name == "attention" {
			snapshot.Activities = []RuntimeActivity{{ID: "fixture-running-tool", Tool: "read", Status: "running", Summary: "Waiting for explicit file approval"}}
		}
		if name == "workflow" {
			workflowProject := project
			workflowProject.ID = "00000000-0000-4000-8000-000000000002"
			data.Project, data.Projects, data.CSRF = &workflowProject, []Project{workflowProject}, "test-csrf"
			data.SessionID = "session-one"
			data.Sessions = &CatalogSessions{Sessions: []SessionSummary{{ID: "session-one", Name: "First task"}, {ID: "session-two", Name: "Second task"}}}
			snapshot.ProjectID, snapshot.InstanceID, snapshot.SessionID, snapshot.SessionName = workflowProject.ID, "instance-one", "session-one", "First task"
			snapshot.Messages = nil
		}
		if name == "runtime-panels" {
			// Presentation-only capability projections: no registry or worker is
			// constructed and the browser mock rejects every mutation endpoint.
			data.VersionsEnabled, data.GoalsEnabled, data.ProcessControlEnabled = true, true, true
			data.ReasoningEnabled, data.CompactionEnabled, data.HistoryControlEnabled = true, true, true
			data.MessageEditEnabled, data.MessageRegenerateEnabled, data.QueueNextEnabled = true, true, true
			snapshot.VersionsEnabled, snapshot.ReasoningEnabled, snapshot.CompactionEnabled, snapshot.HistoryControlEnabled = true, true, true, true
			snapshot.Goal = &RuntimeGoal{SessionID: data.SessionID, BranchID: "fixture-branch", TipID: "fixture-tip", Status: "none"}
			snapshot.Steer = &RuntimeSteer{Token: "", Revision: 1, Items: []RuntimeSteerItem{}}
			snapshot.Queue = &RuntimeQueue{Token: "fixture-queue", Revision: 1, CanEnqueue: true, Items: []RuntimeQueueItem{}}
		}
		// Match the public HTTP projection, including bounded server Markdown.
		snapshot = displaySnapshot(snapshot)
		data.Live = &snapshot
		if name == "workflow" {
			// Preserve explicit legacy Abort coverage. The new sibling exercises
			// the independent per-turn cancellation capability with the same UI.
			fixtures = append(fixtures, harnessVisualFixture{Name: "workflow-cancel", Snapshot: &snapshot, Page: data})
			editData := data
			editData.MessageEditEnabled = true
			editData.SupportsStreaming = true
			fixtures = append(fixtures, harnessVisualFixture{Name: "workflow-edit", Snapshot: &snapshot, Page: editData})
			regenerateData := editData
			regenerateData.MessageRegenerateEnabled = true
			fixtures = append(fixtures, harnessVisualFixture{Name: "workflow-regenerate", Snapshot: &snapshot, Page: regenerateData})
			queueData := regenerateData
			queueData.QueueNextEnabled = true
			fixtures = append(fixtures, harnessVisualFixture{Name: "workflow-queue", Snapshot: &snapshot, Page: queueData})
			data.TurnCancelEnabled = false
		}
		fixtures = append(fixtures, harnessVisualFixture{Name: name, Snapshot: &snapshot, Page: data})
	}
	// A long inactive catalog must scroll as one column; the activation panel
	// must follow all saved rows rather than overlap a flex-shrunk live region.
	inactive := base
	inactive.View, inactive.Project = "projects", &project
	inactive.Sessions = &CatalogSessions{}
	for i := range 35 {
		inactive.Sessions.Sessions = append(inactive.Sessions.Sessions, SessionSummary{
			ID: fmt.Sprintf("fixture-saved-%02d", i+1), Name: fmt.Sprintf("Saved conversation %02d", i+1), Updated: "Yesterday",
		})
	}
	fixtures = append(fixtures, harnessVisualFixture{Name: "inactive", Page: inactive})
	trustedProject := project
	trustedProject.Trusted, trustedProject.TrustRemembered = true, true
	trustedInactive := inactive
	trustedInactive.Project, trustedInactive.Projects = &trustedProject, []Project{trustedProject}
	fixtures = append(fixtures, harnessVisualFixture{Name: "inactive-trusted", Page: trustedInactive})
	saved := base
	saved.View, saved.Project, saved.SessionID = "projects", &project, "00000000-0000-4000-8001-000000000001"
	savedSource := "## Saved public answer\n\nKeep **source** formatting and `inline code`.\n\n```go\nfmt.Println(\"<public> & exact\")\n```\n\n- Saved, not activated\n"
	saved.History = &CatalogMessages{Messages: []HistoryMessage{{ID: "fixture-saved-answer", Role: "assistant", Text: savedSource}}}
	saved.History.Messages[0].Tools = []protocol.RPCHistoryTool{
		{ID: "fixture-public-tool", OwnerID: "fixture-saved-answer", ResultID: "fixture-result", Tool: "read", Status: "completed", Output: "Literal <script>not executable</script> & public output", OutputAvailable: true},
		{ID: "fixture-legacy-tool", OwnerID: "fixture-saved-answer", ResultID: "fixture-legacy-result", Tool: "bash", Status: "failed"},
		{ID: "fixture-unresolved-tool", OwnerID: "fixture-saved-answer", Tool: "write", Status: "unresolved", Truncated: true},
	}
	saved.History.ToolsTruncated = true
	saved.Recovery = &RecoveryHint{SessionID: saved.SessionID, State: RecoveryAdmitted, UpdatedAt: time.Unix(1, 0).UTC()}
	saved.RecoveryURL = "/?view=projects&project=" + project.ID + "&session=" + saved.SessionID
	fixtures = append(fixtures, harnessVisualFixture{Name: "saved-markdown", Source: savedSource, Page: saved})
	// A separate inactive user fixture exercises edit availability without
	// changing the established Markdown/tool fixture or manufacturing browser DOM.
	savedUser := base
	savedUser.View, savedUser.Project, savedUser.SessionID = "projects", &project, "00000000-0000-4000-8001-000000000002"
	savedUser.History = &CatalogMessages{Messages: []HistoryMessage{{ID: "fixture-saved-user-message", Role: "user", Text: "Saved user text must not activate or send itself."}}}
	fixtures = append(fixtures, harnessVisualFixture{Name: "saved-user", Page: savedUser})
	fixtures = append(fixtures, harnessPolishFixtures(base, project)...)
	for _, fixture := range fixtures {
		if fixture.Snapshot != nil && fixture.Snapshot.Input != nil {
			if _, ok := projectInput(fixture.Snapshot.Input); !ok {
				t.Fatalf("%s input exceeds the production projection contract", fixture.Name)
			}
		}
		var html bytes.Buffer
		if err := templates.ExecuteTemplate(&html, fixture.template(), fixture.Page); err != nil {
			t.Fatalf("render %s: %v", fixture.Name, err)
		}
		// The browser harness must exercise the real page, not a hand-built DOM.
		requiredMarkers := []string{`/static/app.css`, `/static/app.js`}
		if fixture.template() == "page" {
			requiredMarkers = append(requiredMarkers, `id="workspace"`, `id="shell-navigation-root"`, `id="shell-react-root"`)
		}
		for _, required := range requiredMarkers {
			if !strings.Contains(html.String(), required) {
				t.Fatalf("%s is missing production page marker %s", fixture.Name, required)
			}
		}
		write(fixture.Name+".html", html.Bytes())
	}
	manifest, err := json.Marshal(fixtures)
	if err != nil {
		t.Fatal(err)
	}
	write("fixtures.json", manifest)
	t.Logf("exported %d production-template fixtures and embedded static assets to %s", len(fixtures), destination)
}
