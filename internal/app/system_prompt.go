package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/elmissouri16/snow-core/internal/config"
)

func loadSystemPromptFile(path, baseDir, confinedRoot string, maxBytes int) (string, error) {
	resolved, err := resolveSystemPromptPath(path, baseDir, confinedRoot == "")
	if err != nil {
		return "", err
	}
	if maxBytes <= 0 {
		maxBytes = config.DefaultContextCapBytes
	}

	var file *os.File
	if confinedRoot == "" {
		info, statErr := os.Stat(resolved)
		if statErr != nil {
			return "", fmt.Errorf("read %s: %w", resolved, statErr)
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("system prompt file %s is not a regular file", resolved)
		}
		file, err = os.Open(resolved)
	} else {
		root, absErr := filepath.Abs(confinedRoot)
		if absErr != nil {
			return "", fmt.Errorf("resolve trusted project root: %w", absErr)
		}
		rel, relErr := filepath.Rel(root, resolved)
		if relErr != nil || !filepath.IsLocal(rel) {
			return "", errors.New("system prompt file escapes trusted project root")
		}
		if err := rejectPromptPathSymlinks(root, rel); err != nil {
			return "", err
		}
		rootHandle, openErr := os.OpenRoot(root)
		if openErr != nil {
			return "", fmt.Errorf("open trusted project root: %w", openErr)
		}
		defer rootHandle.Close()
		file, err = rootHandle.Open(rel)
	}
	if err != nil {
		return "", fmt.Errorf("read %s: %w", resolved, err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", resolved, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("system prompt file %s is not a regular file", resolved)
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(maxBytes)+1))
	if err != nil {
		return "", fmt.Errorf("read %s: %w", resolved, err)
	}
	if len(data) > maxBytes {
		return "", fmt.Errorf("system prompt file %s exceeds context_cap_bytes (%d)", resolved, maxBytes)
	}
	if strings.TrimSpace(string(data)) == "" {
		return "", fmt.Errorf("system prompt file %s is empty", resolved)
	}
	return string(data), nil
}

func resolveSystemPromptPath(path, baseDir string, expandHome bool) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("system prompt file path is empty")
	}
	if expandHome && (path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`)) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, filepath.FromSlash(strings.ReplaceAll(path[2:], `\`, "/")))
		}
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}
	resolved, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve system prompt file: %w", err)
	}
	return filepath.Clean(resolved), nil
}

func rejectPromptPathSymlinks(root, rel string) error {
	current := root
	var target os.FileInfo
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("inspect system prompt path: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("system prompt file path contains a symlink")
		}
		target = info
	}
	if target == nil || !target.Mode().IsRegular() {
		return errors.New("system prompt file is not a regular file")
	}
	return nil
}
