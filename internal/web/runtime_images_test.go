package web

import (
	"encoding/json/v2"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestSentImageMetadataPreservesOriginalIndexesAndPendingBinding(t *testing.T) {
	content := []protocol.ContentBlock{protocol.NewTextBlock("attachment"), {Type: protocol.BlockImage, MIMEType: "image/png", Data: []byte("PRIVATE_IMAGE_BYTES")}}
	for _, text := range []string{"", "caption"} {
		images := promptImages(text, content)
		want := 1
		if text != "" {
			want++
		}
		if len(images) != 1 || images[0].Index != want || images[0].URL != "" {
			t.Fatalf("metadata=%+v", images)
		}
		r := &liveRuntime{instanceID: "instance", turnID: "accepted", snapshot: RuntimeSnapshot{ProjectID: "project", InstanceID: "instance", SessionID: "session"}}
		r.addMessage(RuntimeMessage{Role: "user", Text: text, Images: images})
		r.pendingUserID = r.snapshot.Messages[0].ID
		before := displaySnapshot(r.snapshot).Messages[0]
		if before.CanEdit || before.Images[0].URL != "" {
			t.Fatal("pending image gained authority")
		}
		r.bindPromptUserLocked(protocol.AgentEvent{Type: protocol.EvSessionUpdated, TurnID: "accepted", RootEpoch: 1, TurnSequence: 1})
		if r.snapshot.Messages[0].SourceTurnID != "" {
			t.Fatal("pre-marker update bound image")
		}
		r.bindPromptUserLocked(protocol.AgentEvent{Type: protocol.EvRunStatsUpdated, TurnID: "accepted", RootEpoch: 1, TurnSequence: 1})
		if displaySnapshot(r.snapshot).Messages[0].Images[0].URL != "" {
			t.Fatal("pre-user-append marker published URL")
		}
		r.bindPromptUserLocked(protocol.AgentEvent{Type: protocol.EvSessionUpdated, TurnID: "accepted", RootEpoch: 1, TurnSequence: 1})
		after := displaySnapshot(r.snapshot).Messages[0]
		if after.CanEdit || after.Images[0].URL == "" || !strings.Contains(after.Images[0].URL, "turn_id=accepted") {
			t.Fatalf("bound image=%+v", after)
		}
		raw, err := json.Marshal(displaySnapshot(r.snapshot))
		if err != nil || strings.Contains(string(raw), "PRIVATE_IMAGE_BYTES") || strings.Contains(string(raw), `"data"`) {
			t.Fatalf("snapshot leaked bytes: %s %v", raw, err)
		}
	}
}

func TestSentImageHistoryRetainsImageOnlyAndNeverRenumbersPublicBlocks(t *testing.T) {
	r := &liveRuntime{instanceID: "instance", snapshot: RuntimeSnapshot{ProjectID: "project", InstanceID: "instance", SessionID: "session"}}
	page := protocol.RPCMessagesPage{Messages: []protocol.Message{
		{ID: "image-only", Role: protocol.RoleUser},
		{ID: "caption", Role: protocol.RoleUser, Content: []protocol.ContentBlock{protocol.NewTextBlock("text")}},
		{ID: "unsupported", Role: protocol.RoleUser},
	}, HistoryImages: map[string][]protocol.RPCMessageImage{
		"image-only":  {{Index: 3, MIMEType: "image/png"}},
		"caption":     {{Index: 7, MIMEType: "image/jpeg"}},
		"unsupported": {{Index: 1, MIMEType: ""}},
	}}
	r.projectHistory(page)
	s := displaySnapshot(r.snapshot)
	if len(s.Messages) != 3 {
		t.Fatalf("history dropped image-only: %+v", s.Messages)
	}
	for i, want := range []int{3, 7, 1} {
		if len(s.Messages[i].Images) != 1 || s.Messages[i].Images[0].Index != want || s.Messages[i].CanEdit {
			t.Fatalf("row=%+v", s.Messages[i])
		}
	}
	if s.Messages[2].Images[0].URL != "" {
		t.Fatal("unsupported raster has URL")
	}
	cloned := s.clone()
	cloned.Messages[0].Images[0].MIMEType = "mutated"
	if s.Messages[0].Images[0].MIMEType != "image/png" {
		t.Fatal("clone shared image slice")
	}
	s.Messages[0].Images[0].URL = "https://evil/image"
	display := displaySnapshot(s)
	if strings.Contains(display.Messages[0].Images[0].URL, "evil") || s.Messages[0].Images[0].URL != "https://evil/image" {
		t.Fatal("display trusts URL or mutates source")
	}
}

func TestSavedImageDisplayOwnsSlicesAndURLs(t *testing.T) {
	original := CatalogMessages{Messages: []HistoryMessage{{ID: "message", Role: "user", Images: []MessageImage{{Index: 4, MIMEType: "image/png", URL: "https://evil/image"}}}}}
	displayed := displayCatalogImages("project", "session", original)
	if displayed.Messages[0].Images[0].URL != "/projects/project/sessions/session/images/message/4" {
		t.Fatalf("url=%s", displayed.Messages[0].Images[0].URL)
	}
	displayed.Messages[0].Images[0].MIMEType = "mutated"
	if original.Messages[0].Images[0].MIMEType != "image/png" || original.Messages[0].Images[0].URL != "https://evil/image" {
		t.Fatal("display mutated catalog")
	}
}

func TestSentImageReadAdmissionDoesNotTakeMutationGate(t *testing.T) {
	m, r := streamRuntimeFixture(t, "project")
	r.mu.Lock()
	r.imageReads = 2
	r.mu.Unlock()
	if _, err := m.MessageImage(t.Context(), "project", "instance-one", "session-one", "row", 0); err != ErrRuntimeBusy {
		t.Fatalf("bounded reads=%v", err)
	}
	if !r.control.TryLock() {
		t.Fatal("image admission blocked Stop")
	}
	r.control.Unlock()
	r.mu.Lock()
	r.imageReads = 0
	r.mu.Unlock()
}
