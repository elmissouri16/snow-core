package plugin

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
)

func TestReadFrameTrimsNewlineInPlace(t *testing.T) {
	payload := strings.Repeat("x", 128<<10)
	frame, err := readFrame(bufio.NewReaderSize(strings.NewReader(payload+"\n"), 32), len(payload)+1)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(frame, []byte(payload)) {
		t.Fatalf("frame length = %d, want %d", len(frame), len(payload))
	}
}

func TestReadFramePreservesCarriageReturn(t *testing.T) {
	frame, err := readFrame(bufio.NewReader(strings.NewReader("payload\r\n")), 32)
	if err != nil {
		t.Fatal(err)
	}
	if string(frame) != "payload\r" {
		t.Fatalf("frame = %q, want carriage return preserved", frame)
	}
}

func TestReadFrameRejectsOversize(t *testing.T) {
	if _, err := readFrame(bufio.NewReader(strings.NewReader("oversize\n")), 4); err == nil {
		t.Fatal("expected frame limit error")
	}
}
