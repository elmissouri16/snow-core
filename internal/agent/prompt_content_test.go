package agent

import (
	"testing"

	"github.com/snow-core/snow/pkg/protocol"
)

func TestValidateUserAttachments(t *testing.T) {
	vision := protocol.Model{ID: "vision", SupportsVision: true}
	valid := protocol.ContentBlock{Type: protocol.BlockImage, MIMEType: "image/png", Data: []byte{1}}
	if err := validateUserAttachments(vision, []protocol.ContentBlock{valid}); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		model protocol.Model
		block protocol.ContentBlock
	}{
		{"text model", protocol.Model{ID: "text"}, valid},
		{"wrong block", vision, protocol.ContentBlock{Type: protocol.BlockText, Text: "x"}},
		{"wrong MIME", vision, protocol.ContentBlock{Type: protocol.BlockImage, MIMEType: "image/bmp", Data: []byte{1}}},
		{"empty", vision, protocol.ContentBlock{Type: protocol.BlockImage, MIMEType: "image/png"}},
		{"too large", vision, protocol.ContentBlock{Type: protocol.BlockImage, MIMEType: "image/png", Data: make([]byte, maxUserImageBytes+1)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateUserAttachments(tc.model, []protocol.ContentBlock{tc.block}); err == nil {
				t.Fatal("invalid attachment accepted")
			}
		})
	}
}
