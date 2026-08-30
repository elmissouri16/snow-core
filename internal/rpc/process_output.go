package rpc

import (
	"fmt"
	"io"
	"slices"
	"time"
)

// processOutputWriter bounds writes to inherited stdout handles that expose
// SetWriteDeadline but reject it for their concrete file type. A timed-out
// write leaves at most one goroutine blocked; the RPC server records the error
// and the CLI process exits, closing the inherited descriptor.
type processOutputWriter struct {
	out     io.Writer
	timeout time.Duration
}

func newProcessOutputWriter(out io.Writer) *processOutputWriter {
	return &processOutputWriter{out: out, timeout: rpcWriteTimeout}
}

func (*processOutputWriter) RPCWriteBounded() bool { return true }

func (w *processOutputWriter) Write(p []byte) (int, error) {
	payload := slices.Clone(p)
	type result struct {
		n   int
		err error
	}
	done := make(chan result, 1)
	go func() {
		n, err := w.out.Write(payload)
		done <- result{n: n, err: err}
	}()

	timer := time.NewTimer(w.timeout)
	defer timer.Stop()
	select {
	case result := <-done:
		return result.n, result.err
	case <-timer.C:
		return 0, fmt.Errorf("rpc: output write timed out after %s", w.timeout)
	}
}
