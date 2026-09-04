package builtin

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

func TestReadBoundedSearchLineBoundaries(t *testing.T) {
	for _, size := range []int{0, 1, 15, 16, 17, 31, 32, 33, 100} {
		for _, limit := range []int{0, 1, 15, 16, 17, 31, 32, 100} {
			for _, ending := range []string{"", "\n", "\r\n"} {
				wire := strings.Repeat("x", size) + ending
				reader := bufio.NewReaderSize(strings.NewReader(wire), 16)
				got, oversized, err := readBoundedSearchLine(t.Context(), reader, limit)
				want := wire
				if len(wire) > limit {
					want = ""
				}
				var wantErr error
				if ending == "" {
					wantErr = io.EOF
				}
				if got != want || oversized != (len(wire) > limit) || !errors.Is(err, wantErr) {
					t.Fatalf("size=%d limit=%d ending=%q: got %q/%v/%v, want %q/%v/%v", size, limit, ending, got, oversized, err, want, len(wire) > limit, wantErr)
				}
			}
		}
	}
}

func TestReadBoundedSearchLineOwnsTextAndDrainsOversizedInput(t *testing.T) {
	reader := bufio.NewReaderSize(strings.NewReader("first\n"+strings.Repeat("x", 100)+"\nlast\n"), 16)
	first, oversized, err := readBoundedSearchLine(t.Context(), reader, 16)
	if first != "first\n" || oversized || err != nil {
		t.Fatalf("first read: %q/%v/%v", first, oversized, err)
	}
	if text, oversized, err := readBoundedSearchLine(t.Context(), reader, 16); text != "" || !oversized || err != nil {
		t.Fatalf("oversized read: %q/%v/%v", text, oversized, err)
	}
	if text, oversized, err := readBoundedSearchLine(t.Context(), reader, 16); text != "last\n" || oversized || err != nil {
		t.Fatalf("last read: %q/%v/%v", text, oversized, err)
	}
	if first != "first\n" {
		t.Fatalf("refilling the buffer changed earlier text to %q", first)
	}
}

func TestReadBoundedSearchLinePreservesErrorsAndCancellation(t *testing.T) {
	failure := errors.New("read failed")
	if text, oversized, err := readBoundedSearchLine(t.Context(), bufio.NewReader(iotest.ErrReader(failure)), 16); text != "" || oversized || !errors.Is(err, failure) {
		t.Fatalf("read error: %q/%v/%v", text, oversized, err)
	}
	reader := bufio.NewReader(iotest.DataErrReader(strings.NewReader("tail")))
	if text, oversized, err := readBoundedSearchLine(t.Context(), reader, 16); text != "tail" || oversized || !errors.Is(err, io.EOF) {
		t.Fatalf("partial EOF: %q/%v/%v", text, oversized, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	reader = bufio.NewReader(strings.NewReader("untouched\n"))
	if text, oversized, err := readBoundedSearchLine(ctx, reader, 16); text != "" || oversized || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled read: %q/%v/%v", text, oversized, err)
	}
	if text, _, err := readBoundedSearchLine(t.Context(), reader, 16); text != "untouched\n" || err != nil {
		t.Fatalf("cancellation consumed input: %q/%v", text, err)
	}
}
