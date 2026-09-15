package rpc

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type deleteControlFake struct {
	calls   int
	cwd, id string
	err     error
}

func (d *deleteControlFake) DeleteSession(ctx context.Context, cwd, id string) error {
	if _, ok := ctx.Deadline(); !ok {
		panic("unbounded deletion")
	}
	d.calls++
	d.cwd = cwd
	d.id = id
	return d.err
}
func TestControlSessionDeleteOptInStrictAndPrivate(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "unsupported", true: "enabled"}[enabled], func(t *testing.T) {
			service := &controlTestService{}
			delete := &deleteControlFake{}
			opts := ControlOptions{}
			if enabled {
				opts.SessionDelete = delete
			}
			var out bytes.Buffer
			input := `{"id":"delete","type":"session_delete","params":{"session_id":"saved"}}` + "\n"
			if err := ServeControl(t.Context(), strings.NewReader(input), &out, t.TempDir(), "test", service, opts); err != nil {
				t.Fatal(err)
			}
			frames := catalogFrames(t, out.String())
			if frames[1]["success"] != enabled || (delete.calls == 1) != enabled || len(service.calls) != 0 {
				t.Fatal(frames, delete.calls, service.calls)
			}
			if strings.Contains(out.String(), protocol.RPCSessionDeleteControlCapability) != enabled {
				t.Fatal("wrong capability", out.String())
			}
		})
	}
	for _, params := range []string{`{}`, `{"session_id":"../private"}`, `{"session_id":"saved","path":"PRIVATE"}`, `{"session_id":"saved","cwd":"PRIVATE"}`, `{"session_id":"a","session_id":"b"}`, `{"session_id":null}`} {
		var out bytes.Buffer
		delete := &deleteControlFake{}
		if err := ServeControl(t.Context(), strings.NewReader(`{"type":"session_delete","params":`+params+"}\n"), &out, t.TempDir(), "test", &controlTestService{}, ControlOptions{SessionDelete: delete}); err != nil {
			t.Fatal(err)
		}
		if delete.calls != 0 || strings.Contains(out.String(), "PRIVATE") {
			t.Fatal(out.String(), delete.calls)
		}
	}
	var out bytes.Buffer
	delete := &deleteControlFake{err: errors.New("PRIVATE cleanup path")}
	if err := ServeControl(t.Context(), strings.NewReader(`{"type":"session_delete","params":{"session_id":"saved"}}`+"\n"), &out, t.TempDir(), "test", &controlTestService{}, ControlOptions{SessionDelete: delete}); err != nil {
		t.Fatal(err)
	}
	frames := catalogFrames(t, out.String())
	if frames[1]["error_code"] != "unavailable" || strings.Contains(out.String(), "PRIVATE") || delete.calls != 1 {
		t.Fatal(out.String())
	}
}
