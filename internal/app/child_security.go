package app

import (
	"errors"
	"fmt"
	"os"
)

// ensurePrivateChildDirectory creates the generated .agents directory without
// following a pre-existing symlink. The root session directory is created by
// the session index, so a missing child directory is intentionally one mkdir.
func ensurePrivateChildDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("app: inspect child directory: %w", err)
		}
		if err := os.Mkdir(path, 0o700); err != nil {
			if !errors.Is(err, os.ErrExist) {
				return fmt.Errorf("app: mkdir child directory: %w", err)
			}
		}
		info, err = os.Lstat(path)
		if err != nil {
			return fmt.Errorf("app: inspect child directory: %w", err)
		}
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("app: child directory is not a private real directory")
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("app: chmod child directory: %w", err)
	}
	// Recheck after chmod so a racing replacement is not silently accepted.
	info, err = os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("app: child directory changed during setup")
	}
	return nil
}
