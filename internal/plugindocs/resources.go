package plugindocs

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"sync"
	"unicode/utf8"
)

// These immutable resources are generated from reviewed repository sources.
// No package source, config, or network location is opened by these operations.
//
//go:embed resources
var bundled embed.FS

var resourceFiles = sync.OnceValues(func() ([]resource, error) {
	var out []resource
	var totalBytes int64
	err := fs.WalkDir(bundled, "resources", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > maxResourceBytes {
			return fmt.Errorf("invalid bundled resource %q", path)
		}
		totalBytes += info.Size()
		if len(out) >= 1000 || totalBytes > 16<<20 {
			return errors.New("bundled resource catalog exceeds limits")
		}
		out = append(out, resource{Path: strings.TrimPrefix(path, "resources/"), Bytes: info.Size()})
		return nil
	})
	return out, err
})

type resource struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
}

type match struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

func validPath(path string) bool {
	return path != "." && fs.ValidPath(path) && !strings.ContainsAny(path, "\\\x00\r\n")
}

func readResource(ctx context.Context, path string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !validPath(path) {
		return "", errors.New("path must name an exact embedded resource, without traversal or absolute paths")
	}
	info, err := fs.Stat(bundled, "resources/"+path)
	if err != nil {
		return "", fmt.Errorf("resource %q not found; use list to discover paths", path)
	}
	if !info.Mode().IsRegular() || info.Size() > maxResourceBytes {
		return "", errors.New("resource is not a bounded regular file")
	}
	data, err := bundled.ReadFile("resources/" + path)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !utf8.Valid(data) || strings.ContainsRune(string(data), '\x00') {
		return "", errors.New("resource is not UTF-8 text")
	}
	return string(data), nil
}

func resourceLines(text string) []string {
	if text == "" {
		return nil
	}
	lines := strings.SplitAfter(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func (t *Tool) read(ctx context.Context, args arguments, r *response) error {
	text, err := readResource(ctx, args.Path)
	if err != nil {
		return err
	}
	lines := resourceLines(text)
	r.Path, r.Total = args.Path, len(lines)
	start := min(args.Offset-1, len(lines))
	count := 0
	for _, line := range lines[start : start+min(args.Limit, len(lines)-start)] {
		if err := ctx.Err(); err != nil {
			return err
		}
		old := r.Content
		r.Content += line
		if !t.fits(r) {
			r.Content = old
			if count == 0 {
				return errors.New("a resource line exceeds the configured output limit; increase that limit to read it")
			}
			break
		}
		count++
	}
	r.next(count)
	return nil
}

func (t *Tool) find(ctx context.Context, args arguments, r *response) error {
	if !validFilter(args.Path) {
		return errors.New("path filter must stay inside the embedded resource catalog")
	}
	query := strings.ToLower(strings.TrimSpace(args.Query))
	if args.Action == "search" && query == "" {
		return errors.New("search requires a non-empty literal query")
	}
	files, err := resourceFiles()
	if err != nil {
		return err
	}
	filter := strings.TrimSuffix(args.Path, "/")
	full := false
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if filter != "" && file.Path != filter && !strings.HasPrefix(file.Path, filter+"/") {
			continue
		}
		if args.Action == "list" {
			r.Total++
			if r.Total < args.Offset || full {
				continue
			}
			r.Resources = append(r.Resources, file)
			if !t.fits(r) {
				r.Resources = r.Resources[:len(r.Resources)-1]
				full = true
			} else {
				full = len(r.Resources) >= args.Limit
			}
			continue
		}
		text, err := readResource(ctx, file.Path)
		if err != nil {
			return err
		}
		for i, line := range resourceLines(text) {
			if err := ctx.Err(); err != nil {
				return err
			}
			if !strings.Contains(strings.ToLower(line), query) {
				continue
			}
			r.Total++
			if r.Total < args.Offset || full {
				continue
			}
			// Search is a locator; exact, unabridged content is available via read.
			r.Matches = append(r.Matches, match{Path: file.Path, Line: i + 1, Text: excerpt(strings.TrimSuffix(line, "\n"), 400)})
			if !t.fits(r) {
				r.Matches = r.Matches[:len(r.Matches)-1]
				full = true
			} else {
				full = len(r.Matches) >= args.Limit
			}
		}
	}
	count := len(r.Resources) + len(r.Matches)
	if count == 0 && r.Total >= args.Offset {
		return errors.New("configured output limit cannot fit a result")
	}
	r.next(count)
	return nil
}

func excerpt(s string, maxRunes int) string {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	return string([]rune(s)[:maxRunes]) + "…"
}
