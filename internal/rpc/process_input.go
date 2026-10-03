package rpc

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"golang.org/x/sys/unix"
)

// processInputReader owns stdin as its sole reader. Poll waits for input or a
// private shutdown pipe before reading, so Close can release an idle inherited
// blocking descriptor without relying on os.File.Close interrupting its read.
// Do not change stdin's status flags: stdout or the parent terminal can share
// the same open-file description. No extra read/close goroutine is needed.
type processInputReader struct {
	*os.File
	stdinFD   int32
	socket    bool
	wakeFD    int32
	wakeRead  *os.File
	wakeWrite *os.File
	readMu    sync.Mutex
	closed    bool
	closeOnce sync.Once
	closeErr  error
}

func newProcessInputReader(in *os.File) (*processInputReader, error) {
	info, err := in.Stat()
	if err != nil {
		return nil, err
	}
	finite := info.Mode().IsRegular()
	if !finite && info.Mode()&os.ModeCharDevice != 0 {
		nullInfo, err := os.Stat(os.DevNull)
		finite = err == nil && os.SameFile(info, nullInfo)
	}
	reader := &processInputReader{File: in}
	if finite {
		return reader, nil
	}
	reader.socket = info.Mode()&os.ModeSocket != 0
	if !reader.socket && info.Mode()&os.ModeNamedPipe == 0 {
		return nil, fmt.Errorf("rpc: stdin must be a pipe, socket, regular file, or %s; terminal and other device inputs cannot be safely interrupted", os.DevNull)
	}
	reader.stdinFD, err = processInputFD(in)
	if err != nil {
		return nil, err
	}
	reader.wakeRead, reader.wakeWrite, err = os.Pipe()
	if err != nil {
		return nil, err
	}
	reader.wakeFD, err = processInputFD(reader.wakeRead)
	if err != nil {
		_ = reader.wakeRead.Close()
		_ = reader.wakeWrite.Close()
		return nil, err
	}
	return reader, nil
}

// SyscallConn does not disable a file's deadlines like File.Fd does. These
// descriptors stay owned and open until Close has joined the current Read.
func processInputFD(file *os.File) (int32, error) {
	raw, err := file.SyscallConn()
	if err != nil {
		return 0, err
	}
	var fd int32
	err = raw.Control(func(value uintptr) { fd = int32(value) })
	return fd, err
}

func (r *processInputReader) Read(p []byte) (int, error) {
	r.readMu.Lock()
	defer r.readMu.Unlock()
	if r.closed {
		return 0, os.ErrClosed
	}
	if len(p) == 0 {
		return 0, nil
	}
	if r.wakeRead == nil {
		return r.File.Read(p)
	}
	fds := []unix.PollFd{{Fd: r.stdinFD, Events: unix.POLLIN}, {Fd: r.wakeFD, Events: unix.POLLIN}}
	for {
		_, err := unix.Poll(fds, -1)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return 0, err
		}
		if fds[1].Revents != 0 {
			return 0, os.ErrClosed
		}
		if fds[0].Revents != 0 {
			if r.socket {
				// Socket readiness can be spurious. Per-call nonblocking reads
				// preserve shared stdio flags and return to the cancelable poll.
				n, _, err := unix.Recvfrom(int(r.stdinFD), p, unix.MSG_DONTWAIT)
				if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EINTR) {
					continue
				}
				if err != nil {
					return 0, os.NewSyscallError("recvfrom", err)
				}
				if n == 0 {
					return 0, io.EOF
				}
				return n, nil
			}
			// Only an exclusively owned pipe remains. Buffered bytes cannot
			// disappear before Read, which returns available bytes or EOF.
			return r.File.Read(p)
		}
	}
}

func (*processInputReader) InterruptsReadOnClose() bool { return true }

func (r *processInputReader) Close() error {
	r.closeOnce.Do(func() {
		if r.wakeWrite != nil {
			// The private pipe is empty and written exactly once, so this byte
			// cannot wait for buffer space, even while Read holds readMu.
			_, r.closeErr = r.wakeWrite.Write([]byte{1})
		}
		r.readMu.Lock()
		defer r.readMu.Unlock()
		r.closed = true
		r.closeErr = errors.Join(r.closeErr, r.File.Close())
		if r.wakeRead != nil {
			r.closeErr = errors.Join(r.closeErr, r.wakeRead.Close(), r.wakeWrite.Close())
		}
	})
	return r.closeErr
}
