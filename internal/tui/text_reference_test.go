package tui

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

func referenceOriginalSanitize(value string, maxBytes int) string {
	if maxBytes == 0 {
		return ""
	}
	var b strings.Builder
	if maxBytes > 0 && len(value) > maxBytes {
		b.Grow(maxBytes)
	} else {
		b.Grow(len(value))
	}
	for _, r := range value {
		if r != '\n' && r != '\t' && unicode.IsControl(r) {
			continue
		}
		if maxBytes > 0 && b.Len()+utf8.RuneLen(r) > maxBytes {
			break
		}
		b.WriteRune(r)
	}
	return b.String()
}

func referenceOriginalDiff(output string, width int) string {
	// Keep the leading marker on context lines; only remove framing newlines.
	output = strings.Trim(output, "\n")
	if output == "" {
		return ""
	}
	output = referenceOriginalSanitize(output, 8*1024)
	lines := strings.Split(output, "\n")
	if len(lines) > 80 {
		lines = append(lines[:80], "... [diff preview truncated]")
	}
	maxWidth := max(width-2, 20)
	for i, line := range lines {
		line = referenceOriginalTruncate(line, maxWidth)
		switch {
		case strings.HasPrefix(line, "-"):
			lines[i] = styleDiffDel.Render(line)
		case strings.HasPrefix(line, "+"):
			lines[i] = styleDiffAdd.Render(line)
		default:
			lines[i] = styleHeaderDim.Render(line)
		}
	}
	return strings.Join(lines, "\n")
}

// renderToolOutputPreview keeps tool cards useful without dumping a whole
// read/grep result into the transcript. The complete output remains available
// to the model through the session and to SDK/RPC subscribers.
func referenceOriginalPreview(output string, width int) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return ""
	}
	output = referenceOriginalSanitize(output, 2400)
	lines := strings.Split(output, "\n")
	maxLines := 6
	if len(lines) > maxLines {
		lines = append(lines[:maxLines], "… [preview truncated]")
	}
	maxWidth := max(width-8, 20)
	for i, line := range lines {
		lines[i] = "  │ " + referenceOriginalTruncate(line, maxWidth)
	}
	return styleHeaderDim.Render(strings.Join(lines, "\n"))
}

func referenceOriginalTruncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}
