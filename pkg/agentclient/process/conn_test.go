package process

import (
	"errors"
	"io"
	"os"
	"sync"
	"testing"
	"time"
)

func testPipes(t *testing.T) (*pipeConn, *os.File, *os.File) {
	t.Helper()
	conn, stdin, stdout, err := newPipes()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
		_ = stdin.Close()
		_ = stdout.Close()
	})
	return conn, stdin, stdout
}

func TestPipeDeadlinesAndReset(t *testing.T) {
	conn, _, stdout := testPipes(t)
	if err := conn.SetReadDeadline(time.Now().Add(20 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	var b [1]byte
	if _, err := conn.Read(b[:]); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("read deadline error = %v", err)
	}
	if err := conn.SetWriteDeadline(time.Now().Add(20 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write(make([]byte, 2*1024*1024)); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("write deadline error = %v", err)
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
	if _, err := stdout.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if n, err := conn.Read(b[:]); n != 1 || err != nil || b[0] != 'x' {
		t.Fatalf("read after reset = %d, %v, %q", n, err, b)
	}
	if conn.LocalAddr().Network() != "stdio" || conn.RemoteAddr().String() != "worker" {
		t.Fatal("unexpected pipe addresses")
	}
}

func TestPipeConcurrentCloseInterruptsIO(t *testing.T) {
	conn, _, _ := testPipes(t)
	var wg sync.WaitGroup
	started := make(chan struct{}, 2)
	wg.Go(func() {
		started <- struct{}{}
		var b [1]byte
		if _, err := conn.Read(b[:]); !errors.Is(err, os.ErrClosed) {
			t.Errorf("closed read = %v", err)
		}
	})
	wg.Go(func() {
		started <- struct{}{}
		if _, err := conn.Write(make([]byte, 2*1024*1024)); !errors.Is(err, os.ErrClosed) {
			t.Errorf("closed write = %v", err)
		}
	})
	<-started
	<-started
	for range 16 {
		wg.Go(func() {
			if err := conn.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	await(t, done)
}

func TestHalfCloseDeliversEOFButLeavesReader(t *testing.T) {
	conn, stdin, stdout := testPipes(t)
	if err := conn.closeWrite(); err != nil {
		t.Fatal(err)
	}
	var b [1]byte
	if _, err := stdin.Read(b[:]); !errors.Is(err, io.EOF) {
		t.Fatalf("stdin half-close = %v", err)
	}
	if _, err := stdout.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
}
