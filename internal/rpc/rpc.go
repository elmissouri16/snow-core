// Package rpc implements the JSONL-over-stdio control plane for foreign
// hosts (pi-inspired). Commands are read from stdin, events stream to stdout.
//
// Framing: strictly newline-delimited JSON (LF). Clients must split on '\n'
// only — never on Unicode separators.
package rpc

import (
	"context"
	"io"
	"sync"

	"github.com/snow-core/snow/internal/app"
	"github.com/snow-core/snow/pkg/protocol"
)

// Request is the public JSONL command envelope.
type Request = protocol.RPCRequest

// Response is the public JSONL response envelope.
type Response = protocol.RPCResponse

// Server serves RPC on stdin/stdout.
type Server struct {
	in            io.Reader
	out           io.Writer
	app           *app.App
	mu            sync.Mutex
	writeErr      error
	writeFailed   chan struct{}
	writeFailOnce sync.Once
	readyOnce     sync.Once
	snowVersion   string
	// cancel aborts the in-flight prompt.
	cancel     context.CancelFunc
	promptDone chan struct{}
	promptWG   sync.WaitGroup
}

// ServerOptions configures RPC transport metadata.
type ServerOptions struct {
	SnowVersion string
}
