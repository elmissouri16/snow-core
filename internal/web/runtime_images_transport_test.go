package web

import (
	"bufio"
	"bytes"
	"encoding/json/v2"
	"fmt"
	"image"
	"image/png"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func init() {
	if os.Getenv("SNOW_WEB_IMAGES_TEST_CHILD") != "" {
		os.Exit(sentImagesWorkerFixture())
	}
}

func sentImagesWorkerFixture() int {
	mode := os.Getenv("SNOW_WEB_IMAGES_TEST_CHILD")
	emit := func(value any) { data, _ := json.Marshal(value); fmt.Println(string(data)) }
	ready := protocol.NewRPCReady("image-test")
	ready.Capabilities = []string{"session_management", "session_info", "prompt_completion", "permission_interaction", "user_input", "messages_page", "message_image", "messages_public_history", "runtime_free_catalog", "catalog_image"}
	emit(ready)
	log, _ := os.OpenFile(os.Getenv("SNOW_WEB_RUNTIME_TEST_LOG"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if log != nil {
		defer log.Close()
	}
	cwd, _ := os.Getwd()
	var imageData bytes.Buffer
	_ = png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	var pending *protocol.RPCResponse
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var req protocol.RPCRequest
		if json.Unmarshal(scanner.Bytes(), &req) != nil {
			return 1
		}
		if log != nil {
			fmt.Fprintln(log, req.Type)
		}
		response := protocol.RPCResponse{Type: "response", ID: req.ID, Command: req.Type, Success: true}
		switch req.Type {
		case "session_create", "session_open":
			response.Data = protocol.RPCSessionSummary{SessionID: "session", Active: true}
		case "session_info":
			response.Data = protocol.RPCSessionInfo{SessionID: "session", CWD: cwd, Path: "/private/session.db", Provider: "test", Model: "test", PermissionMode: "ask"}
		case "messages_page":
			var images []protocol.RPCMessageImage
			for index := range 8 {
				images = append(images, protocol.RPCMessageImage{Index: index + 2, MIMEType: "image/png"})
			}
			response.Data = protocol.RPCMessagesPage{Messages: []protocol.Message{{ID: "durable", Role: protocol.RoleUser}}, HistoryImages: map[string][]protocol.RPCMessageImage{"durable": images}}
		case "message_image", "catalog_image":
			var p protocol.RPCMessageImageParams
			if json.Unmarshal(req.Params, &p) != nil {
				return 2
			}
			if p.SessionID != "session" || p.MessageID != "durable" && p.TurnID != "accepted" || p.Index < 2 || p.Index > 9 {
				response.Success = false
				break
			}
			response.Data = protocol.RPCCatalogImage{SessionID: p.SessionID, MessageID: "durable", Index: p.Index, MIMEType: "image/png", Data: imageData.Bytes()}
			if mode == "pending" {
				pending = new(response)
				continue
			}
		case "abort":
			emit(response)
			if pending != nil {
				emit(*pending)
				pending = nil
			}
			continue
		case "shutdown":
			emit(response)
			return 0
		default:
			response.Success = false
		}
		emit(response)
	}
	return 0
}

func sentImagesManager(t *testing.T, mode string) (*RuntimeManager, Project, RuntimeSnapshot, string) {
	t.Helper()
	m, projects, log := runtimeTestManager(t, "")
	m.env = slices.DeleteFunc(m.env, func(value string) bool { return strings.HasPrefix(value, "SNOW_WEB_RUNTIME_TEST_CHILD=") })
	m.env = append(m.env, "SNOW_WEB_IMAGES_TEST_CHILD="+mode)
	snapshot, err := m.Open(t.Context(), projects[0], "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	return m, projects[0], snapshot, log
}

func TestSentImageOwnedWorkerReadsAllEightWithoutMutations(t *testing.T) {
	m, project, snapshot, log := sentImagesManager(t, "normal")
	if len(snapshot.Messages) != 1 || len(snapshot.Messages[0].Images) != 8 || snapshot.Messages[0].CanEdit {
		t.Fatalf("history=%+v", snapshot.Messages)
	}
	before, _ := os.ReadFile(log)
	for _, image := range snapshot.Messages[0].Images {
		result, err := m.MessageImage(t.Context(), project.ID, snapshot.InstanceID, snapshot.SessionID, snapshot.Messages[0].ID, image.Index)
		if err != nil || result.Index != image.Index || !bytes.Equal(result.Data, sentImagePNG(t)) {
			t.Fatalf("image %d: %+v %v", image.Index, result, err)
		}
	}
	after, _ := os.ReadFile(log)
	if string(after[len(before):]) != strings.Repeat("message_image\n", 8) {
		t.Fatalf("unexpected calls=%s", after)
	}
	r, _ := m.runtime(project.ID, snapshot.InstanceID)
	r.mu.Lock()
	r.snapshot.Messages[0].SourceID = ""
	r.snapshot.Messages[0].SourceTurnID = "accepted"
	r.mu.Unlock()
	if result, err := m.MessageImage(t.Context(), project.ID, snapshot.InstanceID, snapshot.SessionID, snapshot.Messages[0].ID, 2); err != nil || result.MessageID != "durable" {
		t.Fatalf("accepted-turn read=%+v %v", result, err)
	}
}

func waitSentImageCall(t *testing.T, log string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(log)
		if strings.Contains(string(data), "message_image\n") {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("image read never reached worker")
}

func TestSentImagePendingWorkerReadDoesNotBlockAbort(t *testing.T) {
	m, project, snapshot, log := sentImagesManager(t, "pending")
	done := make(chan error, 1)
	go func() {
		_, err := m.MessageImage(t.Context(), project.ID, snapshot.InstanceID, snapshot.SessionID, snapshot.Messages[0].ID, 2)
		done <- err
	}()
	waitSentImageCall(t, log)
	if err := m.Abort(t.Context(), project.ID, snapshot.InstanceID); err != nil {
		t.Fatalf("image read blocked abort: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestSentImagePendingWorkerReadRejectsInstanceRotation(t *testing.T) {
	m, project, snapshot, log := sentImagesManager(t, "pending")
	done := make(chan error, 1)
	go func() {
		_, err := m.MessageImage(t.Context(), project.ID, snapshot.InstanceID, snapshot.SessionID, snapshot.Messages[0].ID, 2)
		done <- err
	}()
	waitSentImageCall(t, log)
	r, _ := m.runtime(project.ID, snapshot.InstanceID)
	r.mu.Lock()
	r.instanceID = "replacement"
	r.snapshot.InstanceID = "replacement"
	r.mu.Unlock()
	if err := m.Abort(t.Context(), project.ID, "replacement"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("old image read survived instance rotation")
	}
}

func TestSavedImageWorkerCatalogUsesOnlyBoundedRead(t *testing.T) {
	m, project, _, log := sentImagesManager(t, "normal")
	c := &workerCatalog{executable: m.executable, env: m.env, slots: make(chan struct{}, 2)}
	before, _ := os.ReadFile(log)
	result, err := c.Image(t.Context(), project, protocol.RPCCatalogImageParams{SessionID: "session", MessageID: "durable", Index: 3})
	if err != nil || !bytes.Equal(result.Data, sentImagePNG(t)) {
		t.Fatalf("catalog image=%+v %v", result, err)
	}
	after, _ := os.ReadFile(log)
	if string(after[len(before):]) != "catalog_image\n" {
		t.Fatalf("catalog mutated: %q", after[len(before):])
	}
}
