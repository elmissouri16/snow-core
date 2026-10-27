package web

import (
	"encoding/json/v2"
	"strings"
	"testing"
	"time"
)

// Later frames on one connection abbreviate byte-identical messages and
// activities to stubs; anything that changed or is new arrives in full.
func TestRuntimeStreamFramesStubUnchangedEntries(t *testing.T) {
	_, cookie, _, runtime, server, path := streamHTTPFixture(t, shortStreamPolicy())
	runtime.mu.Lock()
	runtime.addMessage(RuntimeMessage{ID: "m1", Role: "user", Text: "first question"})
	runtime.addMessage(RuntimeMessage{ID: "m2", Role: "assistant", Text: "**first** answer"})
	runtime.publishLocked()
	runtime.mu.Unlock()
	stream := openTestStream(t, server, path, cookie)
	event, data := stream.next(t)
	if event != "snapshot" || strings.Contains(string(data), `"unchanged"`) {
		t.Fatalf("initial frame must be complete: %s %s", event, data)
	}
	time.Sleep(40 * time.Millisecond)
	runtime.mu.Lock()
	last := len(runtime.snapshot.Messages) - 1
	runtime.snapshot.Messages[last].Text += " continued"
	runtime.publishLocked()
	runtime.mu.Unlock()
	event, data = stream.next(t)
	if event != "snapshot" {
		t.Fatalf("update: %s %s", event, data)
	}
	var frame struct {
		Revision int `json:"revision"`
		Messages []struct {
			ID        string `json:"id"`
			Unchanged bool   `json:"unchanged"`
			Text      string `json:"text"`
			HTML      string `json:"html"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(data, &frame); err != nil {
		t.Fatal(err)
	}
	if len(frame.Messages) < 2 {
		t.Fatalf("messages: %s", data)
	}
	first, changed := frame.Messages[len(frame.Messages)-2], frame.Messages[len(frame.Messages)-1]
	if !first.Unchanged || first.ID != "m1" || first.Text != "" {
		t.Fatalf("unchanged message was not stubbed: %+v", first)
	}
	if changed.Unchanged || changed.ID != "m2" || !strings.Contains(changed.Text, "continued") || !strings.Contains(changed.HTML, "<strong>first</strong>") {
		t.Fatalf("changed message must arrive in full: %+v", changed)
	}
}
