package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// boundedCommandTransport preserves the SDK command transport's process
// lifecycle while rejecting an oversized newline-delimited JSON-RPC message
// before the SDK decoder allocates for it.
type boundedCommandTransport struct {
	command         *exec.Cmd
	maxMessageBytes int
}

func (t *boundedCommandTransport) Connect(ctx context.Context) (sdkmcp.Connection, error) {
	stdout, err := t.command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stdin, err := t.command.StdinPipe()
	if err != nil {
		_ = stdout.Close()
		return nil, err
	}
	if err := t.command.Start(); err != nil {
		_ = stdout.Close()
		_ = stdin.Close()
		return nil, err
	}
	process := &commandProcess{command: t.command, stdin: stdin, stdout: stdout, timeout: defaultCloseTimeout}
	transport := &sdkmcp.IOTransport{
		Reader: &boundedMessageReader{reader: stdout, max: t.maxMessageBytes},
		Writer: &commandProcessWriter{writer: stdin, process: process},
	}
	connection, err := transport.Connect(ctx)
	if err != nil {
		_ = process.close()
		return nil, err
	}
	return connection, nil
}

type boundedMessageReader struct {
	reader io.ReadCloser
	max    int
	count  int
}

func (r *boundedMessageReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	for i, value := range p[:n] {
		if value == '\n' {
			r.count = 0
			continue
		}
		r.count++
		if r.count > r.max {
			_ = r.reader.Close()
			return i, errors.New("MCP stdio message exceeds size limit")
		}
	}
	return n, err
}

// The SDK closes its IOTransport reader before its writer. Reader.Close is a
// no-op so writer close can first close stdin and then reap or kill the child.
func (r *boundedMessageReader) Close() error { return nil }

type commandProcessWriter struct {
	writer  io.WriteCloser
	process *commandProcess
}

func (w *commandProcessWriter) Write(p []byte) (int, error) { return w.writer.Write(p) }
func (w *commandProcessWriter) Close() error                { return w.process.close() }

type commandProcess struct {
	command *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.Closer
	timeout time.Duration
	once    sync.Once
	err     error
}

func (p *commandProcess) close() error {
	p.once.Do(func() {
		stdinErr := p.stdin.Close()
		result := make(chan error, 1)
		go func() { result <- p.command.Wait() }()
		select {
		case waitErr := <-result:
			p.err = errors.Join(stdinErr, waitErr)
			return
		case <-time.After(p.timeout):
		}
		killErr := p.command.Process.Kill()
		select {
		case waitErr := <-result:
			p.err = errors.Join(stdinErr, killErr, waitErr)
		case <-time.After(p.timeout):
			p.err = errors.Join(stdinErr, killErr, fmt.Errorf("MCP subprocess did not exit after kill"))
		}
	})
	return p.err
}
