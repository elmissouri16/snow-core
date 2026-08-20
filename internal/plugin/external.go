package plugin

import (
	"encoding/json"
	"io"
	"os/exec"
	"regexp"
	"sync"

	"github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const (
	defaultFrameBytes    = 4 * 1024 * 1024
	defaultOutputBytes   = 256 * 1024
	defaultProgressBytes = 16 * 1024
)

// ExternalInitResult is the version-2 initialize result.
type ExternalInitResult struct {
	Manifest        plugin.Manifest                 `json:"manifest"`
	Capabilities    []string                        `json:"capabilities,omitempty"`
	Tools           []plugin.ExternalToolDefinition `json:"tools,omitempty"`
	SupportedEvents []plugin.EventType              `json:"supported_events,omitempty"`
	Limits          map[string]int                  `json:"limits,omitempty"`

	// Name and Version are accepted as a convenience for small runtimes. New
	// runtimes should return Manifest.
	Name            string `json:"name,omitempty"`
	Version         string `json:"version,omitempty"`
	ProtocolVersion int    `json:"protocol_version,omitempty"`
}

type rpcResponseV2 struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type rpcRequestV2 struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type pendingV2 struct {
	callID   string
	progress func(ProgressNotification)
	result   chan rpcResponseV2
}

// ProgressNotification is a bounded progress notification from a child.
type ProgressNotification struct {
	CallID  string `json:"call_id"`
	Message string `json:"message,omitempty"`
	Done    bool   `json:"done"`
	IsError bool   `json:"is_error,omitempty"`
}

type logNotification struct {
	Severity string `json:"severity,omitempty"`
	Message  string `json:"message,omitempty"`
}

// ExternalHost manages one JSON-RPC 2.0-over-stdio plugin process. It has one
// reader multiplexer and correlates all responses by string request IDs.
type ExternalHost struct {
	spec plugin.PluginSpec
	cwd  string
	cmd  *exec.Cmd
	in   io.WriteCloser

	writeToken chan struct{}
	mu         sync.Mutex
	pending    map[string]*pendingV2
	failed     error
	done       chan struct{}
	failOnce   sync.Once
	nextID     uint64

	maxFrame        int
	maxOutput       int
	maxProgress     int
	maxInput        int
	semaphore       chan struct{}
	stderr          *boundedBuffer
	logs            *boundedBuffer
	waitDone        chan struct{}
	waitErr         error
	notifyQueue     chan []byte
	closeOnce       sync.Once
	closeErr        error
	manifest        plugin.Manifest
	tools           []plugin.ExternalToolDefinition
	supportedEvents map[plugin.EventType]struct{}
}

type toolsCallResult struct {
	Content []protocol.ContentBlock `json:"content"`
	Details json.RawMessage         `json:"details,omitempty"`
	IsError bool                    `json:"is_error,omitempty"`
}

// boundedBuffer stores only the last bounded amount of diagnostics.
type boundedBuffer struct {
	mu   sync.Mutex
	max  int
	data []byte
}

var diagnosticCredentialRE = regexp.MustCompile(`(?i)(^|[^a-z0-9])["']?(?:[a-z0-9]+[_-])*(?:api[_-]?key|access[_-]?token|refresh[_-]?token|authorization|password|secret|token|key)["']?\s*[:=]`)

var externalToolNameRE = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,127}$`)
