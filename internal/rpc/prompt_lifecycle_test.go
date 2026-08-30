package rpc

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/app"
)

type pipelinedPromptWriter struct {
	mu              sync.Mutex
	frames          []string
	terminalStarted chan struct{}
	releaseTerminal chan struct{}
	terminalOnce    sync.Once
}

func (w *pipelinedPromptWriter) Write(p []byte) (int, error) {
	frame := string(p)
	if strings.Contains(frame, `"type":"prompt_completed"`) && strings.Contains(frame, `"request_id":"p1"`) {
		w.terminalOnce.Do(func() { close(w.terminalStarted) })
		<-w.releaseTerminal
	}
	w.mu.Lock()
	w.frames = append(w.frames, frame)
	w.mu.Unlock()
	return len(p), nil
}

func (*pipelinedPromptWriter) RPCWriteBounded() bool { return true }

func TestRPCPipelinedPromptWaitsForPriorTerminalFrame(t *testing.T) {
	a, err := app.New(t.Context(), app.Options{
		Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	writer := &pipelinedPromptWriter{
		terminalStarted: make(chan struct{}),
		releaseTerminal: make(chan struct{}),
	}
	srv := New(t.Context(), a, strings.NewReader(""), writer)
	if err := srv.handlePrompt(t.Context(), Request{ID: "p1", Type: "prompt", Message: "first"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-writer.terminalStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("first prompt did not begin terminal-frame publication")
	}

	attempting := make(chan struct{})
	secondDone := make(chan error, 1)
	go func() {
		close(attempting)
		secondDone <- srv.handlePrompt(t.Context(), Request{ID: "p2", Type: "prompt", Message: "second"})
	}()
	<-attempting
	select {
	case err := <-secondDone:
		t.Fatalf("second prompt returned before prior terminal frame: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(writer.releaseTerminal)
	if err := <-secondDone; err != nil {
		t.Fatalf("second prompt after terminal publication: %v", err)
	}
	srv.promptWG.Wait()

	writer.mu.Lock()
	frames := strings.Join(writer.frames, "")
	writer.mu.Unlock()
	terminal := strings.Index(frames, `"type":"prompt_completed","request_id":"p1"`)
	secondAdmission := strings.Index(frames, `"id":"p2","type":"response","command":"prompt","success":true`)
	if terminal < 0 || secondAdmission < 0 || terminal >= secondAdmission {
		t.Fatalf("frame order is terminal=%d second-admission=%d: %s", terminal, secondAdmission, frames)
	}
}
