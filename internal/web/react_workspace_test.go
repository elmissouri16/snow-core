package web

import (
	"bytes"
	"encoding/json/v2"
	"io"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// Select one page bootstrap from a document with independent sibling owners.
func reactPageProps(t *testing.T, markup, page string) string {
	t.Helper()
	var props string
	count := 0
	tokens := html.NewTokenizer(strings.NewReader(markup))
	for tokens.Next() != html.ErrorToken {
		token := tokens.Token()
		if token.Type != html.StartTagToken {
			continue
		}
		name, value := "", ""
		for _, attr := range token.Attr {
			if attr.Key == "data-react-page" {
				name = attr.Val
			}
			if attr.Key == "data-react-props" {
				value = attr.Val
			}
		}
		if name == page {
			props = value
			count++
		}
	}
	if tokens.Err() != io.EOF || count != 1 {
		t.Fatalf("%s bootstrap count=%d error=%v", page, count, tokens.Err())
	}
	return props
}

func TestReactWorkspaceAndShellBootstrap(t *testing.T) {
	const hostile = `"><script>alert('x')</script><img src=x onerror="alert(1)">&雪`
	project := Project{ID: "00000000-0000-0000-0000-000000000001", Name: hostile, Path: hostile, Available: true, Trusted: true, TrustRemembered: true, SkillsEnabled: true, Pinned: true, Issue: "private-issue", device: "private-device", inode: "private-inode"}
	data := pageData{Projects: []Project{project}, Project: &project, CSRF: hostile, Version: "fixture-version", View: "projects", SessionID: "00000000-0000-0000-0000-000000000002", Error: hostile, RegistryEnabled: true, RuntimeEnabled: true, HostSettingsEnabled: true, HostAPIKeyEnabled: true, TLS: true, PairingCode: "fixture-pair-code", ProjectOperationsEnabled: true,
		Sessions: &CatalogSessions{Sessions: []SessionSummary{{ID: "00000000-0000-0000-0000-000000000002", Name: hostile}}},
		History:  &CatalogMessages{Messages: []HistoryMessage{{ID: "message", Role: "assistant", Text: "public-SSR-history-not-JSON"}}},
	}
	for _, tc := range []struct {
		template, page string
		project        bool
	}{{"home", "home", true}, {"projects", "workspace-catalog", false}, {"projects", "workspace-cold", true}, {"login", "login", true}} {
		current := data
		if !tc.project {
			current.Project = nil
		}
		var output bytes.Buffer
		if err := templates.ExecuteTemplate(&output, tc.template, current); err != nil {
			t.Fatal(err)
		}
		props := reactPageProps(t, output.String(), tc.page)
		if strings.Contains(props, "private-") || strings.Contains(props, "public-SSR-history-not-JSON") || strings.Contains(props, "fixture-pair-code") {
			t.Fatalf("%s received unrelated state", tc.page)
		}
		switch tc.page {
		case "home":
			var got homeFrontendProps
			if err := json.Unmarshal([]byte(props), &got, json.RejectUnknownMembers(true)); err != nil {
				t.Fatal(err)
			}
			if got.Error != hostile || len(got.Projects) != 1 || got.Projects[0] != workspaceProjectProjection(project) {
				t.Fatal("home projection mismatch")
			}
		case "workspace-catalog":
			var got workspaceCatalogFrontendProps
			if err := json.Unmarshal([]byte(props), &got, json.RejectUnknownMembers(true)); err != nil {
				t.Fatal(err)
			}
			if got.CSRF != hostile || !got.RegistryEnabled || !got.ProjectOperationsEnabled || len(got.Projects) != 1 {
				t.Fatal("catalog projection mismatch")
			}
		case "workspace-cold":
			var got workspaceColdFrontendProps
			if err := json.Unmarshal([]byte(props), &got, json.RejectUnknownMembers(true)); err != nil {
				t.Fatal(err)
			}
			if got.CSRF != hostile || got.SessionID != data.SessionID || got.SessionTitle != hostile || !got.HasHistory || !got.RuntimeEnabled {
				t.Fatal("cold projection mismatch")
			}
			for _, marker := range []string{`class="catalog-history"`, `data-react-messages`, `class="message-source" hidden>public-SSR-history-not-JSON`, `data-react-inspection`} {
				if !strings.Contains(output.String(), marker) {
					t.Fatalf("cold SSR lost %s", marker)
				}
			}
		case "login":
			var got loginFrontendProps
			if err := json.Unmarshal([]byte(props), &got, json.RejectUnknownMembers(true)); err != nil {
				t.Fatal(err)
			}
			if got.CSRF != hostile || got.Error != hostile || !strings.Contains(output.String(), "Snow fixture-version") {
				t.Fatal("login projection or outer footer changed")
			}
		}
	}
	data.Live = &RuntimeSnapshot{SessionID: data.SessionID, InstanceID: "instance", SessionName: hostile, CancelToken: "private-cancel", Messages: []RuntimeMessage{{Text: "private-runtime-transcript"}}}
	data.WorkflowEnabled = true
	var output bytes.Buffer
	if err := templates.ExecuteTemplate(&output, "workspace", data); err != nil {
		t.Fatal(err)
	}
	props := reactPageProps(t, output.String(), "shell")
	var shell shellFrontendProps
	if err := json.Unmarshal([]byte(props), &shell, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	if shell.CSRF != hostile || !shell.TLS || !shell.APIKeyEnabled || !shell.HostSettingsEnabled || shell.PairingCode != data.PairingCode || len(shell.Projects) != 1 || !shell.Projects[0].TrustRemembered || len(shell.Sessions) != 1 {
		t.Fatal("shell public projection mismatch")
	}
	if shell.Live == nil || shell.Live.Title != hostile || !shell.Live.RenameAvailable || !shell.Live.RenameDisabled || !shell.Live.NewDisabled {
		t.Fatal("initial shell controls must await verified admission")
	}
	if strings.Contains(props, "private-") {
		t.Fatal("shell received private state")
	}
	if !strings.Contains(output.String(), `<div id="shell-navigation-root" class="react-live-panel"></div>`) || strings.Contains(output.String(), `id="settings-dialog"`) || strings.Contains(output.String(), `data-react-page="host-settings"`) || strings.Contains(output.String(), `data-react-page="browser-access"`) {
		t.Fatal("shell retained competing or nested owners")
	}
	if _, err := workspaceColdReactProps(data); err == nil {
		t.Fatal("cold bootstrap accepted live runtime")
	}
}

func TestReactWorkspaceBootstrapBounds(t *testing.T) {
	projects := make([]Project, MaxProjects+1)
	if _, err := homeReactProps(projects, ""); err == nil {
		t.Fatal("home project limit missing")
	}
	if _, err := workspaceCatalogReactProps(pageData{Projects: projects}); err == nil {
		t.Fatal("catalog project limit missing")
	}
	if _, err := shellReactProps(pageData{Projects: projects}); err == nil {
		t.Fatal("shell project limit missing")
	}
	if _, err := loginReactProps("csrf", strings.Repeat("x", maxReactPropsBytes)); err == nil {
		t.Fatal("login bootstrap byte limit missing")
	}
	if _, err := shellReactProps(pageData{PairingCode: strings.Repeat("x", 129)}); err == nil {
		t.Fatal("pairing-code display limit missing")
	}
	props, err := shellReactProps(pageData{Project: &Project{ID: "project"}, Sessions: &CatalogSessions{Sessions: make([]SessionSummary, 101)}})
	if err != nil {
		t.Fatal(err)
	}
	var shell shellFrontendProps
	if err := json.Unmarshal([]byte(props), &shell); err != nil {
		t.Fatal(err)
	}
	if len(shell.Sessions) != 100 {
		t.Fatal("shell session rows are not bounded")
	}
}
