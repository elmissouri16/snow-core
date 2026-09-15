package session

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// DecodeConfig checks dimensions without allocating a decoded pixel buffer.
// The dedicated reader accepts only an exact supported MIME/signature pair.
func validateMessageImage(mime string, data []byte) error {
	invalid := errors.New("session: unsupported, invalid or oversized raster image")
	if len(data) == 0 || len(data) > protocol.RPCMessageImageMaxBytes || protocol.MessageImageMIME(mime) == "" {
		return invalid
	}
	var width, height int
	if mime == "image/webp" {
		var ok bool
		width, height, ok = webpImageDimensions(data)
		if !ok {
			return invalid
		}
	} else {
		config, format, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || "image/"+format != mime {
			return invalid
		}
		width, height = config.Width, config.Height
	}
	if width < 1 || height < 1 || width > 16384 || height > 16384 || int64(width)*int64(height) > 40_000_000 {
		return invalid
	}
	return nil
}

// WebP's RIFF and first image/extended header carry bounded dimensions. No
// external codec registration or raster decompression is needed for previews.
func webpImageDimensions(data []byte) (int, int, bool) {
	if len(data) < 20 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" || uint64(binary.LittleEndian.Uint32(data[4:8]))+8 != uint64(len(data)) {
		return 0, 0, false
	}
	size := uint64(binary.LittleEndian.Uint32(data[16:20]))
	if size > uint64(len(data)-20) {
		return 0, 0, false
	}
	payload := data[20 : 20+int(size)]
	switch string(data[12:16]) {
	case "VP8 ":
		if len(payload) < 10 || payload[0]&1 != 0 || !bytes.Equal(payload[3:6], []byte{0x9d, 0x01, 0x2a}) {
			return 0, 0, false
		}
		return int(binary.LittleEndian.Uint16(payload[6:8]) & 0x3fff), int(binary.LittleEndian.Uint16(payload[8:10]) & 0x3fff), true
	case "VP8L":
		if len(payload) < 5 || payload[0] != 0x2f || payload[4]>>5 != 0 {
			return 0, 0, false
		}
		bits := binary.LittleEndian.Uint32(payload[1:5])
		return int(bits&0x3fff) + 1, int((bits>>14)&0x3fff) + 1, true
	case "VP8X":
		if len(payload) != 10 || payload[0]&0xc1 != 0 || payload[1] != 0 || payload[2] != 0 || payload[3] != 0 {
			return 0, 0, false
		}
		width := int(payload[4]) | int(payload[5])<<8 | int(payload[6])<<16
		height := int(payload[7]) | int(payload[8])<<8 | int(payload[9])<<16
		return width + 1, height + 1, true
	}
	return 0, 0, false
}
