// Package mcp adapts current MCP servers to Snow's tool registry using the
// official Go SDK. The SDK negotiates the stateless 2026-07-28 protocol and
// falls back across supported legacy protocol revisions.
package mcp

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/snow-core/snow/internal/tools"
	publicmcp "github.com/snow-core/snow/pkg/mcp"
)

const (
	defaultConnectTimeout  = 15 * time.Second
	defaultRefreshTimeout  = 30 * time.Second
	defaultRefreshDebounce = 100 * time.Millisecond
	defaultCloseTimeout    = 5 * time.Second
	defaultMaxOutput       = 256 << 10
	maxPages               = 100
)

// Declaration is one resolved MCP declaration plus its cache-partitioning
// origin. Scope is global, project, or explicit. ProjectIdentity is canonical
// and is only hashed into cache keys.
type Declaration struct {
	Spec            publicmcp.ServerSpec
	Scope           string
	ProjectIdentity string
}

// Options configure an MCP manager.
type Options struct {
	CWD             string
	Roots           []string
	HostName        string
	HostVersion     string
	MaxOutputBytes  int
	RefreshTimeout  time.Duration
	RefreshDebounce time.Duration
	// CacheRoot is Snow's private application directory. Empty disables the
	// durable MCP catalog cache.
	CacheRoot string
	// Now is used by deterministic cache and lifecycle tests.
	Now func() time.Time
	// ForceRefresh bypasses cache reads for explicit cache-refresh operations.
	// Successful live negotiation still replaces the cache atomically.
	ForceRefresh bool
	// ServerStderr optionally receives stderr written by stdio MCP child
	// processes. It defaults to io.Discard so child diagnostics cannot corrupt
	// interactive terminal surfaces.
	ServerStderr io.Writer
}

// Manager owns MCP clients, sessions, dynamically registered tools, and
// secret-free status diagnostics.
type Manager struct {
	mu         sync.RWMutex
	registry   tools.Registry
	opts       Options
	runtimes   map[string]*serverRuntime
	statuses   map[string]publicmcp.Status
	claimed    map[string]bool
	onChanged  func([]tools.ToolDescriptor) error
	closed     bool
	connectWG  sync.WaitGroup
	connectErr error
	cache      *catalogCache
}

type runtimeConnectAttempt struct {
	done chan struct{}
	err  error
}

type serverRuntime struct {
	mu      sync.Mutex
	manager *Manager
	decl    Declaration
	spec    publicmcp.ServerSpec
	client  *sdkmcp.Client
	session *sdkmcp.ClientSession
	closed  bool // retained for compatibility with focused internal tests
	owner   string
	used    map[string]string

	state            runtimeState
	lazyEligible     bool
	connectErr       error
	connectAttempt   *runtimeConnectAttempt
	warning          string
	transitionDone   chan struct{}
	activeCalls      int
	refreshing       bool
	lastUsed         time.Time
	idleTimer        *time.Timer
	generation       uint64
	cached           cachedCatalog
	cacheKey         string
	liveTools        map[string]string
	liveCapabilities map[string]bool
	subscriptions    map[string]struct{}
	runtimeCtx       context.Context
	runtimeCancel    context.CancelFunc

	subscriptionMu sync.Mutex
	refreshMu      sync.Mutex
	refreshCtx     context.Context
	refreshCancel  context.CancelFunc
	refreshReq     chan struct{}
	refreshStop    chan struct{}
	refreshDone    chan struct{}
}

type headerTransport struct {
	base    http.RoundTripper
	headers map[string]string
}

func (m *Manager) now() time.Time {
	if m.opts.Now != nil {
		return m.opts.Now()
	}
	return time.Now()
}
