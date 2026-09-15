package web

import (
	"encoding/json/v2"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

func TestHarnessHTTPAssetsAndLanding(t *testing.T) {
	s, cookie, _ := projectShell(t)
	response := request(t, s, "GET", "/", nil, cookie)
	body := response.Body.String()
	if response.Code != http.StatusOK {
		t.Fatalf("landing status = %d", response.Code)
	}
	for _, marker := range []string{`class="home-landing"`, `id="home-prompt"`, `data-react-page="home"`, `id="shell-navigation-root"`, `id="shell-react-root"`, `data-react-page="shell"`} {
		if !strings.Contains(body, marker) {
			t.Errorf("landing missing %s", marker)
		}
	}
	assetURLs := regexp.MustCompile(`(?:src|href)="(/static/[^"]+)"`).FindAllStringSubmatch(body, -1)
	if len(assetURLs) == 0 {
		t.Fatal("page did not reference static assets")
	}
	for _, match := range assetURLs {
		url := match[1]
		response := request(t, s, "GET", url, nil, cookie)
		want, err := assets.ReadFile(strings.TrimPrefix(url, "/"))
		if err != nil || response.Code != http.StatusOK || response.Body.String() != string(want) {
			t.Errorf("page asset %s is not served intact by production handler: status %d, embedded error %v", url, response.Code, err)
		}
	}
	var shell shellFrontendProps
	if err := json.Unmarshal([]byte(reactPageProps(t, body, "shell")), &shell, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	if shell.CSRF != csrfFor(t, s, cookie) || shell.View != "overview" {
		t.Fatal("Shell settings lost explicit CSRF or view bootstrap")
	}
	// Settings forms are JSX-owned; HTTP authorization and native form behavior
	// remain covered by access/trust tests and the integrated browser gates.
	for _, unsupported := range []string{`data-theme-choice="system"`, `data-settings-section="plugins"`, `data-settings-section="presets"`} {
		if strings.Contains(body, unsupported) {
			t.Errorf("Settings exposes unsupported control %s", unsupported)
		}
	}
	for _, order := range [][2]string{
		{`href="/static/app.css"`, `href="/static/harness.css"`},
		{`src="/static/menus.js"`, `src="/static/generated/app.js"`},
		{`href="/static/scroll.css"`, `href="/static/conversation-width.css"`},
	} {
		first, second := strings.Index(body, order[0]), strings.Index(body, order[1])
		if first < 0 || second < 0 || first >= second {
			t.Errorf("asset order must be %s before %s", order[0], order[1])
		}
	}
	for _, asset := range []struct{ name, contentType, marker string }{
		{"harness.css", "text/css", ".home-landing"},
		{"menus.js", "text/javascript", "SnowMenus"},
		{"menus.css", "text/css", ".snow-menu"},
		{"settings.css", "text/css", ".settings-dialog"},
		{"messages.css", "text/css", ".markdown-body"},
		{"queue.css", "text/css", ".queue"},
		{"conversation-width.css", "text/css", ".chat-width-handle"},
	} {
		t.Run(asset.name, func(t *testing.T) {
			response := request(t, s, "GET", "/static/"+asset.name, nil, cookie)
			if response.Code != http.StatusOK || !strings.HasPrefix(response.Header().Get("Content-Type"), asset.contentType) || !strings.Contains(response.Body.String(), asset.marker) {
				t.Fatal("embedded shell asset is not served by the HTTP allowlist")
			}
		})
	}
}
