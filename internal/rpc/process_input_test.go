package rpc

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestProcessInputReaderFiniteInputs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input")
	if err := os.WriteFile(path, []byte("request\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{path, os.DevNull} {
		t.Run(path, func(t *testing.T) {
			input, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			reader, err := newProcessInputReader(input)
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(reader)
			if err != nil || (path != os.DevNull && string(data) != "request\n") {
				t.Fatalf("read=%q error=%v", data, err)
			}
			if err := reader.Close(); err != nil {
				t.Fatal(err)
			}
			if err := reader.Close(); err != nil {
				t.Fatalf("second close: %v", err)
			}
		})
	}
}

func TestProcessInputReaderLeavesSharedDescriptorModeUntouched(t *testing.T) {
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	defer writer.Close()
	raw, err := input.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var monitorFD int
	var setupErr error
	if err := raw.Control(func(fd uintptr) {
		setupErr = unix.SetNonblock(int(fd), false)
		if setupErr == nil {
			monitorFD, setupErr = unix.FcntlInt(fd, unix.F_DUPFD_CLOEXEC, 0)
		}
	}); err != nil || setupErr != nil {
		t.Fatalf("setup: %v / %v", err, setupErr)
	}
	// This extra descriptor models the parent shell retaining an inherited
	// terminal's open-file description after the child closes its own handles.
	monitor := os.NewFile(uintptr(monitorFD), "monitor")
	defer monitor.Close()
	reader, err := newProcessInputReader(input)
	if err != nil {
		t.Fatal(err)
	}
	flags, err := unix.FcntlInt(uintptr(monitorFD), unix.F_GETFL, 0)
	if err != nil || flags&unix.O_NONBLOCK != 0 {
		t.Fatalf("shared descriptor mode changed during serving: flags=%d error=%v", flags, err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	flags, err = unix.FcntlInt(uintptr(monitorFD), unix.F_GETFL, 0)
	if err != nil || flags&unix.O_NONBLOCK != 0 {
		t.Fatalf("shared descriptor mode changed after close: flags=%d error=%v", flags, err)
	}
}

func TestProcessInputReaderSharedSocket(t *testing.T) {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	input := os.NewFile(uintptr(fds[0]), "shared-stdio")
	peer := os.NewFile(uintptr(fds[1]), "client")
	reader, err := newProcessInputReader(input)
	if err != nil {
		_ = input.Close()
		_ = peer.Close()
		t.Fatal(err)
	}
	defer func() {
		// Also release the peer if a failed regression left a blocking read.
		_ = peer.Close()
		_ = reader.Close()
	}()
	if _, err := peer.Write([]byte("request\n")); err != nil {
		t.Fatal(err)
	}
	var buf [32]byte
	n, err := reader.Read(buf[:])
	if err != nil || string(buf[:n]) != "request\n" {
		t.Fatalf("socket request: %q, %v", buf[:n], err)
	}
	flags, err := unix.FcntlInt(uintptr(fds[0]), unix.F_GETFL, 0)
	if err != nil || flags&unix.O_NONBLOCK != 0 {
		t.Fatalf("shared socket flags changed: %d, %v", flags, err)
	}
	// Write the response through the same open-file description as stdin.
	if _, err := input.Write([]byte("response\n")); err != nil {
		t.Fatal(err)
	}
	n, err = peer.Read(buf[:])
	if err != nil || string(buf[:n]) != "response\n" {
		t.Fatalf("shared socket response: %q, %v", buf[:n], err)
	}
	readDone := make(chan error, 1)
	go func() {
		_, err := reader.Read(buf[:])
		readDone <- err
	}()
	closeDone := make(chan error, 1)
	go func() { closeDone <- reader.Close() }()
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not interrupt idle socket input")
	}
	select {
	case err := <-readDone:
		if !errors.Is(err, os.ErrClosed) {
			t.Fatalf("idle socket read: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("idle socket read did not return after Close")
	}
}

func TestProcessInputReaderRejectsUnsafeDevice(t *testing.T) {
	input, err := os.Open("/dev/zero")
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if reader, err := newProcessInputReader(input); err == nil || reader != nil || !strings.Contains(err.Error(), "cannot be safely interrupted") {
		t.Fatalf("unsafe device accepted: reader=%v, error=%v", reader, err)
	}
	var buf [1]byte
	if n, err := input.Read(buf[:]); n != 1 || err != nil {
		t.Fatalf("constructor failure took ownership of stdin: n=%d, error=%v", n, err)
	}
}
