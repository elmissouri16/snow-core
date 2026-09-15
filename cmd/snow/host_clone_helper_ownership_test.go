//go:build darwin || linux

package main

import (
	"bytes"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/elmissouri16/snow-core/internal/hostops"
)

type fictionalHostFDState struct {
	present         bool
	device          uint64
	inode           uint64
	mode            uint32
	flags           int
	descriptorFlags int
}

func fictionalHostFDStateAt(fd uintptr) fictionalHostFDState {
	var stat unix.Stat_t
	if unix.Fstat(int(fd), &stat) != nil {
		return fictionalHostFDState{}
	}
	flags, err := unix.FcntlInt(fd, unix.F_GETFL, 0)
	if err != nil {
		return fictionalHostFDState{}
	}
	descriptorFlags, err := unix.FcntlInt(fd, unix.F_GETFD, 0)
	if err != nil {
		return fictionalHostFDState{}
	}
	return fictionalHostFDState{present: true, device: uint64(stat.Dev), inode: uint64(stat.Ino), mode: uint32(stat.Mode), flags: flags, descriptorFlags: descriptorFlags}
}

// This subprocess deliberately warms the real Go poller and timers before the
// actual early helper hook. A missing fd3/fd4 can belong to package initialization
// or the runtime, not to our helper. Exercise continued polling, descriptor flags
// and finalizers after rejection rather than immediately exiting and hiding a
// close of somebody else's descriptor. No network listener/connection is used.
func fictionalHostDescriptorProbe() int {
	read, write, err := os.Pipe()
	if err != nil {
		return 90
	}
	defer read.Close()
	defer write.Close()
	poll := func() bool {
		done := make(chan bool, 1)
		go func() {
			var one [1]byte
			n, err := read.Read(one[:])
			done <- n == 1 && err == nil && one[0] == 42
		}()
		<-time.After(5 * time.Millisecond)
		if _, err := write.Write([]byte{42}); err != nil {
			return false
		}
		select {
		case ok := <-done:
			return ok
		case <-time.After(time.Second):
			return false
		}
	}
	if !poll() {
		return 91
	}
	beforeChild, beforeLife := fictionalHostFDStateAt(3), fictionalHostFDStateAt(4)
	handled, code := runHostCloneHelperEarly([]string{hostops.HelperArgument})
	if !handled || code != 0 {
		return 92
	}
	runtime.GC() // Rejected descriptors must not have acquired os.File finalizers.
	if !poll() {
		return 93
	}
	if beforeChild != fictionalHostFDStateAt(3) || beforeLife != fictionalHostFDStateAt(4) {
		return 94
	}
	return 0
}

func TestHostCloneHelperRejectsUnownedFDsAfterRuntimePollInit(t *testing.T) {
	for _, kind := range []string{"missing-pair", "missing-life", "wrong-child", "wrong-life", "writable-life", "swapped"} {
		t.Run(kind, func(t *testing.T) {
			dir, err := os.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer dir.Close()
			regular, err := os.Open(os.DevNull)
			if err != nil {
				t.Fatal(err)
			}
			defer regular.Close()
			read, write, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer read.Close()
			defer write.Close()
			binary, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(binary, "--fictional-host-descriptor-probe")
			cmd.Env = []string{"PATH=/usr/bin:/bin", "GOMAXPROCS=2"}
			cmd.Stdin = strings.NewReader(`{}`)
			switch kind {
			case "missing-life":
				cmd.ExtraFiles = []*os.File{dir}
			case "wrong-child":
				cmd.ExtraFiles = []*os.File{regular, read}
			case "wrong-life":
				cmd.ExtraFiles = []*os.File{dir, regular}
			case "writable-life":
				cmd.ExtraFiles = []*os.File{dir, write}
			case "swapped":
				cmd.ExtraFiles = []*os.File{read, dir}
			}
			var output bytes.Buffer
			cmd.Stdout, cmd.Stderr = &output, &output
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			wait := make(chan error, 1)
			go func() { wait <- cmd.Wait() }()
			select {
			case err := <-wait:
				if err != nil {
					t.Fatalf("descriptor probe failed: %v\n%s", err, output.Bytes())
				}
			case <-time.After(10 * time.Second):
				_ = cmd.Process.Kill()
				<-wait
				t.Fatal("descriptor probe did not reject promptly")
			}
			if output.String() != "{\"status\":\"failed\"}\n" {
				t.Fatalf("expected only fixed failure, got %q", output.Bytes())
			}
		})
	}
}
