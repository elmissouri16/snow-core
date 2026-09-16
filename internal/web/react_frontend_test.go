package web

import (
	"bytes"
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func reactBootstrap(t *testing.T, name string, data pageData) (string, string) {
	t.Helper()
	var output bytes.Buffer
	if err := templates.ExecuteTemplate(&output, name, data); err != nil {
		t.Fatal(err)
	}
	var props string
	roots := 0
	tokens := html.NewTokenizer(strings.NewReader(output.String()))
	for {
		switch tokens.Next() {
		case html.ErrorToken:
			if tokens.Err() != io.EOF {
				t.Fatal(tokens.Err())
			}
			if roots != 1 {
				t.Fatalf("React roots = %d", roots)
			}
			return props, output.String()
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokens.Token()
			if token.Data == "script" {
				t.Fatal("bootstrap created a script element")
			}
			for _, attr := range token.Attr {
				if attr.Key == "data-react-page" || attr.Key == "data-react-inspection" {
					roots++
				}
				if attr.Key == "data-react-props" {
					props = attr.Val
				}
				if strings.HasPrefix(attr.Key, "on") {
					t.Fatalf("bootstrap created event attribute %q", attr.Key)
				}
			}
		}
	}
}

func TestReactBootstrapEscapingAndProjection(t *testing.T) {
	const hostile = `"><script>alert('x')</script><img src=x onerror="alert(1)">&雪`
	project := Project{
		ID: "project-id", Name: hostile, Path: hostile, Available: true, State: "available", Issue: hostile, Pinned: true,
		Trusted: true, TrustRemembered: true, SkillsEnabled: true, Archived: true, device: "private-device", inode: "private-inode",
	}
	data := pageData{
		CSRF: hostile, Error: hostile, PairingCode: "private-pairing", SessionID: "private-session-id",
		Live: &RuntimeSnapshot{InstanceID: "private-instance", CancelToken: "private-cancel", Messages: []RuntimeMessage{{Text: "private-transcript"}}},
		Organization: &OrganizationView{
			CSRF: "private-view-csrf", Projects: []Project{project}, Archived: ArchivedProjects{Projects: []Project{project}}, Project: &project,
			Sessions: []OrganizedSession{{ID: "saved-id", Name: hostile, Updated: "today", Pinned: true, Archived: true}},
			Offset:   25, NextURL: "/?view=organization&offset=50", ArchivedNextURL: "/?view=organization&archived_offset=25", Live: false,
		},
	}
	props, markup := reactBootstrap(t, "organization", data)
	var got organizationFrontendProps
	if err := json.Unmarshal([]byte(props), &got, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	view := got.Organization
	if got.CSRF != hostile || got.Error != hostile || view == nil || len(view.Projects) != 1 || len(view.Archived) != 1 || view.Project == nil || len(view.Sessions) != 1 {
		t.Fatalf("lost bootstrap fields: %+v", got)
	}
	wantProject := organizationFrontendProject{ID: "project-id", Name: hostile, Path: hostile, Available: true, State: "available", Issue: hostile, Pinned: true}
	if view.Projects[0] != wantProject || view.Archived[0] != wantProject || *view.Project != wantProject || view.Sessions[0] != (organizationFrontendSession{ID: "saved-id", Name: hostile, Updated: "today", Pinned: true, Archived: true}) {
		t.Fatal("public projection changed")
	}
	if view.Offset != 25 || view.NextURL != data.Organization.NextURL || view.ArchivedNextURL != data.Organization.ArchivedNextURL || view.Live {
		t.Fatal("lost organization navigation/live fields")
	}
	for _, forbidden := range []string{"private-", `"Trusted"`, `"trusted"`, `"skillsEnabled"`, `"trustRemembered"`, `"device"`, `"inode"`, `"HasMore"`, `"NextOffset"`} {
		if strings.Contains(props, forbidden) || strings.Contains(markup, forbidden) {
			t.Fatalf("private field disclosed: %s", forbidden)
		}
	}
	props, _ = reactBootstrap(t, "manager-activity", pageData{RegistryEnabled: true, Error: hostile, CSRF: "private-csrf", Live: data.Live})
	var activity activityFrontendProps
	if err := json.Unmarshal([]byte(props), &activity, json.RejectUnknownMembers(true)); err != nil || !activity.RegistryEnabled || activity.Error != hostile {
		t.Fatalf("activity bootstrap: %+v, %v", activity, err)
	}
}

func TestReactBootstrapEmptyAndBounded(t *testing.T) {
	props, err := organizationReactProps("", "", &OrganizationView{})
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{`"projects":[]`, `"archived":[]`, `"project":null`, `"sessions":[]`, `"live":false`} {
		if !strings.Contains(props, required) {
			t.Fatalf("missing %s: %s", required, props)
		}
	}
	props, err = organizationReactProps("", "public error", nil)
	if err != nil || !strings.Contains(props, `"organization":null`) || !strings.Contains(props, `"error":"public error"`) {
		t.Fatalf("nil view: %q, %v", props, err)
	}
	empty, err := activityReactProps(false, "")
	if err != nil {
		t.Fatal(err)
	}
	atLimit := strings.Repeat("x", maxReactPropsBytes-len(empty))
	props, err = activityReactProps(false, atLimit)
	if err != nil || len(props) != maxReactPropsBytes {
		t.Fatalf("exact limit = %d bytes, %v", len(props), err)
	}
	if props, err := activityReactProps(false, atLimit+"x"); err == nil || props != "" {
		t.Fatal("oversized bootstrap did not fail closed")
	}
	s := testShell(t)
	w := httptest.NewRecorder()
	s.render(w, http.StatusOK, "manager-activity", pageData{Error: strings.Repeat("x", maxReactPropsBytes)})
	if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "data-react") {
		t.Fatal("oversized page emitted a partial bootstrap")
	}
}

func TestReactGeneratedAssetRoutesAndHead(t *testing.T) {
	s := testShell(t)
	for name, mime := range map[string]string{
		"app.js": "text/javascript; charset=utf-8", "THIRD-PARTY-NOTICES.txt": "text/plain; charset=utf-8",
	} {
		w := request(t, s, http.MethodGet, "/static/generated/"+name, nil)
		want, err := assets.ReadFile("static/generated/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), want) || len(want) == 0 || w.Header().Get("Content-Type") != mime {
			t.Fatalf("generated %s: status=%d MIME=%q", name, w.Code, w.Header().Get("Content-Type"))
		}
		csp := w.Header().Get("Content-Security-Policy")
		if !strings.Contains(csp, "script-src 'self'") || strings.Contains(csp, "unsafe-") || w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("asset protections changed: %v", w.Header())
		}
	}
	for _, name := range []string{"", "app.js.map", "package.json", "source.tsx", "extra.js", "nested/app.js", "app.js/extra"} {
		if w := request(t, s, http.MethodGet, "/static/generated/"+name, nil); w.Code != http.StatusNotFound {
			t.Errorf("unlisted generated asset %q = %d", name, w.Code)
		}
	}
	var head bytes.Buffer
	if err := templates.ExecuteTemplate(&head, "head", pageData{}); err != nil {
		t.Fatal(err)
	}
	if strings.Count(head.String(), `<script type="module" src="/static/generated/app.js"></script>`) != 1 {
		t.Fatal("React entrypoint missing")
	}
	for _, name := range []string{"manager-activity", "organization", "host-settings", "browser-access", "reasoning", "compaction", "goals", "versions", "history-controls", "conversation", "steer", "queue", "composer-context", "messages", "markdown", "visibility", "processes", "attention", "scroll", "shell", "sidebar-sessions", "session-actions", "project-operations", "conversation-width", "costs"} {
		if strings.Contains(head.String(), `src="/static/`+name+`.js"`) {
			t.Fatalf("retired DOM owner still loaded: %s", name)
		}
		if w := request(t, s, http.MethodGet, "/static/"+name+".js", nil); w.Code != http.StatusNotFound {
			t.Errorf("retired DOM owner still routed: %s = %d", name, w.Code)
		}
		switch name {
		case "conversation", "markdown", "visibility", "shell", "sidebar-sessions", "session-actions":
			continue // These owners use shared styles rather than a same-named CSS file.
		}
		if !strings.Contains(head.String(), `href="/static/`+name+`.css"`) {
			t.Errorf("existing styles no longer loaded: %s", name)
		}
	}
}

func TestReactHostSettingsAndBrowserBootstrap(t *testing.T) {
	const hostile = `"><script>alert('x')</script><img src=x onerror="alert(1)">&雪`
	data := pageData{
		CSRF: hostile, HostSettingsEnabled: true,
		PairingCode: "private-pairing", Error: "private-error",
		Projects: []Project{
			{ID: "available-id", Name: hostile, Available: true, Path: "private-path", Issue: "private-issue", Trusted: true, SkillsEnabled: true, TrustRemembered: true, Pinned: true, device: "private-device", inode: "private-inode"},
			{ID: "private-unavailable-id", Name: "private-unavailable-name", Available: false},
		},
		Live: &RuntimeSnapshot{InstanceID: "private-instance", CancelToken: "private-cancel", Messages: []RuntimeMessage{{Text: "private-transcript"}}},
	}
	props, markup := reactBootstrap(t, "host-settings", data)
	var host hostSettingsFrontendProps
	if err := json.Unmarshal([]byte(props), &host, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	if host.CSRF != hostile || !host.Enabled || len(host.Projects) != 1 || host.Projects[0] != (hostSettingsFrontendProject{ID: "available-id", Name: hostile}) {
		t.Fatal("host settings bootstrap lost its available-project projection")
	}
	if !strings.Contains(markup, `data-react-page="host-settings"`) || strings.Contains(markup, "host-api-key") {
		t.Fatal("host settings retained the removed browser API-key panel")
	}
	for _, forbidden := range []string{"private-", `"path"`, `"issue"`, `"trusted"`, `"available"`, `"pinned"`, `"skillsEnabled"`, `"trustRemembered"`} {
		if strings.Contains(props, forbidden) {
			t.Fatalf("host settings exposed non-projected data: %s", forbidden)
		}
	}
	props, _ = reactBootstrap(t, "host-settings", pageData{})
	if props != `{"csrf":"","enabled":false,"projects":[]}` {
		t.Fatalf("disabled host settings bootstrap: %s", props)
	}
	props, markup = reactBootstrap(t, "browser-inventory", data)
	var inventory browserInventoryFrontendProps
	if err := json.Unmarshal([]byte(props), &inventory, json.RejectUnknownMembers(true)); err != nil || inventory.CSRF != hostile {
		t.Fatalf("browser inventory bootstrap: %+v, %v", inventory, err)
	}
	if strings.Contains(props, "private-") || !strings.Contains(markup, `data-react-page="browser-access"`) {
		t.Fatal("browser bootstrap owner or privacy boundary changed")
	}
	tooLarge := strings.Repeat("x", maxReactPropsBytes)
	for name, marshal := range map[string]func() (string, error){
		"host csrf": func() (string, error) { return hostSettingsReactProps(tooLarge, true, nil) },
		"host project": func() (string, error) {
			return hostSettingsReactProps("", true, []Project{{ID: "available-id", Available: true, Name: tooLarge}})
		},
		"browser csrf": func() (string, error) { return browserInventoryReactProps(tooLarge) },
	} {
		t.Run(name, func(t *testing.T) {
			if props, err := marshal(); err == nil || props != "" {
				t.Fatal("oversized bootstrap did not fail closed")
			}
		})
	}
}

func TestReactLiveMountsAndEscapedAttributes(t *testing.T) {
	const hostile = `"><script>alert('x')</script><img src=x onerror="alert(1)">&雪`
	for _, enabled := range []bool{false, true} {
		data := pageData{
			Project:         &Project{ID: "project", Name: hostile, Path: hostile},
			Live:            &RuntimeSnapshot{InstanceID: "instance", SessionID: "session", Status: "idle", SessionName: hostile, Provider: hostile, Model: hostile, Error: hostile},
			WorkflowEnabled: enabled, PermissionPolicyEnabled: enabled, QueueNextEnabled: enabled, MessageRegenerateEnabled: enabled,
		}
		var output bytes.Buffer
		if err := templates.ExecuteTemplate(&output, "live-runtime", data); err != nil {
			t.Fatal(err)
		}
		document, err := html.Parse(&output)
		if err != nil {
			t.Fatal(err)
		}
		attribute := func(node *html.Node, key string) string {
			for _, attr := range node.Attr {
				if attr.Key == key {
					return attr.Val
				}
			}
			return ""
		}
		mounts := map[string]*html.Node{}
		var live *html.Node
		var visit func(*html.Node)
		visit = func(node *html.Node) {
			if node.Type == html.ElementNode {
				if node.Data == "script" {
					t.Fatal("public attributes produced executable markup")
				}
				for _, attr := range node.Attr {
					if attr.Key == "style" || strings.HasPrefix(attr.Key, "on") {
						t.Fatalf("unexpected inline attribute %s", attr.Key)
					}
					key := ""
					if attr.Key == "id" && strings.HasSuffix(attr.Val, "-view") {
						key = attr.Val
					}
					if attr.Key == "data-composer-context-root" || attr.Key == "data-composer-context-tools-root" || attr.Key == "data-react-composer-queue" || attr.Key == "data-react-reader-controls" || attr.Key == "data-react-width-controls" {
						key = attr.Key
					}
					if key != "" {
						if mounts[key] != nil {
							t.Fatalf("duplicate mount %s", key)
						}
						mounts[key] = node
					}
				}
				if attribute(node, "id") == "live-session" {
					live = node
				}
			}
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				visit(child)
			}
		}
		visit(document)
		if live == nil {
			t.Fatal("live identity root missing")
		}
		for _, key := range []string{"data-project-name", "data-project-path", "data-session-name", "data-provider", "data-model"} {
			if attribute(live, key) != hostile {
				t.Fatalf("attribute did not round-trip: %s", key)
			}
		}
		for _, key := range []string{"live-chrome-view", "live-composer-notices-view", "live-composer-editor-view", "live-composer-actions-view", "live-composer-status-view", "live-turn-status-view", "live-regenerate-dialog-view", "data-composer-context-root", "data-composer-context-tools-root", "data-react-composer-queue", "data-react-reader-controls", "data-react-width-controls"} {
			node := mounts[key]
			if node == nil || node.FirstChild != nil || attribute(node, "class") != "react-live-panel" {
				t.Fatalf("missing, nonempty or unstyled mount %s", key)
			}
			parent := node.Parent
			for parent != nil && parent != live {
				parent = parent.Parent
			}
			if parent == nil {
				t.Fatalf("mount outside live identity root: %s", key)
			}
		}
		if attribute(mounts["data-react-width-controls"].Parent, "id") != "live-stream" || mounts["data-react-reader-controls"].Parent != live {
			t.Fatal("width and reader controls lost native stream placement")
		}
		if attribute(mounts["live-chrome-view"], "data-initial-error") != hostile {
			t.Fatal("initial error did not round-trip")
		}
	}
}

func TestReactInspectionBootstrapProjection(t *testing.T) {
	const hostile = `"><script>alert('x')</script><img src=x onerror="alert(1)">&雪`
	project := &Project{ID: "project", Name: hostile, Path: hostile, Available: true, Issue: "private-issue", Trusted: true, TrustRemembered: true, SkillsEnabled: true, device: "private-device", inode: "private-inode"}
	live := &RuntimeSnapshot{SessionID: hostile, Provider: hostile, Model: hostile, InstanceID: "private-instance", CancelToken: "private-cancel", Messages: []RuntimeMessage{{Text: "private-transcript"}}}
	for _, runtime := range []*RuntimeSnapshot{nil, live} {
		props, markup := reactBootstrap(t, "project-inspection", pageData{Project: project, CSRF: hostile, Live: runtime, Error: "private-error", PairingCode: "private-pairing"})
		var got inspectionFrontendProps
		if err := json.Unmarshal([]byte(props), &got, json.RejectUnknownMembers(true)); err != nil {
			t.Fatal(err)
		}
		if got.CSRF != hostile || got.Project != (inspectionFrontendProject{ID: "project", Name: hostile, Path: hostile, Available: true}) {
			t.Fatal("lost public inspector data")
		}
		if runtime == nil {
			if got.Live != nil || strings.Contains(props, `"live"`) {
				t.Fatal("inactive inspector received live state")
			}
		} else if got.Live == nil || *got.Live != (inspectionFrontendLive{SessionID: hostile, Provider: hostile, Model: hostile}) {
			t.Fatal("lost public live inspector fields")
		}
		for _, forbidden := range []string{"private-", "trusted", "trust_remembered", "skills_enabled"} {
			if strings.Contains(props, forbidden) {
				t.Fatalf("inspector leaked %q", forbidden)
			}
		}
		if !strings.Contains(markup, `<aside id="project-inspector"`) || !strings.Contains(markup, `data-available="true" hidden>`) || !strings.Contains(markup, `data-react-inspection class="react-live-panel"`) {
			t.Fatal("inspector outer owner or mount changed")
		}
	}
	project.Name = strings.Repeat("x", maxReactPropsBytes)
	if _, err := inspectionReactProps(project, "csrf", live); err == nil {
		t.Fatal("oversized inspector bootstrap accepted")
	}
}

func TestReactLiveActivitySSRMetadata(t *testing.T) {
	const markerID = `event"><script>untrusted</script>`
	var output bytes.Buffer
	if err := templates.ExecuteTemplate(&output, "live-runtime", pageData{
		Project: &Project{ID: "project"},
		Live:    &RuntimeSnapshot{Messages: []RuntimeMessage{{ID: "before", Role: "assistant", Text: "Before"}, {ID: markerID, Role: "tool_activity"}, {ID: "after", Role: "assistant", Text: "After"}}, Activities: []RuntimeActivity{{ID: "activity", MessageID: markerID, Tool: "read", Status: "completed"}}},
	}); err != nil {
		t.Fatal(err)
	}
	markers, activities, transcripts := 0, 0, 0
	tokens := html.NewTokenizer(&output)
	for tokens.Next() != html.ErrorToken {
		token := tokens.Token()
		if token.Type != html.StartTagToken {
			continue
		}
		attrs := map[string]string{}
		for _, attr := range token.Attr {
			attrs[attr.Key] = attr.Val
		}
		if token.Data == "script" {
			t.Fatal("activity ID produced markup")
		}
		if _, ok := attrs["data-react-messages"]; ok {
			transcripts++
		}
		if _, ok := attrs["data-runtime-activity-group"]; ok {
			markers++
			if token.Data != "section" || attrs["data-message-id"] != markerID || attrs["data-message-role"] != "tool_activity" {
				t.Fatal("public event-step marker lost identity")
			}
		}
		if attrs["data-activity-id"] == "activity" {
			activities++
			if token.Data != "details" || attrs["data-message-id"] != markerID {
				t.Fatal("tool activity lost exact public event association")
			}
		}
	}
	if tokens.Err() != io.EOF {
		t.Fatal(tokens.Err())
	}
	if markers != 1 || activities != 1 || transcripts != 1 {
		t.Fatalf("markers=%d activities=%d transcripts=%d", markers, activities, transcripts)
	}
}
