package web

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"image"
	_ "image/gif" // Register bounded header decoders; never decompress image pixels.
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const (
	composerMaxBlocks     = 16
	composerMaxImages     = 8
	composerMaxPrompt     = 64 << 10
	composerMaxText       = 128 << 10
	composerMaxImageBytes = 2 << 20
	composerMaxJSON       = 4 << 20
	composerMaxDimension  = 16384
	composerMaxPixels     = 40_000_000
)

// RuntimeComposerBackend is optional: existing text-only RuntimeBackend
// implementations retain their public Prompt contract unchanged.
// Content is a bounded user attachment payload, never arbitrary RPC parameters.
type RuntimeComposerBackend interface {
	PromptContent(context.Context, string, string, string, []protocol.ContentBlock) error
}

// PromptContent validates before any worker control or durable prompt intent.
// It shares Prompt's serial admission, ACK and definitive-completion machinery.
func (m *RuntimeManager) PromptContent(ctx context.Context, projectID, instanceID, text string, content []protocol.ContentBlock) error {
	if err := validateComposerContent(text, content); err != nil {
		return err
	}
	owned := make([]protocol.ContentBlock, len(content))
	for i, block := range content {
		owned[i] = block
		owned[i].Data = bytes.Clone(block.Data)
	}
	return m.prompt(ctx, projectID, instanceID, text, owned)
}

// runtimeComposerPrompt runs only after authentication, bounded form parsing,
// CSRF authorization and registered-project lookup in runtimeAction.
func (s *shell) runtimeComposerPrompt(ctx context.Context, w http.ResponseWriter, r *http.Request, project Project) {
	if r.URL.RawQuery != "" || len(r.PostForm) != 4 {
		http.Error(w, "Invalid attachment prompt fields", http.StatusBadRequest)
		return
	}
	for _, key := range []string{"csrf", "instance_id", "text", "content"} {
		if len(r.PostForm[key]) != 1 {
			http.Error(w, "Invalid attachment prompt fields", http.StatusBadRequest)
			return
		}
	}
	instance, text := r.PostForm.Get("instance_id"), r.PostForm.Get("text")
	if r.PostForm.Get("csrf") == "" || instance == "" || !runtimeOption(instance) {
		http.Error(w, "Invalid attachment prompt authority", http.StatusBadRequest)
		return
	}
	content, err := decodeComposerContent(r.PostForm.Get("content"))
	if err != nil || validateComposerContent(text, content) != nil {
		http.Error(w, "Invalid attachment prompt content", http.StatusBadRequest)
		return
	}
	backend, ok := s.runtimes.(RuntimeComposerBackend)
	if !ok {
		http.Error(w, "Attachment prompts are unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := backend.PromptContent(ctx, project.ID, instance, text, content); err != nil {
		http.Error(w, runtimePublicError(err), http.StatusConflict)
		return
	}
	s.runtimeJSON(w, struct {
		Success bool `json:"success"`
	}{true})
}

// Decode at most sixteen blocks (eight images plus labels) rather than
// allocating an attacker-sized array. Image count is bounded independently.
// Each variant has a closed field set, even when forbidden fields are null or
// zero-valued. json/v2 also rejects duplicate members and invalid UTF-8.
func decodeComposerContent(raw string) ([]protocol.ContentBlock, error) {
	if len(raw) == 0 || len(raw) > composerMaxJSON || !utf8.ValidString(raw) {
		return nil, ErrRuntimeInvalid
	}
	d := jsontext.NewDecoder(strings.NewReader(raw))
	start, err := d.ReadToken()
	if err != nil || start.Kind() != '[' {
		return nil, ErrRuntimeInvalid
	}
	var content []protocol.ContentBlock
	images := 0
	for d.PeekKind() != ']' {
		if len(content) == composerMaxBlocks {
			return nil, ErrRuntimeInvalid
		}
		rawBlock, err := d.ReadValue()
		if err != nil {
			return nil, ErrRuntimeInvalid
		}
		var header struct {
			Type protocol.ContentBlockType `json:"type"`
		}
		if json.Unmarshal(rawBlock, &header) != nil {
			return nil, ErrRuntimeInvalid
		}
		var block protocol.ContentBlock
		switch header.Type {
		case protocol.BlockText:
			var input struct {
				Type protocol.ContentBlockType `json:"type"`
				Text *string                   `json:"text"`
			}
			if json.Unmarshal(rawBlock, &input, json.RejectUnknownMembers(true)) != nil || input.Text == nil {
				return nil, ErrRuntimeInvalid
			}
			block = protocol.ContentBlock{Type: input.Type, Text: *input.Text}
		case protocol.BlockImage:
			images++
			if images > composerMaxImages {
				return nil, ErrRuntimeInvalid
			}
			var input struct {
				Type     protocol.ContentBlockType `json:"type"`
				MIMEType *string                   `json:"mime_type"`
				Data     *[]byte                   `json:"data"`
			}
			if json.Unmarshal(rawBlock, &input, json.RejectUnknownMembers(true)) != nil || input.MIMEType == nil || input.Data == nil {
				return nil, ErrRuntimeInvalid
			}
			block = protocol.ContentBlock{Type: input.Type, MIMEType: *input.MIMEType, Data: *input.Data}
		default:
			return nil, ErrRuntimeInvalid
		}
		content = append(content, block)
	}
	if _, err := d.ReadToken(); err != nil {
		return nil, ErrRuntimeInvalid
	}
	if _, err := d.ReadValue(); !errors.Is(err, io.EOF) {
		return nil, ErrRuntimeInvalid
	}
	if len(content) == 0 {
		return nil, ErrRuntimeInvalid
	}
	return content, nil
}

func composerText(text string) bool {
	return utf8.ValidString(text) && !strings.ContainsRune(text, 0)
}

func validateComposerContent(text string, content []protocol.ContentBlock) error {
	if len(text) > composerMaxPrompt || !composerText(text) || len(content) == 0 || len(content) > composerMaxBlocks {
		return ErrRuntimeInvalid
	}
	textBytes, imageBytes, images := len(text), 0, 0
	for _, block := range content {
		if block.PlanComplete || block.ToolCallID != "" || block.Name != "" || len(block.Arguments) != 0 {
			return ErrRuntimeInvalid
		}
		switch block.Type {
		case protocol.BlockText:
			if block.Text == "" || !composerText(block.Text) || block.MIMEType != "" || len(block.Data) != 0 {
				return ErrRuntimeInvalid
			}
			if len(block.Text) > composerMaxText-textBytes {
				return ErrRuntimeInvalid
			}
			textBytes += len(block.Text)
		case protocol.BlockImage:
			images++
			if images > composerMaxImages {
				return ErrRuntimeInvalid
			}
			if block.Text != "" || len(block.Data) == 0 || len(block.Data) > composerMaxImageBytes-imageBytes || !validComposerImage(block.MIMEType, block.Data) {
				return ErrRuntimeInvalid
			}
			imageBytes += len(block.Data)
		default:
			return ErrRuntimeInvalid
		}
	}
	if strings.TrimSpace(text) == "" && images == 0 {
		return ErrRuntimeInvalid
	}
	return nil
}

func validComposerImage(mime string, data []byte) bool {
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
	default:
		return false
	}
	if http.DetectContentType(data) != mime {
		return false
	}
	var width, height int
	if mime == "image/webp" {
		width, height = composerWebPDimensions(data)
	} else {
		config, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return false
		}
		width, height = config.Width, config.Height
	}
	return width > 0 && height > 0 && width <= composerMaxDimension && height <= composerMaxDimension && int64(width)*int64(height) <= composerMaxPixels
}

// WebP stores dimensions in its first VP8, VP8L or extended VP8X chunk. Reading
// that bounded header avoids both a new image dependency and pixel allocation.
func composerWebPDimensions(data []byte) (int, int) {
	if len(data) < 25 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" || uint64(binary.LittleEndian.Uint32(data[4:8]))+8 != uint64(len(data)) {
		return 0, 0
	}
	size := uint64(binary.LittleEndian.Uint32(data[16:20]))
	if size > uint64(len(data)-20) || size+size%2 > uint64(len(data)-20) {
		return 0, 0
	}
	payload := data[20 : 20+int(size)]
	switch string(data[12:16]) {
	case "VP8 ":
		if len(payload) >= 10 && payload[0]&1 == 0 && bytes.Equal(payload[3:6], []byte{0x9d, 0x01, 0x2a}) {
			return int(binary.LittleEndian.Uint16(payload[6:8]) & 0x3fff), int(binary.LittleEndian.Uint16(payload[8:10]) & 0x3fff)
		}
	case "VP8L":
		if len(payload) >= 5 && payload[0] == 0x2f && payload[4]&0xe0 == 0 {
			bits := binary.LittleEndian.Uint32(payload[1:5])
			return int(bits&0x3fff) + 1, int((bits>>14)&0x3fff) + 1
		}
	case "VP8X":
		if len(payload) == 10 {
			width := uint32(payload[4]) | uint32(payload[5])<<8 | uint32(payload[6])<<16
			height := uint32(payload[7]) | uint32(payload[8])<<8 | uint32(payload[9])<<16
			return int(width) + 1, int(height) + 1
		}
	}
	return 0, 0
}

var _ RuntimeComposerBackend = (*RuntimeManager)(nil)
