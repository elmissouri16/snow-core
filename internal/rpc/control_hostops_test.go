package rpc

import (
	"bufio"
	"bytes"
	"context"
	json "encoding/json/v2"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/hostops"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type controlFakeHost struct {
	prepared *controlFakePrepared
	calls    atomic.Int64
}

func (s *controlFakeHost) Prepare(_ context.Context, params protocol.RPCProjectPrepareParams) (PreparedHostOperation, error) {
	s.calls.Add(1)
	s.prepared.result = protocol.RPCProjectPrepared{OperationID: params.OperationID, Parent: params.Parent, Leaf: params.Leaf,
		Child: protocol.HostDirectoryIdentity{Path: filepath.Join(params.Parent.Path, params.Leaf), Device: "1", Inode: "2"}}
	return s.prepared, nil
}

type controlFakePrepared struct {
	result protocol.RPCProjectPrepared
	closed atomic.Bool
	clone  *controlFakeClone
}

func (p *controlFakePrepared) Result() protocol.RPCProjectPrepared { return p.result }
func (p *controlFakePrepared) Close() error                        { p.closed.Store(true); return nil }
func (p *controlFakePrepared) StartClone(ctx context.Context, start protocol.RPCProjectCloneStartParams) (HostCloneOperation, error) {
	p.clone.result = protocol.RPCProjectCompletion{OperationID: start.OperationID, Child: start.Child}
	p.clone.ctx = ctx
	go func() {
		defer close(p.clone.done)
		select {
		case <-p.clone.cancel:
			p.clone.result.Status = protocol.HostOperationCanceled
		case <-ctx.Done():
			p.clone.result.Status = protocol.HostOperationCanceled
		case <-p.clone.finish:
			p.clone.result.Status = protocol.HostOperationSucceeded
		}
	}()
	return p.clone, nil
}

type controlFakeClone struct {
	ctx          context.Context
	done         chan struct{}
	cancel       chan struct{}
	finish       chan struct{}
	released     chan struct{}
	releaseCheck func()
	releaseOnce  sync.Once
	cancelOnce   sync.Once
	result       protocol.RPCProjectCompletion
}

func (c *controlFakeClone) Release() {
	c.releaseOnce.Do(func() {
		if c.releaseCheck != nil {
			c.releaseCheck()
		}
		close(c.released)
	})
}
func (c *controlFakeClone) Cancel()               { c.cancelOnce.Do(func() { close(c.cancel) }) }
func (c *controlFakeClone) Done() <-chan struct{} { return c.done }
func (c *controlFakeClone) Completion() (protocol.RPCProjectCompletion, bool) {
	select {
	case <-c.done:
		return c.result, true
	default:
		return protocol.RPCProjectCompletion{}, false
	}
}

func newControlFakeHost() (*controlFakeHost, *controlFakeClone) {
	clone := &controlFakeClone{done: make(chan struct{}), cancel: make(chan struct{}), finish: make(chan struct{}), released: make(chan struct{})}
	return &controlFakeHost{prepared: &controlFakePrepared{clone: clone}}, clone
}

// This bounded writer hands each completed write to the test without allowing
// the test to block the server. Observation therefore occurs after ACK writes.
type controlFrameWriter struct {
	frames      chan []byte
	failCommand string
}

func (*controlFrameWriter) RPCWriteBounded() bool { return true }
func (w *controlFrameWriter) Write(frame []byte) (int, error) {
	if w.failCommand != "" && strings.Contains(string(frame), `"command":"`+w.failCommand+`"`) {
		return 0, errors.New("PRIVATE_ACK_WRITE_FAILURE")
	}
	w.frames <- bytes.Clone(frame)
	return len(frame), nil
}

func readControlTestFrame(t *testing.T, writer *controlFrameWriter) map[string]any {
	t.Helper()
	select {
	case data := <-writer.frames:
		var frame map[string]any
		if err := json.Unmarshal(data, &frame); err != nil {
			t.Fatal(err)
		}
		return frame
	case <-time.After(2 * time.Second):
		t.Fatal("control frame timed out")
		return nil
	}
}

func writeControlTestRequest(t *testing.T, writer io.Writer, id, command string, params any) {
	t.Helper()
	frame, err := json.Marshal(map[string]any{"id": id, "type": command, "params": params})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(append(frame, '\n')); err != nil {
		t.Fatal(err)
	}
}

func TestControlHostCloneACKReleaseCancelAndTerminal(t *testing.T) {
	cwd, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	host, clone := newControlFakeHost()
	input, writer := io.Pipe()
	defer writer.Close()
	output := &controlFrameWriter{frames: make(chan []byte, 32)}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- ServeControl(ctx, input, output, cwd, "", &controlTestService{}, ControlOptions{HostOperations: host})
	}()
	ready := readControlTestFrame(t, output)
	if caps := ready["capabilities"].([]any); len(caps) != 6 || caps[3] != protocol.HostOperationsCapability || caps[4] != protocol.RPCProjectCreateCapability || caps[5] != protocol.RPCProjectCloneCapability {
		t.Fatalf("ready=%v", ready)
	}
	parent := protocol.HostDirectoryIdentity{Path: cwd, Device: "1", Inode: "1"}
	writeControlTestRequest(t, writer, "prepare", "project_prepare", protocol.RPCProjectPrepareParams{OperationID: "operation", Parent: parent, Leaf: "child"})
	prepared := readControlTestFrame(t, output)
	if prepared["success"] != true {
		t.Fatalf("prepare=%v", prepared)
	}
	if host.calls.Load() != 1 {
		t.Fatal("prepare not dispatched")
	}
	clone.releaseCheck = func() {
		// The ACK has already entered the writer queue; no release can precede it.
		if len(output.frames) != 1 {
			t.Error("clone released before ACK write")
		}
	}
	start := protocol.RPCProjectCloneStartParams{OperationID: "operation", Child: host.prepared.result.Child, URL: "https://example.invalid/repository.git"}
	writeControlTestRequest(t, writer, "start", "project_clone_start", start)
	select {
	case <-clone.released:
	case <-time.After(time.Second):
		t.Fatal("clone not released")
	}
	ack := readControlTestFrame(t, output)
	if ack["command"] != "project_clone_start" || ack["success"] != true {
		t.Fatalf("start=%v", ack)
	}
	deadline, ok := clone.ctx.Deadline()
	if !ok || time.Until(deadline) > controlJobTimeout || time.Until(deadline) < controlJobTimeout-time.Second {
		t.Fatal("job lacks ten-minute budget")
	}
	// Requests remain responsive while the clone is active; no clone network or
	// process is involved in this fake lifecycle test.
	writeControlTestRequest(t, writer, "cancel", "project_cancel", protocol.RPCProjectCancelParams{OperationID: "operation"})
	cancelACK := readControlTestFrame(t, output)
	if cancelACK["command"] != "project_cancel" || cancelACK["success"] != true {
		t.Fatalf("cancel=%v", cancelACK)
	}
	terminal := readControlTestFrame(t, output)
	if terminal["type"] != protocol.RPCTypeProjectCompleted || terminal["request_id"] != "start" || terminal["operation_id"] != "operation" || terminal["status"] != "canceled" {
		t.Fatalf("terminal=%v", terminal)
	}
	_ = writer.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !host.prepared.closed.Load() {
		t.Fatal("prepared resources leaked")
	}
}

func TestControlHostCloneFailedACKNeverReleasesAndJoins(t *testing.T) {
	cwd, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	host, clone := newControlFakeHost()
	parent := protocol.HostDirectoryIdentity{Path: cwd, Device: "1", Inode: "1"}
	child := protocol.HostDirectoryIdentity{Path: filepath.Join(cwd, "child"), Device: "1", Inode: "2"}
	var input strings.Builder
	writeControlTestRequest(t, &input, "prepare", "project_prepare", protocol.RPCProjectPrepareParams{OperationID: "op", Parent: parent, Leaf: "child"})
	writeControlTestRequest(t, &input, "start", "project_clone_start", protocol.RPCProjectCloneStartParams{OperationID: "op", Child: child, URL: "https://example.invalid/repository.git"})
	output := &controlFrameWriter{frames: make(chan []byte, 32), failCommand: "project_clone_start"}
	if err := ServeControl(t.Context(), strings.NewReader(input.String()), output, cwd, "", &controlTestService{}, ControlOptions{HostOperations: host}); !errors.Is(err, errControlUnavailable) {
		t.Fatalf("error=%v", err)
	}
	select {
	case <-clone.released:
		t.Fatal("failed ACK released clone")
	default:
	}
	select {
	case <-clone.done:
	default:
		t.Fatal("failed ACK did not join clone")
	}
	if !host.prepared.closed.Load() {
		t.Fatal("prepared resources leaked")
	}
}

func TestControlHostRejectsMalformedAndMismatchedFramesBeforePrepare(t *testing.T) {
	cwd, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range []string{
		`{"id":"p","type":"project_prepare","params":{}}`,
		`{"id":"p","type":"project_prepare","params":{"operation_id":"op","parent":{"path":"/other","device":"1","inode":"1"},"leaf":"child"}}`,
		`{"id":"p","type":"project_prepare","params":{"operation_id":"op","parent":{"path":"/other","device":"1","inode":"1"},"leaf":"child","command":"PRIVATE_EXECUTE"}}`,
		`{"id":"p","type":"project_prepare","params":{"operation_id":"op","operation_id":"other"}}`,
		`{"id":"s","type":"project_clone_start","params":{"operation_id":"op","child":{},"url":"ssh://private/repo"}}`,
		`{"id":"c","type":"project_cancel","params":{"operation_id":"op","force":true}}`,
	} {
		host, _ := newControlFakeHost()
		output := &controlFrameWriter{frames: make(chan []byte, 32)}
		if err := ServeControl(t.Context(), strings.NewReader(request), output, cwd, "", &controlTestService{}, ControlOptions{HostOperations: host}); err != nil {
			t.Fatal(err)
		}
		_ = readControlTestFrame(t, output)
		response := readControlTestFrame(t, output)
		if response["success"] != false || host.calls.Load() != 0 {
			t.Fatalf("accepted frame: %v", response)
		}
	}
}

func TestControlHostEOFCancelsActiveCloneAndJoins(t *testing.T) {
	cwd, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	host, clone := newControlFakeHost()
	parent := protocol.HostDirectoryIdentity{Path: cwd, Device: "1", Inode: "1"}
	child := protocol.HostDirectoryIdentity{Path: filepath.Join(cwd, "child"), Device: "1", Inode: "2"}
	var input strings.Builder
	writeControlTestRequest(t, &input, "prepare", "project_prepare", protocol.RPCProjectPrepareParams{OperationID: "op", Parent: parent, Leaf: "child"})
	writeControlTestRequest(t, &input, "start", "project_clone_start", protocol.RPCProjectCloneStartParams{OperationID: "op", Child: child, URL: "https://example.invalid/repository.git"})
	output := &controlFrameWriter{frames: make(chan []byte, 32)}
	if err := ServeControl(t.Context(), bufio.NewReader(strings.NewReader(input.String())), output, cwd, "", &controlTestService{}, ControlOptions{HostOperations: host}); err == nil {
		t.Fatal("undeclared interruptible input accepted")
	}
	if err := ServeControl(t.Context(), strings.NewReader(input.String()), output, cwd, "", &controlTestService{}, ControlOptions{HostOperations: host}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-clone.done:
	default:
		t.Fatal("EOF did not join clone")
	}
	if !host.prepared.closed.Load() {
		t.Fatal("EOF leaked prepared resources")
	}
}

func TestControlHostCapabilitiesAreLazyHandlerAvailability(t *testing.T) {
	// Nonexistent trusted executable selections cannot prevent a defaults read
	// or handshake: hostops construction remains deferred until job admission.
	cwd := t.TempDir()
	operations := NewControlHostOperations(hostops.Options{
		GitExecutable:    filepath.Join(cwd, "git-not-installed"),
		HelperExecutable: filepath.Join(cwd, "helper-not-installed"),
	})
	var output bytes.Buffer
	if err := ServeControl(t.Context(), strings.NewReader(`{"id":"read","type":"defaults_get","params":{"scope":"global"}}`),
		&output, cwd, "", &controlTestService{}, ControlOptions{HostOperations: operations}); err != nil {
		t.Fatal(err)
	}
	frames := catalogFrames(t, output.String())
	caps := frames[0]["capabilities"].([]any)
	if len(caps) != 6 || caps[3] != protocol.HostOperationsCapability ||
		caps[4] != protocol.RPCProjectCreateCapability || caps[5] != protocol.RPCProjectCloneCapability || frames[1]["success"] != true {
		t.Fatalf("lazy optional capabilities/read=%v", frames)
	}
}
