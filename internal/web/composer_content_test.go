package web

import (
	"bytes"
	"encoding/binary"
	"encoding/json/v2"
	"errors"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"reflect"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func composerTestPNG(t *testing.T) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := png.Encode(&out, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestComposerContentValidation(t *testing.T) {
	pngData := composerTestPNG(t)
	imageBlock := protocol.ContentBlock{Type: protocol.BlockImage, MIMEType: "image/png", Data: pngData}
	textBlock := protocol.ContentBlock{Type: protocol.BlockText, Text: "Attachment: note.txt\nhello"}
	maxBlocks := make([]protocol.ContentBlock, composerMaxBlocks)
	for i := range maxBlocks {
		maxBlocks[i] = textBlock
	}
	tooManyImages := make([]protocol.ContentBlock, composerMaxImages+1)
	for i := range tooManyImages {
		tooManyImages[i] = imageBlock
	}
	for _, tc := range []struct {
		name    string
		text    string
		content []protocol.ContentBlock
	}{
		{"text attachment", "Review attachment", []protocol.ContentBlock{textBlock}},
		{"image only", "", []protocol.ContentBlock{imageBlock}},
		{"mixed", "Look at sample.png", []protocol.ContentBlock{textBlock, imageBlock}},
		{"text boundary", strings.Repeat("p", composerMaxPrompt), []protocol.ContentBlock{{Type: protocol.BlockText, Text: strings.Repeat("a", composerMaxText-composerMaxPrompt)}}},
		{"block boundary", "sixteen", maxBlocks},
		{"image boundary", "large", []protocol.ContentBlock{{Type: protocol.BlockImage, MIMEType: "image/png", Data: append(bytes.Clone(pngData), make([]byte, composerMaxImageBytes-len(pngData))...)}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateComposerContent(tc.text, tc.content); err != nil {
				t.Fatal(err)
			}
		})
	}
	bad := []struct {
		name    string
		text    string
		content []protocol.ContentBlock
	}{
		{"no blocks", "text", nil},
		{"too many", "text", make([]protocol.ContentBlock, composerMaxBlocks+1)},
		{"too many images", "images", tooManyImages},
		{"empty message text only", "", []protocol.ContentBlock{textBlock}},
		{"blank message text only", " \n", []protocol.ContentBlock{textBlock}},
		{"prompt too large", strings.Repeat("a", composerMaxPrompt+1), []protocol.ContentBlock{textBlock}},
		{"total text too large", "a", []protocol.ContentBlock{{Type: protocol.BlockText, Text: strings.Repeat("a", composerMaxText)}}},
		{"nul prompt", "a\x00", []protocol.ContentBlock{imageBlock}},
		{"utf8 prompt", "\xff", []protocol.ContentBlock{imageBlock}},
		{"nul attachment", "read", []protocol.ContentBlock{{Type: protocol.BlockText, Text: "x\x00"}}},
		{"utf8 attachment", "read", []protocol.ContentBlock{{Type: protocol.BlockText, Text: "\xff"}}},
		{"empty attachment", "read", []protocol.ContentBlock{{Type: protocol.BlockText}}},
		{"text with data", "read", []protocol.ContentBlock{{Type: protocol.BlockText, Text: "x", Data: pngData}}},
		{"text with mime", "read", []protocol.ContentBlock{{Type: protocol.BlockText, Text: "x", MIMEType: "image/png"}}},
		{"thinking", "read", []protocol.ContentBlock{{Type: protocol.BlockThinking, Text: "private"}}},
		{"provider", "read", []protocol.ContentBlock{{Type: protocol.BlockProviderData, Data: []byte("private")}}},
		{"tool", "read", []protocol.ContentBlock{{Type: protocol.BlockToolCall, Name: "bash"}}},
		{"plan", "read", []protocol.ContentBlock{{Type: protocol.BlockPlan, Text: "plan"}}},
		{"text with plan flag", "read", []protocol.ContentBlock{{Type: protocol.BlockText, Text: "x", PlanComplete: true}}},
		{"text with tool id", "read", []protocol.ContentBlock{{Type: protocol.BlockText, Text: "x", ToolCallID: "id"}}},
		{"text with name", "read", []protocol.ContentBlock{{Type: protocol.BlockText, Text: "x", Name: "tool"}}},
		{"text with args", "read", []protocol.ContentBlock{{Type: protocol.BlockText, Text: "x", Arguments: []byte("null")}}},
		{"image text", "read", []protocol.ContentBlock{{Type: protocol.BlockImage, MIMEType: "image/png", Data: pngData, Text: "x"}}},
		{"no image bytes", "read", []protocol.ContentBlock{{Type: protocol.BlockImage, MIMEType: "image/png"}}},
		{"mime mismatch", "read", []protocol.ContentBlock{{Type: protocol.BlockImage, MIMEType: "image/jpeg", Data: pngData}}},
		{"svg", "read", []protocol.ContentBlock{{Type: protocol.BlockImage, MIMEType: "image/svg+xml", Data: []byte("<svg/>")}}},
		{"invalid image", "read", []protocol.ContentBlock{{Type: protocol.BlockImage, MIMEType: "image/png", Data: []byte("not png")}}},
		{"truncated png", "read", []protocol.ContentBlock{{Type: protocol.BlockImage, MIMEType: "image/png", Data: pngData[:16]}}},
		{"oversized image", "read", []protocol.ContentBlock{{Type: protocol.BlockImage, MIMEType: "image/png", Data: make([]byte, composerMaxImageBytes+1)}}},
		{"total images", "read", []protocol.ContentBlock{{Type: protocol.BlockImage, MIMEType: "image/png", Data: append(bytes.Clone(pngData), make([]byte, composerMaxImageBytes-len(pngData))...)}, imageBlock}},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			// No worker is installed: invalid typed content must fail validation
			// rather than getting as far as runtime lookup or prompt intent.
			m := &RuntimeManager{}
			if err := m.PromptContent(t.Context(), "missing", "instance", tc.text, tc.content); !errors.Is(err, ErrRuntimeInvalid) {
				t.Fatalf("validation = %v", err)
			}
		})
	}
}

func TestComposerContentStrictJSON(t *testing.T) {
	for _, raw := range []string{
		"", "null", "{}", "[]", "[null]", "[1]", `[{}]`, `[{"type":"text"}]`,
		`[{"type":"text","text":null}]`, `[{"type":"text","text":123}]`,
		`[{"type":"text","text":"x","type":"image"}]`,
		`[{"type":"text","text":"x","Text":"y"}]`,
		`[{"type":"text","text":"x","plan_complete":false}]`,
		`[{"type":"text","text":"x","tool_call_id":""}]`,
		`[{"type":"text","text":"x","name":null}]`,
		`[{"type":"text","text":"x","arguments":{}}]`,
		`[{"type":"text","text":"x","mime_type":""}]`,
		`[{"type":"text","text":"x","data":null}]`,
		`[{"type":"thinking","text":"x"}]`,
		`[{"type":"provider_data","data":"c2VjcmV0"}]`,
		`[{"type":"image","mime_type":"image/png","data":"!"}]`,
		`[{"type":"image","mime_type":"image/png","data":[1,2,3]}]`,
		`[{"type":"image","mime_type":"image/png","data":null}]`,
		`[{"type":"image","mime_type":null,"data":"YQ=="}]`,
		`[{"type":"image","mime_type":"image/png","data":"YQ==","text":""}]`,
		`[{"type":"text","text":"x"}] {}`, `[{"type":"text","text":"x"},]`,
		"[{\"type\":\"text\",\"text\":\"\xff\"}]", `[{"type":"text","text":"\ud800"}]`,
		"[" + strings.Repeat(`{"type":"text","text":"x"},`, composerMaxBlocks) + `{"type":"text","text":"x"}]`,
		strings.Repeat(" ", composerMaxJSON+1),
	} {
		if _, err := decodeComposerContent(raw); !errors.Is(err, ErrRuntimeInvalid) {
			t.Fatalf("accepted malformed JSON (length %d): %.100q", len(raw), raw)
		}
	}
	content := []protocol.ContentBlock{{Type: protocol.BlockText, Text: "Label\nbody"}, {Type: protocol.BlockImage, MIMEType: "image/png", Data: composerTestPNG(t)}}
	raw, err := json.Marshal(content)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeComposerContent(string(raw))
	if err != nil || len(got) != 2 || got[0].Text != content[0].Text || !bytes.Equal(got[1].Data, content[1].Data) {
		t.Fatalf("decode lost content: %v", err)
	}
}

func TestComposerImageHeadersAndDimensions(t *testing.T) {
	for _, mime := range []string{"image/png", "image/jpeg", "image/gif"} {
		var out bytes.Buffer
		picture := image.NewRGBA(image.Rect(0, 0, 2, 3))
		var err error
		switch mime {
		case "image/png":
			err = png.Encode(&out, picture)
		case "image/jpeg":
			err = jpeg.Encode(&out, picture, nil)
		case "image/gif":
			err = gif.Encode(&out, picture, nil)
		}
		if err != nil || !validComposerImage(mime, out.Bytes()) {
			t.Fatalf("%s: %v", mime, err)
		}
	}
	webp := func(kind string, payload []byte) []byte {
		b := append([]byte("RIFF\x00\x00\x00\x00WEBP"+kind+"\x00\x00\x00\x00"), payload...)
		if len(payload)%2 != 0 {
			b = append(b, 0)
		}
		binary.LittleEndian.PutUint32(b[4:8], uint32(len(b)-8))
		binary.LittleEndian.PutUint32(b[16:20], uint32(len(payload)))
		return b
	}
	for _, data := range [][]byte{
		webp("VP8 ", []byte{0, 0, 0, 0x9d, 1, 0x2a, 2, 0, 3, 0}),
		webp("VP8L", []byte{0x2f, 1, 0x80, 0, 0}),
		webp("VP8X", []byte{0, 0, 0, 0, 1, 0, 0, 2, 0, 0}),
	} {
		if !validComposerImage("image/webp", data) {
			t.Fatalf("valid WebP header rejected: %x", data)
		}
		if validComposerImage("image/webp", data[:len(data)-1]) {
			t.Fatal("truncated RIFF accepted")
		}
	}
	oversized := webp("VP8X", []byte{0, 0, 0, 0, 0xff, 0xff, 0, 2, 0, 0})
	if validComposerImage("image/webp", oversized) {
		t.Fatal("oversized WebP accepted")
	}
	pixelBomb := webp("VP8X", []byte{0, 0, 0, 0, 0x0f, 0x27, 0, 0x0f, 0x27, 0})
	if validComposerImage("image/webp", pixelBomb) {
		t.Fatal("WebP pixel limit ignored")
	}
	var out bytes.Buffer
	if err := png.Encode(&out, image.NewRGBA(image.Rect(0, 0, composerMaxDimension+1, 1))); err != nil {
		t.Fatal(err)
	}
	if validComposerImage("image/png", out.Bytes()) {
		t.Fatal("PNG dimension limit ignored")
	}
}

func TestComposerContentEightLabeledImages(t *testing.T) {
	imageBlock := protocol.ContentBlock{Type: protocol.BlockImage, MIMEType: "image/png", Data: composerTestPNG(t)}
	var content []protocol.ContentBlock
	for range composerMaxImages {
		content = append(content, protocol.ContentBlock{Type: protocol.BlockText, Text: "Attachment: $name.png"}, imageBlock)
	}
	if len(content) != composerMaxBlocks {
		t.Fatal("fixture must fill the block limit")
	}
	if err := validateComposerContent("Review attached images", content); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(content)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeComposerContent(string(raw))
	if err != nil || !reflect.DeepEqual(decoded, content) {
		t.Fatalf("eight labeled images changed on decode: %v", err)
	}
	// Nine bare images remain below sixteen blocks, but exceed the independent
	// image limit. Both the JSON boundary and direct typed entry reject them.
	nineImages := make([]protocol.ContentBlock, composerMaxImages+1)
	for i := range nineImages {
		nineImages[i] = imageBlock
	}
	raw, err = json.Marshal(nineImages)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeComposerContent(string(raw)); !errors.Is(err, ErrRuntimeInvalid) {
		t.Fatalf("decoder accepted nine images: %v", err)
	}
	if err := validateComposerContent("images", nineImages); !errors.Is(err, ErrRuntimeInvalid) {
		t.Fatalf("typed validator accepted nine images: %v", err)
	}
}
