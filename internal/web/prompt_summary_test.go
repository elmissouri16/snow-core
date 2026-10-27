package web

import (
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// The live projection of a sent message shows the typed text and every text
// attachment, like saved history does, never just the typed text.
func TestPromptSummaryShowsTextAttachments(t *testing.T) {
	content := []protocol.ContentBlock{
		{Type: protocol.BlockText, Text: "Attachment: notes.txt\nfirst file"},
		{Type: protocol.BlockText, Text: "Attachment: todo.txt\nsecond file"},
		{Type: protocol.BlockText, Text: "Attachment: photo.png\n[Image]"},
		{Type: protocol.BlockImage, MIMEType: "image/png", Data: []byte{1, 2, 3}},
	}
	summary, truncated := promptSummary("Please review", content)
	if truncated || summary != "Please review\n\nAttachment: notes.txt\nfirst file\n\nAttachment: todo.txt\nsecond file" {
		t.Fatalf("summary = %q truncated=%v", summary, truncated)
	}
	if summary, _ := promptSummary("", content[:2]); !strings.HasPrefix(summary, "Attachment: notes.txt") || !strings.Contains(summary, "\n\nAttachment: todo.txt") {
		t.Fatalf("attachment-only summary = %q", summary)
	}
	if summary, _ := promptSummary("", content[2:]); summary != "[Image attachment]" {
		// An image label alone is not display text: the thumbnail shows the image.
		t.Fatalf("image-only summary = %q", summary)
	}
	if summary, truncated := promptSummary(strings.Repeat("x", runtimeMessageBytes), content[:1]); !truncated || len(summary) > runtimeMessageBytes {
		t.Fatalf("oversized summary not bounded: %d truncated=%v", len(summary), truncated)
	}
}
