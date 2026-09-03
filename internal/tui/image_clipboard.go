package tui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const (
	maxClipboardImageBytes   = 20 << 20
	maxPromptImageTotalBytes = 40 << 20
	maxPromptImages          = 8
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
		data, err = readMacOSClipboardImage(ctx)
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
	return protocol.ContentBlock{Type: protocol.BlockImage, MIMEType: mime, Data: slices.Clone(data)}, nil
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
	for field := range strings.FieldsSeq(types) {
		available[strings.TrimSpace(field)] = true
	}
	for _, mime := range []string{"image/png", "image/jpeg", "image/webp", "image/gif"} {
		if available[mime] {
			return mime
		}
	}
	return ""
}

func readMacOSClipboardImage(ctx context.Context) ([]byte, error) {
	file, err := os.CreateTemp("", "snow-clipboard-image-*")
	if err != nil {
		return nil, err
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	defer os.Remove(path)

	format, err := boundedCommandOutputLimit(ctx, 64<<10, "osascript", "-e", macOSClipboardImageScript, path)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(format)) == "tiff" {
		convertedPath := path + ".png"
		defer os.Remove(convertedPath)
		if _, err := boundedCommandOutputLimit(ctx, 64<<10, "sips", "-s", "format", "png", path, "--out", convertedPath); err != nil {
			return nil, err
		}
		path = convertedPath
	}
	file, err = os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(io.LimitReader(file, maxClipboardImageBytes+1))
}

const macOSClipboardImageScript = `on run argv
set outputPath to POSIX file (item 1 of argv)
set imageKind to "native"
try
	set imageData to the clipboard as «class PNGf»
on error
	try
		set imageData to the clipboard as JPEG picture
	on error
		try
			set imageData to the clipboard as GIF picture
		on error
			try
				set imageData to the clipboard as «class WebP»
			on error
				set imageData to the clipboard as TIFF picture
				set imageKind to "tiff"
			end try
		end try
	end try
end try
set outputFile to open for access outputPath with write permission
try
	set eof outputFile to 0
	write imageData to outputFile
on error errorMessage number errorNumber
	try
		close access outputFile
	end try
	error errorMessage number errorNumber
end try
close access outputFile
return imageKind
end run`

func imageAttachmentToken(index int) string {
	return fmt.Sprintf("[Image #%d]", index+1)
}

func imageAttachmentInsertion(current string, row, column, index int) string {
	lines := strings.Split(current, "\n")
	if row < 0 || row >= len(lines) {
		return imageAttachmentToken(index) + " "
	}
	line := []rune(lines[row])
	column = max(0, min(column, len(line)))
	prefix := ""
	if column > 0 && !unicode.IsSpace(line[column-1]) {
		prefix = " "
	}
	suffix := ""
	if column == len(line) || !unicode.IsSpace(line[column]) {
		suffix = " "
	}
	return prefix + imageAttachmentToken(index) + suffix
}

func stripImageAttachmentTokens(text string, count int) string {
	for i := range count {
		token := imageAttachmentToken(i)
		if strings.Contains(text, token+" ") {
			text = strings.Replace(text, token+" ", "", 1)
			continue
		}
		text = strings.Replace(text, token, "", 1)
	}
	return text
}

func removeImageAttachmentToken(text string, index int) string {
	token := imageAttachmentToken(index)
	if strings.Contains(text, token+" ") {
		return strings.Replace(text, token+" ", "", 1)
	}
	return strings.Replace(text, token, "", 1)
}

func promptImageBytes(images []protocol.ContentBlock) int {
	total := 0
	for _, image := range images {
		total += len(image.Data)
	}
	return total
}
