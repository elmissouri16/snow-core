//go:build darwin || linux

package web

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

const (
	InspectionPageSize     = 256
	InspectionScanLimit    = 4096
	InspectionFileLimit    = 128 << 10
	InspectionPreviewLimit = 64 << 10
)

var (
	ErrInspectionPath        = errors.New("web: invalid or protected project-relative path")
	ErrInspectionUnavailable = errors.New("web: project content is unavailable or changed")
	ErrInspectionTooLarge    = errors.New("web: file exceeds the 128 KiB inspection limit")
	ErrInspectionBinary      = errors.New("web: only regular UTF-8 text files without NUL bytes can be inspected")
)

// InspectionEntry describes only a direct child, never an absolute host path.
type InspectionEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Kind string `json:"kind"` // file or directory
}

type InspectionFiles struct {
	Path       string            `json:"path"`
	Entries    []InspectionEntry `json:"entries"`
	NextOffset int               `json:"next_offset"`
	HasMore    bool              `json:"has_more"`
	Limited    bool              `json:"limited"`
}

type InspectionFile struct {
	Path      string `json:"path"`
	Text      string `json:"text"`
	Size      int64  `json:"size"`
	Truncated bool   `json:"truncated"`
}

// inspectionPath deliberately denies all .env* names (including examples),
// credential stores, private-key names/extensions, and .git at every depth.
// Listings apply the identical policy. This is a conservative name exclusion,
// not secret detection: ordinary source files can still contain secrets.
func inspectionPath(path string, allowRoot bool) (string, error) {
	if allowRoot && (path == "" || path == ".") {
		return ".", nil
	}
	if path == "" || len(path) > 4096 || !utf8.ValidString(path) || filepath.IsAbs(path) || strings.ContainsAny(path, "\\\x00:") {
		return "", ErrInspectionPath
	}
	for _, r := range path {
		if unicode.IsControl(r) {
			return "", ErrInspectionPath
		}
	}
	depth := 0
	for part := range strings.SplitSeq(path, "/") {
		depth++
		if depth > 64 || part == "" || part == "." || part == ".." || inspectionSensitive(part) {
			return "", ErrInspectionPath
		}
	}
	return path, nil
}

func inspectionSensitive(name string) bool {
	name = strings.ToLower(name)
	if strings.HasPrefix(name, ".env") || strings.HasPrefix(name, "id_rsa") || strings.HasPrefix(name, "id_dsa") || strings.HasPrefix(name, "id_ecdsa") || strings.HasPrefix(name, "id_ed25519") {
		return true
	}
	switch name {
	case ".git", ".ssh", ".aws", ".azure", ".gnupg", ".kube", ".config", ".snow", ".netrc", ".npmrc", ".pypirc", ".git-credentials", ".gitconfig", "auth.json", "access.json", "credentials", "credentials.json", "credentials.yml", "credentials.yaml", "secrets.json", "secrets.yml", "secrets.yaml":
		return true
	}
	switch filepath.Ext(name) {
	case ".pem", ".key", ".p12", ".pfx", ".keystore", ".kdbx":
		return true
	}
	return false
}

// inspectionRoot pins the registered identity rather than trusting Available
// alone. Callers must obtain Project from a fresh Registry.Lookup. Diagnostics
// intentionally never wrap OS errors (which can expose host paths).
func inspectionRoot(ctx context.Context, p Project) (*os.Root, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !p.Available || !validStoredProject(p) {
		return nil, ErrInspectionUnavailable
	}
	p.checkIdentity()
	if !p.Available {
		return nil, ErrInspectionUnavailable
	}
	root, err := os.OpenRoot(p.Path)
	if err != nil {
		return nil, ErrInspectionUnavailable
	}
	info, err := root.Stat(".")
	if err == nil {
		dev, ino, ok := registryIdentity(info)
		if ok && info.IsDir() && dev == p.device && ino == p.inode {
			p.checkIdentity()
			if p.Available {
				return root, nil
			}
		}
	}
	_ = root.Close()
	return nil, ErrInspectionUnavailable
}

// inspectionOpen walks single components using no-follow openat calls from a
// pinned root. A directory-to-symlink swap cannot redirect an intermediate open;
// nonblocking opens cannot hang on a concurrently substituted FIFO. The final
// path/inode check also rejects renamed/replaced paths. No project file executes.
func inspectionOpen(ctx context.Context, root *os.Root, path string, directory bool) (*os.File, os.FileInfo, error) {
	current, err := root.OpenFile(".", os.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, nil, ErrInspectionUnavailable
	}
	if path != "." {
		parts := strings.Split(path, "/")
		for i, part := range parts {
			if err := ctx.Err(); err != nil {
				_ = current.Close()
				return nil, nil, err
			}
			flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
			if i < len(parts)-1 || directory {
				flags |= unix.O_DIRECTORY
			}
			fd, openErr := unix.Openat(int(current.Fd()), part, flags, 0)
			_ = current.Close()
			if openErr != nil {
				return nil, nil, ErrInspectionUnavailable
			}
			current = os.NewFile(uintptr(fd), part)
		}
	}
	info, err := current.Stat()
	if err != nil || (directory && !info.IsDir()) || (!directory && !inspectionRegular(info)) {
		_ = current.Close()
		return nil, nil, ErrInspectionUnavailable
	}
	if err := inspectionCheck(ctx, root, path, info); err != nil {
		_ = current.Close()
		return nil, nil, err
	}
	return current, info, nil
}

func inspectionRegular(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && info.Mode().IsRegular() && stat.Nlink == 1
}

func inspectionCheck(ctx context.Context, root *os.Root, path string, expected os.FileInfo) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	prefix := ""
	for part := range strings.SplitSeq(path, "/") {
		if prefix == "" {
			prefix = part
		} else {
			prefix += "/" + part
		}
		info, err := root.Lstat(prefix)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return ErrInspectionUnavailable
		}
		if prefix == path && (!os.SameFile(expected, info) || expected.Size() != info.Size() || expected.ModTime() != info.ModTime()) {
			return ErrInspectionUnavailable
		}
	}
	return nil
}
