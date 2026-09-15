package process

import (
	"errors"
	"net"
	"os"
	"sync"
	"time"
)

// pipeConn owns only the parent's pipe ends. os.Pipe supplies pollable files
// where supported; reject other platforms rather than fake interruptible I/O.
// File operations and the memoized Close methods support concurrent callers.
type pipeConn struct {
	read       *os.File
	write      *os.File
	closeWrite func() error
	close      func() error
}

var _ net.Conn = (*pipeConn)(nil)

func newPipes() (conn *pipeConn, stdin, stdout *os.File, err error) {
	stdin, write, err := os.Pipe()
	if err != nil {
		return nil, nil, nil, err
	}
	read, stdout, err := os.Pipe()
	if err != nil {
		_ = stdin.Close()
		_ = write.Close()
		return nil, nil, nil, err
	}
	conn = &pipeConn{read: read, write: write}
	conn.closeWrite = sync.OnceValue(write.Close)
	conn.close = sync.OnceValue(func() error {
		return errors.Join(conn.closeWrite(), read.Close())
	})
	if err := conn.SetDeadline(time.Time{}); err != nil {
		_ = conn.Close()
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, nil, nil, err
	}
	return conn, stdin, stdout, nil
}

func (c *pipeConn) Read(p []byte) (int, error)         { return c.read.Read(p) }
func (c *pipeConn) Write(p []byte) (int, error)        { return c.write.Write(p) }
func (c *pipeConn) Close() error                       { return c.close() }
func (c *pipeConn) LocalAddr() net.Addr                { return pipeAddr("parent") }
func (c *pipeConn) RemoteAddr() net.Addr               { return pipeAddr("worker") }
func (c *pipeConn) SetReadDeadline(t time.Time) error  { return c.read.SetReadDeadline(t) }
func (c *pipeConn) SetWriteDeadline(t time.Time) error { return c.write.SetWriteDeadline(t) }
func (c *pipeConn) SetDeadline(t time.Time) error {
	return errors.Join(c.SetReadDeadline(t), c.SetWriteDeadline(t))
}

type pipeAddr string

func (pipeAddr) Network() string  { return "stdio" }
func (a pipeAddr) String() string { return string(a) }
