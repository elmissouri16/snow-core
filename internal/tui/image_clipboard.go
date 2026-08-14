package tui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/snow-core/snow/pkg/protocol"
)

const (
	maxClipboardImageBytes = 20 << 20
	maxPromptImages        = 8
)

type clipboardImageMsg struct {
	generation uint64
	block      protocol.ContentBlock
	err        error
}

var (
	errClipboardHasNoImage = errors.New("clipboard does not contain an image")
	readClipboardImageFunc = readClipboardImage
)

func readClipboardImage() (protocol.ContentBlock, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var data []byte
	var err error
	switch runtime.GOOS {
	case "darwin":
		data, err = boundedCommandOutput(ctx, "osascript", "-l", "JavaScript", "-e", macOSClipboardImageScript)
	case "linux":
		data, err = readLinuxClipboardImage(ctx)
	default:
		return protocol.ContentBlock{}, fmt.Errorf("clipboard images are unsupported on %s", runtime.GOOS)
	}
	if err != nil {
		return protocol.ContentBlock{}, fmt.Errorf("read clipboard image: %w", err)
	}
	if len(data) == 0 {
		return protocol.ContentBlock{}, errClipboardHasNoImage
	}
	if len(data) > maxClipboardImageBytes {
		return protocol.ContentBlock{}, fmt.Errorf("clipboard image exceeds %d MiB limit", maxClipboardImageBytes>>20)
	}
	mime, ok := clipboardImageMIME(data)
	if !ok {
		return protocol.ContentBlock{}, errors.New("clipboard image is not PNG, JPEG, GIF, or WebP")
	}
	return protocol.ContentBlock{Type: protocol.BlockImage, MIMEType: mime, Data: append([]byte(nil), data...)}, nil
}

func boundedCommandOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	return boundedCommandOutputLimit(ctx, maxClipboardImageBytes, name, args...)
}

func boundedCommandOutputLimit(ctx context.Context, limit int, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(stdout, int64(limit)+1))
	waitErr := cmd.Wait()
	if readErr != nil {
		return nil, readErr
	}
	if len(data) > limit {
		return nil, fmt.Errorf("clipboard output exceeds %d byte limit", limit)
	}
	if waitErr != nil {
		return nil, waitErr
	}
	return data, nil
}

func clipboardImageMIME(data []byte) (string, bool) {
	switch {
	case len(data) >= 8 && bytes.Equal(data[:8], []byte("\x89PNG\r\n\x1a\n")):
		return "image/png", true
	case len(data) >= 3 && bytes.Equal(data[:3], []byte{0xff, 0xd8, 0xff}):
		return "image/jpeg", true
	case len(data) >= 6 && (bytes.Equal(data[:6], []byte("GIF87a")) || bytes.Equal(data[:6], []byte("GIF89a"))):
		return "image/gif", true
	case len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")):
		return "image/webp", true
	default:
		return "", false
	}
}

func readLinuxClipboardImage(ctx context.Context) ([]byte, error) {
	var failures []error
	if _, err := exec.LookPath("wl-paste"); err == nil {
		types, listErr := boundedCommandOutputLimit(ctx, 64<<10, "wl-paste", "--list-types")
		if listErr == nil {
			if mime := preferredClipboardImageType(string(types)); mime != "" {
				if data, readErr := boundedCommandOutput(ctx, "wl-paste", "--no-newline", "--type", mime); readErr == nil {
					return data, nil
				} else {
					failures = append(failures, readErr)
				}
			} else {
				failures = append(failures, errClipboardHasNoImage)
			}
		} else {
			failures = append(failures, listErr)
		}
	}
	if _, err := exec.LookPath("xclip"); err == nil {
		types, listErr := boundedCommandOutputLimit(ctx, 64<<10, "xclip", "-selection", "clipboard", "-t", "TARGETS", "-o")
		if listErr == nil {
			if mime := preferredClipboardImageType(string(types)); mime != "" {
				if data, readErr := boundedCommandOutput(ctx, "xclip", "-selection", "clipboard", "-t", mime, "-o"); readErr == nil {
					return data, nil
				} else {
					failures = append(failures, readErr)
				}
			} else {
				failures = append(failures, errClipboardHasNoImage)
			}
		} else {
			failures = append(failures, listErr)
		}
	}
	if len(failures) == 0 {
		// Bubbles may still have a functional text backend (for example xsel).
		return nil, errClipboardHasNoImage
	}
	for _, failure := range failures {
		if !errors.Is(failure, errClipboardHasNoImage) {
			return nil, errors.Join(failures...)
		}
	}
	return nil, errClipboardHasNoImage
}

func preferredClipboardImageType(types string) string {
	available := make(map[string]bool)
	for _, field := range strings.Fields(types) {
		available[strings.TrimSpace(field)] = true
	}
	for _, mime := range []string{"image/png", "image/jpeg", "image/webp", "image/gif"} {
		if available[mime] {
			return mime
		}
	}
	return ""
}

const macOSClipboardImageScript = `ObjC.import('AppKit'); ObjC.import('Foundation');
const p = $.NSPasteboard.generalPasteboard;
const out = $.NSFileHandle.fileHandleWithStandardOutput;
let wrote = false;
const types = ['public.png','public.jpeg','com.compuserve.gif','org.webmproject.webp'];
for (const t of types) { const d = p.dataForType(t); if (ObjC.unwrap(d) !== undefined && Number(d.length) > 0) { out.writeData(d); wrote = true; break; } }
if (!wrote) { const d = p.dataForType('public.tiff'); if (ObjC.unwrap(d) !== undefined) { const image = $.NSImage.alloc.initWithData(d); const tiff = image ? image.TIFFRepresentation : null; const r = tiff ? $.NSBitmapImageRep.imageRepWithData(tiff) : null; if (r) { const png = r.representationUsingTypeProperties(4, $({})); if (png) out.writeData(png); } } }`

func imageAttachmentLabel(block protocol.ContentBlock, index int) string {
	kind := strings.TrimPrefix(block.MIMEType, "image/")
	return fmt.Sprintf("[image %d · %s · %s]", index+1, kind, formatImageBytes(len(block.Data)))
}

func formatImageBytes(size int) string {
	if size >= 1<<20 {
		return fmt.Sprintf("%.1f MiB", float64(size)/(1<<20))
	}
	return fmt.Sprintf("%.1f KiB", float64(size)/(1<<10))
}
