package web

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// This browser lists directories on the Snow host. It does not use browser
// filesystem APIs or launch a desktop dialog. Like registration itself, access
// has the host user's OS privileges; this is not a filesystem sandbox.
type hostFolder struct {
	Name string `json:"name"`
	Path string `json:"path"`
}
type hostFolders struct {
	Path       string       `json:"path"`
	Parent     string       `json:"parent"`
	Folders    []hostFolder `json:"folders"`
	NextOffset int          `json:"next_offset"`
	HasMore    bool         `json:"has_more"`
	Limited    bool         `json:"limited"`
}

func readHostFolders(ctx context.Context, path string, offset int) (hostFolders, error) {
	result := hostFolders{Folders: []hostFolder{}}
	if path == "" {
		var err error
		path, err = os.UserHomeDir()
		if err != nil {
			return result, errors.New("host home is unavailable")
		}
	}
	if !filepath.IsAbs(path) || len(path) > 4096 || !utf8.ValidString(path) || offset < 0 || offset > 4096 {
		return result, errors.New("invalid host directory")
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return result, errors.New("host directory is unavailable")
	}
	root, err := os.OpenRoot(canonical)
	if err != nil {
		return result, errors.New("host directory is unavailable")
	}
	defer root.Close()
	dir, err := root.Open(".")
	if err != nil {
		return result, errors.New("host directory is unavailable")
	}
	defer dir.Close()
	result.Path = canonical
	result.Parent = filepath.Dir(canonical)
	// Offset counts scanned host entries, not only displayed folders. Batches
	// bound work even when a directory contains many files or inaccessible links.
	scanned := 0
	boundary := min(offset+256, 4096)
	for scanned < boundary {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		entries, readErr := dir.ReadDir(min(64, boundary-scanned))
		for _, entry := range entries {
			scanned++
			if scanned <= offset {
				continue
			}
			name := entry.Name()
			if !utf8.ValidString(name) || strings.ContainsAny(name, "\r\n\x00") {
				continue
			}
			if entry.IsDir() {
				result.Folders = append(result.Folders, hostFolder{Name: name, Path: filepath.Join(canonical, name)})
			} else if entry.Type()&os.ModeSymlink != 0 {
				// Directory aliases are visible but resolve canonically on navigation and
				// registration. Never read file contents or recurse during this request.
				if info, err := root.Stat(name); err == nil && info.IsDir() {
					result.Folders = append(result.Folders, hostFolder{Name: name, Path: filepath.Join(canonical, name)})
				}
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return result, errors.New("host directory could not be read")
			}
			break
		}
	}
	result.NextOffset = scanned
	if scanned == boundary {
		extra, err := dir.ReadDir(1)
		more := len(extra) > 0 && err == nil
		result.HasMore = more && scanned < 4096
		result.Limited = more && scanned == 4096
	}
	slices.SortFunc(result.Folders, func(a, b hostFolder) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	return result, nil
}

func (s *shell) hostFolders(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizeForm(w, r); !ok {
		return
	}
	offset := 0
	if value := r.PostForm.Get("offset"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			http.Error(w, "Invalid folder page", http.StatusBadRequest)
			return
		}
		offset = parsed
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	page, err := readHostFolders(ctx, r.PostForm.Get("path"), offset)
	if err != nil {
		http.Error(w, "Cannot browse that host folder. Check access or enter an absolute host path.", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.MarshalWrite(w, page)
}
