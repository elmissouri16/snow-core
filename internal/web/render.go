package web

import (
	"bytes"
	"context"
	"crypto/rand"
	"embed"
	"html/template"
	"net/http"
	"sync"
	"time"
)

//go:embed templates/*.html static/* static/vendor/*
var assets embed.FS

var templates = template.Must(template.New("web").Funcs(template.FuncMap{
	"markdown":                   markdownHTML,
	"activityReactProps":         activityReactProps,
	"organizationReactProps":     organizationReactProps,
	"hostSettingsReactProps":     hostSettingsReactProps,
	"browserInventoryReactProps": browserInventoryReactProps,
	"inspectionReactProps":       inspectionReactProps,
	"homeReactProps":             homeReactProps,
	"workspaceCatalogReactProps": workspaceCatalogReactProps,
	"workspaceColdReactProps":    workspaceColdReactProps,
	"loginReactProps":            loginReactProps,
	"shellReactProps":            shellReactProps,
}).ParseFS(assets, "templates/*.html"))

type shell struct {
	origin         string
	host           string
	version        string
	initialCode    string
	access         accessState
	now            func() time.Time
	registry       *Registry
	catalog        Catalog
	runtimes       RuntimeBackend
	hostSettings   HostSettingsBackend
	operations     *ProjectOperations
	hostAPIKeys    hostAPIKeyState
	projectControl sync.Mutex // Serial admission of activation versus registration removal.
	sidebarReads   sidebarReadAdmission
	inspectSlots   chan struct{}
	imageSlots     chan struct{}
}

type pageData struct {
	TLS                      bool
	HostAPIKeyEnabled        bool
	ReasoningEnabled         bool
	HistoryControlEnabled    bool
	CompactionEnabled        bool
	ProjectOperationsEnabled bool
	HostSettingsEnabled      bool
	GoalsEnabled             bool
	ProcessControlEnabled    bool
	VersionsEnabled          bool
	Organization             *OrganizationView
	QueueNextEnabled         bool
	View                     string
	Version                  string
	CSRF                     string
	Error                    string
	PairingCode              string
	RegistryEnabled          bool
	Projects                 []Project
	Project                  *Project
	Sessions                 *CatalogSessions
	SessionID                string
	NewSession               bool
	History                  *CatalogMessages
	Recovery                 *RecoveryHint
	RecoveryURL              string
	NextURL                  string
	RuntimeEnabled           bool
	TurnCancelEnabled        bool
	WorkflowEnabled          bool
	MessageEditEnabled       bool
	MessageRegenerateEnabled bool
	PermissionPolicyEnabled  bool
	SupportsStreaming        bool
	Live                     *RuntimeSnapshot
}

func newShell(origin, version string) (*shell, error) {
	canonical, host, err := canonicalLoopbackOrigin(origin)
	if err != nil {
		return nil, err
	}
	s := &shell{origin: canonical, host: host, version: safeVersion(version), now: time.Now, inspectSlots: make(chan struct{}, 4), imageSlots: make(chan struct{}, 4)}
	_, _ = rand.Read(s.access.key[:])
	s.access.sessions = make(map[[32]byte]browserSession)
	s.initialCode = s.issuePairingLocked()
	return s, nil
}

func (s *shell) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("GET /login", s.loginPage)
	mux.HandleFunc("POST /login", s.login)
	mux.HandleFunc("POST /logout", s.logout)
	mux.HandleFunc("POST /access/pair", s.pair)
	mux.HandleFunc("POST /access/revoke-all", s.revokeAll)
	mux.HandleFunc("POST /projects/add", s.addProject)
	mux.HandleFunc("POST /projects/folders", s.hostFolders)
	mux.HandleFunc("POST /projects/{project}/remove", s.removeProject)
	mux.HandleFunc("GET /activity", s.managerActivity)
	s.registerOrganizationRoutes(mux)
	s.registerProcessControlRoutes(mux)
	s.registerBrowserAccessRoutes(mux)
	s.registerHostSettingsRoutes(mux)
	s.registerHostAPIKeyRoutes(mux)
	s.registerProjectOperationRoutes(mux, s.operations)
	mux.HandleFunc("GET /projects/{project}/sidebar-sessions", s.sidebarSessions)
	mux.HandleFunc("POST /projects/{project}/sessions/{session}/delete", s.deleteSession)
	mux.HandleFunc("GET /projects/{project}/runtime", s.runtimeSnapshot)
	mux.HandleFunc("GET /projects/{project}/runtime/images/{message}/{index}", s.runtimeImageHTTP)
	mux.HandleFunc("GET /projects/{project}/sessions/{session}/images/{message}/{index}", s.savedImageHTTP)
	mux.Handle("GET /projects/{project}/runtime/events", s.runtimeEventsHandler())
	mux.HandleFunc("POST /projects/{project}/runtime/{action}", s.runtimeAction)
	mux.HandleFunc("POST /projects/{project}/trust/revoke", s.revokeProjectTrust)
	mux.HandleFunc("POST /projects/{project}/skills", s.saveProjectSkills)
	mux.HandleFunc("POST /projects/{project}/inspect/{action}", s.inspectProject)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	// Explicit asset allowlist: no directory listings or template exposure.
	for name, contentType := range map[string]string{
		"app.css": "text/css; charset=utf-8", "app.js": "text/javascript; charset=utf-8",
		"generated/app.js":                  "text/javascript; charset=utf-8",
		"generated/THIRD-PARTY-NOTICES.txt": "text/plain; charset=utf-8",
		"harness.css":                       "text/css; charset=utf-8", "menus.css": "text/css; charset=utf-8", "menus.js": "text/javascript; charset=utf-8",
		"dialogs.css":  "text/css; charset=utf-8",
		"messages.css": "text/css; charset=utf-8", "queue.css": "text/css; charset=utf-8",
		"manager-activity.css": "text/css; charset=utf-8",
		"organization.css":     "text/css; charset=utf-8",
		"versions.css":         "text/css; charset=utf-8",
		"goals.css":            "text/css; charset=utf-8",
		"processes.css":        "text/css; charset=utf-8", "browser-access.css": "text/css; charset=utf-8",
		"host-settings.css":      "text/css; charset=utf-8",
		"host-api-key.css":       "text/css; charset=utf-8",
		"reasoning.css":          "text/css; charset=utf-8",
		"history-controls.css":   "text/css; charset=utf-8",
		"compaction.css":         "text/css; charset=utf-8",
		"steer.css":              "text/css; charset=utf-8",
		"project-operations.css": "text/css; charset=utf-8", "costs.css": "text/css; charset=utf-8",
		"attention.css": "text/css; charset=utf-8", "scroll.css": "text/css; charset=utf-8", "conversation-width.css": "text/css; charset=utf-8", "stream.js": "text/javascript; charset=utf-8", "inspection.css": "text/css; charset=utf-8",
		"composer-context.css": "text/css; charset=utf-8",
		"settings.css":         "text/css; charset=utf-8", "HARNESS-NOTICE.txt": "text/plain; charset=utf-8",
		"vendor/htmx-2.0.10.min.js": "text/javascript; charset=utf-8",
	} {
		data, err := assets.ReadFile("static/" + name)
		if err != nil {
			panic(err) // embed declarations and route list are compile-time inputs.
		}
		mux.HandleFunc("GET /static/"+name, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", contentType)
			_, _ = w.Write(data)
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		// no-referrer makes normal browser form POSTs send Origin: null.
		// same-origin preserves our strict Origin check without external referrers.
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		// blob: is only for local composer object-URL previews; saved/sent images
		// use authenticated same-origin reads. Data and external images stay blocked.
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' blob:; connect-src 'self'; font-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'; object-src 'none'")
		// Never trust proxy headers; this first increment is direct loopback only.
		if r.Host != s.host || r.URL.IsAbs() || len(r.Header.Values("Origin")) > 1 {
			http.Error(w, "Unexpected host or origin", http.StatusForbidden)
			return
		}
		origin := r.Header.Get("Origin")
		if origin != "" && origin != s.origin || r.Method != http.MethodGet && r.Method != http.MethodHead && origin != s.origin {
			http.Error(w, "Same-origin request required", http.StatusForbidden)
			return
		}
		site := r.Header.Get("Sec-Fetch-Site")
		if site != "" && site != "same-origin" && site != "none" {
			http.Error(w, "Cross-site request rejected", http.StatusForbidden)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (s *shell) requireLogin(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/login")
		http.Error(w, "Pair this browser to continue", http.StatusUnauthorized)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *shell) loginPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.browser(r); ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	csrf := s.pairCSRF()
	http.SetCookie(w, s.localCookie(pairCookie, csrf, 5*60))
	s.render(w, http.StatusOK, "login", pageData{CSRF: csrf})
}

func (s *shell) home(w http.ResponseWriter, r *http.Request) {
	browser, ok := s.browser(r)
	if !ok {
		s.requireLogin(w, r)
		return
	}
	view := r.URL.Query().Get("view")
	if view == "" {
		view = "overview"
	}
	switch view {
	case "overview", "projects", "preview", "access", "activity", "organization":
	default:
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Vary", "HX-Request, HX-History-Restore-Request")
	name := "page"
	if r.Header.Get("HX-Request") == "true" && r.Header.Get("HX-History-Restore-Request") != "true" {
		name = "workspace"
	}
	data := pageData{View: view, CSRF: browser.CSRF, TLS: r.TLS != nil, HostSettingsEnabled: s.hostSettings != nil, HostAPIKeyEnabled: r.TLS != nil && s.hostAPIKeyEnabled()}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	if err := s.projectData(ctx, r.URL.Query(), &data); err != nil {
		data.Error = err.Error() // Only fixed public messages, never worker diagnostics.
	}
	if view == "access" {
		data.PairingCode = s.takePairingCode(r)
	}
	if view == "organization" {
		var err error
		data.Organization, err = s.organizationData(ctx, r.URL.Query(), browser.CSRF)
		if err != nil {
			data.Error = err.Error()
		}
	}
	s.render(w, http.StatusOK, name, data)
}

func (s *shell) render(w http.ResponseWriter, status int, name string, data pageData) {
	data.Version = s.version
	_, data.SupportsStreaming = s.runtimes.(RuntimeSubscriber)
	var buffer bytes.Buffer
	if err := templates.ExecuteTemplate(&buffer, name, data); err != nil {
		http.Error(w, "Unable to render this page", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buffer.Bytes())
}
