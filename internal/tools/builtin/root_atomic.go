package builtin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

func atomicReplaceRooted(ctx context.Context, target rootedPath, data []byte, mode os.FileMode) error {
	parent := filepath.Dir(target.name)
	if err := target.root.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create parent dirs: %w", err)
	}
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		return fmt.Errorf("temporary name: %w", err)
	}
	tempName := filepath.Join(parent, ".snow-write-"+hex.EncodeToString(random[:]))
	temp, err := target.root.OpenFile(tempName, os.O_CREATE|os.O_EXCL|os.O_RDWR, mode.Perm())
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	cleanup := true
	defer func() {
		_ = temp.Close()
		if cleanup {
			_ = target.root.Remove(tempName)
		}
	}()
	if err := temp.Chmod(mode.Perm()); err != nil {
		return fmt.Errorf("set permissions: %w", err)
	}
	if err := writeAll(ctx, temp, data); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("sync: %w", err)
	}
	if err := preserveRootedReplacementSecurity(target.root, target.name, temp); err != nil {
		return fmt.Errorf("preserve security metadata: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}
	if err := target.root.Rename(tempName, target.name); err != nil {
		return fmt.Errorf("replace: %w", err)
	}
	cleanup = false
	return nil
}
