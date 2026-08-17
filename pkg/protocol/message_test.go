package protocol

import "testing"

func TestMessageCloneCopiesToolDisplay(t *testing.T) {
	message := NewToolResultMessage("result", "call", "call-1", "grep", []ContentBlock{NewTextBlock("output")}, false)
	message.ToolDisplay = &ToolDisplay{
		Started:      true,
		StartMessage: "running",
		Progress:     []string{"first", "second"},
		Output:       "output preview",
		DurationMS:   12,
	}

	cloned := message.Clone()
	cloned.ToolDisplay.Progress[0] = "changed"
	cloned.ToolDisplay.Output = "changed"

	if message.ToolDisplay.Progress[0] != "first" || message.ToolDisplay.Output != "output preview" {
		t.Fatalf("clone mutated source tool display: source=%+v clone=%+v", message.ToolDisplay, cloned.ToolDisplay)
	}
}
