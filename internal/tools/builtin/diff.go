package builtin

import (
	"fmt"
	"strings"

	udiff "github.com/aymanbagabas/go-udiff"
)

const (
	// maxDiffInputBytes bounds each side of a file-change preview. File writes
	// still complete above this threshold; only the optional UI diff is omitted.
	maxDiffInputBytes = 512 * 1024
	// maxDiffEdits prevents replace_all previews from allocating one diff edit
	// per match on highly repetitive files.
	maxDiffEdits = 1_000
	// maxDiffPreviewBytes bounds private UI metadata before it reaches clients.
	maxDiffPreviewBytes = 64 * 1024
)

const diffTruncationMarker = "\n... [diff preview truncated]"

// editDiff returns a compact, line-oriented preview of an edit. It keeps the
// useful context from a unified diff while omitting file headers and hunk
// metadata so it reads naturally in the terminal transcript.
func editDiff(before, after, oldStr, newStr string, replaceAll bool) string {
	if before == after || oldStr == newStr {
		return ""
	}
	if len(before) > maxDiffInputBytes || len(after) > maxDiffInputBytes {
		return ""
	}

	var edits []udiff.Edit
	from := 0
	for {
		rel := strings.Index(before[from:], oldStr)
		if rel < 0 {
			break
		}
		if len(edits) >= maxDiffEdits {
			return ""
		}
		start := from + rel
		edits = append(edits, udiff.Edit{Start: start, End: start + len(oldStr), New: newStr})
		from = start + len(oldStr)
		if !replaceAll {
			break
		}
	}
	return formatDiff(before, edits)
}

// contentDiff computes the same preview for a full-file overwrite. This is
// important because models often use write to modify an existing file.
func contentDiff(before, after string) string {
	if before == after {
		return ""
	}
	// Avoid making a full-file overwrite unexpectedly expensive for large
	// files. The normal write still completes; only the optional preview is
	// omitted when diffing would be disproportionate.
	if len(before) > maxDiffInputBytes || len(after) > maxDiffInputBytes {
		return ""
	}
	return formatDiff(before, udiff.Strings(before, after))
}

func formatDiff(before string, edits []udiff.Edit) string {
	if len(edits) == 0 || len(edits) > maxDiffEdits {
		return ""
	}
	diff, err := udiff.ToUnifiedDiff("", "", before, edits, 3)
	if err != nil || len(diff.Hunks) == 0 {
		return ""
	}
	oldLines := lineCount(before)
	var b strings.Builder
	for hunkIndex, hunk := range diff.Hunks {
		if hunkIndex == 0 {
			if hunk.FromLine > 1 {
				b.WriteString("...\n")
			}
		} else {
			b.WriteString("...\n")
		}

		oldLine, newLine := hunk.FromLine, hunk.ToLine
		oldCovered := 0
		for _, line := range hunk.Lines {
			switch line.Kind {
			case udiff.Delete:
				writeDiffLine(&b, '-', oldLine, line.Content)
				oldLine++
				oldCovered++
			case udiff.Insert:
				writeDiffLine(&b, '+', newLine, line.Content)
				newLine++
			default:
				writeDiffLine(&b, ' ', oldLine, line.Content)
				oldLine++
				newLine++
				oldCovered++
			}
		}
		if hunkIndex == len(diff.Hunks)-1 && hunk.FromLine-1+oldCovered < oldLines {
			b.WriteString("...\n")
		}
	}
	return boundDiffPreview(strings.TrimSuffix(b.String(), "\n"))
}

func boundDiffPreview(diff string) string {
	if len(diff) <= maxDiffPreviewBytes {
		return diff
	}
	budget := maxDiffPreviewBytes - len(diffTruncationMarker)
	if budget <= 0 {
		return truncateRunes(diffTruncationMarker, maxDiffPreviewBytes)
	}
	return truncateRunes(diff, budget) + diffTruncationMarker
}

func writeDiffLine(b *strings.Builder, marker byte, lineNo int, content string) {
	content = strings.TrimSuffix(content, "\n")
	fmt.Fprintf(b, "%c%d %s\n", marker, lineNo, content)
}

func lineCount(content string) int {
	if content == "" {
		return 0
	}
	count := strings.Count(content, "\n")
	if !strings.HasSuffix(content, "\n") {
		count++
	}
	return count
}
