package hostops

import (
	"strings"
	"testing"
)

func TestLeafValidation(t *testing.T) {
	for _, leaf := range []string{"a", "a b", "日本語", strings.Repeat("x", 128)} {
		if !ValidLeaf(leaf) {
			t.Errorf("rejected %q", leaf)
		}
	}
	for _, leaf := range []string{"", ".", "..", "../x", "a/b", `a\b`, " a", "a ", "a\n", "\u00a0a", "a\u0085", "\xff", strings.Repeat("é", 65)} {
		if ValidLeaf(leaf) {
			t.Errorf("accepted %q", leaf)
		}
	}
}

func TestAnonymousHTTPSValidation(t *testing.T) {
	for _, value := range []string{"https://example.invalid/team/repo.git", "https://example.invalid:8443/repo", "https://[::1]/repo", "https://example.invalid/日本語"} {
		if !ValidCloneURL(value) {
			t.Errorf("rejected %q", value)
		}
	}
	for _, value := range []string{"", "https://example.invalid", "https://example.invalid/", "http://example.invalid/repo", "ssh://example.invalid/repo", "git@example.invalid:repo", "file:///tmp/repo", "ext::helper", "--upload-pack=bad", "https://u@example.invalid/repo", "https://u:p@example.invalid/repo", "https://example.invalid/repo?q=1", "https://example.invalid/repo?", "https://example.invalid/repo#", "https://example.invalid/repo#ref", "HTTPS://example.invalid/repo", "https://example.invalid:0/repo", "https://example.invalid:65536/repo", "https://example.invalid:/repo", "https://example.invalid/a%00b", "https://example.invalid/a%0ab", "https://example.invalid/a%5cb", "https://example.invalid/a b", "https://example.invalid\\@other.invalid/repo"} {
		if ValidCloneURL(value) {
			t.Errorf("accepted %q", value)
		}
	}
}

func TestOutputBudgetCombinedAndBounded(t *testing.T) {
	canceled := 0
	capture := &limitedCapture{limit: 10, discard: true, cancel: func() { canceled++ }}
	for range 10 {
		if n, err := capture.Write([]byte("12345")); n != 5 || err != nil {
			t.Fatal(n, err)
		}
	}
	if !capture.exceeded() || canceled != 1 || len(capture.bytes()) != 0 || capture.count != 11 {
		t.Fatalf("unbounded capture: %#v", capture)
	}
}
