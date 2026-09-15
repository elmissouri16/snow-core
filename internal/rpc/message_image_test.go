package rpc

import (
	"bytes"
	"encoding/base64"
	json "encoding/json/v2"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func rpcImageMessage(t *testing.T) protocol.Message {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a9ioAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	return protocol.Message{ID: "image-user", Role: protocol.RoleUser, Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: "look"}, {Type: protocol.BlockImage, MIMEType: "image/png", Data: data, Name: "PRIVATE-FILENAME"}, {Type: protocol.BlockProviderData, Data: []byte("PRIVATE")}}}
}

func TestPublicImagePageMetadataExcludesBytesAndUsesOriginalIndex(t *testing.T) {
	message := rpcImageMessage(t)
	assistant := message.Clone()
	assistant.ID = "assistant-image"
	assistant.Role = protocol.RoleAssistant
	page, err := buildMessagesPage("history", []protocol.Message{message, assistant}, publicPageParams(32))
	if err != nil {
		t.Fatal(err)
	}
	images := page.HistoryImages[message.ID]
	if len(images) != 1 || images[0].Index != 1 || images[0].MIMEType != "image/png" || len(page.HistoryImages[assistant.ID]) != 0 {
		t.Fatalf("metadata=%+v", page.HistoryImages)
	}
	frame, err := json.Marshal(Response{Type: "response", Command: "messages_page", Success: true, Data: page})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(frame, []byte("PRIVATE")) || bytes.Contains(frame, []byte(base64.StdEncoding.EncodeToString(message.Content[1].Data))) || bytes.Contains(frame, []byte(`"data":"`)) {
		t.Fatal("general history leaked bytes/private labels")
	}
	if err := resolveWireSchema(t, "output.schema.json").Validate(decodedJSON(t, frame)); err != nil {
		t.Fatal(err)
	}
}

func TestMessageImageRPCDurableIdentityAndSchemas(t *testing.T) {
	a := editRPCApp(t)
	message := rpcImageMessage(t)
	for _, entry := range []session.Entry{{Type: session.EntryMeta, ID: "image-turn", Key: session.MetaAgentTurn, Value: "user"}, {Type: session.EntryMessage, ID: message.ID, Message: &message}} {
		if err := a.Session.Append(entry); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []protocol.RPCMessageImageParams{{SessionID: a.Session.ID(), MessageID: message.ID, Index: 1}, {SessionID: a.Session.ID(), TurnID: "image-turn", Index: 1}} {
		var output bytes.Buffer
		srv := New(t.Context(), a, strings.NewReader(""), &output)
		params, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		req := Request{ID: "image", Type: "message_image", Params: params}
		wire, err := json.Marshal(req)
		if err != nil {
			t.Fatal(err)
		}
		if err := resolveWireSchema(t, "request.schema.json").Validate(decodedJSON(t, wire)); err != nil {
			t.Fatal(err)
		}
		if err := srv.handle(t.Context(), req); err != nil {
			t.Fatal(err)
		}
		srv.promptWG.Wait()
		if err := resolveWireSchema(t, "output.schema.json").Validate(decodedJSON(t, output.Bytes())); err != nil {
			t.Fatal(err)
		}
		var response struct {
			Data protocol.RPCMessageImageResult `json:"data"`
		}
		if err := json.Unmarshal(output.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if response.Data.MessageID != message.ID || response.Data.Index != 1 || !bytes.Equal(response.Data.Data, message.Content[1].Data) {
			t.Fatalf("result=%+v", response.Data)
		}
	}
}

func TestImageParamsFailClosed(t *testing.T) {
	for _, raw := range []string{
		`{}`, `null`, `{"session_id":"s","message_id":"m"}`, `{"session_id":"s","message_id":"m","index":null}`,
		`{"session_id":"s","message_id":"m","index":"0"}`, `{"session_id":"s","message_id":"m","index":-1}`,
		`{"session_id":"s","message_id":"m","index":10001}`, `{"session_id":"s","message_id":"m","index":0,"data":"x"}`,
		`{"session_id":"s","message_id":"m","index":0,"index":1}`, `{"session_id":"s","message_id":"m","turn_id":"t","index":0}`,
	} {
		for _, live := range []bool{false, true} {
			if validateImageParams([]byte(raw), live) == nil {
				t.Fatalf("accepted live=%v params=%s", live, raw)
			}
		}
	}
	if validateImageParams([]byte(`{"session_id":"s","turn_id":"t","index":0}`), false) == nil {
		t.Fatal("catalog accepted turn alias")
	}
}

func TestCatalogImageOnlyOutputBoundException(t *testing.T) {
	data := protocol.RPCCatalogImage{SessionID: "s", MessageID: "m", MIMEType: "image/png", Data: bytes.Repeat([]byte{1}, protocol.RPCMessageImageMaxBytes)}
	response := Response{Type: "response", Command: "catalog_image", Success: true, Data: data}
	if _, err := boundCatalogResponse(response); err != nil {
		t.Fatal(err)
	}
	response.Command = "catalog_messages"
	if _, err := boundCatalogResponse(response); err == nil {
		t.Fatal("image output exception escaped command fence")
	}
}

func TestImageMetadataRetainsUnsupportedUserImages(t *testing.T) {
	message := protocol.Message{ID: "unsupported", Role: protocol.RoleUser, Content: []protocol.ContentBlock{
		{Type: protocol.BlockProviderData, MIMEType: "image/png", Data: []byte("private")},
		{Type: protocol.BlockImage, MIMEType: "image/svg+xml", Data: []byte("<svg/>")},
		{Type: protocol.BlockImage, MIMEType: "https://example.test/name.png", Data: []byte("url")},
	}}
	page, err := buildMessagesPage("history", []protocol.Message{message}, publicPageParams(32))
	if err != nil {
		t.Fatal(err)
	}
	images := page.HistoryImages[message.ID]
	if len(images) != 2 || images[0].Index != 1 || images[1].Index != 2 || images[0].MIMEType != "" || images[1].MIMEType != "" {
		t.Fatalf("unsupported image presence lost or private data projected: %+v", images)
	}
}
