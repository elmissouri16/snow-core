package web

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed templates/*.html static/*.css static/*.txt static/app.js static/menus.js static/stream.js static/generated/*
var assets embed.FS

var templates = template.Must(template.New("web").Funcs(template.FuncMap{
	"markdown":                   markdownHTML,
	"activityReactProps":         activityReactProps,
	"organizationReactProps":     organizationReactProps,
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
	network        requestNetworkPolicy
	initialCode    string
	access         accessState
	now            func() time.Time
	registry       *Registry
	catalog        Catalog
	runtimes       RuntimeBackend
	hostSettings   HostSettingsBackend
	operations     *ProjectOperations
	projectControl sync.Mutex // Serial admission of activation versus registration removal.
	sidebarReads   sidebarReadAdmission
	inspectSlots   chan struct{}
	imageSlots     chan struct{}
}

type pageData struct {
	NetworkProfile           string
	ReasoningEnabled         bool
	AuthEnabled              bool
	MCPEnabled               bool
	SubagentsEnabled         bool
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
	ProjectsDir              string
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
	return newShellWithNetwork(origin, version, requestNetworkPolicy{})
}

func newShellWithNetwork(origin, version string, network requestNetworkPolicy) (*shell, error) {
	canonicalOrigin := canonicalBrowserOrigin
	if network.profile == "trusted-lan-http" {
		canonicalOrigin = canonicalTrustedLANOrigin
	}
	canonical, host, err := canonicalOrigin(origin)
	if err != nil {
		return nil, err
	}
	if network.profile == "" {
		network.profile = "local"
	}
	s := &shell{origin: canonical, host: host, version: safeVersion(version), network: network, now: time.Now, inspectSlots: make(chan struct{}, 4), imageSlots: make(chan struct{}, 4)}
	_, _ = rand.Read(s.access.key[:])
	s.access.sessions = make(map[[32]byte]browserSession)
	s.access.sourceAttempts = make(map[string]pairingAttemptWindow)
	s.initialCode = s.issuePairingLocked()
	return s, nil
}

func (s *shell) handler() http.Handler {
	return s.handlerForBoundary(requestBoundary{origin: s.origin, host: s.host, network: s.network})
}

func (s *shell) handlerForOrigin(origin string, network requestNetworkPolicy) (http.Handler, error) {
	canonicalOrigin := canonicalBrowserOrigin
	if network.profile == "trusted-lan-http" {
		canonicalOrigin = canonicalTrustedLANOrigin
	}
	canonical, host, err := canonicalOrigin(origin)
	if err != nil {
		return nil, err
	}
	if network.profile == "" {
		network.profile = "local"
	}
	return s.handlerForBoundary(requestBoundary{origin: canonical, host: host, network: network}), nil
}

func (s *shell) requestBoundary(r *http.Request) requestBoundary {
	if boundary, ok := r.Context().Value(requestBoundaryKey{}).(requestBoundary); ok {
		return boundary
	}
	return requestBoundary{origin: s.origin, host: s.host, network: s.network}
}

func (s *shell) handlerForBoundary(boundary requestBoundary) http.Handler {
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
	mux.HandleFunc("POST /settings/projects-dir", s.saveProjectsDir)
	mux.HandleFunc("GET /activity", s.managerActivity)
	s.registerOrganizationRoutes(mux)
	s.registerProcessControlRoutes(mux)
	s.registerBrowserAccessRoutes(mux)
	s.registerHostSettingsRoutes(mux)
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
	for name, asset := range staticAssets() {
		mux.HandleFunc("GET /static/"+name, asset.serve)
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
		source, admitted := boundary.network.admit(r)
		if !admitted || r.Host != boundary.host || r.URL.IsAbs() || len(r.Header.Values("Origin")) > 1 {
			http.Error(w, "Unexpected host or origin", http.StatusForbidden)
			return
		}
		ctx := context.WithValue(r.Context(), requestSourceKey{}, source)
		r = r.WithContext(context.WithValue(ctx, requestBoundaryKey{}, boundary))
		origin := r.Header.Get("Origin")
		if origin != "" && origin != boundary.origin || r.Method != http.MethodGet && r.Method != http.MethodHead && origin != boundary.origin {
			http.Error(w, "Same-origin request required", http.StatusForbidden)
			return
		}
		site := r.Header.Get("Sec-Fetch-Site")
		if site != "" && site != "same-origin" && site != "none" && !isTopLevelNavigation(r) {
			http.Error(w, "Cross-site request rejected", http.StatusForbidden)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

// isTopLevelNavigation reports a side-effect-free document navigation such as
// a typed URL, bookmark, or link from another site. GET handlers never mutate
// state, so admitting these keeps links to the manager working while
// cross-site fetches, subresources, embeds, and every POST stay rejected.
func isTopLevelNavigation(r *http.Request) bool {
	return (r.Method == http.MethodGet || r.Method == http.MethodHead) &&
		r.Header.Get("Sec-Fetch-Mode") == "navigate" &&
		r.Header.Get("Sec-Fetch-Dest") == "document"
}

const workspaceNavigationHeader = "X-Snow-Navigation"

func isWorkspaceNavigation(r *http.Request) bool {
	return r.Header.Get(workspaceNavigationHeader) == "workspace"
}

func (s *shell) requireLogin(w http.ResponseWriter, r *http.Request) {
	if isWorkspaceNavigation(r) || strings.HasPrefix(r.Header.Get("Accept"), "application/json") {
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
	http.SetCookie(w, s.localCookie(s.pairCookieName(r), csrf, 5*60))
	s.render(w, http.StatusOK, "login", pageData{CSRF: csrf, NetworkProfile: s.requestBoundary(r).network.profile})
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
	w.Header().Set("Vary", workspaceNavigationHeader)
	name := "page"
	if isWorkspaceNavigation(r) {
		name = "workspace"
	}
	data := pageData{View: view, CSRF: browser.CSRF, NetworkProfile: s.requestBoundary(r).network.profile, HostSettingsEnabled: s.hostSettings != nil}
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

// staticAssetTypes is the explicit asset allowlist with its served MIME types.
var staticAssetTypes = map[string]string{
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
	"reasoning.css":          "text/css; charset=utf-8",
	"auth.css":               "text/css; charset=utf-8",
	"history-controls.css":   "text/css; charset=utf-8",
	"compaction.css":         "text/css; charset=utf-8",
	"steer.css":              "text/css; charset=utf-8",
	"project-operations.css": "text/css; charset=utf-8", "costs.css": "text/css; charset=utf-8",
	"attention.css": "text/css; charset=utf-8", "scroll.css": "text/css; charset=utf-8", "conversation-width.css": "text/css; charset=utf-8", "stream.js": "text/javascript; charset=utf-8", "inspection.css": "text/css; charset=utf-8",
	"composer-context.css": "text/css; charset=utf-8",
	"settings.css":         "text/css; charset=utf-8", "HARNESS-NOTICE.txt": "text/plain; charset=utf-8",
}

// staticAssets compresses and hashes the embedded allowlist once per process;
// handlers for every listener and every test share the same immutable bodies.
var staticAssets = sync.OnceValue(func() map[string]*staticAsset {
	types := make(map[string]string, len(staticAssetTypes)+8)
	for name, contentType := range staticAssetTypes {
		types[name] = contentType
	}
	// Code-split chunks carry build hashes in their names, so they are listed
	// from the embedded tree rather than spelled out; only plain .js files
	// with safe names qualify, never maps, sources or nested paths.
	entries, err := assets.ReadDir("static/generated")
	if err != nil {
		panic(err)
	}
	for _, entry := range entries {
		if name := entry.Name(); !entry.IsDir() && generatedChunkName(name) {
			types["generated/"+name] = "text/javascript; charset=utf-8"
		}
	}
	built := make(map[string]*staticAsset, len(types))
	for name, contentType := range types {
		data, err := assets.ReadFile("static/" + name)
		if err != nil {
			panic(err) // embed declarations and route list are compile-time inputs.
		}
		built[name] = newStaticAsset(data, contentType)
	}
	return built
})

// staticAsset serves one embedded file with a content hash for revalidation
// and a precompressed body. Pages stay no-store, but the megabyte of scripts
// and stylesheets behind them only needs a 304 after the first load.
type staticAsset struct {
	contentType string
	etag        string
	raw, gzip   []byte
}

func newStaticAsset(data []byte, contentType string) *staticAsset {
	sum := sha256.Sum256(data)
	asset := &staticAsset{contentType: contentType, etag: `"` + hex.EncodeToString(sum[:16]) + `"`, raw: data}
	var buffer bytes.Buffer
	writer, _ := gzip.NewWriterLevel(&buffer, gzip.BestCompression)
	if _, err := writer.Write(data); err == nil && writer.Close() == nil && buffer.Len() < len(data) {
		asset.gzip = buffer.Bytes()
	}
	return asset
}

func (a *staticAsset) serve(w http.ResponseWriter, r *http.Request) {
	header := w.Header()
	header.Set("Content-Type", a.contentType)
	header.Set("Cache-Control", "no-cache")
	header.Set("ETag", a.etag)
	header.Set("Vary", "Accept-Encoding")
	for _, candidate := range strings.Split(r.Header.Get("If-None-Match"), ",") {
		if strings.TrimSpace(candidate) == a.etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	body := a.raw
	if a.gzip != nil && acceptsGzip(r.Header.Get("Accept-Encoding")) {
		header.Set("Content-Encoding", "gzip")
		body = a.gzip
	}
	header.Set("Content-Length", strconv.Itoa(len(body)))
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

func acceptsGzip(value string) bool {
	for _, encoding := range strings.Split(value, ",") {
		name, _, _ := strings.Cut(strings.TrimSpace(encoding), ";")
		if name == "gzip" {
			return true
		}
	}
	return false
}

func generatedChunkName(name string) bool {
	if !strings.HasSuffix(name, ".js") || len(name) > 128 || name == "app.js" {
		return false
	}
	for _, ch := range strings.TrimSuffix(name, ".js") {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_') {
			return false
		}
	}
	return true
}
