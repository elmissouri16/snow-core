package tui

import (
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"
)

func TestBoundedRenderedTailDoesNotStartInsideANSISequence(t *testing.T) {
	value := strings.Repeat("x", 32) + "\x1b[38;5;196mstyled\x1b[0m"
	// This cutoff begins inside the final SGR sequence.
	limit := len(value) - (32 + 4)
	got := boundedRenderedTail(value, limit)
	if strings.Contains(got, "38;5;196m") && !strings.Contains(got, "\x1b[38;5;196m") {
		t.Fatalf("tail starts inside ANSI sequence: %q", got)
	}
	if !strings.HasPrefix(got, "\x1b[0m") || !strings.HasSuffix(got, "\x1b[0m") {
		t.Fatalf("tail is not style-contained: %q", got)
	}
	if strings.ContainsRune(xansi.Strip(got), '\x1b') {
		t.Fatalf("stripped tail retained escape fragment: %q", got)
	}
}
