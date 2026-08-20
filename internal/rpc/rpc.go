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
	"time"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const (
	maxConcurrentWaits = 64
	rpcWriteTimeout    = time.Second
)

// Request is the public JSONL command envelope.
type Request = protocol.RPCRequest

// Response is the public JSONL response envelope.
type Response = protocol.RPCResponse

// Server serves RPC on stdin/stdout.
type Server struct {
	in                            io.ReadCloser
	inputInterruptible            bool
	inputIndependentInterruptible bool
	inputDeadline                 func(time.Time) error
	out                           io.Writer
	outputBounded                 bool
	outputIndependentBound        bool
	app                           *app.App
	mu                            sync.Mutex
	writeMu                       sync.Mutex
	writeErr                      error
	writeFailed                   chan struct{}
	writeFailOnce                 sync.Once
	readyOnce                     sync.Once
	snowVersion                   string
	// cancel aborts the in-flight prompt.
	cancel     context.CancelFunc
	promptDone chan struct{}
	promptWG   sync.WaitGroup
	waitSlots  chan struct{}
}

// ServerOptions configures RPC transport metadata.
type ServerOptions struct {
	SnowVersion string
}

// InterruptibleInput lets a custom RPC transport assert that Close reliably
// unblocks an in-flight Read. Plain io.ReadCloser is not a sufficient contract.
type InterruptibleInput interface {
	io.ReadCloser
	InterruptsReadOnClose() bool
}

// BoundedOutput lets a custom RPC transport assert that Write completes under
// its own finite bound. Deadline-capable writers are detected automatically.
type BoundedOutput interface {
	io.Writer
	RPCWriteBounded() bool
}
