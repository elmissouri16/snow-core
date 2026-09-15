//go:build darwin || linux

package web

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"slices"
	"strings"
	"unicode/utf8"
)

// InspectFiles scans at most 256 entries per page and 4096 entries including
// skipped offsets. Offsets count scanned entries, not just visible entries.
// This is best-effort filesystem pagination, not a snapshot across requests.
func InspectFiles(ctx context.Context, p Project, relativePath string, offset int) (InspectionFiles, error) {
	result := InspectionFiles{Entries: []InspectionEntry{}}
	path, err := inspectionPath(relativePath, true)
	if err != nil {
		return result, err
	}
	if offset < 0 || offset >= InspectionScanLimit {
		return result, ErrInspectionPath
	}
	root, err := inspectionRoot(ctx, p)
	if err != nil {
		return result, err
	}
	defer root.Close()
	dir, info, err := inspectionOpen(ctx, root, path, true)
	if err != nil {
		return result, err
	}
	defer dir.Close()
	result.Path = path
	boundary := min(offset+InspectionPageSize, InspectionScanLimit)
	scanned := 0
	eof := false
	for scanned < boundary {
		if err := ctx.Err(); err != nil {
			return InspectionFiles{}, err
		}
		entries, readErr := dir.ReadDir(min(64, boundary-scanned))
		for _, entry := range entries {
			scanned++
			if scanned <= offset {
				continue
			}
			name := entry.Name()
			child := name
			if path != "." {
				child = path + "/" + name
			}
			if _, err := inspectionPath(child, false); err != nil {
				continue
			}
			// Lstat never follows a child symlink; do not open any child files here.
			childInfo, statErr := root.Lstat(child)
			if statErr != nil || childInfo.Mode()&os.ModeSymlink != 0 {
				continue
			}
			kind := "file"
			if childInfo.IsDir() {
				kind = "directory"
			} else if !inspectionRegular(childInfo) {
				continue
			}
			result.Entries = append(result.Entries, InspectionEntry{Name: name, Path: child, Kind: kind})
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return InspectionFiles{}, ErrInspectionUnavailable
			}
			eof = true
			break
		}
	}
	if err := inspectionCheck(ctx, root, path, info); err != nil {
		return InspectionFiles{}, err
	}
	// No lookahead beyond the scan cap. A full page conservatively advertises
	// more; an exact multiple may therefore end with an empty final page.
	result.NextOffset = scanned
	result.HasMore = !eof && scanned == boundary && scanned < InspectionScanLimit
	result.Limited = !eof && scanned == InspectionScanLimit
	slices.SortFunc(result.Entries, func(a, b InspectionEntry) int { return strings.Compare(a.Name, b.Name) })
	return result, nil
}

// InspectFile returns at most 64 KiB of a regular, single-link UTF-8 file.
// Files exceeding 128 KiB, including files that grow while read, are refused.
// NUL and UTF-8 validation cover the entire bounded file, not just the preview.
func InspectFile(ctx context.Context, p Project, relativePath string) (InspectionFile, error) {
	path, err := inspectionPath(relativePath, false)
	if err != nil {
		return InspectionFile{}, err
	}
	root, err := inspectionRoot(ctx, p)
	if err != nil {
		return InspectionFile{}, err
	}
	defer root.Close()
	file, info, err := inspectionOpen(ctx, root, path, false)
	if err != nil {
		return InspectionFile{}, err
	}
	defer file.Close()
	if info.Size() > InspectionFileLimit {
		return InspectionFile{}, ErrInspectionTooLarge
	}
	data := make([]byte, 0, min(info.Size()+1, InspectionFileLimit+1))
	buffer := make([]byte, 8192)
	for len(data) <= InspectionFileLimit {
		if err := ctx.Err(); err != nil {
			return InspectionFile{}, err
		}
		n, readErr := file.Read(buffer[:min(len(buffer), InspectionFileLimit+1-len(data))])
		data = append(data, buffer[:n]...)
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return InspectionFile{}, ErrInspectionUnavailable
			}
			break
		}
		if n == 0 {
			return InspectionFile{}, ErrInspectionUnavailable
		}
	}
	if len(data) > InspectionFileLimit {
		return InspectionFile{}, ErrInspectionTooLarge
	}
	if err := inspectionCheck(ctx, root, path, info); err != nil {
		return InspectionFile{}, err
	}
	if int64(len(data)) != info.Size() {
		return InspectionFile{}, ErrInspectionUnavailable
	}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return InspectionFile{}, ErrInspectionBinary
	}
	result := InspectionFile{Path: path, Size: int64(len(data)), Truncated: len(data) > InspectionPreviewLimit}
	end := min(len(data), InspectionPreviewLimit)
	for end < len(data) && !utf8.RuneStart(data[end]) {
		end--
	}
	result.Text = string(data[:end])
	return result, nil
}
