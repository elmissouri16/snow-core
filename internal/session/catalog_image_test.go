package session

import (
	"bytes"
	"context"
	"encoding/base64"
	json "encoding/json/v2"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func imageTestPNG(t *testing.T) []byte {
	t.Helper()
	var out bytes.Buffer
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.NRGBA{R: 255, A: 255})
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func imageTestMessage(t *testing.T) protocol.Message {
	return protocol.Message{ID: "user-image", Role: protocol.RoleUser, Content: []protocol.ContentBlock{
		{Type: protocol.BlockText, Text: "look"},
		{Type: protocol.BlockImage, MIMEType: "image/png", Data: imageTestPNG(t), Text: "UNTRUSTED-FILENAME", Name: "PRIVATE-LABEL"},
		{Type: protocol.BlockProviderData, MIMEType: "image/png", Data: []byte("PRIVATE-CONTINUITY")},
	}}
}

func TestCatalogImageMetadataAndDedicatedBytesReadOnly(t *testing.T) {
	root, cwd := t.TempDir(), t.TempDir()
	message := imageTestMessage(t)
	assistant := message.Clone()
	assistant.ID = "assistant"
	assistant.Role = protocol.RoleAssistant
	id, _ := catalogFixture(t, root, cwd, "images", message, assistant)
	before := catalogSnapshot(t, root)
	catalog := NewCatalog(root, cwd)
	page, err := catalog.Messages(t.Context(), id, 0, 50, true)
	if err != nil {
		t.Fatal(err)
	}
	want := []protocol.RPCMessageImage{{Index: 1, MIMEType: "image/png"}}
	if !reflect.DeepEqual(page.Messages[0].Images, want) || len(page.Messages[1].Images) != 0 {
		t.Fatalf("images=%+v", page)
	}
	encoded, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"UNTRUSTED-FILENAME", "PRIVATE-LABEL", "PRIVATE-CONTINUITY", base64.StdEncoding.EncodeToString(message.Content[1].Data), `"data"`} {
		if bytes.Contains(encoded, []byte(secret)) {
			t.Fatalf("public metadata leaked %q", secret)
		}
	}
	result, err := catalog.Image(t.Context(), protocol.RPCCatalogImageParams{SessionID: id, MessageID: message.ID, Index: 1})
	if err != nil || result.SessionID != id || result.MessageID != message.ID || result.Index != 1 || result.MIMEType != "image/png" || !bytes.Equal(result.Data, message.Content[1].Data) {
		t.Fatalf("image=%+v err=%v", result, err)
	}
	if !reflect.DeepEqual(before, catalogSnapshot(t, root)) {
		t.Fatal("image access mutated sessions or sidecars")
	}
	for _, params := range []protocol.RPCCatalogImageParams{
		{SessionID: id, MessageID: message.ID, Index: 0}, {SessionID: id, MessageID: message.ID, Index: 2},
		{SessionID: id, MessageID: message.ID, Index: -1}, {SessionID: id, MessageID: message.ID, Index: 10001},
		{SessionID: id, MessageID: "assistant", Index: 1}, {SessionID: id, MessageID: "missing", Index: 1},
		{SessionID: "foreign", MessageID: message.ID, Index: 1}, {SessionID: id, MessageID: strings.Repeat("x", 4097), Index: 1},
	} {
		if _, err := catalog.Image(t.Context(), params); err == nil {
			t.Fatalf("accepted invalid selector %+v", params)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := catalog.Image(ctx, protocol.RPCCatalogImageParams{SessionID: id, MessageID: message.ID, Index: 1}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
}

func TestCatalogImageProjectBranchLeaseAndSymlinkFences(t *testing.T) {
	root, cwd := t.TempDir(), t.TempDir()
	message := imageTestMessage(t)
	store, err := NewFileIndex(root).Create(cwd)
	if err != nil {
		t.Fatal(err)
	}
	s := store.(*SQLiteStore)
	id, path := s.ID(), s.Path()
	if err := s.Append(Entry{Type: EntryMessage, ID: message.ID, Message: &message}); err != nil {
		t.Fatal(err)
	}
	p := protocol.RPCCatalogImageParams{SessionID: id, MessageID: message.ID, Index: 1}
	if _, err := NewCatalog(root, cwd).Image(t.Context(), p); err == nil {
		t.Fatal("catalog opened active lease")
	}
	if err := s.SetBranchTip("root"); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(Entry{Type: EntryMessage, ID: "other", Message: new(protocol.NewUserMessage("other", "", "other branch"))}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := NewCatalog(root, cwd).Image(t.Context(), p); err == nil {
		t.Fatal("read nonvisible branch image")
	}
	goodID, goodPath := catalogFixture(t, root, cwd, "good", message)
	p.SessionID = goodID
	if _, err := NewCatalog(root, t.TempDir()).Image(t.Context(), p); err == nil {
		t.Fatal("read cross-project image")
	}
	linkRoot := t.TempDir()
	if err := os.Symlink(filepath.Dir(goodPath), filepath.Join(linkRoot, EncodeCWD(cwd))); err != nil {
		t.Fatal(err)
	}
	if _, err := NewCatalog(linkRoot, cwd).Image(t.Context(), p); err == nil {
		t.Fatal("followed symlink ancestor")
	}
	p.SessionID = path
	if _, err := NewCatalog(root, cwd).Image(t.Context(), p); err == nil {
		t.Fatal("accepted path as session ID")
	}
}

func TestCatalogImageMetadataSurvivesOversizedMessage(t *testing.T) {
	root, cwd := t.TempDir(), t.TempDir()
	message := imageTestMessage(t)
	message.Content = append(message.Content, protocol.ContentBlock{Type: protocol.BlockProviderData, Data: bytes.Repeat([]byte("x"), catalogMaxMessageBytes)})
	id, _ := catalogFixture(t, root, cwd, "large", message)
	catalog := NewCatalog(root, cwd)
	page, err := catalog.Messages(t.Context(), id, 0, 1)
	if err != nil || !page.Messages[0].Truncated || len(page.Messages[0].Images) != 1 {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	result, err := catalog.Image(t.Context(), protocol.RPCCatalogImageParams{SessionID: id, MessageID: message.ID, Index: 1})
	if err != nil || !bytes.Equal(result.Data, message.Content[1].Data) {
		t.Fatalf("selected block unavailable: %v", err)
	}
}

func TestMessageImageRasterValidation(t *testing.T) {
	pngData := imageTestPNG(t)
	for _, test := range []struct {
		name, mime string
		data       []byte
	}{
		{"svg", "image/svg+xml", []byte(`<svg/>`)}, {"url", "image/png", []byte("https://example.test/image.png")},
		{"wrong-mime", "image/jpeg", pngData}, {"empty", "image/png", nil}, {"oversize", "image/png", bytes.Repeat([]byte{1}, protocol.RPCMessageImageMaxBytes+1)},
		{"private-type", "application/private", pngData}, {"invalid-webp", "image/webp", []byte("RIFFbadWEBP")},
	} {
		t.Run(test.name, func(t *testing.T) {
			if validateMessageImage(test.mime, test.data) == nil {
				t.Fatal("accepted invalid raster")
			}
		})
	}
	for _, format := range []string{"png", "jpeg", "gif"} {
		var data bytes.Buffer
		img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
		var err error
		switch format {
		case "png":
			err = png.Encode(&data, img)
		case "jpeg":
			err = jpeg.Encode(&data, img, nil)
		case "gif":
			err = gif.Encode(&data, img, nil)
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := validateMessageImage("image/"+format, data.Bytes()); err != nil {
			t.Fatalf("%s: %v", format, err)
		}
	}
	// GIF logical-screen headers suffice for DecodeConfig; test both independent
	// dimension and pixel-count limits without allocating enormous pixel buffers.
	for _, dims := range [][2]int{{16385, 1}, {10000, 4001}} {
		header := []byte{'G', 'I', 'F', '8', '9', 'a', byte(dims[0]), byte(dims[0] >> 8), byte(dims[1]), byte(dims[1] >> 8), 0, 0, 0}
		if validateMessageImage("image/gif", header) == nil {
			t.Fatalf("accepted dimensions %v", dims)
		}
	}
	webp, err := base64.StdEncoding.DecodeString("UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA")
	if err != nil {
		t.Fatal(err)
	}
	if err := validateMessageImage("image/webp", webp); err != nil {
		t.Fatal(err)
	}
}

func TestCatalogImageUnsupportedMetadataAndOversizeRetrieval(t *testing.T) {
	root, cwd := t.TempDir(), t.TempDir()
	message := protocol.Message{ID: "unsupported", Role: protocol.RoleUser, Content: []protocol.ContentBlock{
		{Type: protocol.BlockProviderData, MIMEType: "image/png", Data: []byte("private")},
		{Type: protocol.BlockImage, MIMEType: "image/svg+xml", Data: []byte("<svg/>")},
		{Type: protocol.BlockImage, MIMEType: "image/png", Data: bytes.Repeat([]byte{1}, protocol.RPCMessageImageMaxBytes+1)},
	}}
	id, _ := catalogFixture(t, root, cwd, "unsupported", message)
	catalog := NewCatalog(root, cwd)
	page, err := catalog.Messages(t.Context(), id, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	images := page.Messages[0].Images
	if len(images) != 2 || images[0].Index != 1 || images[0].MIMEType != "" || images[1].Index != 2 {
		t.Fatalf("unsupported image presence lost: %+v", images)
	}
	for _, index := range []int{0, 1, 2} {
		if _, err := catalog.Image(t.Context(), protocol.RPCCatalogImageParams{SessionID: id, MessageID: message.ID, Index: index}); err == nil {
			t.Fatalf("accepted private/unsupported/oversize index %d", index)
		}
	}
}
