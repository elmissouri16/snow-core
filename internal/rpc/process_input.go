package rpc

import "io"

// processInputReader marks inherited stdin as safely interruptible by Close.
// Some inherited pipe handles expose SetReadDeadline but reject it for their
// concrete file type; closing the process-owned descriptor still releases a
// blocked read during RPC shutdown.
type processInputReader struct {
	io.ReadCloser
}

func newProcessInputReader(in io.ReadCloser) *processInputReader {
	return &processInputReader{ReadCloser: in}
}

func (*processInputReader) InterruptsReadOnClose() bool { return true }
